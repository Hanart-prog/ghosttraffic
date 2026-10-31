package metrics

import (
	"strings"
	"testing"
	"time"

	"github.com/Hanart-prog/ghosttraffic/internal/report"
)

func TestQuantileNearestRank(t *testing.T) {
	values := make([]float64, 100)
	for i := range values {
		values[i] = float64(i + 1)
	}
	for perMille, want := range map[int]float64{500: 50, 950: 95, 990: 99, 1000: 100, 1: 1} {
		if got := *Quantile(values, perMille); got != want {
			t.Errorf("Quantile(%d) = %v, want %v", perMille, got, want)
		}
	}
	if got := *Quantile([]float64{7}, 990); got != 7 {
		t.Errorf("single value = %v", got)
	}
	if Quantile(nil, 500) != nil {
		t.Error("empty input must give nil, never zero")
	}
}

func TestStatsCountsEveryOutcome(t *testing.T) {
	results := []Result{
		{DurationMS: 10, ErrorCategory: ErrorNone, Status: 200},
		{DurationMS: 20, ErrorCategory: ErrorNone, Status: 500},
		{DurationMS: 30000, ErrorCategory: ErrorTimeout},
		{ErrorCategory: ErrorConn},
		{ErrorCategory: ErrorDropped},
		{ErrorCategory: ErrorCancelled},
	}
	got := Stats(results, 2*time.Second)
	if got.Total != 6 || got.Successes != 1 || got.Errors != 5 || got.TransportErrors != 2 || got.Dropped != 2 {
		t.Errorf("counts = %+v", got)
	}
	if got.Latency.Samples != 3 {
		t.Errorf("samples = %d", got.Latency.Samples)
	}
	if got.ThroughputRPS != 1 {
		t.Errorf("throughput = %v", got.ThroughputRPS)
	}
}

// A timeout is the slowest outcome there is; dropping it would flatter the target.
func TestTimeoutsStayInTheTail(t *testing.T) {
	var results []Result
	for range 9 {
		results = append(results, Result{DurationMS: 1, ErrorCategory: ErrorNone, Status: 200})
	}
	results = append(results, Result{DurationMS: 30000, ErrorCategory: ErrorTimeout})
	if got := *Stats(results, time.Second).Latency.P99MS; got != 30000 {
		t.Fatalf("p99 = %v", got)
	}
}

// Same work, one target stalled: only response time exposes the stall.
func TestResponseTimeExposesQueueing(t *testing.T) {
	results := []Result{
		{DurationMS: 5, SchedulingDelayMS: 0, ErrorCategory: ErrorNone, Status: 200},
		{DurationMS: 5, SchedulingDelayMS: 200, ErrorCategory: ErrorNone, Status: 200},
	}
	got := Stats(results, time.Second)
	if *got.Latency.P99MS != 5 || *got.ResponseTime.P99MS != 205 || *got.SchedulingDelay.P99MS != 200 {
		t.Fatalf("got %+v", got)
	}
	warnings := Warnings("candidate", got)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "keep up") {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestStatsEmptyIsNullNotZero(t *testing.T) {
	got := Stats(nil, 0)
	if got.Latency.P50MS != nil || got.ThroughputRPS != 0 {
		t.Fatalf("got %+v", got)
	}
	if len(Warnings("x", got)) != 0 {
		t.Fatal("no data, nothing to warn about")
	}
}

func TestDropsAreWarned(t *testing.T) {
	results := []Result{{ErrorCategory: ErrorNone, Status: 200, DurationMS: 1}, {ErrorCategory: ErrorDropped}}
	warnings := Warnings("baseline", Stats(results, time.Second))
	if len(warnings) != 1 || !strings.Contains(warnings[0], "dropped") {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestDeadTargetHasNoThroughputAndIsWarned(t *testing.T) {
	results := []Result{{ErrorCategory: ErrorConn}, {ErrorCategory: ErrorConn}}
	got := Stats(results, time.Millisecond)
	if got.ThroughputRPS != 0 {
		t.Errorf("throughput = %v", got.ThroughputRPS)
	}
	warnings := Warnings("candidate", got)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "no request succeeded") {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestHistogramKeepsEverySample(t *testing.T) {
	results := []Result{
		{DurationMS: 0.5, ErrorCategory: ErrorNone},
		{DurationMS: 1, ErrorCategory: ErrorNone},
		{DurationMS: 7, ErrorCategory: ErrorNone},
		{DurationMS: 90000, ErrorCategory: ErrorNone},
	}
	var total int64
	buckets := Stats(results, time.Second).Histogram
	for _, b := range buckets {
		total += b.Count
	}
	if total != 4 {
		t.Fatalf("buckets hold %d of 4 samples", total)
	}
	if last := buckets[len(buckets)-1]; last.UpperMS != 90000 || last.Count != 1 {
		t.Fatalf("overflow bucket = %+v", last)
	}
}

func TestAssess(t *testing.T) {
	th := report.DefaultThresholds()
	n := th.MinSamples
	slow := TrialPair{BaselineMS: 10, CandidateMS: 40, BaselineSamples: n, CandidateSamples: n}
	same := TrialPair{BaselineMS: 10, CandidateMS: 10.5, BaselineSamples: n, CandidateSamples: n}
	small := TrialPair{BaselineMS: 10, CandidateMS: 500, BaselineSamples: n - 1, CandidateSamples: n}
	noisy := TrialPair{BaselineMS: 50, CandidateMS: 70, BaselineSamples: n, CandidateSamples: n}
	tiny := TrialPair{BaselineMS: 0.5, CandidateMS: 2, BaselineSamples: n, CandidateSamples: n}

	tests := []struct {
		name  string
		pairs []TrialPair
		want  report.Verdict
	}{
		{"no data", nil, report.VerdictInsufficient},
		{"huge effect, too few samples", []TrialPair{small}, report.VerdictInsufficient},
		{"one small trial spoils the set", []TrialPair{slow, slow, small, slow, slow}, report.VerdictInsufficient},
		{"no effect", []TrialPair{same, same, same}, report.VerdictNone},
		{"ratio without meaningful delta", []TrialPair{tiny}, report.VerdictNone},
		{"single trial is only observed", []TrialPair{slow}, report.VerdictObserved},
		{"four trials are only observed", []TrialPair{slow, slow, slow, slow}, report.VerdictObserved},
		{"one dissenting trial", []TrialPair{slow, slow, slow, slow, same}, report.VerdictObserved},
		{"five agreeing trials", []TrialPair{slow, slow, slow, slow, slow}, report.VerdictConfirmed},
		{"effect inside baseline noise", []TrialPair{slow, slow, slow, slow, noisy}, report.VerdictObserved},
		{"even split", []TrialPair{slow, same}, report.VerdictNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Assess(th, tt.pairs); got != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}
