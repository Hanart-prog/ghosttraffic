package replay

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Hanart-prog/ghosttraffic/internal/capture"
	"github.com/Hanart-prog/ghosttraffic/internal/egress"
	"github.com/Hanart-prog/ghosttraffic/internal/metrics"
	"github.com/Hanart-prog/ghosttraffic/internal/report"
	"github.com/Hanart-prog/ghosttraffic/internal/trace"
	"github.com/Hanart-prog/ghosttraffic/pkg/config"
)

const (
	resultsFile = "results.jsonl"
	runFile     = "run.json"
)

// Workload is a validated trace ready to send, in arrival order.
type Workload struct {
	Records []trace.Record
	SHA256  string
	Total   int
}

// LoadWorkload validates a whole trace before anything is sent, so a bad line
// cannot end a replay partway through. Unreplayable records stay out of Records
// and are counted, never sent.
func LoadWorkload(path string) (Workload, error) {
	file, err := os.Open(path)
	if err != nil {
		return Workload{}, fmt.Errorf("open trace: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	var w Workload
	err = trace.Read(io.TeeReader(file, hash), func(r trace.Record) error {
		w.Total++
		if r.Replayable {
			w.Records = append(w.Records, r)
		}
		return nil
	})
	if err != nil {
		return Workload{}, fmt.Errorf("invalid trace %s: %w", path, err)
	}
	if w.Total == 0 {
		return Workload{}, fmt.Errorf("trace %s is empty", path)
	}
	slices.SortStableFunc(w.Records, func(a, b trace.Record) int {
		return cmp.Compare(a.ArrivalOffsetMS, b.ArrivalOffsetMS)
	})
	w.SHA256 = hex.EncodeToString(hash.Sum(nil))
	return w, nil
}

// Options configures Execute. Client must come from internal/egress.
type Options struct {
	Workload    Workload
	Baseline    config.Target
	Candidate   config.Target
	Client      *http.Client
	Router      *metrics.Router
	OutDir      string
	Concurrency int
	QueueLimit  int
	Speed       float64
	// Trials is the number of independent repetitions, each with both targets.
	Trials int
	// Warmup sends this many read-only requests before each pass and discards
	// them, so cold connections and caches are not measured.
	Warmup int
	// Reset restores both targets before each trial. Required when Trials > 1
	// and the workload writes: later trials must not start from earlier ones.
	Reset            func(context.Context) error
	Timeout          time.Duration
	MaxResponseBytes int64
	// CaptureBodies stores sanitized response JSON. Off by default: responses can
	// hold data that the request-side sanitizers never saw.
	CaptureBodies bool
	// AllowExternalMutation permits non-safe methods against an allowlisted host.
	AllowExternalMutation bool
	Build                 report.Build
}

// Outcome summarizes a finished run.
type Outcome struct {
	Run   report.Run
	Stats map[string]report.TargetStats
}

// Execute replays the workload against each target in turn, writing run.json
// and results.jsonl under OutDir. A target that fails requests still yields a
// completed run: failures are results. An error means the run itself could not
// proceed, or was cancelled.
func Execute(ctx context.Context, opts Options) (Outcome, error) {
	opts.Trials = max(opts.Trials, 1)
	if err := checkMutation(opts); err != nil {
		return Outcome{}, err
	}
	if opts.Trials > 1 && opts.Reset == nil && hasMutation(opts.Workload) {
		return Outcome{}, errors.New("replay: a trace with writes needs a reset command for more than one trial")
	}
	base, cand, err := prepareTrial(ctx, opts)
	if err != nil {
		return Outcome{}, err
	}
	// Mode 0700: results can hold sanitized responses. Mkdir, not MkdirAll, on the
	// last element: an existing run is never reused.
	if err := os.MkdirAll(filepath.Dir(opts.OutDir), 0o700); err != nil {
		return Outcome{}, fmt.Errorf("create run directory: %w", err)
	}
	if err := os.Mkdir(opts.OutDir, 0o700); err != nil {
		return Outcome{}, fmt.Errorf("create run directory: %w", err)
	}
	results, err := metrics.CreateResults(filepath.Join(opts.OutDir, resultsFile))
	if err != nil {
		return Outcome{}, err
	}
	defer results.Close()

	run, err := newRun(opts, base, cand)
	if err != nil {
		return Outcome{}, err
	}
	if err := writeRun(opts.OutDir, run); err != nil {
		return Outcome{}, err
	}

	kept := map[string][]metrics.Result{}
	elapsed := map[string]time.Duration{}
	var writeErr error
	targets := map[string]config.Target{"baseline": opts.Baseline, "candidate": opts.Candidate}
	for trial := 1; trial <= opts.Trials && writeErr == nil; trial++ {
		if trial > 1 && ctx.Err() == nil {
			nextBase, nextCand, err := prepareTrial(ctx, opts)
			switch {
			case err != nil:
				writeErr = fmt.Errorf("trial %d: %w", trial, err)
				continue
			case nextBase != base || nextCand != cand:
				writeErr = fmt.Errorf("trial %d: reset did not restore the initial state", trial)
				continue
			}
		}
		for _, name := range TargetOrder(trial) {
			pass, passResults, err := executePass(ctx, opts, trial, name, targets[name], results)
			run.Passes = append(run.Passes, pass)
			kept[name] = append(kept[name], passResults...)
			elapsed[name] += time.Duration(pass.ElapsedMS * float64(time.Millisecond))
			writeErr = errors.Join(writeErr, err)
		}
	}
	stats := make(map[string]report.TargetStats)
	for _, name := range []string{"baseline", "candidate"} {
		stats[name] = metrics.Stats(kept[name], elapsed[name])
		run.Warnings = append(run.Warnings, metrics.Warnings(name, stats[name])...)
	}
	ended := time.Now().UTC()
	run.EndedAt = &ended
	switch {
	case ctx.Err() != nil:
		run.Status = report.RunCancelled
	case writeErr != nil:
		run.Status = report.RunFailed
	default:
		run.Status = report.RunCompleted
	}
	if err := writeRun(opts.OutDir, run); err != nil {
		return Outcome{Run: run, Stats: stats}, err
	}
	if run.Status == report.RunCompleted {
		return Outcome{Run: run, Stats: stats}, nil
	}
	return Outcome{Run: run, Stats: stats}, errors.Join(ctx.Err(), writeErr)
}

// prepareTrial resets the targets if a reset was given, then proves their state.
func prepareTrial(ctx context.Context, opts Options) (base, cand string, err error) {
	if opts.Reset != nil {
		if err := opts.Reset(ctx); err != nil {
			return "", "", fmt.Errorf("reset targets: %w", err)
		}
	}
	return SeedDigests(ctx, opts.Client, baseURL(opts.Baseline), baseURL(opts.Candidate))
}

func baseURL(t config.Target) string {
	return strings.TrimRight(t.URL.String(), "/")
}

func isMutating(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

// checkMutation refuses to send writes to a non-local host unless told to: an
// allowlisted host is only known to be intended, not to be disposable.
func checkMutation(opts Options) error {
	if opts.AllowExternalMutation || !(opts.Baseline.External || opts.Candidate.External) || !hasMutation(opts.Workload) {
		return nil
	}
	return errors.New("replay: trace contains writes and a target is non-local; pass --allow-external-mutation only if that target is disposable")
}

func hasMutation(w Workload) bool {
	for _, r := range w.Records {
		if isMutating(r.Method) {
			return true
		}
	}
	return false
}

func newRun(opts Options, base, cand string) (report.Run, error) {
	cfg, err := json.Marshal(map[string]any{
		"capture_bodies":     opts.CaptureBodies,
		"concurrency":        opts.Concurrency,
		"max_response_bytes": opts.MaxResponseBytes,
		"queue_limit":        opts.QueueLimit,
		"speed":              opts.Speed,
		"timeout":            opts.Timeout.String(),
		"trials":             opts.Trials,
		"warmup":             opts.Warmup,
	})
	if err != nil {
		return report.Run{}, err
	}
	workload := opts.Workload
	run := report.Run{
		SchemaVersion: report.SchemaVersion,
		RunID:         filepath.Base(opts.OutDir),
		TraceSHA256:   workload.SHA256,
		Config:        cfg,
		Baseline:      baseURL(opts.Baseline),
		Candidate:     baseURL(opts.Candidate),
		BaselineSeed:  base,
		CandidateSeed: cand,
		Requests: report.RequestCounts{
			Total:   int64(workload.Total),
			Sent:    int64(len(workload.Records)),
			Skipped: int64(workload.Total - len(workload.Records)),
		},
		StartedAt: time.Now().UTC(),
		Build:     opts.Build,
		Status:    report.RunRunning,
		Passes:    []report.Pass{},
		Warnings:  []string{},
	}
	if run.Requests.Skipped > 0 {
		run.Warnings = append(run.Warnings, fmt.Sprintf("%d of %d recorded requests are unreplayable and were not sent", run.Requests.Skipped, run.Requests.Total))
	}
	if base == "" || cand == "" {
		run.Warnings = append(run.Warnings, "a target reported no seed digest, so equal initial state is unknown")
	}
	return run, nil
}

// writeRun replaces run.json atomically, so a reader never sees a partial file.
func writeRun(dir string, run report.Run) error {
	data, err := report.Marshal(run)
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, runFile+".tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, runFile))
}

// warm sends read-only requests from the trace and discards the outcomes. Writes
// are never used: they would change the state the pass is about to measure.
func warm(ctx context.Context, opts Options, base string) {
	sender := sender{opts: opts, base: base}
	work := make(chan trace.Record)
	var workers sync.WaitGroup
	for range min(opts.Concurrency, 16) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for record := range work {
				sender.send(ctx, record, &metrics.Result{})
			}
		}()
	}
	sent := 0
	for sent < opts.Warmup && ctx.Err() == nil {
		found := false
		for _, record := range opts.Workload.Records {
			if sent >= opts.Warmup {
				break
			}
			if !isMutating(record.Method) {
				found = true
				sent++
				work <- record
			}
		}
		if !found {
			break
		}
	}
	close(work)
	workers.Wait()
}

func executePass(ctx context.Context, opts Options, trial int, name string, target config.Target, sink *metrics.ResultWriter) (report.Pass, []metrics.Result, error) {
	if opts.Warmup > 0 {
		warm(ctx, opts, baseURL(target))
	}
	records := opts.Workload.Records
	offsets := make([]time.Duration, len(records))
	for i, r := range records {
		offsets[i] = time.Duration(r.ArrivalOffsetMS) * time.Millisecond
	}
	collected := make(chan metrics.Result, opts.Concurrency+1)
	summary := make(chan []metrics.Result, 1)
	var writeErr error
	go func() {
		kept := make([]metrics.Result, 0, len(records))
		for result := range collected {
			writeErr = errors.Join(writeErr, sink.Write(result))
			result.ResponseJSON = nil // stats need timings; bodies stay on disk only
			kept = append(kept, result)
		}
		summary <- kept
	}()

	sender := sender{opts: opts, trial: trial, target: name, base: baseURL(target)}
	began := time.Now()
	err := Schedule(ctx, Config{Concurrency: opts.Concurrency, QueueLimit: opts.QueueLimit, Speed: opts.Speed}, offsets,
		func(ctx context.Context, slot Slot) {
			collected <- sender.attempt(ctx, slot, records[slot.Index])
		})
	elapsed := time.Since(began)
	close(collected)
	kept := <-summary
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		writeErr = errors.Join(writeErr, err)
	}
	pass := report.Pass{Target: name, Trial: trial, ElapsedMS: milliseconds(elapsed)}
	return pass, kept, writeErr
}

