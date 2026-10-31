// Package analysis turns a run directory into a report. Every number comes from
// results.jsonl, and the same inputs always give the same report.
package analysis

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/Hanart-prog/ghosttraffic/internal/compare"
	"github.com/Hanart-prog/ghosttraffic/internal/metrics"
	"github.com/Hanart-prog/ghosttraffic/internal/report"
)

const (
	// maxExamples bounds evidence per endpoint and classification.
	maxExamples = 5
	// disturbedFactor flags a trial this many times above its target's median.
	disturbedFactor = 3
)

// Options configures Build.
type Options struct {
	IgnoreJSONPaths []string
	Thresholds      report.Thresholds
}

type attempt struct {
	trial int
	id    string
}

type buckets map[string]map[string][]metrics.Result // endpoint -> target -> results

// Build reads run.json and results.jsonl under dir.
func Build(dir string, opts Options) (report.Report, error) {
	data, err := os.ReadFile(filepath.Join(dir, "run.json"))
	if err != nil {
		return report.Report{}, err
	}
	run, err := report.ParseRun(data)
	if err != nil {
		return report.Report{}, err
	}
	if run.Status == report.RunRunning {
		return report.Report{}, fmt.Errorf("run %s is still running or crashed: no end state recorded", run.RunID)
	}
	canonicalizer, err := compare.NewCanonicalizer(opts.IgnoreJSONPaths)
	if err != nil {
		return report.Report{}, err
	}
	file, err := os.Open(filepath.Join(dir, "results.jsonl"))
	if err != nil {
		return report.Report{}, err
	}
	defer file.Close()

	byEndpoint := buckets{}
	all := map[string][]metrics.Result{}
	pairs := map[attempt]map[string]metrics.Result{}
	trials := 0
	err = metrics.ReadResults(file, func(r metrics.Result) error {
		if byEndpoint[r.EndpointKey] == nil {
			byEndpoint[r.EndpointKey] = map[string][]metrics.Result{}
		}
		byEndpoint[r.EndpointKey][r.Target] = append(byEndpoint[r.EndpointKey][r.Target], r)
		all[r.Target] = append(all[r.Target], r)
		key := attempt{r.Trial, r.RequestID}
		if pairs[key] == nil {
			pairs[key] = map[string]metrics.Result{}
		}
		pairs[key][r.Target] = r
		trials = max(trials, r.Trial)
		return nil
	})
	if err != nil {
		return report.Report{}, fmt.Errorf("read results: %w", err)
	}

	elapsed := map[string]time.Duration{}
	for _, pass := range run.Passes {
		elapsed[pass.Target] += time.Duration(pass.ElapsedMS * float64(time.Millisecond))
	}

	endpoints := make([]report.Endpoint, 0, len(byEndpoint))
	verdict := report.VerdictNone
	warnings := slices.Clone(run.Warnings)
	for key, targets := range byEndpoint {
		endpoint := buildEndpoint(key, targets, elapsed, opts.Thresholds, trials)
		endpoints = append(endpoints, endpoint)
		if endpoint.Verdict.Severity() > verdict.Severity() {
			verdict = endpoint.Verdict
		}
		warnings = append(warnings, disturbedTrials(endpoint, opts.Thresholds)...)
		if endpoint.Verdict == report.VerdictInsufficient {
			warnings = append(warnings, fmt.Sprintf("%s: fewer than %d samples in a trial, so no latency judgment was made", key, opts.Thresholds.MinSamples))
		}
	}
	slices.SortFunc(endpoints, func(a, b report.Endpoint) int { return cmp.Compare(a.Key, b.Key) })
	if len(endpoints) == 0 {
		verdict = report.VerdictInsufficient
	}

	comparisons, differences, perTrial, err := comparePairs(pairs, canonicalizer)
	if err != nil {
		return report.Report{}, err
	}
	warnings = append(warnings, compare.Warnings(comparisons)...)
	if trials == 1 {
		warnings = append(warnings, "one trial: a single run can show a difference but cannot confirm one")
	}
	if run.Status != report.RunCompleted {
		warnings = append(warnings, fmt.Sprintf("run ended as %s, so these results are partial", run.Status))
	}
	slices.Sort(warnings)
	warnings = slices.Compact(warnings)

	ignored := slices.Clone(opts.IgnoreJSONPaths)
	if ignored == nil {
		ignored = []string{}
	}
	return report.Report{
		SchemaVersion: report.SchemaVersion,
		RunID:         run.RunID,
		Provenance: report.Provenance{
			TraceSHA256: run.TraceSHA256, Baseline: run.Baseline, Candidate: run.Candidate,
			BaselineSeed: run.BaselineSeed, CandidateSeed: run.CandidateSeed,
			StartedAt: run.StartedAt, EndedAt: run.EndedAt, Status: run.Status,
			Build: run.Build, Config: run.Config, Trials: trials,
		},
		Verdict:          verdict,
		Thresholds:       opts.Thresholds,
		IgnoredJSONPaths: ignored,
		Summary: report.Summary{
			Baseline:  metrics.Stats(all["baseline"], elapsed["baseline"]),
			Candidate: metrics.Stats(all["candidate"], elapsed["candidate"]),
		},
		Endpoints:        endpoints,
		Comparisons:      comparisons,
		Differences:      differences,
		TrialDifferences: perTrial,
		Warnings:         warnings,
	}, nil
}

func byTrial(results []metrics.Result) map[int][]metrics.Result {
	out := map[int][]metrics.Result{}
	for _, r := range results {
		out[r.Trial] = append(out[r.Trial], r)
	}
	return out
}

