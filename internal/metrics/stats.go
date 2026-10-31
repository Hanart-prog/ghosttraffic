package metrics

import (
	"slices"
	"time"

	"github.com/Hanart-prog/ghosttraffic/internal/report"
)

// histogramBounds are fixed so histograms from any two runs line up.
var histogramBounds = []float64{1, 2, 5, 10, 20, 50, 100, 200, 500, 1000, 2000, 5000, 10000, 30000}

// Quantile returns the exact nearest-rank value for perMille (950 is p95) from
// sorted values, or nil when empty. Integer arithmetic keeps the rank exact.
func Quantile(sorted []float64, perMille int) *float64 {
	if len(sorted) == 0 {
		return nil
	}
	rank := (len(sorted)*perMille + 999) / 1000
	value := sorted[max(rank, 1)-1]
	return &value
}

func latency(values []float64) report.Latency {
	slices.Sort(values)
	return report.Latency{
		Samples: int64(len(values)),
		P50MS:   Quantile(values, 500),
		P95MS:   Quantile(values, 950),
		P99MS:   Quantile(values, 990),
	}
}

func histogram(sorted []float64) []report.Bucket {
	buckets := make([]report.Bucket, 0, len(histogramBounds)+1)
	next := 0
	for _, upper := range histogramBounds {
		start := next
		for next < len(sorted) && sorted[next] <= upper {
			next++
		}
		buckets = append(buckets, report.Bucket{UpperMS: upper, Count: int64(next - start)})
	}
	if next < len(sorted) {
		buckets = append(buckets, report.Bucket{UpperMS: sorted[len(sorted)-1], Count: int64(len(sorted) - next)})
	}
	return buckets
}

// Stats aggregates attempts against one target over wall-clock elapsed time.
// Throughput counts completed HTTP exchanges only: a refused connection fails
// instantly, so counting failures would give a dead target a high rate.
//
// Timeouts stay in the latency samples at their timeout value: removing them
// would erase the slowest requests from the distribution. Drops and
// cancellations have no latency, so they are counted and left to warnings.
func Stats(results []Result, elapsed time.Duration) report.TargetStats {
	var stats report.TargetStats
	var service, response, delay []float64
	var completed int64
	for _, r := range results {
		stats.Total++
		if r.ErrorCategory == ErrorNone {
			completed++
		}
		switch r.ErrorCategory {
		case ErrorNone:
		case ErrorDropped, ErrorCancelled:
			stats.Dropped++
			stats.Errors++
			continue
		default:
			stats.TransportErrors++
		}
		if r.ErrorCategory == ErrorNone && r.Status < 400 {
			stats.Successes++
		} else {
			stats.Errors++
		}
		if r.ErrorCategory == ErrorNone || r.ErrorCategory == ErrorTimeout {
			service = append(service, r.DurationMS)
			response = append(response, r.SchedulingDelayMS+r.DurationMS)
			delay = append(delay, r.SchedulingDelayMS)
		}
	}
	stats.Latency = latency(service)
	stats.ResponseTime = latency(response)
	stats.SchedulingDelay = latency(delay)
	stats.Histogram = histogram(response)
	if elapsed > 0 {
		stats.ThroughputRPS = float64(completed) / elapsed.Seconds()
	}
	return stats
}

// TrialPair is one trial's summary latency and sample counts for an endpoint.
type TrialPair struct {
	BaselineMS, CandidateMS           float64
	BaselineSamples, CandidateSamples int64
}

// confirmTrials is the fewest paired trials that can reach one-sided sign-test
// significance: all 5 agreeing has p = 1/32, about 0.03.
const confirmTrials = 5

func (p TrialPair) exceeds(t report.Thresholds) bool {
	return p.CandidateMS >= p.BaselineMS*t.MinRatio && p.CandidateMS-p.BaselineMS >= t.MinDeltaMS
}

// Assess turns paired trials into a verdict without overclaiming:
//
//   - any trial under MinSamples gives insufficient_samples
//   - a minority of trials over the thresholds gives no_regression
//   - a majority gives observed_regression
//   - confirmed_regression needs at least confirmTrials pairs, all over the
//     thresholds, with every candidate value above every baseline value, so
//     the effect exceeds the baseline's own trial-to-trial noise
func Assess(t report.Thresholds, pairs []TrialPair) report.Verdict {
	if len(pairs) == 0 {
		return report.VerdictInsufficient
	}
	exceeded := 0
	maxBaseline, minCandidate := pairs[0].BaselineMS, pairs[0].CandidateMS
	for _, p := range pairs {
		if p.BaselineSamples < t.MinSamples || p.CandidateSamples < t.MinSamples {
			return report.VerdictInsufficient
		}
		if p.exceeds(t) {
			exceeded++
		}
		maxBaseline = max(maxBaseline, p.BaselineMS)
		minCandidate = min(minCandidate, p.CandidateMS)
	}
	switch {
	case exceeded*2 <= len(pairs):
		return report.VerdictNone
	case len(pairs) >= confirmTrials && exceeded == len(pairs) && minCandidate > maxBaseline:
		return report.VerdictConfirmed
	}
	return report.VerdictObserved
}

// Warnings describes why stats may mislead. A saturated replayer or a slow
// target can both cause delay, so the wording does not pick one.
func Warnings(name string, stats report.TargetStats) []string {
	warnings := []string{}
	if stats.Total > 0 && stats.Successes == 0 {
		warnings = append(warnings, name+": no request succeeded; this target's numbers describe a failure, not its performance")
	}
	if stats.Dropped > 0 {
		warnings = append(warnings, name+": some requests were dropped or cancelled and have no latency; quantiles understate the load actually offered")
	}
	if d := stats.SchedulingDelay.P95MS; d != nil && *d > 0 {
		if p95 := stats.Latency.P95MS; p95 != nil && *d >= *p95 {
			warnings = append(warnings, name+": p95 scheduling delay is at least p95 service time; the replayer or the target could not keep up, so compare response_time rather than latency")
		}
	}
	return warnings
}
