package trace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func record(id string) Record {
	return Record{
		SchemaVersion: SchemaVersion, RequestID: id, Method: "GET", Path: "/a",
		Headers: map[string]string{}, Capture: Capture{Status: 200}, Replayable: true,
	}
}

func TestWriterRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "t.jsonl")
	w, err := Create(path)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := w.Write(record("r" + strings.Repeat("x", i))); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	count := 0
	if err := Read(f, func(Record) error { count++; return nil }); err != nil {
		t.Fatal(err)
	}
	if count != 100 {
		t.Fatalf("read %d records", count)
	}
}

func TestCreateRefusesToOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(path); !errors.Is(err, os.ErrExist) {
		t.Fatalf("got %v", err)
	}
}

func TestWriterRejectsInvalidRecords(t *testing.T) {
	w, err := Create(filepath.Join(t.TempDir(), "t.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	bad := record("r")
	bad.Headers = map[string]string{"authorization": "Bearer x"}
	if err := w.Write(bad); err == nil {
		t.Fatal("wrote a credential")
	}
}

func TestReadRejects(t *testing.T) {
	line, err := MarshalLine(record("r"))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ name, input, wantErr string }{
		{"duplicate id", string(line) + string(line), "line 2: duplicate"},
		{"partial last line", string(line) + `{"schema_version":1`, "line 2"},
		{"future version", `{"schema_version":9}`, "line 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Read(strings.NewReader(tt.input), func(Record) error { return nil })
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("got %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}
