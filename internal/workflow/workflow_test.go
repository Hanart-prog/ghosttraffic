package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hanart-prog/ghosttraffic/internal/trace"
)

func records() []trace.Record {
	return []trace.Record{
		{RequestID: "r1", Method: "POST", Path: "/api/carts"},
		{RequestID: "r2", Method: "POST", Path: "/api/carts/17/items", BodyJSON: json.RawMessage(`{"product_id":1}`)},
		{RequestID: "r3", Method: "POST", Path: "/api/orders", BodyJSON: json.RawMessage(`{"cart_id":17,"note":"x"}`)},
	}
}

func checkout() Workflow {
	return Workflow{SessionID: "s", Steps: []Step{
		{ID: "cart", RequestID: "r1", Extract: map[string]string{"cart_id": "$.cart.id"}},
		{ID: "item", RequestID: "r2", DependsOn: []string{"cart"},
			Substitute: Substitute{Path: map[string]string{"/api/carts/17": "/api/carts/${cart_id}"}}},
		{ID: "order", RequestID: "r3", DependsOn: []string{"item"},
			Substitute: Substitute{Body: map[string]string{"$.cart_id": "${cart_id}"}}},
	}}
}

func compiled(t *testing.T) *Plan {
	t.Helper()
	plan, err := Compile(File{Workflows: []Workflow{checkout()}}, records())
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

// order reads cart_id through item, so this also covers transitive variables.
func TestCompileAcceptsAChain(t *testing.T) {
	plan := compiled(t)
	if len(plan.Steps) != 3 || plan.Steps["r3"].After[0] != "r2" {
		t.Fatalf("plan = %+v", plan.Steps)
	}
}

func TestCompileRejects(t *testing.T) {
	tests := []struct {
		name    string
		change  func(*Workflow)
		wantErr string
	}{
		{"no session", func(w *Workflow) { w.SessionID = "" }, "session_id"},
		{"policy", func(w *Workflow) { w.OnDependencyFailure = "retry" }, "on_dependency_failure"},
		{"no step id", func(w *Workflow) { w.Steps[0].ID = "" }, "step id"},
		{"duplicate step", func(w *Workflow) { w.Steps[1].ID = "cart" }, "duplicate step"},
		{"unknown dependency", func(w *Workflow) { w.Steps[1].DependsOn = []string{"nope"} }, "unknown step"},
		{"cycle", func(w *Workflow) { w.Steps[0].DependsOn = []string{"order"} }, "cycle"},
		{"self cycle", func(w *Workflow) { w.Steps[0].DependsOn = []string{"cart"} }, "cycle"},
		{"unknown request", func(w *Workflow) { w.Steps[0].RequestID = "nope" }, "not in the replayable trace"},
		{"reused request", func(w *Workflow) { w.Steps[1].RequestID = "r1" }, "already used"},
		{"bad extract path", func(w *Workflow) { w.Steps[0].Extract["cart_id"] = "cart.id" }, "must begin"},
		{"wildcard extract", func(w *Workflow) { w.Steps[0].Extract["cart_id"] = "$.items[*].id" }, "wildcard"},
		{"bad variable name", func(w *Workflow) { w.Steps[0].Extract["a-b"] = "$.a" }, "invalid variable"},
		{"variable from nowhere", func(w *Workflow) { w.Steps[1].DependsOn = nil }, "not extracted"},
		{"path literal absent", func(w *Workflow) {
			w.Steps[1].Substitute.Path = map[string]string{"/nope": "/x"}
		}, "exactly once"},
		{"path literal ambiguous", func(w *Workflow) {
			w.Steps[1].Substitute.Path = map[string]string{"/": "x"}
		}, "exactly once"},
		{"body path absent", func(w *Workflow) {
			w.Steps[2].Substitute.Body = map[string]string{"$.missing": "${cart_id}"}
		}, "not present"},
		{"body without json", func(w *Workflow) {
			w.Steps[0].Substitute.Body = map[string]string{"$.a": "x"}
		}, "no JSON body"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := checkout()
			tt.change(&w)
			_, err := Compile(File{Workflows: []Workflow{w}}, records())
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("got %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestCompileRejectsDuplicateSessions(t *testing.T) {
	_, err := Compile(File{Workflows: []Workflow{{SessionID: "s"}, {SessionID: "s"}}}, records())
	if err == nil || !strings.Contains(err.Error(), "duplicate session") {
		t.Fatalf("got %v", err)
	}
}

func TestReadVariables(t *testing.T) {
	step := compiled(t).Steps["r1"]
	vars := Variables{}
	if err := step.ReadVariables([]byte(`{"cart":{"id":501}}`), vars); err != nil {
		t.Fatal(err)
	}
	if vars["cart_id"] != json.Number("501") {
		t.Fatalf("vars = %v", vars)
	}
	for _, bad := range []string{`nope`, `{"cart":{}}`, `{"cart":{"id":{"x":1}}}`, `{"cart":{"id":null}}`} {
		if err := step.ReadVariables([]byte(bad), Variables{}); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestApplySubstitutesPathAndTypedBody(t *testing.T) {
	plan := compiled(t)
	all := records()
	vars := Variables{"cart_id": json.Number("501")}
	path, _, err := plan.Steps["r2"].Apply(all[1], vars)
	if err != nil {
		t.Fatal(err)
	}
	if path != "/api/carts/501/items" {
		t.Errorf("path = %q", path)
	}
	_, body, err := plan.Steps["r3"].Apply(all[2], vars)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"cart_id":501,"note":"x"}` {
		t.Errorf("body = %s", body)
	}
}

func TestApplyEscapesValuesFromTheTarget(t *testing.T) {
	plan := compiled(t)
	path, _, err := plan.Steps["r2"].Apply(records()[1], Variables{"cart_id": "1/../../admin?x=1"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(path, "/admin") || strings.Contains(path, "?") {
		t.Fatalf("path = %q", path)
	}
	if err := (trace.Record{SchemaVersion: 1, RequestID: "r", Method: "GET", Path: path, Replayable: true}).Validate(); err != nil {
		t.Fatalf("result is not a valid path: %v", err)
	}
}

func TestApplyMissingVariableIsAnError(t *testing.T) {
	plan := compiled(t)
	if _, _, err := plan.Steps["r2"].Apply(records()[1], Variables{}); err == nil {
		t.Error("path: accepted a missing variable")
	}
	if _, _, err := plan.Steps["r3"].Apply(records()[2], Variables{}); err == nil {
		t.Error("body: accepted a missing variable")
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "w.yaml")
	if err := os.WriteFile(good, []byte(`workflows:
  - session_id: s
    steps:
      - id: cart
        request_id: r1
        extract:
          cart_id: $.cart.id
      - id: item
        request_id: r2
        depends_on: [cart]
        substitute:
          path: {"/api/carts/17": "/api/carts/${cart_id}"}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := Load(good)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Compile(file, records()); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "b.yaml")
	if err := os.WriteFile(bad, []byte("workflows:\n  - sesion_id: s\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(bad); err == nil {
		t.Error("accepted an unknown key")
	}
	if _, err := Load(filepath.Join(dir, "missing")); err == nil {
		t.Error("accepted a missing file")
	}
}