func buildEndpoint(key string, targets map[string][]metrics.Result, elapsed map[string]time.Duration, thresholds report.Thresholds, trials int) report.Endpoint {
	baseline, candidate := byTrial(targets["baseline"]), byTrial(targets["candidate"])
	endpoint := report.Endpoint{
		Key:       key,
		Baseline:  metrics.Stats(targets["baseline"], elapsed["baseline"]),
		Candidate: metrics.Stats(targets["candidate"], elapsed["candidate"]),
		Trials:    []report.TrialComparison{},
	}
	var pairs []metrics.TrialPair
	complete := true
	for trial := 1; trial <= trials; trial++ {
		a := metrics.Stats(baseline[trial], 0).ResponseTime
		b := metrics.Stats(candidate[trial], 0).ResponseTime
		endpoint.Trials = append(endpoint.Trials, report.TrialComparison{
			Trial: trial, BaselineP95MS: a.P95MS, CandidateP95MS: b.P95MS,
			BaselineSamples: a.Samples, CandidateSamples: b.Samples,
		})
		if a.P95MS == nil || b.P95MS == nil {
			complete = false
			continue
		}
		pairs = append(pairs, metrics.TrialPair{
			BaselineMS: *a.P95MS, CandidateMS: *b.P95MS,
			BaselineSamples: a.Samples, CandidateSamples: b.Samples,
		})
	}
	if !complete {
		pairs = nil
	}
	endpoint.Verdict = metrics.Assess(thresholds, pairs)
	return endpoint
}

// disturbedTrials names trials far from their own target's typical value. Each
// target is held against itself, so a regression affecting every trial is not
// flagged, while a brief stall on one trial is.
func disturbedTrials(e report.Endpoint, t report.Thresholds) []string {
	var warnings []string
	series := map[string]func(report.TrialComparison) *float64{
		"baseline":  func(c report.TrialComparison) *float64 { return c.BaselineP95MS },
		"candidate": func(c report.TrialComparison) *float64 { return c.CandidateP95MS },
	}
	for name, value := range series {
		var values []float64
		for _, trial := range e.Trials {
			if v := value(trial); v != nil {
				values = append(values, *v)
			}
		}
		if len(values) < 3 {
			continue
		}
		slices.Sort(values)
		median := values[len(values)/2]
		for _, trial := range e.Trials {
			if v := value(trial); v != nil && *v > median*disturbedFactor && *v-median >= t.MinDeltaMS {
				warnings = append(warnings, fmt.Sprintf("%s: trial %d on %s had p95 %.1f ms against its typical %.1f ms; something disturbed that trial", e.Key, trial.Trial, name, *v, median))
			}
		}
	}
	return warnings
}

// comparePairs classifies every request present for both targets, and keeps a
// bounded, deterministic sample of the differences.
func comparePairs(pairs map[attempt]map[string]metrics.Result, c *compare.Canonicalizer) ([]report.ComparisonStat, []report.Difference, []report.TrialDifference, error) {
	keys := make([]attempt, 0, len(pairs))
	for key := range pairs {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b attempt) int {
		return cmp.Or(cmp.Compare(a.trial, b.trial), cmp.Compare(a.id, b.id))
	})

	type group struct {
		endpoint string
		class    report.Classification
	}
	counts := map[group]int64{}
	differences := []report.Difference{}
	perTrial := []report.TrialDifference{}
	for _, key := range keys {
		if len(perTrial) < key.trial {
			perTrial = append(perTrial, report.TrialDifference{Trial: key.trial})
		}
		baseline, okA := pairs[key]["baseline"]
		candidate, okB := pairs[key]["candidate"]
		var class report.Classification
		var paths []string
		if !okA || !okB {
			class = report.ClassNotComparable
			if !okA {
				baseline = candidate
			}
		} else {
			var err error
			class, paths, err = Classify(baseline, candidate, c)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("%s trial %d: %w", key.id, key.trial, err)
			}
		}
		perTrial[key.trial-1].Compared++
		if class != report.ClassMatch && class != report.ClassBothFailed && class != report.ClassBodyUnavailable {
			perTrial[key.trial-1].Different++
		}
		g := group{baseline.EndpointKey, class}
		counts[g]++
		if class != report.ClassMatch && counts[g] <= maxExamples {
			differences = append(differences, report.Difference{
				Endpoint: baseline.EndpointKey, RequestID: key.id, Trial: key.trial, Classification: class,
				BaselineStatus: baseline.Status, CandidateStatus: candidate.Status, Paths: paths,
			})
		}
	}
	stats := make([]report.ComparisonStat, 0, len(counts))
	for g, count := range counts {
		stats = append(stats, report.ComparisonStat{Endpoint: g.endpoint, Classification: g.class, Count: count})
	}
	slices.SortFunc(stats, func(a, b report.ComparisonStat) int {
		return cmp.Or(cmp.Compare(a.Endpoint, b.Endpoint), cmp.Compare(a.Classification, b.Classification))
	})
	return stats, differences, perTrial, nil
}

// Classify compares canonical bodies, not raw digests: only stored bodies can
// have ignore paths applied, so a run without them reads body_unavailable.
func Classify(baseline, candidate metrics.Result, c *compare.Canonicalizer) (report.Classification, []string, error) {
	var err error
	if baseline.ResponseDigest, err = c.Digest(baseline.ResponseJSON); err != nil {
		return "", nil, err
	}
	if candidate.ResponseDigest, err = c.Digest(candidate.ResponseJSON); err != nil {
		return "", nil, err
	}
	class := compare.Classify(baseline, candidate)
	paths := []string{}
	if class == report.ClassBodyMismatch {
		if paths, err = c.Diff(baseline.ResponseJSON, candidate.ResponseJSON); err != nil {
			return "", nil, err
		}
	}
	return class, paths, nil
}
