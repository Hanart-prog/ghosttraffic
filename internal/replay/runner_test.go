package replay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hanart-prog/ghosttraffic/internal/egress"
	"github.com/Hanart-prog/ghosttraffic/internal/metrics"
	"github.com/Hanart-prog/ghosttraffic/internal/report"
	"github.com/Hanart-prog/ghosttraffic/internal/trace"
	"github.com/Hanart-prog/ghosttraffic/pkg/config"
)

func rec(id, method, path string, offsetMS int64, body string) trace.Record {
	r := trace.Record{
		SchemaVersion: trace.SchemaVersion, RequestID: id, ArrivalOffsetMS: offsetMS,
		Method: method, Path: path, Headers: map[string]string{"accept": "application/json"},
		Capture: trace.Capture{Status: 200}, Replayable: true,
	}
	if body != "" {
		r.BodyJSON = json.RawMessage(body)
		r.Headers["content-type"] = "application/json"
	}
	return r
}

func writeTrace(t *testing.T, records ...trace.Record) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "trace.jsonl")
	w, err := trace.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range records {
		if err := w.Write(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func target(t *testing.T, handler http.Handler) (config.Target, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	got, err := config.CheckTarget(server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	return got, server
}

func options(t *testing.T, baseline, candidate config.Target, records ...trace.Record) Options {
	t.Helper()
	workload, err := LoadWorkload(writeTrace(t, records...))
	if err != nil {
		t.Fatal(err)
	}
	router, err := metrics.NewRouter(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := egress.NewClient(egress.Options{Timeout: 5 * time.Second, MaxConns: 8})
	t.Cleanup(client.CloseIdleConnections)
	return Options{
		Workload: workload, Baseline: baseline, Candidate: candidate, Client: client, Router: router,
		OutDir: filepath.Join(t.TempDir(), "runs", "run-1"), Concurrency: 8, QueueLimit: 100, Speed: 100,
		Timeout: 2 * time.Second, MaxResponseBytes: 1 << 20, Build: report.Build{Version: "test"},
	}
}

func results(t *testing.T, dir string) map[string]map[string]metrics.Result {
	t.Helper()
	file, err := os.Open(filepath.Join(dir, resultsFile))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	out := map[string]map[string]metrics.Result{"baseline": {}, "candidate": {}}
	err = metrics.ReadResults(file, func(r metrics.Result) error {
		if _, dup := out[r.Target][r.RequestID]; dup {
			t.Errorf("two results for %s on %s", r.RequestID, r.Target)
		}
		out[r.Target][r.RequestID] = r
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func readRun(t *testing.T, dir string) report.Run {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, runFile))
	if err != nil {
		t.Fatal(err)
	}
	run, err := report.ParseRun(data)
	if err != nil {
		t.Fatal(err)
	}
	return run
}

// recorder notes what a target received, for workload equivalence checks.
type recorder struct {
	mu   sync.Mutex
	seen []string
}

func (r *recorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	r.mu.Lock()
	r.seen = append(r.seen, fmt.Sprintf("%s %s %s auth=%q cookie=%q", req.Method, req.URL.RequestURI(), body, req.Header.Get("Authorization"), req.Header.Get("Cookie")))
	r.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func (r *recorder) sorted(t *testing.T) string {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	lines := append([]string(nil), r.seen...)
	sortStrings(lines)
	return strings.Join(lines, "\n")
}

func sortStrings(s []string) {
	for i := range s {
		for j := i + 1; j < len(s); j++ {
			if s[j] < s[i] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

func TestBothTargetsReceiveTheSameWorkload(t *testing.T) {
	var a, b recorder
	baseline, _ := target(t, &a)
	candidate, _ := target(t, &b)
	records := []trace.Record{
		rec("r1", "GET", "/api/products?query=shoe", 0, ""),
		rec("r2", "POST", "/api/carts", 5, `{"customer_id":1}`),
		rec("r3", "GET", "/api/carts/7", 10, ""),
	}
	opts := options(t, baseline, candidate, records...)
	outcome, err := Execute(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}

	if a.sorted(t) != b.sorted(t) {
		t.Errorf("workloads differ:\n%s\n---\n%s", a.sorted(t), b.sorted(t))
	}
	if !strings.Contains(a.sorted(t), `POST /api/carts {"customer_id":1} auth="" cookie=""`) {
		t.Errorf("received:\n%s", a.sorted(t))
	}
	got := results(t, opts.OutDir)
	for _, name := range []string{"baseline", "candidate"} {
		if len(got[name]) != 3 {
			t.Errorf("%s has %d results", name, len(got[name]))
		}
		r := got[name]["r3"]
		if r.EndpointKey != "GET /api/carts/{id}" || r.Status != 200 || r.Trial != 1 || r.ErrorCategory != metrics.ErrorNone || r.ResponseDigest == "" {
			t.Errorf("%s r3 = %+v", name, r)
		}
		if outcome.Stats[name].Successes != 3 {
			t.Errorf("%s stats = %+v", name, outcome.Stats[name])
		}
	}
	if got["baseline"]["r1"].ResponseJSON != nil {
		t.Error("response body stored without opt-in")
	}
	run := readRun(t, opts.OutDir)
	if run.Status != report.RunCompleted || run.EndedAt == nil || len(run.Passes) != 2 || run.Passes[0].Target != "baseline" {
		t.Errorf("run = %+v", run)
	}
	if run.TraceSHA256 != opts.Workload.SHA256 || run.Requests.Sent != 3 {
		t.Errorf("run = %+v", run)
	}
}

func TestFailuresAreClassified(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		case "/500":
			http.Error(w, "boom", http.StatusInternalServerError)
		case "/malformed":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"a":`))
		case "/text":
			_, _ = w.Write([]byte(`not json, and not claiming to be`))
		case "/slow":
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
		case "/reset":
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				conn.Close()
			}
		case "/truncated":
			w.Header().Set("Content-Length", "100")
			_, _ = w.Write([]byte("short"))
		}
	})
	baseline, _ := target(t, handler)
	candidate, _ := target(t, handler)
	opts := options(t, baseline, candidate,
		rec("ok", "GET", "/ok", 0, ""), rec("500", "GET", "/500", 0, ""),
		rec("malformed", "GET", "/malformed", 0, ""), rec("text", "GET", "/text", 0, ""),
		rec("slow", "GET", "/slow", 0, ""), rec("reset", "GET", "/reset", 0, ""),
		rec("truncated", "GET", "/truncated", 0, ""))
	opts.Timeout = 200 * time.Millisecond
	if _, err := Execute(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	got := results(t, opts.OutDir)["candidate"]
	tests := []struct {
		id       string
		category metrics.ErrorCategory
		status   int
	}{
		{"ok", metrics.ErrorNone, 200},
		{"500", metrics.ErrorNone, 500},
		{"malformed", metrics.ErrorMalformed, 200},
		{"text", metrics.ErrorNone, 200},
		{"slow", metrics.ErrorTimeout, 0},
		{"truncated", metrics.ErrorConn, 200},
	}
	for _, tt := range tests {
		if r := got[tt.id]; r.ErrorCategory != tt.category || r.Status != tt.status {
			t.Errorf("%s = %s/%d, want %s/%d", tt.id, r.ErrorCategory, r.Status, tt.category, tt.status)
		}
	}
	if c := got["reset"].ErrorCategory; c != metrics.ErrorReset && c != metrics.ErrorConn {
		t.Errorf("reset = %s", c)
	}
	if d := got["slow"].DurationMS; d < 190 {
		t.Errorf("timeout duration = %v", d)
	}
}

func TestNoAutomaticRetryOfMutatingRequests(t *testing.T) {
	var hits atomic.Int64
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			hits.Add(1)
		}
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	})
	baseline, _ := target(t, handler)
	candidate, _ := target(t, handler)
	opts := options(t, baseline, candidate, rec("r1", "POST", "/api/orders", 0, `{"cart_id":1}`))
	if _, err := Execute(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 {
		t.Fatalf("hits = %d, want one per target", hits.Load())
	}
}

func TestOptInCapturesSanitizedResponses(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1,"access_token":"sekrit"}`))
	})
	baseline, _ := target(t, handler)
	candidate, _ := target(t, handler)
	opts := options(t, baseline, candidate, rec("r1", "GET", "/a", 0, ""))
	opts.CaptureBodies = true
	if _, err := Execute(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if got := string(results(t, opts.OutDir)["baseline"]["r1"].ResponseJSON); got != `{"access_token":"[REDACTED]","id":1}` {
		t.Fatalf("response_json = %s", got)
	}
}

func TestOversizedResponseIsDigestedButNotKept(t *testing.T) {
	big := strings.Repeat("x", 1<<12)
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`"` + big + `"`))
	})
	baseline, _ := target(t, handler)
	candidate, _ := target(t, handler)
	opts := options(t, baseline, candidate, rec("r1", "GET", "/a", 0, ""))
	opts.CaptureBodies, opts.MaxResponseBytes = true, 1<<10
	if _, err := Execute(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	r := results(t, opts.OutDir)["baseline"]["r1"]
	if r.ResponseDigest == "" || r.ResponseJSON != nil || r.ErrorCategory != metrics.ErrorNone {
		t.Fatalf("result = %+v", r)
	}
}

func TestCancellationLeavesACompleteRecord(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			http.NotFound(w, r)
			return
		}
		cancel()
		<-r.Context().Done()
	})
	baseline, _ := target(t, handler)
	candidate, _ := target(t, handler)
	opts := options(t, baseline, candidate,
		rec("r1", "GET", "/a", 0, ""), rec("r2", "GET", "/b", 3_600_000, ""), rec("r3", "POST", "/c", 7_200_000, `{}`))
	opts.Speed = 1
	_, err := Execute(ctx, opts)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	got := results(t, opts.OutDir)
	for _, name := range []string{"baseline", "candidate"} {
		for _, id := range []string{"r1", "r2", "r3"} {
			if got[name][id].ErrorCategory != metrics.ErrorCancelled {
				t.Errorf("%s %s = %+v", name, id, got[name][id])
			}
		}
	}
	if run := readRun(t, opts.OutDir); run.Status != report.RunCancelled || run.EndedAt == nil {
		t.Errorf("run = %+v", run)
	}
}

