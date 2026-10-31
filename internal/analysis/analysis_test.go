package analysis

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Hanart-prog/ghosttraffic/internal/report"
)

var update = flag.Bool("update", false, "rewrite the golden report")

func options() Options {
	return Options{IgnoreJSONPaths: []string{"$.id"}, Thresholds: report.DefaultThresholds()}
}

func endpoint(t *testing.T, r report.Report, key string) report.Endpoint {
	t.Helper()
	for _, e := range r.Endpoints {
		if e.Key == key {
			return e
		}
	}
	t.Fatalf("no endpoint %q", key)
	return report.Endpoint{}
}

func TestBuildMatchesGolden(t *testing.T) {
	built, err := Build("testdata/run", options())
	if err != nil {
		t.Fatal(err)
	}
	got, err := report.Marshal(built)
	if err != nil {
		t.Fatal(err)
	}
	if *update {
		if err := os.WriteFile("testdata/report.golden.json", got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile("testdata/report.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("report changed; rerun with -update if intended\n%s", got)
	}
	if _, err := report.ParseReport(got); err != nil {
		t.Fatalf("report does not parse: %v", err)
	}
}

func TestBuildIsDeterministic(t *testing.T) {
	var outputs [][]byte
	for range 5 {
		built, err := Build("testdata/run", options())
		if err != nil {
			t.Fatal(err)
		}
		data, err := report.Marshal(built)
		if err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, data)
	}
	for _, output := range outputs[1:] {
		if !bytes.Equal(output, outputs[0]) {
			t.Fatal("reports differ between builds")
		}
	}
}

func TestVerdictsAndClasses(t *testing.T) {
	built, err := Build("testdata/run", options())
	if err != nil {
		t.Fatal(err)
	}
	if built.Verdict != report.VerdictConfirmed {
		t.Errorf("overall = %s", built.Verdict)
	}
	if got := endpoint(t, built, "GET /api/items/{id}").Verdict; got != report.VerdictConfirmed {
		t.Errorf("items = %s", got)
	}
	if got := endpoint(t, built, "GET /api/gone").Verdict; got != report.VerdictInsufficient {
		t.Errorf("gone = %s", got)
	}
	counts := map[string]report.Classification{}
	for _, c := range built.Comparisons {
		counts[c.Endpoint] = c.Classification
	}
	want := map[string]report.Classification{
		"GET /api/items/{id}": report.ClassMatch, // ids ignored
		"GET /api/gone":       report.ClassBothFailed,
		"GET /api/diff":       report.ClassBodyMismatch,
		"GET /api/nobody":     report.ClassTransportFailure,
	}
	for key, class := range want {
		if counts[key] != class {
			t.Errorf("%s = %s, want %s", key, counts[key], class)
		}
	}
	var sawPath bool
	for _, d := range built.Differences {
		if d.Classification == report.ClassBodyMismatch {
			sawPath = slices.Contains(d.Paths, "$.v")
		}
	}
	if !sawPath {
		t.Errorf("differences = %+v", built.Differences)
	}
}

func TestWithoutIgnoreIDsAreDifferences(t *testing.T) {
	opts := options()
	opts.IgnoreJSONPaths = nil
	built, err := Build("testdata/run", opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range built.Comparisons {
		if c.Endpoint == "GET /api/items/{id}" && c.Classification != report.ClassBodyMismatch {
			t.Fatalf("got %+v", c)
		}
	}
}

func copyRun(t *testing.T, edit func(run, results string) (string, string)) string {
	t.Helper()
	run, err := os.ReadFile("testdata/run/run.json")
	if err != nil {
		t.Fatal(err)
	}
	results, err := os.ReadFile("testdata/run/results.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	newRun, newResults := edit(string(run), string(results))
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "run.json"), []byte(newRun), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "results.jsonl"), []byte(newResults), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRunningRunIsRefused(t *testing.T) {
	dir := copyRun(t, func(run, results string) (string, string) {
		return strings.Replace(run, `"completed"`, `"running"`, 1), results
	})
	if _, err := Build(dir, options()); err == nil {
		t.Fatal("reported on a run with no end state")
	}
}

func TestPartialRunIsLabeled(t *testing.T) {
	dir := copyRun(t, func(run, results string) (string, string) {
		return strings.Replace(run, `"completed"`, `"cancelled"`, 1), results
	})
	built, err := Build(dir, options())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(built.Warnings, "\n"), "partial") {
		t.Fatalf("warnings = %v", built.Warnings)
	}
}

func TestSingleTrialNeverConfirms(t *testing.T) {
	dir := copyRun(t, func(run, results string) (string, string) {
		var kept []string
		for _, line := range strings.Split(results, "\n") {
			if strings.Contains(line, `"trial":1,`) {
				kept = append(kept, line)
			}
		}
		return run, strings.Join(kept, "\n") + "\n"
	})
	built, err := Build(dir, options())
	if err != nil {
		t.Fatal(err)
	}
	if built.Verdict != report.VerdictObserved {
		t.Fatalf("verdict = %s", built.Verdict)
	}
	if !strings.Contains(strings.Join(built.Warnings, "\n"), "cannot confirm") {
		t.Fatalf("warnings = %v", built.Warnings)
	}
}

func TestDisturbedTrialIsNamedAndDoesNotChangeTheVerdict(t *testing.T) {
	dir := copyRun(t, func(run, results string) (string, string) {
		var lines []string
		for _, line := range strings.Split(strings.TrimSpace(results), "\n") {
			if strings.Contains(line, `"trial":3,`) && strings.Contains(line, `"target":"baseline"`) && strings.Contains(line, "items/{id}") {
				line = strings.Replace(line, `"duration_ms":1`, `"duration_ms":90`, 1)
			}
			lines = append(lines, line)
		}
		return run, strings.Join(lines, "\n") + "\n"
	})
	built, err := Build(dir, options())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(built.Warnings, "\n"), "trial 3 on baseline") {
		t.Fatalf("warnings = %v", built.Warnings)
	}
	if got := endpoint(t, built, "GET /api/items/{id}").Verdict; got != report.VerdictObserved {
		t.Fatalf("verdict = %s", got)
	}
}

