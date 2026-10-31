package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func exec(args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = run(context.Background(), args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestTopLevel(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantOut  string
		wantErr  string
	}{
		{name: "no args", wantCode: exitUsage, wantErr: "Commands:"},
		{name: "help", args: []string{"help"}, wantOut: "Commands:"},
		{name: "--help", args: []string{"--help"}, wantOut: "Commands:"},
		{name: "unknown", args: []string{"nope"}, wantCode: exitUsage, wantErr: `unknown command "nope"`},
		{name: "version", args: []string{"version"}, wantOut: "ghosttraffic dev"},
		{name: "version extra arg", args: []string{"version", "x"}, wantCode: exitUsage, wantErr: "unexpected argument"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, out, errOut := exec(tt.args...)
			if code != tt.wantCode {
				t.Errorf("code = %d, want %d", code, tt.wantCode)
			}
			if !strings.Contains(out, tt.wantOut) {
				t.Errorf("stdout = %q, want containing %q", out, tt.wantOut)
			}
			if !strings.Contains(errOut, tt.wantErr) {
				t.Errorf("stderr = %q, want containing %q", errOut, tt.wantErr)
			}
		})
	}
}

func TestSubcommandHelp(t *testing.T) {
	for name := range commands {
		t.Run(name, func(t *testing.T) {
			code, out, errOut := exec(name, "--help")
			if code != exitOK {
				t.Fatalf("code = %d, stderr = %q", code, errOut)
			}
			if !strings.Contains(out, "Usage: ghosttraffic "+name) {
				t.Fatalf("stdout = %q", out)
			}
			if errOut != "" {
				t.Fatalf("help wrote to stderr: %q", errOut)
			}
		})
	}
}

func TestValidation(t *testing.T) {
	goodReplay := []string{"replay", "--trace", "t.jsonl", "--baseline", "http://localhost:8000", "--candidate", "http://localhost:8001", "--out", "r"}
	goodCapture := []string{"capture", "--upstream", "http://localhost:8000", "--out", "t.jsonl"}

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"capture no upstream", []string{"capture", "--out", "t"}, "--upstream is required"},
		{"capture no out", []string{"capture", "--upstream", "http://localhost:1"}, "--out is required"},
		{"capture bad listen", append(goodCapture, "--listen", "9000"), "invalid --listen"},
		{"capture remote upstream", []string{"capture", "--upstream", "http://example.com", "--out", "t"}, "not local"},
		{"capture missing config", append(goodCapture, "--config", "/does/not/exist"), "read config"},
		{"replay no trace", []string{"replay", "--out", "r"}, "--trace is required"},
		{"replay no out", []string{"replay", "--trace", "t"}, "--out is required"},
		{"replay no baseline", []string{"replay", "--trace", "t", "--out", "r", "--candidate", "http://localhost:1"}, "--baseline is required"},
		{"replay zero concurrency", append(goodReplay, "--concurrency", "0"), "--concurrency"},
		{"replay huge concurrency", append(goodReplay, "--concurrency", "10001"), "--concurrency"},
		{"replay zero speed", append(goodReplay, "--speed", "0"), "--speed"},
		{"replay negative speed", append(goodReplay, "--speed", "-1"), "--speed"},
		{"replay NaN speed", append(goodReplay, "--speed", "NaN"), "--speed"},
		{"replay same target", []string{"replay", "--trace", "t", "--out", "r", "--baseline", "http://localhost:1", "--candidate", "http://localhost:1"}, "different backends"},
		{"replay same target respelled", []string{"replay", "--trace", "t", "--out", "r", "--baseline", "http://localhost:1", "--candidate", "http://127.0.0.1:1/"}, "different backends"},
		{"replay no targets", []string{"replay", "--trace", "t", "--out", "r"}, "--baseline is required"},
		{"replay remote target", []string{"replay", "--trace", "t", "--out", "r", "--baseline", "http://localhost:1", "--candidate", "http://prod.example.com"}, "not local"},
		{"replay zero trials", append(goodReplay, "--trials", "0"), "--trials"},
		{"report no run", []string{"report"}, "--run is required"},
		{"report bad format", []string{"report", "--run", "r", "--format", "pdf"}, "unknown format"},
		{"serve bad listen", []string{"serve", "--listen", "nope"}, "invalid --listen"},
		{"serve empty dir", []string{"serve", "--runs-dir", ""}, "--runs-dir"},
		{"unknown flag", []string{"replay", "--bogus"}, "bogus"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, errOut := exec(tt.args...)
			if code != exitUsage {
				t.Errorf("code = %d, want %d", code, exitUsage)
			}
			if !strings.Contains(errOut, tt.wantErr) {
				t.Errorf("stderr = %q, want containing %q", errOut, tt.wantErr)
			}
		})
	}
}

