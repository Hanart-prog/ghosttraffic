package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

// Valid invocations of unbuilt commands must fail visibly, not pretend to work.
func TestUnbuiltCommandsExitNonzero(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"Phase 1", []string{"capture", "--upstream", "http://localhost:8000", "--out", "t"}},
		{"Phase 2", []string{"replay", "--trace", "t", "--out", "r", "--baseline", "http://localhost:8000", "--candidate", "http://localhost:8001"}},
		{"Phase 3", []string{"report", "--run", "r"}},
		{"Phase 5", []string{"serve"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, errOut := exec(tt.args...)
			if code != exitFailure {
				t.Errorf("code = %d, want %d", code, exitFailure)
			}
			if !strings.Contains(errOut, "not implemented yet (planned for "+tt.name+")") {
				t.Errorf("stderr = %q", errOut)
			}
		})
	}
}

func TestAllowHostWarnsOnExternalTarget(t *testing.T) {
	code, _, errOut := exec("replay", "--trace", "t", "--out", "r",
		"--baseline", "http://localhost:8000", "--candidate", "http://staging.example.com",
		"--allow-host", "staging.example.com")
	if code != exitFailure {
		t.Errorf("code = %d, want %d (validation passes, engine unbuilt)", code, exitFailure)
	}
	if !strings.Contains(errOut, "WARNING") || !strings.Contains(errOut, "staging.example.com") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestCaptureWarnsOnExternalUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(path, []byte("replay:\n  allow_hosts: [staging.example.com]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := exec("capture", "--out", "t", "--config", path, "--upstream", "http://staging.example.com")
	if code != exitFailure || !strings.Contains(errOut, "WARNING") {
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
