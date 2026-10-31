package capture

import (
	"bytes"
	"crypto/sha256"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestFilterHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set("Authorization", "Bearer secret")
	h.Set("X-Forwarded-For", "203.0.113.9")
	h.Set("X-Custom", "no")
	got := FilterHeaders(h, []string{"Content-Type", "Authorization", "X-Forwarded-For"})
	if len(got) != 1 || got["content-type"] != "application/json" {
		t.Fatalf("got %v", got)
	}
}

func TestSanitizeQuery(t *testing.T) {
	tests := []struct{ in, want string }{
		{"/a", "/a"},
		{"/a?query=shoe&limit=5", "/a?query=shoe&limit=5"},
		{"/a?access_token=abc&q=1", "/a?access_token=[REDACTED]&q=1"},
		{"/a?q=1&API-KEY=abc", "/a?q=1&API-KEY=[REDACTED]"},
		{"/a?pass%77ord=abc", "/a?pass%77ord=[REDACTED]"},
		{"/a?flag", "/a?flag"},
	}
	for _, tt := range tests {
		if got := SanitizeQuery(tt.in); got != tt.want {
			t.Errorf("SanitizeQuery(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestExcluded(t *testing.T) {
	prefixes := []string{"/admin", "/internal/"}
	for path, want := range map[string]bool{
		"/admin":         true,
		"/admin/users":   true,
		"/admin?x=1":     true,
		"/internal/a":    true,
		"/administrator": false,
		"/api/admin":     false,
	} {
		if got := Excluded(path, prefixes); got != want {
			t.Errorf("Excluded(%q) = %v", path, got)
		}
	}
}

func TestRedactBody(t *testing.T) {
	tests := []struct {
		name, in string
		paths    []string
		want     string
	}{
		{name: "clean", in: `{"id":1}`, want: `{"id":1}`},
		{name: "key at depth", in: `{"a":[{"user_password":"x"}]}`, want: `{"a":[{"user_password":"[REDACTED]"}]}`},
		{name: "secret object replaced whole", in: `{"session":{"id":1}}`, want: `{"session":"[REDACTED]"}`},
		{name: "exact numbers", in: `{"n":12345678901234567890.5}`, want: `{"n":12345678901234567890.5}`},
		{name: "configured path", in: `{"user":{"email":"a@b"}}`, paths: []string{"$.user.email"}, want: `{"user":{"email":"[REDACTED]"}}`},
		{name: "wildcard", in: `{"items":[{"e":1},{"e":2}]}`, paths: []string{"$.items[*].e"}, want: `{"items":[{"e":"[REDACTED]"},{"e":"[REDACTED]"}]}`},
		{name: "wildcard leaf", in: `{"tags":["a","b"]}`, paths: []string{"$.tags[*]"}, want: `{"tags":["[REDACTED]","[REDACTED]"]}`},
		{name: "missing path", in: `{"a":1}`, paths: []string{"$.b.c"}, want: `{"a":1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RedactBody([]byte(tt.in), tt.paths)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestRedactBodyRejects(t *testing.T) {
	for _, in := range []string{`nope`, `{"a":1}{"b":2}`} {
		if _, err := RedactBody([]byte(in), nil); err == nil {
			t.Errorf("accepted %q", in)
		}
	}
	if _, err := RedactBody([]byte(`{}`), []string{"a.b"}); err == nil {
		t.Error("accepted a bad path")
	}
}

func TestParsePathRejects(t *testing.T) {
	for _, path := range []string{"", "$", "$.", "a.b", "$.a[0]", "$.a..b", "$..a"} {
		if _, err := ParsePath(path); err == nil {
			t.Errorf("accepted %q", path)
		}
	}
}

func TestCappedTeePassesEverythingAndKeepsTheCap(t *testing.T) {
	const size, limit = 8 << 20, 1 << 10
	body := bytes.Repeat([]byte("ghost"), size/5)
	tee := NewCappedTee(io.NopCloser(bytes.NewReader(body)), limit)
	got, err := io.ReadAll(tee)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(got) != sha256.Sum256(body) {
		t.Fatal("body was altered in transit")
	}
	kept, truncated := tee.Retained()
	if !truncated || len(kept) != limit {
		t.Fatalf("kept %d bytes, truncated=%v", len(kept), truncated)
	}
}

func TestCappedTeeExactFitIsNotTruncated(t *testing.T) {
	tee := NewCappedTee(io.NopCloser(strings.NewReader("abcd")), 4)
	if _, err := io.ReadAll(tee); err != nil {
		t.Fatal(err)
	}
	if kept, truncated := tee.Retained(); truncated || string(kept) != "abcd" {
		t.Fatalf("kept %q, truncated=%v", kept, truncated)
	}
}
