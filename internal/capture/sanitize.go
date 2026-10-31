// Package capture will hold the recording proxy. This file holds the sanitizers
// every recorded request must pass through, so no secret reaches a trace by
// omission (ADR 0005).
package capture

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/Hanart-prog/ghosttraffic/internal/trace"
)

// Redacted replaces every sensitive value.
const Redacted = "[REDACTED]"

// sensitiveNames are matched as substrings of lowercased names with - and _
// removed, so access_token and X-Api-Key style variants are all caught.
var sensitiveNames = []string{
	"password", "passwd", "secret", "token", "apikey", "authorization",
	"cookie", "session", "creditcard", "cardnumber", "cvv", "ssn",
}

func isSensitiveName(name string) bool {
	folded := strings.NewReplacer("-", "", "_", "").Replace(strings.ToLower(name))
	for _, s := range sensitiveNames {
		if strings.Contains(folded, s) {
			return true
		}
	}
	return false
}

// FilterHeaders keeps only allowlisted, non-sensitive headers, with lowercase names.
func FilterHeaders(h http.Header, allowed []string) map[string]string {
	out := make(map[string]string)
	for _, name := range allowed {
		if value := h.Get(name); value != "" && !trace.IsSensitiveHeader(name) && !isSensitiveName(name) {
			out[strings.ToLower(name)] = value
		}
	}
	return out
}

// SanitizeQuery redacts sensitive parameters, keeping order and all other bytes.
func SanitizeQuery(requestURI string) string {
	path, query, found := strings.Cut(requestURI, "?")
	if !found {
		return requestURI
	}
	params := strings.Split(query, "&")
	for i, param := range params {
		key, _, _ := strings.Cut(param, "=")
		if decoded, err := url.QueryUnescape(key); err == nil && isSensitiveName(decoded) {
			params[i] = key + "=" + Redacted
		}
	}
	return path + "?" + strings.Join(params, "&")
}

// Excluded reports whether path is under an excluded prefix. Matching is on
// whole segments: /admin covers /admin/x but not /administrator.
func Excluded(path string, prefixes []string) bool {
	path, _, _ = strings.Cut(path, "?")
	for _, prefix := range prefixes {
		prefix = strings.TrimRight(prefix, "/")
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

var segmentPattern = regexp.MustCompile(`^([A-Za-z0-9_-]+)(\[\*\])?$`)

type segment struct {
	key  string
	each bool
}

// ParsePath parses the documented subset: $.a.b and $.items[*].id.
func ParsePath(path string) ([]segment, error) {
	rest, ok := strings.CutPrefix(path, "$.")
	if !ok {
		return nil, fmt.Errorf("JSON path %q must begin with \"$.\" and a key", path)
	}
	var segments []segment
	for _, part := range strings.Split(rest, ".") {
		m := segmentPattern.FindStringSubmatch(part)
		if m == nil {
			return nil, fmt.Errorf("JSON path %q: unsupported segment %q", path, part)
		}
		segments = append(segments, segment{key: m[1], each: m[2] != ""})
	}
	return segments, nil
}

// RedactBody redacts every key with a sensitive name at any depth, then each of
// paths. Numbers keep their exact text. Invalid JSON is an error: the caller
// must then store metadata only, never the raw bytes.
func RedactBody(body []byte, paths []string) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("body is not JSON: %w", err)
	}
	if dec.More() {
		return nil, fmt.Errorf("body has trailing data")
	}
	redactNames(doc)
	for _, path := range paths {
		segments, err := ParsePath(path)
		if err != nil {
			return nil, err
		}
		ReplacePath(doc, segments, Redacted)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func redactNames(v any) {
	switch v := v.(type) {
	case map[string]any:
		for key, child := range v {
			if isSensitiveName(key) {
				v[key] = Redacted
			} else {
				redactNames(child)
			}
		}
	case []any:
		for _, child := range v {
			redactNames(child)
		}
	}
}

// ReplacePath overwrites every value at segments in a decoded document. Missing
// keys are left missing, so absence stays distinguishable from a replaced value.
func ReplacePath(v any, segments []segment, replacement any) {
	object, ok := v.(map[string]any)
	if !ok {
		return
	}
	head, tail := segments[0], segments[1:]
	child, ok := object[head.key]
	if !ok {
		return
	}
	if head.each {
		items, ok := child.([]any)
		if !ok {
			return
		}
		for i, item := range items {
			if len(tail) == 0 {
				items[i] = replacement
			} else {
				ReplacePath(item, tail, replacement)
			}
		}
		return
	}
	if len(tail) == 0 {
		object[head.key] = replacement
		return
	}
	ReplacePath(child, tail, replacement)
}