func milliseconds(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

type sender struct {
	opts   Options
	trial  int
	target string
	base   string
}

// attempt turns one scheduling decision into exactly one result.
func (s sender) attempt(ctx context.Context, slot Slot, record trace.Record) metrics.Result {
	result := metrics.Result{
		SchemaVersion:     metrics.ResultSchemaVersion,
		Trial:             s.trial,
		RequestID:         record.RequestID,
		Target:            s.target,
		EndpointKey:       s.opts.Router.Key(record.Method, record.Path),
		ScheduledOffsetMS: milliseconds(slot.Scheduled),
	}
	switch slot.Status {
	case Dropped:
		result.ErrorCategory = metrics.ErrorDropped
		return result
	case Cancelled:
		result.ErrorCategory = metrics.ErrorCancelled
		return result
	}
	result.ActualStartOffsetMS = milliseconds(slot.Start)
	result.SchedulingDelayMS = milliseconds(slot.Delay)
	began := time.Now()
	s.send(ctx, record, &result)
	result.DurationMS = milliseconds(time.Since(began))
	return result
}

func (s sender) send(ctx context.Context, record trace.Record, result *metrics.Result) {
	requestCtx, cancel := context.WithTimeout(ctx, s.opts.Timeout)
	defer cancel()
	var body io.Reader
	if len(record.BodyJSON) > 0 && string(record.BodyJSON) != "null" {
		body = bytes.NewReader(record.BodyJSON)
	}
	request, err := http.NewRequestWithContext(requestCtx, record.Method, s.base+record.Path, body)
	if err != nil {
		result.ErrorCategory = metrics.ErrorConn
		return
	}
	// Validated at load: no credentials, Host, or hop-by-hop headers can be here.
	for name, value := range record.Headers {
		request.Header.Set(name, value)
	}
	request.Header.Set("User-Agent", "ghosttraffic/"+s.opts.Build.Version)

	response, err := s.opts.Client.Do(request)
	if err != nil {
		result.ErrorCategory = classify(ctx, err)
		return
	}
	defer response.Body.Close()
	result.Status = response.StatusCode

	hash := sha256.New()
	var kept bytes.Buffer
	limit := s.opts.MaxResponseBytes
	if _, err := io.Copy(io.MultiWriter(hash, &cappedBuffer{buf: &kept, limit: limit}), response.Body); err != nil {
		result.ErrorCategory = classify(ctx, err)
		return
	}
	result.ErrorCategory = metrics.ErrorNone
	result.ResponseDigest = "sha256:" + hex.EncodeToString(hash.Sum(nil))
	complete := int64(kept.Len()) < limit
	if !complete || kept.Len() == 0 || !declaresJSON(response.Header.Get("Content-Type")) {
		return
	}
	if !json.Valid(kept.Bytes()) {
		result.ErrorCategory = metrics.ErrorMalformed
		return
	}
	if s.opts.CaptureBodies {
		if redacted, err := capture.RedactBody(kept.Bytes(), nil); err == nil {
			result.ResponseJSON = redacted
		}
	}
}

// cappedBuffer keeps a prefix and discards the rest without failing the copy.
type cappedBuffer struct {
	buf   *bytes.Buffer
	limit int64
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := c.limit - int64(c.buf.Len()); room > 0 {
		c.buf.Write(p[:min(int64(len(p)), room)])
	}
	return len(p), nil
}

func declaresJSON(contentType string) bool {
	media, _, err := mime.ParseMediaType(contentType)
	return err == nil && (media == "application/json" || strings.HasSuffix(media, "+json"))
}

// classify maps a transport error to its category. The parent context is
// checked first: a shutdown is not the target's fault.
func classify(parent context.Context, err error) metrics.ErrorCategory {
	var netErr net.Error
	switch {
	case parent.Err() != nil:
		return metrics.ErrorCancelled
	case errors.Is(err, egress.ErrRefused):
		return metrics.ErrorBlocked
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return metrics.ErrorTimeout
	case errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.EPIPE):
		return metrics.ErrorReset
	}
	return metrics.ErrorConn
}
