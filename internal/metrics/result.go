// Package metrics defines per-attempt results, the raw data every report
// figure is computed from.
package metrics

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Hanart-prog/ghosttraffic/internal/schema"
)

// ResultSchemaVersion is the results.jsonl schema version.
const ResultSchemaVersion = 1

// ErrorCategory is the terminal transport-level classification of an attempt.
// Every scheduled request produces exactly one Result, including those never
// sent. An HTTP 5xx is a completed exchange: it is ErrorNone with its Status.
type ErrorCategory string

const (
	ErrorNone      ErrorCategory = "none"
	ErrorTimeout   ErrorCategory = "timeout"
	ErrorConn      ErrorCategory = "connection"
	ErrorReset     ErrorCategory = "connection_reset"
	ErrorBlocked   ErrorCategory = "blocked"
	ErrorMalformed ErrorCategory = "malformed_response"
	ErrorCancelled ErrorCategory = "cancelled"
	ErrorDropped   ErrorCategory = "dropped"
)

func (c ErrorCategory) valid() bool {
	switch c {
	case ErrorNone, ErrorTimeout, ErrorConn, ErrorReset, ErrorBlocked, ErrorMalformed, ErrorCancelled, ErrorDropped:
		return true
	}
	return false
}

// Result is one line of results.jsonl. Times are fractional milliseconds:
// localhost latencies are often below 1ms, so integers would lose them.
//
// DurationMS is service time from actual start. Response time as a client
// would see it is SchedulingDelayMS + DurationMS; see ADR 0004. Trial counts
// from 1, so (Trial, Target, RequestID) identifies an attempt.
type Result struct {
	SchemaVersion       int             `json:"schema_version"`
	Trial               int             `json:"trial"`
	RequestID           string          `json:"request_id"`
	Target              string          `json:"target"`
	EndpointKey         string          `json:"endpoint_key"`
	ScheduledOffsetMS   float64         `json:"scheduled_offset_ms"`
	ActualStartOffsetMS float64         `json:"actual_start_offset_ms"`
	SchedulingDelayMS   float64         `json:"scheduling_delay_ms"`
	DurationMS          float64         `json:"duration_ms"`
	Status              int             `json:"status"`
	ErrorCategory       ErrorCategory   `json:"error_category"`
	ResponseDigest      string          `json:"response_digest,omitempty"`
	ResponseJSON        json.RawMessage `json:"response_json,omitempty"`
}

// ParseResult decodes and validates one results.jsonl line.
func ParseResult(line []byte) (Result, error) {
	var r Result
	if err := schema.Decode(line, ResultSchemaVersion, &r); err != nil {
		return Result{}, fmt.Errorf("invalid result: %w", err)
	}
	if r.RequestID == "" {
		return Result{}, errors.New("request_id is required")
	}
	if r.Target != "baseline" && r.Target != "candidate" {
		return Result{}, fmt.Errorf("invalid target %q", r.Target)
	}
	if r.Trial < 1 {
		return Result{}, fmt.Errorf("trial must be >= 1, got %d", r.Trial)
	}
	if !r.ErrorCategory.valid() {
		return Result{}, fmt.Errorf("invalid error_category %q", r.ErrorCategory)
	}
	return r, nil
}

// MarshalResult encodes r as one line, including the trailing newline.
func MarshalResult(r Result) ([]byte, error) {
	return schema.MarshalLine(r)
}