func TestOverflowBecomesDroppedResults(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			http.NotFound(w, r)
			return
		}
		select {
		case <-release:
		case <-time.After(100 * time.Millisecond):
			once.Do(func() { close(release) })
		}
	})
	baseline, _ := target(t, handler)
	candidate, _ := target(t, handler)
	var records []trace.Record
	for i := range 20 {
		records = append(records, rec(fmt.Sprintf("r%d", i), "GET", "/a", 0, ""))
	}
	opts := options(t, baseline, candidate, records...)
	opts.Concurrency, opts.QueueLimit = 1, 1
	outcome, err := Execute(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	got := results(t, opts.OutDir)
	if len(got["baseline"]) != 20 || outcome.Stats["baseline"].Dropped == 0 {
		t.Fatalf("results = %d, stats = %+v", len(got["baseline"]), outcome.Stats["baseline"])
	}
	if !strings.Contains(strings.Join(readRun(t, opts.OutDir).Warnings, "\n"), "dropped") {
		t.Error("no drop warning")
	}
}

func TestUnreplayableRecordsAreCountedNotSent(t *testing.T) {
	var a recorder
	baseline, _ := target(t, &a)
	candidate, _ := target(t, &recorder{})
	skipped := rec("r2", "POST", "/never", 0, "")
	skipped.Replayable, skipped.UnreplayableReason = false, "body is not JSON"
	opts := options(t, baseline, candidate, rec("r1", "GET", "/a", 0, ""), skipped)
	if _, err := Execute(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(a.sorted(t), "/never") {
		t.Error("sent an unreplayable request")
	}
	run := readRun(t, opts.OutDir)
	if run.Requests != (report.RequestCounts{Total: 2, Sent: 1, Skipped: 1}) {
		t.Errorf("requests = %+v", run.Requests)
	}
	if !strings.Contains(strings.Join(run.Warnings, "\n"), "unreplayable") {
		t.Errorf("warnings = %v", run.Warnings)
	}
}

func TestMismatchedSeedSendsNothing(t *testing.T) {
	var sends atomic.Int64
	digest := func(value string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/health" {
				_, _ = w.Write([]byte(`{"seed_digest":"` + value + `"}`))
				return
			}
			sends.Add(1)
		})
	}
	baseline, _ := target(t, digest("one"))
	candidate, _ := target(t, digest("two"))
	opts := options(t, baseline, candidate, rec("r1", "POST", "/a", 0, `{}`))
	if _, err := Execute(context.Background(), opts); !errors.Is(err, ErrSeedMismatch) {
		t.Fatalf("err = %v", err)
	}
	if sends.Load() != 0 {
		t.Fatalf("sent %d requests", sends.Load())
	}
	if _, err := os.Stat(opts.OutDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("run directory should not exist: %v", err)
	}
}

