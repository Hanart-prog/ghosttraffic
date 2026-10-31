package schema

import (
	"errors"
	"strings"
	"testing"
)

type doc struct {
	SchemaVersion int    `json:"schema_version"`
	Name          string `json:"name"`
}

func TestDecode(t *testing.T) {
	var d doc
	if err := Decode([]byte(`{"schema_version":1,"name":"a"}`), 1, &d); err != nil {
		t.Fatal(err)
	}
	if d.Name != "a" {
		t.Fatalf("name = %q", d.Name)
	}
}

func TestDecodeRejects(t *testing.T) {
	tests := []struct{ name, input, wantErr string }{
		{"malformed", `{"schema_version":1`, "invalid JSON"},
		{"missing version", `{"name":"a"}`, "schema_version is required"},
		{"unknown field", `{"schema_version":1,"x":1}`, "unknown field"},
		{"trailing object", `{"schema_version":1}{"schema_version":1}`, "invalid JSON"},
		{"trailing garbage", `{"schema_version":1} x`, "invalid JSON"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Decode([]byte(tt.input), 1, &doc{})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("got error %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

// A future version may add fields; the version must be reported before them.
func TestDecodeReportsVersionBeforeUnknownFields(t *testing.T) {
	err := Decode([]byte(`{"schema_version":2,"new_field":true}`), 1, &doc{})
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("got %v, want ErrUnsupportedVersion", err)
	}
}

func TestMarshalLine(t *testing.T) {
	got, err := MarshalLine(doc{SchemaVersion: 1, Name: "a&b"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"schema_version":1,"name":"a&b"}` + "\n"; string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
