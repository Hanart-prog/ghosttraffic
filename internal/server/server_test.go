package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hanart-prog/ghosttraffic/internal/metrics"
	"github.com/Hanart-prog/ghosttraffic/internal/report"
	"github.com/Hanart-prog/ghosttraffic/internal/trace"
)

const started = "2026-01-02T03:04:05Z"

func manifest(id string) string {
	return fmt.Sprintf(`{"schema_version":1,"run_id":%q,"trace_sha256":"ab","config":{},"baseline":"http://b","candidate":"http://c",
"requests":{"total":3,"sent":3,"skipped":0},"started_at":%q,"ended_at":null,"build":{"version":"v","commit":"c"},
"status":"completed","passes":[],"warnings":[]}`, id, started)
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T) string {
	t.Helper()
	runs := t.TempDir()
	dir := filepath.Join(runs, "run-1")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "run.json"), manifest("run-1"))
	w, err := metrics.CreateResults(filepath.Join(dir, "results.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for i := range 5 {
		for _, target := range []string{"baseline", "candidate"} {
			body := fmt.Sprintf(`{"id":%d,"v":"%s"}`, i, target)
			if err := w.Write(metrics.Result{
				SchemaVersion: 1, Trial: 1 + i%2, RequestID: fmt.Sprintf("r%d", i), Target: target,
				EndpointKey: "GET /a", ScheduledOffsetMS: float64(10 - i), Status: 200,
				ErrorCategory: metrics.ErrorNone, ResponseJSON: json.RawMessage(body),
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	built, err := report.Marshal(report.Report{
		SchemaVersion: 1, RunID: "run-1", Verdict: report.VerdictNone, Endpoints: []report.Endpoint{{Key: "GET /a"}},
		IgnoredJSONPaths: []string{"$.id"}, Provenance: report.Provenance{Config: json.RawMessage(`{}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "report.json"), string(built))
	tr, err := trace.Create(filepath.Join(dir, "trace.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Write(trace.Record{SchemaVersion: 1, RequestID: "r0", Method: "GET", Path: "/a", Replayable: true}); err != nil {
		t.Fatal(err)
	}
	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}
	return runs
}

func request(h http.Handler, method, target string, change ...func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	r.Host = "127.0.0.1:7070"
	for _, c := range change {
		c(r)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func get[T any](t *testing.T, h http.Handler, target string) T {
	t.Helper()
	w := request(h, "GET", target)
	if w.Code != http.StatusOK {
		t.Fatalf("%s: %d %s", target, w.Code, w.Body)
	}
	var out T
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func handler(t *testing.T) http.Handler {
	t.Helper()
	return New(Options{RunsDir: fixture(t)}).Handler()
}

func TestHealth(t *testing.T) {
	if w := request(handler(t), "GET", "/health"); w.Code != 200 {
		t.Fatal(w.Code)
	}
}

func TestListRunsSkipsWhatIsNotARun(t *testing.T) {
	runs := fixture(t)
	if err := os.Mkdir(filepath.Join(runs, "not-a-run"), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(runs, "stray.txt"), "x")
	got := get[[]RunSummary](t, New(Options{RunsDir: runs}).Handler(), "/api/runs")
	if len(got) != 1 || got[0].RunID != "run-1" || got[0].Verdict == nil || *got[0].Verdict != report.VerdictNone {
		t.Fatalf("got %+v", got)
	}
}

func TestListRunsEmptyIsAnArray(t *testing.T) {
	w := request(New(Options{RunsDir: filepath.Join(t.TempDir(), "missing")}).Handler(), "GET", "/api/runs")
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("%d %q", w.Code, w.Body)
	}
}

func TestRunDetail(t *testing.T) {
	got := get[RunDetail](t, handler(t), "/api/runs/run-1")
	if got.Run.RunID != "run-1" || got.Report == nil {
		t.Fatalf("got %+v", got)
	}
}

func TestRunWithoutReport(t *testing.T) {
	runs := fixture(t)
	if err := os.Remove(filepath.Join(runs, "run-1", "report.json")); err != nil {
		t.Fatal(err)
	}
	h := New(Options{RunsDir: runs}).Handler()
	if got := get[RunDetail](t, h, "/api/runs/run-1"); got.Report != nil {
		t.Fatalf("got %+v", got)
	}
	if w := request(h, "GET", "/api/runs/run-1/endpoints"); w.Code != 404 {
		t.Fatal(w.Code)
	}
}

func TestUnreadableReportIsSaid(t *testing.T) {
	runs := fixture(t)
	write(t, filepath.Join(runs, "run-1", "report.json"), `{"schema_version":9}`)
	got := get[RunDetail](t, New(Options{RunsDir: runs}).Handler(), "/api/runs/run-1")
	if got.Report != nil || got.ReportError == "" {
		t.Fatalf("got %+v", got)
	}
}

func TestEndpoints(t *testing.T) {
	if got := get[[]report.Endpoint](t, handler(t), "/api/runs/run-1/endpoints"); len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestPaginationVisitsEveryResultOnce(t *testing.T) {
	h := handler(t)
	seen := map[string]bool{}
	target := "/api/runs/run-1/requests?limit=3"
	pages := 0
	for {
		page := get[RequestPage](t, h, target)
		pages++
		for _, item := range page.Items {
			key := item.Target + item.RequestID
			if seen[key] {
				t.Fatalf("%s seen twice", key)
			}
			seen[key] = true
			if item.ResponseJSON != nil {
				t.Fatal("list must not carry bodies")
			}
		}
		if page.NextCursor == nil {
			break
		}
		target = fmt.Sprintf("/api/runs/run-1/requests?limit=3&cursor=%d", *page.NextCursor)
	}
	if len(seen) != 10 || pages != 4 {
		t.Fatalf("seen %d in %d pages", len(seen), pages)
	}
}

func TestExactLastPageHasNoNextCursor(t *testing.T) {
	if page := get[RequestPage](t, handler(t), "/api/runs/run-1/requests?limit=10"); page.NextCursor != nil {
		t.Fatalf("next = %d", *page.NextCursor)
	}
}

func TestRequestFilters(t *testing.T) {
	h := handler(t)
	base := "/api/runs/run-1/requests?"
	tests := []struct {
		query string
		want  int
	}{
		{"target=candidate", 5},
		{"trial=1", 6},
		{"trial=1&target=baseline", 3},
		{"endpoint=" + url.QueryEscape("GET /a"), 10},
		{"endpoint=" + url.QueryEscape("GET /nope"), 0},
	}
	for _, tt := range tests {
		if got := get[RequestPage](t, h, base+tt.query); len(got.Items) != tt.want {
			t.Errorf("%s: %d items", tt.query, len(got.Items))
		}
	}
}

func TestBadParameters(t *testing.T) {
	h := handler(t)
	for _, target := range []string{
		"/api/runs/run-1/requests?limit=0", "/api/runs/run-1/requests?limit=501",
		"/api/runs/run-1/requests?limit=x", "/api/runs/run-1/requests?cursor=-1",
		"/api/runs/run-1/requests?cursor=7", "/api/runs/run-1/requests?target=prod",
		"/api/runs/run-1/requests?trial=x", "/api/runs/run-1/timeline?limit=0",
		"/api/runs/run-1/timeline?target=prod", "/api/runs/run-1/timeline?limit=1001",
		"/api/runs/run-1/requests/r0?trial=x",
	} {
		if w := request(h, "GET", target); w.Code != 400 {
			t.Errorf("%s: %d", target, w.Code)
		}
	}
}

func TestRequestDetail(t *testing.T) {
	got := get[RequestDetail](t, handler(t), "/api/runs/run-1/requests/r0?trial=1")
	if got.Request == nil || got.Request.Path != "/a" || got.Baseline == nil || got.Candidate == nil {
		t.Fatalf("got %+v", got)
	}
	if got.Classification != report.ClassBodyMismatch || len(got.DifferingPaths) != 1 || got.DifferingPaths[0] != "$.v" {
		t.Fatalf("got %+v", got)
	}
}

func TestRequestDetailNotFound(t *testing.T) {
	h := handler(t)
	for _, target := range []string{"/api/runs/run-1/requests/nope", "/api/runs/run-1/requests/r0?trial=2"} {
		if w := request(h, "GET", target); w.Code != 404 {
			t.Errorf("%s: %d", target, w.Code)
		}
	}
}

func TestTimelineIsSortedAndWindowed(t *testing.T) {
	h := handler(t)
	got := get[Timeline](t, h, "/api/runs/run-1/timeline?trial=1&target=candidate&limit=2")
	if got.Total != 3 || len(got.Items) != 2 || got.Items[0].ScheduledOffsetMS > got.Items[1].ScheduledOffsetMS {
		t.Fatalf("got %+v", got)
	}
	if past := get[Timeline](t, h, "/api/runs/run-1/timeline?offset=99"); len(past.Items) != 0 {
		t.Fatalf("got %+v", past)
	}
}

func TestPathTraversalIsRejected(t *testing.T) {
	runs := fixture(t)
	h := New(Options{RunsDir: runs}).Handler()
	for _, id := range []string{"..", "..%2f..", "%2e%2e", ".hidden", "a%2fb", "run-1%00", "a..b", strings.Repeat("a", 129)} {
		w := request(h, "GET", "/api/runs/"+id)
		if w.Code == 200 {
			t.Errorf("%s: served", id)
		}
	}
}

func TestSymlinkedRunIsRefused(t *testing.T) {
	runs := fixture(t)
	outside := t.TempDir()
	write(t, filepath.Join(outside, "run.json"), manifest("outside"))
	if err := os.Symlink(outside, filepath.Join(runs, "link")); err != nil {
		t.Fatal(err)
	}
	h := New(Options{RunsDir: runs}).Handler()
	if w := request(h, "GET", "/api/runs/link"); w.Code != 404 {
		t.Fatalf("%d", w.Code)
	}
	if got := get[[]RunSummary](t, h, "/api/runs"); len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestSymlinkedArtifactIsRefused(t *testing.T) {
	runs := fixture(t)
	secret := filepath.Join(t.TempDir(), "secret")
	write(t, secret, manifest("secret"))
	target := filepath.Join(runs, "run-1", "run.json")
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, target); err != nil {
		t.Fatal(err)
	}
	if w := request(New(Options{RunsDir: runs}).Handler(), "GET", "/api/runs/run-1"); w.Code != 404 {
		t.Fatalf("%d", w.Code)
	}
}

func TestDNSRebindingIsRejected(t *testing.T) {
	h := handler(t)
	for host, want := range map[string]int{
		"evil.example:7070": 403, "evil.example": 403, "192.168.1.5:7070": 403, "localhost.evil.example": 403,
		"localhost:7070": 200, "127.0.0.1:7070": 200, "[::1]:7070": 200, "localhost": 200,
	} {
		w := request(h, "GET", "/api/runs", func(r *http.Request) { r.Host = host })
		if w.Code != want {
			t.Errorf("%s: %d, want %d", host, w.Code, want)
		}
	}
}

func TestCORSOnlyForAllowedOrigins(t *testing.T) {
	h := New(Options{RunsDir: fixture(t), AllowOrigins: []string{"http://127.0.0.1:5173"}}).Handler()
	origin := func(value string) func(*http.Request) {
		return func(r *http.Request) { r.Header.Set("Origin", value) }
	}
	if got := request(h, "GET", "/api/runs", origin("http://127.0.0.1:5173")).Header().Get("Access-Control-Allow-Origin"); got != "http://127.0.0.1:5173" {
		t.Errorf("allowed origin got %q", got)
	}
	if got := request(h, "GET", "/api/runs", origin("https://evil.example")).Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("other origin got %q", got)
	}
}

func TestReadOnly(t *testing.T) {
	h := handler(t)
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		if w := request(h, method, "/api/runs"); w.Code != 405 {
			t.Errorf("%s: %d", method, w.Code)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	w := request(handler(t), "GET", "/api/runs")
	if w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("headers = %v", w.Header())
	}
}

func TestDashboardFiles(t *testing.T) {
	web := t.TempDir()
	write(t, filepath.Join(web, "index.html"), "<title>app</title>")
	write(t, filepath.Join(web, "app.js"), "ok()")
	write(t, filepath.Join(filepath.Dir(web), "outside.txt"), "secret")
	h := New(Options{RunsDir: fixture(t), WebDir: web}).Handler()
	tests := []struct{ path, want string }{
		{"/", "app"}, {"/runs/x/timeline", "app"}, {"/app.js", "ok()"},
	}
	for _, tt := range tests {
		if w := request(h, "GET", tt.path); !strings.Contains(w.Body.String(), tt.want) || strings.Contains(w.Body.String(), "secret") {
			t.Errorf("%s: %d %q", tt.path, w.Code, w.Body)
		}
	}
	// The mux redirects this to /outside.txt, which then falls back to the app.
	if w := request(h, "GET", "/../outside.txt"); strings.Contains(w.Body.String(), "secret") {
		t.Errorf("escaped the web directory: %q", w.Body)
	}
	if w := request(h, "GET", "/outside.txt"); strings.Contains(w.Body.String(), "secret") || !strings.Contains(w.Body.String(), "app") {
		t.Errorf("escaped the web directory: %q", w.Body)
	}
	if w := request(h, "GET", "/api/nope"); w.Code != 404 || strings.Contains(w.Body.String(), "app") {
		t.Errorf("unknown API path: %d %q", w.Code, w.Body)
	}
}
