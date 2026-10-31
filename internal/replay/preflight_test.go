package replay

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func health(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if body == "" {
			http.NotFound(w, nil)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
}

func TestSeedDigests(t *testing.T) {
	tests := []struct {
		name, a, b string
		wantErr    error
		wantA      string
	}{
		{name: "equal", a: `{"seed_digest":"x"}`, b: `{"seed_digest":"x"}`, wantA: "x"},
		{name: "different", a: `{"seed_digest":"x"}`, b: `{"seed_digest":"y"}`, wantErr: ErrSeedMismatch, wantA: "x"},
		{name: "one unknown", a: `{"seed_digest":"x"}`, b: "", wantA: "x"},
		{name: "not json", a: `nope`, b: `nope`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, b := health(tt.a), health(tt.b)
			defer a.Close()
			defer b.Close()
			got, _, err := SeedDigests(context.Background(), a.Client(), a.URL, b.URL)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got != tt.wantA {
				t.Fatalf("baseline digest = %q", got)
			}
		})
	}
}
