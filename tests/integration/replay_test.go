package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Hanart-prog/ghosttraffic/internal/capture"
	"github.com/Hanart-prog/ghosttraffic/internal/egress"
	"github.com/Hanart-prog/ghosttraffic/internal/metrics"
	"github.com/Hanart-prog/ghosttraffic/internal/replay"
	"github.com/Hanart-prog/ghosttraffic/internal/report"
	"github.com/Hanart-prog/ghosttraffic/internal/trace"
	"github.com/Hanart-prog/ghosttraffic/pkg/config"
)

type backend struct {
	mu   sync.Mutex
	seen []string
}

func (b *backend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/health" {
		_, _ = w.Write([]byte(`{"seed_digest":"same"}`))
		return
	}
	var body bytes.Buffer
	_, _ = body.ReadFrom(r.Body)
	b.mu.Lock()
	b.seen = append(b.seen, r.Method+" "+r.URL.RequestURI()+" "+body.String())
	b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func (b *backend) sorted() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := slices.Clone(b.seen)
	slices.Sort(out)
	return out
}

// The whole pipeline: requests through the recorder, into a trace, replayed
// against two independent backends, into artifacts that parse.
func TestCaptureThenReplay(t *testing.T) {
	var origin, baseline, candidate backend
	originServer := httptest.NewServer(&origin)
	defer originServer.Close()
	baselineServer := httptest.NewServer(&baseline)
	defer baselineServer.Close()
	candidateServer := httptest.NewServer(&candidate)
	defer candidateServer.Close()

	dir := t.TempDir()
	tracePath := filepath.Join(dir, "demo.jsonl")
	writer, err := trace.Create(tracePath)
	if err != nil {
		t.Fatal(err)
	}
	upstream, err := url.Parse(originServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	proxy := capture.New(capture.Options{
		Upstream:       upstream,
		Transport:      egress.NewClient(egress.Options{Timeout: 5 * time.Second}).Transport,
		Sink:           writer,
		AllowedHeaders: cfg.Capture.AllowedHeaders,
		MaxBodyBytes:   cfg.Capture.MaxBodyBytes,
		SessionHeader:  cfg.Capture.SessionHeader,
	})
	proxyServer := httptest.NewServer(proxy)

	const total = 20
	for i := range total {
		body := fmt.Sprintf(`{"n":%d,"password":"hunter2"}`, i)
		request, err := http.NewRequest("POST", proxyServer.URL+fmt.Sprintf("/api/items/%d?token=abc", i), bytes.NewReader([]byte(body)))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer hunter2")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
	}
	proxyServer.Close()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("hunter2")) {
		t.Fatal("secret reached the trace")
	}

	workload, err := replay.LoadWorkload(tracePath)
	if err != nil {
		t.Fatal(err)
	}
	baseTarget, err := config.CheckTarget(baselineServer.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	candTarget, err := config.CheckTarget(candidateServer.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	router, err := metrics.NewRouter([]string{"/api/items/{id}"})
	if err != nil {
		t.Fatal(err)
	}
	client := egress.NewClient(egress.Options{Timeout: 5 * time.Second, MaxConns: 4})
	defer client.CloseIdleConnections()
	runDir := filepath.Join(dir, "run")
	outcome, err := replay.Execute(context.Background(), replay.Options{
		Workload: workload, Baseline: baseTarget, Candidate: candTarget, Client: client, Router: router,
		OutDir: runDir, Concurrency: 4, QueueLimit: 100, Speed: 10, Timeout: 2 * time.Second, MaxResponseBytes: 1 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}

	got, want := baseline.sorted(), candidate.sorted()
	if len(got) != total || !slices.Equal(got, want) {
		t.Fatalf("workloads differ:\n%v\n%v", got, want)
	}
	if got[0] != `POST /api/items/0?token=[REDACTED] {"n":0,"password":"[REDACTED]"}` {
		t.Errorf("replayed %q", got[0])
	}
	if outcome.Run.Status != report.RunCompleted || outcome.Run.BaselineSeed != "same" {
		t.Errorf("run = %+v", outcome.Run)
	}
	file, err := os.Open(filepath.Join(runDir, "results.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	count := 0
	if err := metrics.ReadResults(file, func(r metrics.Result) error {
		count++
		if r.EndpointKey != "POST /api/items/{id}" || r.Status != 200 {
			return fmt.Errorf("result: %+v", r)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if count != 2*total {
		t.Fatalf("got %d results", count)
	}
	data, err := os.ReadFile(filepath.Join(runDir, "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := report.ParseRun(data); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) {
		t.Fatal("run.json is not JSON")
	}
}
