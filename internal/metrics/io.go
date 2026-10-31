package metrics

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

// maxResultLineBytes bounds one results.jsonl line when reading.
const maxResultLineBytes = 256 << 20

// ResultWriter appends to a new results.jsonl, flushing each line so a crash
// loses at most the line in progress.
type ResultWriter struct {
	mu   sync.Mutex
	file *os.File
	buf  *bufio.Writer
}

// CreateResults starts a results file. An existing file is an error.
func CreateResults(path string) (*ResultWriter, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create results: %w", err)
	}
	return &ResultWriter{file: file, buf: bufio.NewWriter(file)}, nil
}

// Write appends one result.
func (w *ResultWriter) Write(r Result) error {
	line, err := MarshalResult(r)
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
func (w *ResultWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return errors.Join(w.buf.Flush(), w.file.Close())
}

// ReadResults streams every result to fn.
func ReadResults(r io.Reader, fn func(Result) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), maxResultLineBytes)
	for line := 1; scanner.Scan(); line++ {
		result, err := ParseResult(scanner.Bytes())
		if err != nil {
			return fmt.Errorf("line %d: %w", line, err)
		}
		if err := fn(result); err != nil {
			return err
		}
	}
	return scanner.Err()
}