func TestServeStopsWhenCancelled(t *testing.T) {
	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	var stderr bytes.Buffer
	go func() {
		done <- run(ctx, []string{"serve", "--listen", addr, "--runs-dir", t.TempDir()}, io.Discard, &stderr)
	}()
	var resp *http.Response
	for range 200 {
		var err error
		if resp, err = http.Get("http://" + addr + "/health"); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if resp == nil {
		t.Fatal("server never came up")
	}
	resp.Body.Close()
	cancel()
	if code := <-done; code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
}

func TestAllowHostWarnsOnExternalTarget(t *testing.T) {
	code, _, errOut := exec("replay", "--trace", "t", "--out", "r",
		"--baseline", "http://localhost:8000", "--candidate", "http://staging.example.com",
		"--allow-host", "staging.example.com")
	if code != exitFailure {
		t.Errorf("code = %d, want %d (validation passes, trace missing)", code, exitFailure)
	}
	if !strings.Contains(errOut, "WARNING") || !strings.Contains(errOut, "staging.example.com") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestCaptureWarnsOnExternalUpstream(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(path, []byte("replay:\n  allow_hosts: [staging.example.com]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errOut bytes.Buffer
	code := run(ctx, []string{"capture", "--listen", "127.0.0.1:0", "--out", filepath.Join(dir, "t.jsonl"), "--config", path,
		"--upstream", "http://staging.example.com"}, &out, &errOut)
	if code != exitOK || !strings.Contains(errOut.String(), "WARNING") {
		t.Errorf("code = %d, stderr = %q", code, errOut.String())
	}
}

func freeAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().String()
}

func TestCaptureRecordsUntilCancelled(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer upstream.Close()
	out := filepath.Join(t.TempDir(), "t.jsonl")
	addr := freeAddr(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	var stderr bytes.Buffer
	go func() {
		done <- run(ctx, []string{"capture", "--listen", addr, "--upstream", upstream.URL, "--out", out}, io.Discard, &stderr)
	}()

	var resp *http.Response
	for range 200 {
		var err error
		if resp, err = http.Get("http://" + addr + "/api/a?b=1"); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if resp == nil {
		t.Fatal("proxy never came up")
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status = %d", resp.StatusCode)
	}
	cancel()
	if code := <-done; code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "recorded 1 replayable") {
		t.Errorf("stderr = %q", stderr.String())
	}

	code, stdout, errOut := exec("validate", "--trace", out)
	if code != exitOK || !strings.Contains(stdout, "1 records valid, 1 replayable") {
		t.Errorf("code = %d, stdout = %q, stderr = %q", code, stdout, errOut)
	}
}

func TestCaptureRefusesExistingTrace(t *testing.T) {
	out := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(out, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := exec("capture", "--listen", "127.0.0.1:0", "--upstream", "http://localhost:1", "--out", out)
	if code != exitFailure || !strings.Contains(errOut, "exists") {
		t.Errorf("code = %d, stderr = %q", code, errOut)
	}
}

func TestValidateRejectsBadTraces(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(bad, []byte("{\"schema_version\":9}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"validate", "--trace", bad}, {"validate", "--trace", bad + ".missing"}} {
		if code, _, _ := exec(args...); code != exitFailure {
			t.Errorf("%v: code = %d", args, code)
		}
	}
	if code, _, errOut := exec("validate"); code != exitUsage || !strings.Contains(errOut, "--trace") {
		t.Errorf("code = %d, stderr = %q", code, errOut)
	}
}

func TestConfigAllowHostsAreHonored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(path, []byte("replay:\n  allow_hosts: [staging.example.com]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := exec("replay", "--trace", "t", "--out", "r", "--config", path,
		"--baseline", "http://localhost:8000", "--candidate", "http://staging.example.com")
	if code != exitFailure || !strings.Contains(errOut, "WARNING") {
		t.Errorf("code = %d, stderr = %q", code, errOut)
	}
}
