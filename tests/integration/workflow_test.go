package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hanart-prog/ghosttraffic/internal/egress"
	"github.com/Hanart-prog/ghosttraffic/internal/metrics"
	"github.com/Hanart-prog/ghosttraffic/internal/replay"
	"github.com/Hanart-prog/ghosttraffic/internal/trace"
	"github.com/Hanart-prog/ghosttraffic/internal/workflow"
	"github.com/Hanart-prog/ghosttraffic/pkg/config"
)

// shop is a stateful backend whose IDs start wherever it is told, so recorded
// IDs are wrong on it unless workflows substitute real ones.
type shop struct {
	mu      sync.Mutex
	next    int
	carts   map[int]int // cart -> items
	ordered map[int]bool
	bad     []string
	failID  bool // respond without the cart ID
}

func newShop(first int) *shop {
	return &shop{next: first, carts: map[int]int{}, ordered: map[int]bool{}}
}

func (s *shop) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	reply := func(status int, body string) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
	var id int
	switch {
	case r.URL.Path == "/health":
		reply(200, `{}`)
	case r.Method == "POST" && r.URL.Path == "/api/carts":
		id = s.next
		s.next++
		s.carts[id] = 0
		if s.failID {
			reply(201, `{"cart":{}}`)
			return
		}
		reply(201, fmt.Sprintf(`{"cart":{"id":%d}}`, id))
	case r.Method == "POST" && r.URL.Path == "/api/orders":
		var body struct {
			CartID int `json:"cart_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if s.carts[body.CartID] == 0 {
			s.bad = append(s.bad, fmt.Sprintf("order for cart %d with no items", body.CartID))
			reply(409, `{}`)
			return
		}
		s.ordered[body.CartID] = true
		reply(201, `{"order":{}}`)
	default:
		if _, err := fmt.Sscanf(r.URL.Path, "/api/carts/%d/items", &id); err != nil {
			reply(404, `{}`)
			return
		}
		if _, ok := s.carts[id]; !ok {
			s.bad = append(s.bad, fmt.Sprintf("unknown cart %d", id))
			reply(404, `{}`)
			return
		}
		s.carts[id]++
		reply(201, `{}`)
	}
}

// recordedCheckout writes sessions whose IDs came from the capture backend.
func recordedCheckout(t *testing.T, dir string, sessions int) (string, workflow.File) {
	t.Helper()
	path := filepath.Join(dir, "t.jsonl")
	w, err := trace.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	var file workflow.File
	for i := range sessions {
		recorded := 17 + i
		ids := []string{fmt.Sprintf("c%d", i), fmt.Sprintf("i%d", i), fmt.Sprintf("o%d", i)}
		records := []trace.Record{
			{RequestID: ids[0], Method: "POST", Path: "/api/carts", BodyJSON: json.RawMessage(`{}`)},
			{RequestID: ids[1], Method: "POST", Path: fmt.Sprintf("/api/carts/%d/items", recorded), BodyJSON: json.RawMessage(`{}`)},
			{RequestID: ids[2], Method: "POST", Path: "/api/orders", BodyJSON: json.RawMessage(fmt.Sprintf(`{"cart_id":%d}`, recorded))},
		}
		for step, record := range records {
			record.SchemaVersion, record.Replayable = trace.SchemaVersion, true
			record.ArrivalOffsetMS = int64(i + step)
			record.Headers = map[string]string{"content-type": "application/json"}
			record.Capture.Status = 201
			if err := w.Write(record); err != nil {
				t.Fatal(err)
			}
		}
		file.Workflows = append(file.Workflows, workflow.Workflow{
			SessionID: fmt.Sprintf("s%d", i),
			Steps: []workflow.Step{
				{ID: "cart", RequestID: ids[0], Extract: map[string]string{"cart_id": "$.cart.id"}},
				{ID: "item", RequestID: ids[1], DependsOn: []string{"cart"}, Substitute: workflow.Substitute{
					Path: map[string]string{fmt.Sprintf("/api/carts/%d", recorded): "/api/carts/${cart_id}"}}},
				{ID: "order", RequestID: ids[2], DependsOn: []string{"item"}, Substitute: workflow.Substitute{
					Body: map[string]string{"$.cart_id": "${cart_id}"}}},
			},
		})
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return path, file
}

func runWorkflows(t *testing.T, baseline, candidate *shop, sessions int) map[string]map[string]metrics.Result {
	t.Helper()
	baselineServer := httptest.NewServer(baseline)
	defer baselineServer.Close()
	candidateServer := httptest.NewServer(candidate)
	defer candidateServer.Close()
	dir := t.TempDir()
	tracePath, file := recordedCheckout(t, dir, sessions)
	workload, err := replay.LoadWorkload(tracePath)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := workflow.Compile(file, workload.Records)
	if err != nil {
		t.Fatal(err)
	}
	a, err := config.CheckTarget(baselineServer.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := config.CheckTarget(candidateServer.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	router, err := metrics.NewRouter(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := egress.NewClient(egress.Options{Timeout: 5 * time.Second, MaxConns: 16})
	defer client.CloseIdleConnections()
	runDir := filepath.Join(dir, "run")
	if _, err := replay.Execute(context.Background(), replay.Options{
		Workload: workload, Plan: plan, Baseline: a, Candidate: b, Client: client, Router: router, OutDir: runDir,
		Concurrency: 16, QueueLimit: 1000, Speed: 5, Timeout: 2 * time.Second, MaxResponseBytes: 1 << 20,
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.Open(filepath.Join(runDir, "results.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	out := map[string]map[string]metrics.Result{"baseline": {}, "candidate": {}}
	if err := metrics.ReadResults(data, func(r metrics.Result) error {
		out[r.Target][r.RequestID] = r
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// The Phase 4 gate: concurrent checkouts complete independently on two targets
// whose generated IDs share nothing with each other or with the recording.
func TestCheckoutWorksOnBothTargetsWithDifferentIDs(t *testing.T) {
	const sessions = 40
	baseline, candidate := newShop(1000), newShop(900000)
	results := runWorkflows(t, baseline, candidate, sessions)
	for name, s := range map[string]*shop{"baseline": baseline, "candidate": candidate} {
		if len(s.bad) != 0 {
			t.Errorf("%s saw: %s", name, strings.Join(s.bad, "; "))
		}
		if len(s.ordered) != sessions {
			t.Errorf("%s has %d orders, want %d", name, len(s.ordered), sessions)
		}
		for id, r := range results[name] {
			if r.ErrorCategory != metrics.ErrorNone || r.Status != 201 {
				t.Errorf("%s %s = %+v", name, id, r)
			}
		}
	}
}

func TestFailedExtractionStopsThatSessionOnThatTargetOnly(t *testing.T) {
	baseline, candidate := newShop(1000), newShop(2000)
	candidate.failID = true
	results := runWorkflows(t, baseline, candidate, 3)
	if len(baseline.ordered) != 3 {
		t.Errorf("baseline orders = %d", len(baseline.ordered))
	}
	if len(candidate.ordered) != 0 {
		t.Errorf("candidate orders = %d", len(candidate.ordered))
	}
	for i := range 3 {
		got := results["candidate"]
		if c := got[fmt.Sprintf("c%d", i)].ErrorCategory; c != metrics.ErrorExtraction {
			t.Errorf("cart step = %s", c)
		}
		for _, id := range []string{fmt.Sprintf("i%d", i), fmt.Sprintf("o%d", i)} {
			if c := got[id].ErrorCategory; c != metrics.ErrorDependency {
				t.Errorf("%s = %s", id, c)
			}
		}
	}
}
