// Package schema holds the decoding rules shared by every versioned artifact
// (traces, results, run manifests, reports).
package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrUnsupportedVersion is returned for documents whose schema_version differs
// from the one the reader supports, so callers can distinguish "too new" from
// "malformed".
var ErrUnsupportedVersion = errors.New("unsupported schema version")

// Decode strictly decodes one JSON document into v. The version is checked
// first so a future schema is reported as such rather than as an unknown field.
// Unknown fields are rejected: new fields require a version bump. Trailing data
// is rejected by the initial Unmarshal, which validates the whole input.
func Decode(data []byte, version int, v any) error {
	var header struct {
		SchemaVersion *int `json:"schema_version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if header.SchemaVersion == nil {
		return errors.New("schema_version is required")
	}
	if *header.SchemaVersion != version {
		return fmt.Errorf("%w: %d (supported: %d)", ErrUnsupportedVersion, *header.SchemaVersion, version)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// MarshalLine encodes v as one JSONL line, including the trailing newline. HTML
// escaping is disabled so query strings stay readable and byte-stable.
func MarshalLine(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
