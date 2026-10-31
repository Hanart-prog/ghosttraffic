// Package report defines the persisted run manifest (run.json) and report
// (report.json) contracts. Struct field order fixes JSON key order, and slices
// rather than maps keep the output deterministic.
package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Hanart-prog/ghosttraffic/internal/schema"
)

// SchemaVersion is shared by run.json and report.json.
const SchemaVersion = 1

// RunStatus is the lifecycle state recorded in run.json.
type RunStatus string

const (
	RunRunning   RunStatus = "running"
	RunCompleted RunStatus = "completed"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
)

// Classification is the outcome of comparing one request across targets.
type Classification string

const (
	ClassMatch            Classification = "match"
	ClassStatusMismatch   Classification = "status_mismatch"
	ClassBodyMismatch     Classification = "body_mismatch"
	ClassTransportFailure Classification = "transport_failure"
	ClassNotComparable    Classification = "not_comparable"
	// ClassBothFailed is equal failure on both targets. It is kept apart from a
	// match because it is usually a broken replay, not agreement (ADR 0003).
	ClassBothFailed Classification = "both_failed"
	// ClassBodyUnavailable means statuses agree but a body was not captured.
	ClassBodyUnavailable Classification = "body_unavailable"
)

// Verdict is a latency judgment, worded to claim no more than the data supports.
type Verdict string

const (
	VerdictInsufficient Verdict = "insufficient_samples"
	VerdictNone         Verdict = "no_regression"
	VerdictObserved     Verdict = "observed_regression"
	VerdictConfirmed    Verdict = "confirmed_regression"
)

// Thresholds label what a verdict means. A regression needs both the ratio and
// the delta, so sub-millisecond jitter cannot register as a large ratio.
type Thresholds struct {
	MinSamples int64   `json:"min_samples"`
	MinRatio   float64 `json:"min_ratio"`
	MinDeltaMS float64 `json:"min_delta_ms"`
}

// DefaultThresholds requires 100 samples per trial, the fewest for which p99 is
// not simply the maximum value.
func DefaultThresholds() Thresholds {
	return Thresholds{MinSamples: 100, MinRatio: 1.2, MinDeltaMS: 5}
}

// Run is run.json. Target addresses carry no credentials. Seed digests are
// empty when a target does not report one: equal state is then unknown.
type Run struct {
	SchemaVersion int             `json:"schema_version"`
	RunID         string          `json:"run_id"`
	TraceSHA256   string          `json:"trace_sha256"`
	Config        json.RawMessage `json:"config"`
	Baseline      string          `json:"baseline"`
	Candidate     string          `json:"candidate"`
	BaselineSeed  string          `json:"baseline_seed,omitempty"`
	CandidateSeed string          `json:"candidate_seed,omitempty"`
	Requests      RequestCounts   `json:"requests"`
	StartedAt     time.Time       `json:"started_at"`
	EndedAt       *time.Time      `json:"ended_at"`
	Build         Build           `json:"build"`
	Status        RunStatus       `json:"status"`
	Passes        []Pass          `json:"passes"`
	Warnings      []string        `json:"warnings"`
}

// RequestCounts accounts for every record in the trace. Skipped records were
// unreplayable and appear in no target's results.
type RequestCounts struct {
	Total   int64 `json:"total"`
	Sent    int64 `json:"sent"`
	Skipped int64 `json:"skipped"`
}

// Pass is one target's replay. ElapsedMS is the wall-clock basis for throughput.
type Pass struct {
	Target    string  `json:"target"`
	Trial     int     `json:"trial"`
	ElapsedMS float64 `json:"elapsed_ms"`
}

// Build identifies the binary that produced a run.
type Build struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

// Report is report.json. Every number is derived from results.jsonl.
type Report struct {
	SchemaVersion int              `json:"schema_version"`
	RunID         string           `json:"run_id"`
	Thresholds    Thresholds       `json:"thresholds"`
	Summary       Summary          `json:"summary"`
	Endpoints     []Endpoint       `json:"endpoints"`
	Comparisons   []ComparisonStat `json:"comparisons"`
	Warnings      []string         `json:"warnings"`
}

// Summary holds whole-run aggregates per target.
type Summary struct {
	Baseline  TargetStats `json:"baseline"`
	Candidate TargetStats `json:"candidate"`
}

// TargetStats is the aggregate for one target over one scope. ThroughputRPS is
// completed HTTP exchanges divided by wall-clock elapsed time. Latency is service time
// and ResponseTime adds scheduling delay; both are reported because quantiles
// cannot be recombined afterwards (ADR 0004). SchedulingDelay is per target so a
// saturated replayer is visible. Dropped counts requests never sent: queue
// overflow and cancellation.
type TargetStats struct {
	Total           int64    `json:"total"`
	Successes       int64    `json:"successes"`
	Errors          int64    `json:"errors"`
	TransportErrors int64    `json:"transport_errors"`
	Dropped         int64    `json:"dropped"`
	ThroughputRPS   float64  `json:"throughput_rps"`
	Latency         Latency  `json:"latency"`
	ResponseTime    Latency  `json:"response_time"`
	SchedulingDelay Latency  `json:"scheduling_delay"`
	Histogram       []Bucket `json:"histogram"`
}

// Latency holds nearest-rank quantiles in milliseconds over Samples values.
// Quantiles are null when there are no samples, never zero.
type Latency struct {
	Samples int64    `json:"samples"`
	P50MS   *float64 `json:"p50_ms"`
	P95MS   *float64 `json:"p95_ms"`
	P99MS   *float64 `json:"p99_ms"`
}

// Bucket counts samples in (previous UpperMS, UpperMS].
type Bucket struct {
	UpperMS float64 `json:"upper_ms"`
	Count   int64   `json:"count"`
}

// Endpoint is the per-route summary, keyed by normalized route template.
type Endpoint struct {
	Key       string      `json:"key"`
	Verdict   Verdict     `json:"verdict"`
	Baseline  TargetStats `json:"baseline"`
	Candidate TargetStats `json:"candidate"`
}

// ComparisonStat counts one classification on one endpoint.
type ComparisonStat struct {
	Endpoint       string         `json:"endpoint"`
	Classification Classification `json:"classification"`
	Count          int64          `json:"count"`
}

// ParseRun decodes run.json.
func ParseRun(data []byte) (Run, error) {
	var r Run
	if err := schema.Decode(data, SchemaVersion, &r); err != nil {
		return Run{}, fmt.Errorf("invalid run: %w", err)
	}
	return r, nil
}

// ParseReport decodes report.json.
func ParseReport(data []byte) (Report, error) {
	var r Report
	if err := schema.Decode(data, SchemaVersion, &r); err != nil {
		return Report{}, fmt.Errorf("invalid report: %w", err)
	}
	return r, nil
}

// Marshal renders a run or report as stable, indented JSON.
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
