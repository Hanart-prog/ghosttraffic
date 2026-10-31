package capture

import (
	"bytes"
	"io"
	"sync"
)

// CappedTee passes a body through untouched while retaining at most max bytes,
// so recording never buffers an arbitrary body. The transport may read from a
// different goroutine than the one calling Retained, hence the lock.
type CappedTee struct {
	body io.ReadCloser
	max  int64

	mu        sync.Mutex
	kept      bytes.Buffer
	truncated bool
}

// NewCappedTee wraps body, which the tee closes.
func NewCappedTee(body io.ReadCloser, max int64) *CappedTee {
	return &CappedTee{body: body, max: max}
}

func (t *CappedTee) Read(p []byte) (int, error) {
	n, err := t.body.Read(p)
	t.mu.Lock()
	defer t.mu.Unlock()
	room := t.max - int64(t.kept.Len())
	switch {
	case int64(n) <= room:
		t.kept.Write(p[:n])
	default:
		t.kept.Write(p[:room])
		t.truncated = true
	}
	return n, err
}

// Close closes the wrapped body.
func (t *CappedTee) Close() error { return t.body.Close() }

// Retained returns a copy of the kept bytes and whether the body exceeded max.
// A truncated body must be recorded as unreplayable.
func (t *CappedTee) Retained() ([]byte, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return bytes.Clone(t.kept.Bytes()), t.truncated
}
