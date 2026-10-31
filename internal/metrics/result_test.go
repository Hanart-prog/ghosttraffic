package metrics

import (
	"bufio"
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestResultGoldenRoundTrip(t *testing.T) {
	f, err := os.Open("testdata/results_v1.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	lines := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines++
		r, err := ParseResult(sc.Bytes())
		if err != nil {
			t.Fatalf("line %d: %v", lines, err)
		}
		got, err := MarshalResult(r)
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

func TestParseResultRejects(t *testing.T) {
	const valid = `{"schema_version":1,"trial":1,"request_id":"r","target":"baseline","endpoint_key":"GET /a","scheduled_offset_ms":0,"actual_start_offset_ms":0,"scheduling_delay_ms":0,"duration_ms":1,"status":200,"error_category":"none"}`
	if _, err := ParseResult([]byte(valid)); err != nil {
		t.Fatalf("baseline fixture must be valid: %v", err)
	}
	tests := []struct{ name, old, new, wantErr string }{
		{"version", `"schema_version":1`, `"schema_version":2`, "unsupported"},
		{"request id", `"request_id":"r"`, `"request_id":""`, "request_id"},
		{"target", `"baseline"`, `"staging"`, "invalid target"},
		{"category", `"none"`, `"oops"`, "invalid error_category"},
		{"trial", `"trial":1`, `"trial":0`, "trial"},
		{"trailing data", `"error_category":"none"}`, `"error_category":"none"}{}`, "invalid JSON"},
		{"unknown field", `"status":200`, `"status":200,"x":1`, "unknown field"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseResult([]byte(strings.Replace(valid, tt.old, tt.new, 1)))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("got error %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}