func TestSteadyRegressionIsNotCalledDisturbed(t *testing.T) {
	built, err := Build("testdata/run", options())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(built.Warnings, "\n"), "disturbed") {
		t.Fatalf("warnings = %v", built.Warnings)
	}
}

func TestTrialDifferencesCountEachTrial(t *testing.T) {
	built, err := Build("testdata/run", options())
	if err != nil {
		t.Fatal(err)
	}
	if len(built.TrialDifferences) != 5 {
		t.Fatalf("got %+v", built.TrialDifferences)
	}
	// One body mismatch and one timeout per trial; both_failed does not count.
	for _, d := range built.TrialDifferences {
		if d.Compared != 123 || d.Different != 2 {
			t.Fatalf("got %+v", d)
		}
	}
}

func TestMissingArtifactsAreErrors(t *testing.T) {
	if _, err := Build(t.TempDir(), options()); err == nil {
		t.Fatal("built a report from nothing")
	}
}

func TestHTMLEscapesRecordedValues(t *testing.T) {
	built, err := Build("testdata/run", options())
	if err != nil {
		t.Fatal(err)
	}
	built.Endpoints[0].Key = `GET /<script>alert(1)</script>`
	built.Warnings = append(built.Warnings, `<img src=x onerror=alert(1)>`)
	page, err := report.RenderHTML(built)
	if err != nil {
		t.Fatal(err)
	}
	for _, unsafe := range []string{"<script>alert", "<img src=x"} {
		if bytes.Contains(page, []byte(unsafe)) {
			t.Errorf("page contains %q", unsafe)
		}
	}
	for _, external := range []string{"http://cdn", "https://", "<link", "<script"} {
		if bytes.Contains(page, []byte(external)) {
			t.Errorf("page is not standalone: found %q", external)
		}
	}
}
