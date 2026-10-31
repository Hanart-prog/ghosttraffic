package trace

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Hanart-prog/ghosttraffic/internal/schema"
)

func TestGoldenRoundTrip(t *testing.T) {
	f, err := os.Open("testdata/v1_valid.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	lines := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines++
		rec, err := ParseLine(sc.Bytes())
		if err != nil {
			t.Fatalf("line %d: %v", lines, err)
		}
		got, err := MarshalLine(rec)
		if err != nil {
			t.Fatalf("line %d: %v", lines, err)
		}
		if want := append(bytes.Clone(sc.Bytes()), '\n'); !bytes.Equal(got, want) {
			t.Errorf("line %d not byte-stable\n got: %s\nwant: %s", lines, got, want)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if lines == 0 {
		t.Fatal("golden file is empty")
	}
}

func TestParseLineRejects(t *testing.T) {
	const valid = `{"schema_version":1,"request_id":"req_1","session_id":"","arrival_offset_ms":0,"method":"GET","path":"/a","headers":{},"body_json":null,"capture":{"status":200,"duration_ms":1},"replayable":true}`
	if _, err := ParseLine([]byte(valid)); err != nil {
		t.Fatalf("baseline fixture must be valid: %v", err)
	}

	tests := []struct {
		name, old, new, wantErr string
	}{
		{"missing version", `"schema_version":1,`, ``, "schema_version is required"},
		{"empty id", `"req_1"`, `""`, "request_id"},
		{"negative offset", `"arrival_offset_ms":0`, `"arrival_offset_ms":-1`, "arrival_offset_ms"},
		{"bad method", `"GET"`, `"TRACE"`, "unsupported method"},
		{"lowercase method", `"GET"`, `"get"`, "unsupported method"},
		{"absolute url", `"/a"`, `"http://evil.example/a"`, "origin-form"},
		{"network path", `"/a"`, `"//evil.example/a"`, "origin-form"},
		{"crlf injection", `"/a"`, `"/a\r\nX: y"`, "control character"},
		{"authorization header", `"headers":{}`, `"headers":{"authorization":"Bearer x"}`, "must not be stored"},
		{"cookie header", `"headers":{}`, `"headers":{"COOKIE":"a=b"}`, "must not be stored"},
		{"host header", `"headers":{}`, `"headers":{"host":"evil.example"}`, "must not be stored"},
		{"forwarded for header", `"headers":{}`, `"headers":{"x-forwarded-for":"203.0.113.9"}`, "must not be stored"},
		{"hop-by-hop header", `"headers":{}`, `"headers":{"transfer-encoding":"chunked"}`, "must not be stored"},
		{"header injection", `"headers":{}`, `"headers":{"accept":"a\r\nX: y"}`, "control character"},
		{"status range", `"status":200`, `"status":700`, "capture.status"},
		{"negative duration", `"duration_ms":1`, `"duration_ms":-1`, "capture.duration_ms"},
		{"truncated but replayable", `"replayable":true`, `"body_truncated":true,"replayable":true`, "truncated"},
		{"unreplayable without reason", `"replayable":true`, `"replayable":false`, "unreplayable_reason"},
		{"unknown field", `"replayable":true`, `"replayable":true,"extra":1`, "unknown field"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := strings.Replace(valid, tt.old, tt.new, 1)
			if line == valid {
				t.Fatalf("test did not alter the fixture")
			}
			_, err := ParseLine([]byte(line))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("got error %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestParseLineUnsupportedVersion(t *testing.T) {
	// A future schema may add fields; the version must be reported first.
	_, err := ParseLine([]byte(`{"schema_version":2,"new_field":true}`))
	if !errors.Is(err, schema.ErrUnsupportedVersion) {
		t.Fatalf("got %v, want schema.ErrUnsupportedVersion", err)
	}
}

func TestParseLineMalformedJSON(t *testing.T) {
	if _, err := ParseLine([]byte(`{"schema_version":1`)); err == nil {
		t.Fatal("expected an error")
	}
}