func TestMutationToExternalTargetIsRefused(t *testing.T) {
	var a recorder
	baseline, server := target(t, &a)
	candidate, _ := target(t, &recorder{})
	external := baseline
	external.External = true
	opts := options(t, external, candidate, rec("r1", "POST", "/a", 0, `{}`))
	if _, err := Execute(context.Background(), opts); err == nil || !strings.Contains(err.Error(), "--allow-external-mutation") {
		t.Fatalf("err = %v", err)
	}
	if a.sorted(t) != "" {
		t.Fatalf("%s received: %s", server.URL, a.sorted(t))
	}

	reads := options(t, external, candidate, rec("r1", "GET", "/a", 0, ""))
	if _, err := Execute(context.Background(), reads); err != nil {
		t.Errorf("safe methods must pass: %v", err)
	}
	optIn := options(t, external, candidate, rec("r1", "POST", "/a", 0, `{}`))
	optIn.AllowExternalMutation = true
	if _, err := Execute(context.Background(), optIn); err != nil {
		t.Errorf("explicit opt-in must pass: %v", err)
	}
}

func TestPolicyRefusalStopsBeforeSending(t *testing.T) {
	var a recorder
	baseline, _ := target(t, &a)
	candidate, _ := target(t, &recorder{})
	mapped, err := config.CheckTarget(strings.Replace(baseline.URL.String(), "127.0.0.1", "0.0.0.0", 1), []string{"0.0.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	opts := options(t, mapped, candidate, rec("r1", "GET", "/a", 0, ""))
	if _, err := Execute(context.Background(), opts); !errors.Is(err, egress.ErrRefused) {
		t.Fatalf("err = %v", err)
	}
	if a.sorted(t) != "" {
		t.Fatal("request reached the target")
	}
}

func TestRunDirectoryIsNeverReused(t *testing.T) {
	baseline, _ := target(t, &recorder{})
	candidate, _ := target(t, &recorder{})
	opts := options(t, baseline, candidate, rec("r1", "GET", "/a", 0, ""))
	if _, err := Execute(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(context.Background(), opts); !errors.Is(err, os.ErrExist) {
		t.Fatalf("err = %v", err)
	}
}

func TestConcurrencyCapHoldsOverHTTP(t *testing.T) {
	const limit = 3
	var inFlight, peak atomic.Int64
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			http.NotFound(w, r)
			return
		}
		n := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
	})
	baseline, _ := target(t, handler)
	candidate, _ := target(t, handler)
	var records []trace.Record
	for i := range 50 {
		records = append(records, rec(fmt.Sprintf("r%d", i), "GET", "/a", 0, ""))
	}
	opts := options(t, baseline, candidate, records...)
	opts.Concurrency = limit
	if _, err := Execute(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if peak.Load() != limit {
		t.Fatalf("peak = %d, want %d", peak.Load(), limit)
	}
}

func TestArrivalTimingIsPreservedAtEachSpeed(t *testing.T) {
	for _, speed := range []float64{1, 2} {
		t.Run(fmt.Sprintf("speed %v", speed), func(t *testing.T) {
			baseline, _ := target(t, &recorder{})
			candidate, _ := target(t, &recorder{})
			opts := options(t, baseline, candidate,
				rec("r1", "GET", "/a", 0, ""), rec("r2", "GET", "/b", 100, ""), rec("r3", "GET", "/c", 200, ""))
			opts.Speed = speed
			if _, err := Execute(context.Background(), opts); err != nil {
				t.Fatal(err)
			}
			for _, r := range results(t, opts.OutDir)["candidate"] {
				var want float64
				switch r.RequestID {
				case "r2":
					want = 100 / speed
				case "r3":
					want = 200 / speed
				}
				if r.ScheduledOffsetMS != want {
					t.Errorf("%s scheduled %v, want %v", r.RequestID, r.ScheduledOffsetMS, want)
				}
				// Generous upper bound: CI machines stall, and this is not a benchmark.
				if r.ActualStartOffsetMS < want || r.ActualStartOffsetMS > want+75 {
					t.Errorf("%s started %v, due %v", r.RequestID, r.ActualStartOffsetMS, want)
				}
			}
		})
	}
}

func TestLoadWorkloadRejects(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(bad, []byte(`{"schema_version":9}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadWorkload(bad); err == nil {
		t.Error("accepted a future schema")
	}
	empty := filepath.Join(t.TempDir(), "e.jsonl")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadWorkload(empty); err == nil {
		t.Error("accepted an empty trace")
	}
	if _, err := LoadWorkload(bad + ".missing"); err == nil {
		t.Error("accepted a missing file")
	}
}

func TestLoadWorkloadSortsByArrival(t *testing.T) {
	workload, err := LoadWorkload(writeTrace(t, rec("late", "GET", "/a", 50, ""), rec("early", "GET", "/b", 10, "")))
	if err != nil {
		t.Fatal(err)
	}
	if workload.Records[0].RequestID != "early" {
		t.Fatalf("records = %+v", workload.Records)
	}
}
