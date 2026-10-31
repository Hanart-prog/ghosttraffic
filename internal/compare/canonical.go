package compare

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/Hanart-prog/ghosttraffic/internal/capture"
)

// Ignored replaces ignored values, so a value's absence still counts as a difference.
const Ignored = "[IGNORED]"

// Canonicalizer normalizes response bodies for comparison.
type Canonicalizer struct {
	paths []string
}

// NewCanonicalizer validates the ignored paths.
func NewCanonicalizer(ignore []string) (*Canonicalizer, error) {
	for _, path := range ignore {
		if _, err := capture.ParsePath(path); err != nil {
			return nil, err
		}
	}
	return &Canonicalizer{paths: ignore}, nil
}

func decode(body []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func (c *Canonicalizer) normalize(body []byte) (any, error) {
	doc, err := decode(body)
	if err != nil {
		return nil, err
	}
	for _, path := range c.paths {
		segments, err := capture.ParsePath(path)
		if err != nil {
			return nil, err
		}
		capture.ReplacePath(doc, segments, Ignored)
	}
	return doc, nil
}

// Digest hashes the canonical form: object key order and ignored paths cannot
// change it. An empty body has no digest, which classifies as body_unavailable.
func (c *Canonicalizer) Digest(body []byte) (string, error) {
	if len(body) == 0 {
		return "", nil
	}
	doc, err := c.normalize(body)
	if err != nil {
		return "", err
	}
	canonical, err := json.Marshal(doc) // sorts object keys
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// Diff lists the JSON paths where two bodies differ, sorted.
func (c *Canonicalizer) Diff(baseline, candidate []byte) ([]string, error) {
	a, err := c.normalize(baseline)
	if err != nil {
		return nil, err
	}
	b, err := c.normalize(candidate)
	if err != nil {
		return nil, err
	}
	paths := []string{}
	walk("$", a, b, &paths)
	slices.Sort(paths)
	return paths, nil
}

func walk(path string, a, b any, out *[]string) {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok {
			*out = append(*out, path)
			return
		}
		for key, value := range x {
			if other, ok := y[key]; ok {
				walk(path+"."+key, value, other, out)
			} else {
				*out = append(*out, path+"."+key)
			}
		}
		for key := range y {
			if _, ok := x[key]; !ok {
				*out = append(*out, path+"."+key)
			}
		}
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			*out = append(*out, path)
			return
		}
		for i := range x {
			walk(fmt.Sprintf("%s[%d]", path, i), x[i], y[i], out)
		}
	default:
		if a != b {
			*out = append(*out, path)
		}
	}
}
