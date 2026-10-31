package trace

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// maxLineBytes bounds one trace line when reading: well above any sensible
// capture.max_body_bytes, and a limit on what a corrupt file can allocate.
const maxLineBytes = 256 << 20

// Writer appends records to a new trace file. Each line is flushed as written,
// so a crash loses at most the line in progress. Lines are in completion order;
// readers order by arrival_offset_ms.
type Writer struct {
	mu   sync.Mutex
	file *os.File
	buf  *bufio.Writer
}

// Create starts a trace at path. An existing file is an error: traces are never
// overwritten. Traces may hold sensitive data, so they are private to the user.
func Create(path string) (*Writer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create trace: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create trace: %w", err)
	}
	return &Writer{file: file, buf: bufio.NewWriter(file)}, nil
}

// Write validates r and appends it. An invalid record is never written.
func (w *Writer) Write(r Record) error {
	if err := r.Validate(); err != nil {
		return err
	}
	line, err := MarshalLine(r)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := w.buf.Write(line); err != nil {
		return err
	}
	return w.buf.Flush()
}

// Close flushes and closes the file.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return errors.Join(w.buf.Flush(), w.file.Close())
}

// Read streams every record to fn, failing on the first invalid line or
// duplicate request ID. It keeps the IDs seen, not the records.
func Read(r io.Reader, fn func(Record) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), maxLineBytes)
	seen := make(map[string]bool)
	for line := 1; scanner.Scan(); line++ {
		record, err := ParseLine(scanner.Bytes())
		if err != nil {
			return fmt.Errorf("line %d: %w", line, err)
		}
		if seen[record.RequestID] {
			return fmt.Errorf("line %d: duplicate request_id %q", line, record.RequestID)
		}
		seen[record.RequestID] = true
		if err := fn(record); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read trace: %w", err)
	}
	return nil
}
