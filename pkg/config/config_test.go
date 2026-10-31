package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultIsValid(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadEmptyPathReturnsDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Replay.Timeout != 30*time.Second {
		t.Fatalf("timeout = %v", cfg.Replay.Timeout)
	}
}

func TestLoadOverlaysDefaults(t *testing.T) {
	cfg, err := Load(writeConfig(t, "replay:\n  timeout: 5s\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Replay.Timeout != 5*time.Second {
		t.Errorf("timeout = %v", cfg.Replay.Timeout)
	}
	if cfg.Capture.MaxBodyBytes != Default().Capture.MaxBodyBytes {
		t.Errorf("unset value lost its default: %d", cfg.Capture.MaxBodyBytes)
	}
}

func TestLoadEmptyFileReturnsDefaults(t *testing.T) {
	if _, err := Load(writeConfig(t, "")); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRejects(t *testing.T) {
	tests := []struct{ name, yaml, wantErr string }{
		{"unknown key", "capture:\n  alowed_headers: []\n", "alowed_headers"},
		{"authorization", "capture:\n  allowed_headers: [authorization]\n", "cannot be captured"},
		{"cookie", "capture:\n  allowed_headers: [Cookie]\n", "cannot be captured"},
		{"body cap", "capture:\n  max_body_bytes: 0\n", "max_body_bytes"},
		{"timeout", "replay:\n  timeout: 0s\n", "timeout"},
		{"queue", "replay:\n  queue_limit: -1\n", "queue_limit"},
		{"redact path", "capture:\n  redact_json_paths: [user.email]\n", "redact_json_paths"},
		{"exclude path", "capture:\n  exclude_paths: [admin]\n", "exclude_paths"},
		{"response cap", "replay:\n  max_response_bytes: 0\n", "max_response_bytes"},
		{"route", "replay:\n  routes: [api/x]\n", "replay.routes"},
		{"ignore path", "report:\n  ignore_json_paths: [a.b]\n", "ignore_json_paths"},
		{"ratio", "report:\n  min_ratio: 1\n", "report thresholds"},
		{"not yaml", "capture: [", "parse"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tt.yaml))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("got error %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatal("expected an error")
	}
}

func TestCheckTarget(t *testing.T) {
	tests := []struct {
		url          string
		allow        []string
		wantErr      string
		wantExternal bool
	}{
		{url: "http://localhost:8000"},
		{url: "http://LOCALHOST:8000"},
		{url: "http://127.0.0.1:8000"},
		{url: "http://127.8.8.8"},
		{url: "http://[::1]:8000"},
		{url: "http://[::ffff:127.0.0.1]:8000"},
		{url: "https://baseline:8000"},
		{url: "http://demo_candidate-1:8000"},

		{url: "http://example.com", wantErr: "not local"},
		{url: "http://8.8.8.8", wantErr: "not local"},
		{url: "http://192.168.1.10", wantErr: "not local"},
		{url: "http://[2001:4860:4860::8888]", wantErr: "not local"},
		{url: "http://0.0.0.0:8000", wantErr: "not local"},
		{url: "http://134744072", wantErr: "not local"},
		{url: "http://0x08080808", wantErr: "not local"},
		{url: "http://localhost.evil.example", wantErr: "not local"},
		{url: "http://127.0.0.1.evil.example", wantErr: "not local"},

		{url: "http://Example.com", allow: []string{"example.com"}, wantExternal: true},
		{url: "http://other.example.com", allow: []string{"example.com"}, wantErr: "not local"},

		{url: "ftp://localhost", wantErr: "http or https"},
		{url: "localhost:8000", wantErr: "http or https"},
		{url: "http://", wantErr: "no host"},
		{url: "http://user:pw@localhost", wantErr: "credentials"},
		{url: "http://localhost/?a=1", wantErr: "query or fragment"},
		{url: "http://localhost/#a", wantErr: "query or fragment"},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got, err := CheckTarget(tt.url, tt.allow)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("got error %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.External != tt.wantExternal {
				t.Fatalf("External = %v, want %v", got.External, tt.wantExternal)
			}
		})
	}
}

func TestSameBackend(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"http://localhost:8000", "http://localhost:8000", true},
		{"http://localhost:8000", "http://LOCALHOST:8000/", true},
		{"http://localhost:8000", "http://127.0.0.1:8000", true},
		{"http://localhost:8000", "http://[::1]:8000", true},
		{"http://127.0.0.1", "http://localhost:80", true},
		{"https://baseline", "https://baseline:443", true},
		{"http://localhost:8000", "http://localhost:8001", false},
		{"http://baseline:8000", "http://candidate:8000", false},
		{"http://localhost:8000/a", "http://localhost:8000/b", false},
	}
	for _, tt := range tests {
		t.Run(tt.a+" "+tt.b, func(t *testing.T) {
			a, err := CheckTarget(tt.a, nil)
			if err != nil {
				t.Fatal(err)
			}
			b, err := CheckTarget(tt.b, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := a.SameBackend(b); got != tt.want {
				t.Fatalf("SameBackend = %v, want %v", got, tt.want)
			}
		})
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
