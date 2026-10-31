package metrics

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRouterKey(t *testing.T) {
	router, err := NewRouter([]string{"/api/carts/{cart_id}/items", "/api/products/{product}"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ method, path, want string }{
		{"POST", "/api/carts/17/items", "POST /api/carts/{cart_id}/items"},
		{"GET", "/api/products/abc?x=1", "GET /api/products/{product}"},
		{"GET", "/api/products?query=shoe", "GET /api/products"},
		{"GET", "/other/42/x", "GET /other/{id}/x"},
		{"GET", "/u/550e8400-e29b-41d4-a716-446655440000", "GET /u/{id}"},
		{"GET", "/h/0123456789abcdef", "GET /h/{id}"},
		{"GET", "/plain/name", "GET /plain/name"},
	}
	for _, tt := range tests {
		if got := router.Key(tt.method, tt.path); got != tt.want {
			t.Errorf("Key(%s %s) = %q, want %q", tt.method, tt.path, got, tt.want)
		}
	}
}

func TestNewRouterRejectsRelativeTemplates(t *testing.T) {
	if _, err := NewRouter([]string{"api/x"}); err == nil {
		t.Fatal("accepted a relative template")
	}
}

func TestResultFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "results.jsonl")
	w, err := CreateResults(path)
	if err != nil {
		t.Fatal(err)
	}
	want := Result{SchemaVersion: ResultSchemaVersion, Trial: 1, RequestID: "r", Target: "baseline", ErrorCategory: ErrorNone}
	if err := w.Write(want); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	if err := ReadResults(bytes.NewReader(data), func(r Result) error {
		count++
		if r.RequestID != "r" {
			t.Errorf("got %+v", r)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("read %d results", count)
	}
	if _, err := CreateResults(path); !errors.Is(err, os.ErrExist) {
		t.Fatalf("overwrite allowed: %v", err)
	}
	if err := ReadResults(bytes.NewReader([]byte("nope\n")), func(Result) error { return nil }); err == nil {
		t.Fatal("accepted a bad line")
	}
}
