package report

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestRunGolden(t *testing.T) {
	data := read(t, "testdata/run_v1.json")
	r, err := ParseRun(data)
	if err != nil {
		t.Fatal(err)
	}
	assertStable(t, r, data)
}

func TestReportGolden(t *testing.T) {
	data := read(t, "testdata/report_v1.json")
	r, err := ParseReport(data)
	if err != nil {
		t.Fatal(err)
	}
	assertStable(t, r, data)
}

func TestParseRejects(t *testing.T) {
	run := string(read(t, "testdata/run_v1.json"))
	if _, err := ParseRun([]byte(strings.Replace(run, `"schema_version": 1`, `"schema_version": 9`, 1))); err == nil {
		t.Error("unsupported run version accepted")
	}
	if _, err := ParseRun([]byte(strings.Replace(run, `"run_id"`, `"id"`, 1))); err == nil {
		t.Error("unknown run field accepted")
	}
	rep := string(read(t, "testdata/report_v1.json"))
	if _, err := ParseReport([]byte(strings.Replace(rep, `"schema_version": 1`, `"schema_version": 9`, 1))); err == nil {
		t.Error("unsupported report version accepted")
	}
	if _, err := ParseReport([]byte(strings.Replace(rep, `"summary"`, `"totals"`, 1))); err == nil {
		t.Error("unknown report field accepted")
	}
	if _, err := ParseReport(append(read(t, "testdata/report_v1.json"), []byte("{}")...)); err == nil {
		t.Error("trailing data accepted")
	}
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertStable(t *testing.T, v any, want []byte) {
	t.Helper()
	got, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("not byte-stable\n got: %s\nwant: %s", got, want)
	}
}
