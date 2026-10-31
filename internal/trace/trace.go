// Package trace defines the versioned JSONL trace format. Each line of a trace
// file is one independently parseable Record.
package trace

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/textproto"
	"net/url"
	"strings"

	"github.com/Hanart-prog/ghosttraffic/internal/schema"
)

// SchemaVersion is the only trace schema version this build reads or writes.
const SchemaVersion = 1

// sensitiveHeaders must never be persisted: credentials, client identity, and
// connection-level headers that a replay must not inherit. Validate enforces
// this on read as well as capture filtering on write, so a hand-edited trace
// cannot smuggle them into a replay.
var sensitiveHeaders = map[string]bool{
	"Authorization":       true,
	"Connection":          true,
	"Content-Length":      true,
	"Cookie":              true,
	"Forwarded":           true,
	"Host":                true,
	"Keep-Alive":          true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"Set-Cookie":          true,
	"Te":                  true,
	"Trailer":             true,
	"Transfer-Encoding":   true,
	"Upgrade":             true,
	"Www-Authenticate":    true,
	"X-Api-Key":           true,
	"X-Auth-Token":        true,
	"X-Csrf-Token":        true,
	"X-Forwarded-For":     true,
	"X-Real-Ip":           true,
	"X-Xsrf-Token":        true,
}

// IsSensitiveHeader reports whether name may never appear in a trace.
func IsSensitiveHeader(name string) bool {
	return sensitiveHeaders[textproto.CanonicalMIMEHeaderKey(name)]
}

// Record is one captured request. Paths are origin-form (path and query only):
// a trace never stores an absolute target URL.
type Record struct {
	SchemaVersion   int               `json:"schema_version"`
	RequestID       string            `json:"request_id"`
	SessionID       string            `json:"session_id"`
	ArrivalOffsetMS int64             `json:"arrival_offset_ms"`
	Method          string            `json:"method"`
	Path            string            `json:"path"`
	Headers         map[string]string `json:"headers"`
	BodyJSON        json.RawMessage   `json:"body_json"`
	BodyTruncated   bool              `json:"body_truncated,omitempty"`
	Capture         Capture           `json:"capture"`
	Replayable      bool              `json:"replayable"`
	// UnreplayableReason is required when Replayable is false.
	UnreplayableReason string `json:"unreplayable_reason,omitempty"`
}

// Capture is what the recorder observed from the upstream. Status is 0 when
// the upstream produced no response.
type Capture struct {
	Status     int     `json:"status"`
	DurationMS float64 `json:"duration_ms"`
}

var methods = map[string]bool{
	"GET": true, "HEAD": true, "POST": true, "PUT": true,
	"PATCH": true, "DELETE": true, "OPTIONS": true,
}

// Validate checks the invariants every reader may rely on.
func (r Record) Validate() error {
	if r.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: %d (supported: %d)", schema.ErrUnsupportedVersion, r.SchemaVersion, SchemaVersion)
	}
	if r.RequestID == "" {
		return errors.New("request_id is required")
	}
	if r.ArrivalOffsetMS < 0 {
		return fmt.Errorf("arrival_offset_ms must be >= 0, got %d", r.ArrivalOffsetMS)
	}
	if !methods[r.Method] {
		return fmt.Errorf("unsupported method %q", r.Method)
	}
	if err := validatePath(r.Path); err != nil {
		return err
	}
	for name, value := range r.Headers {
		if IsSensitiveHeader(name) {
			return fmt.Errorf("header %q must not be stored in a trace", name)
		}
		if strings.ContainsAny(name+value, "\r\n\x00") {
			return fmt.Errorf("header %q contains a control character", name)
		}
	}
	if len(r.BodyJSON) > 0 && !json.Valid(r.BodyJSON) {
		return errors.New("body_json is not valid JSON")
	}
	if r.Capture.Status != 0 && (r.Capture.Status < 100 || r.Capture.Status > 599) {
		return fmt.Errorf("capture.status out of range: %d", r.Capture.Status)
	}
	if r.Capture.DurationMS < 0 {
		return fmt.Errorf("capture.duration_ms must be >= 0, got %v", r.Capture.DurationMS)
	}
	if r.BodyTruncated && r.Replayable {
		return errors.New("a truncated body must not be marked replayable")
	}
	if !r.Replayable && r.UnreplayableReason == "" {
		return errors.New("unreplayable_reason is required when replayable is false")
	}
	return nil
}

func validatePath(p string) error {
	// "//host/x" parses as a network-path reference and would change the
	// target host once joined to a base URL.
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") {
		return fmt.Errorf("path must be origin-form, got %q", p)
	}
	for _, c := range []byte(p) {
		if c < 0x20 || c == 0x7f {
			return fmt.Errorf("path contains a control character: %q", p)
		}
	}
	if _, err := url.ParseRequestURI(p); err != nil {
		return fmt.Errorf("invalid path %q: %w", p, err)
	}
	return nil
}

// ParseLine decodes and validates one trace line.
func ParseLine(line []byte) (Record, error) {
	var r Record
	if err := schema.Decode(line, SchemaVersion, &r); err != nil {
		return Record{}, fmt.Errorf("invalid record: %w", err)
	}
	if err := r.Validate(); err != nil {
		return Record{}, err
	}
	return r, nil
}

// MarshalLine encodes r as one line, including the trailing newline.
func MarshalLine(r Record) ([]byte, error) {
	return schema.MarshalLine(r)
}
