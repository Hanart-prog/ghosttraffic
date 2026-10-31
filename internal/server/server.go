// Package server is the read-only local API behind the dashboard. It only reads
// run directories, and every number it returns comes from those files.
package server

import (
	"bufio"
	"cmp"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Hanart-prog/ghosttraffic/internal/analysis"
	"github.com/Hanart-prog/ghosttraffic/internal/compare"
	"github.com/Hanart-prog/ghosttraffic/internal/metrics"
	"github.com/Hanart-prog/ghosttraffic/internal/report"
	"github.com/Hanart-prog/ghosttraffic/internal/trace"
)

const (
	defaultPage  = 50
	maxPage      = 500
	maxTimeline  = 1000
	maxLineBytes = 256 << 20
)

var runID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// Options configures a Server.
type Options struct {
	RunsDir string
	// WebDir, if set, serves the built dashboard from the same address.
	WebDir string
	// AllowOrigins may read the API across origins, such as a dev server.
	AllowOrigins []string
}

// Server serves run data.
type Server struct {
	opts Options
}

// New returns a Server.
func New(opts Options) *Server { return &Server{opts: opts} }

// Handler returns the API with its protections applied.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/runs", s.listRuns)
	mux.HandleFunc("GET /api/runs/{id}", s.getRun)
	mux.HandleFunc("GET /api/runs/{id}/endpoints", s.getEndpoints)
	mux.HandleFunc("GET /api/runs/{id}/requests", s.listRequests)
	mux.HandleFunc("GET /api/runs/{id}/requests/{request}", s.getRequest)
	mux.HandleFunc("GET /api/runs/{id}/timeline", s.getTimeline)
	if s.opts.WebDir != "" {
		mux.Handle("GET /", spa(s.opts.WebDir))
	}
	return s.protect(mux)
}

// protect rejects other hosts and methods. The Host check stops DNS rebinding,
// where a web page points its own name at 127.0.0.1 to read this API; CORS alone
// does not, because the browser then sees the request as same-origin.
func (s *Server) protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("Referrer-Policy", "no-referrer")
		header.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'")
		if !localHost(r.Host) {
			writeError(w, http.StatusForbidden, "this API only answers on localhost")
			return
		}
		if origin := r.Header.Get("Origin"); slices.Contains(s.opts.AllowOrigins, origin) {
			header.Set("Access-Control-Allow-Origin", origin)
			header.Add("Vary", "Origin")
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, http.StatusMethodNotAllowed, "this API is read-only")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func localHost(host string) bool {
	if name, _, err := net.SplitHostPort(host); err == nil {
		host = name
	}
	host = strings.Trim(host, "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// spa serves built files, and index.html for client-side routes.
func spa(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		clean := filepath.Join(dir, filepath.FromSlash(filepath.Clean("/"+r.URL.Path)))
		if info, err := os.Stat(clean); err != nil || info.IsDir() {
			http.ServeFile(w, r, filepath.Join(dir, "index.html"))
			return
		}
		files.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	data, err := report.Marshal(v)
	if err != nil {
		http.Error(w, `{"error":"encoding failed"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// runDir resolves a run ID to a real directory directly inside RunsDir. The
// pattern excludes separators and leading dots; Lstat refuses symlinks, which
// could otherwise point anywhere on the disk.
func (s *Server) runDir(id string) (string, bool) {
	if !runID.MatchString(id) || strings.Contains(id, "..") {
		return "", false
	}
	dir := filepath.Join(s.opts.RunsDir, id)
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return "", false
	}
	return dir, true
}

func regularFile(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fs.ErrNotExist
	}
	return os.Open(path)
}

func readRun(dir string) (report.Run, error) {
	file, err := regularFile(filepath.Join(dir, "run.json"))
	if err != nil {
		return report.Run{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return report.Run{}, err
	}
	return report.ParseRun(data)
}

// readReport returns nil when no report has been built.
func readReport(dir string) (*report.Report, error) {
	file, err := regularFile(filepath.Join(dir, "report.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	built, err := report.ParseReport(data)
	if err != nil {
		return nil, err
	}
	return &built, nil
}

// RunSummary is one row of the run list. Verdict is null without a report.
type RunSummary struct {
	RunID     string               `json:"run_id"`
	Status    report.RunStatus     `json:"status"`
	StartedAt time.Time            `json:"started_at"`
	EndedAt   *time.Time           `json:"ended_at"`
	Baseline  string               `json:"baseline"`
	Candidate string               `json:"candidate"`
	Requests  report.RequestCounts `json:"requests"`
	Verdict   *report.Verdict      `json:"verdict"`
}

func (s *Server) listRuns(w http.ResponseWriter, _ *http.Request) {
	entries, err := os.ReadDir(s.opts.RunsDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		writeError(w, http.StatusInternalServerError, "cannot read the runs directory")
		return
	}
	runs := []RunSummary{}
	for _, entry := range entries {
		dir, ok := s.runDir(entry.Name())
		if !ok {
			continue
		}
		run, err := readRun(dir)
		if err != nil {
			continue // not a run directory, or one from an unsupported version
		}
		summary := RunSummary{
			RunID: entry.Name(), Status: run.Status, StartedAt: run.StartedAt, EndedAt: run.EndedAt,
			Baseline: run.Baseline, Candidate: run.Candidate, Requests: run.Requests,
		}
		if built, err := readReport(dir); err == nil && built != nil {
			summary.Verdict = &built.Verdict
		}
		runs = append(runs, summary)
	}
	slices.SortFunc(runs, func(a, b RunSummary) int {
		return cmp.Or(b.StartedAt.Compare(a.StartedAt), cmp.Compare(a.RunID, b.RunID))
	})
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) dirOr404(w http.ResponseWriter, r *http.Request) (string, bool) {
	dir, ok := s.runDir(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "run not found")
	}
	return dir, ok
}

// RunDetail pairs the manifest with its report. Report is null if none is built.
type RunDetail struct {
	Run         report.Run     `json:"run"`
	Report      *report.Report `json:"report"`
	ReportError string         `json:"report_error,omitempty"`
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.dirOr404(w, r)
	if !ok {
		return
	}
	run, err := readRun(dir)
	if err != nil {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	detail := RunDetail{Run: run}
	if detail.Report, err = readReport(dir); err != nil {
		detail.ReportError = "this run's report cannot be read; rebuild it with ghosttraffic report"
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) getEndpoints(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.dirOr404(w, r)
	if !ok {
		return
	}
	built, err := readReport(dir)
	if err != nil || built == nil {
		writeError(w, http.StatusNotFound, "this run has no report; build one with ghosttraffic report")
		return
	}
	writeJSON(w, http.StatusOK, built.Endpoints)
}

// RequestPage is one page of results. NextCursor is null on the last page.
type RequestPage struct {
	Items      []metrics.Result `json:"items"`
	NextCursor *int64           `json:"next_cursor"`
}

func intParam(r *http.Request, name string, fallback int64) (int64, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}
	return value, nil
}

// listRequests pages through results.jsonl. The cursor is a byte offset, so a
// page costs the same however deep it is and needs no state on the server.
func (s *Server) listRequests(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.dirOr404(w, r)
	if !ok {
		return
	}
	limit, err := intParam(r, "limit", defaultPage)
	if err == nil && (limit < 1 || limit > maxPage) {
		err = fmt.Errorf("limit must be between 1 and %d", maxPage)
	}
	var cursor, trial int64
	if err == nil {
		cursor, err = intParam(r, "cursor", 0)
	}
	if err == nil {
		trial, err = intParam(r, "trial", 0)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	query := r.URL.Query()
	endpoint, target := query.Get("endpoint"), query.Get("target")
	if target != "" && target != "baseline" && target != "candidate" {
		writeError(w, http.StatusBadRequest, "target must be baseline or candidate")
		return
	}

	file, err := regularFile(filepath.Join(dir, "results.jsonl"))
	if err != nil {
		writeError(w, http.StatusNotFound, "this run has no results")
		return
	}
	defer file.Close()
	if _, err := file.Seek(cursor, io.SeekStart); err != nil {
		writeError(w, http.StatusBadRequest, "invalid cursor")
		return
	}
	reader := bufio.NewReader(file)
	page := RequestPage{Items: []metrics.Result{}}
	offset := cursor
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) == 0 && errors.Is(err, io.EOF) {
			break
		}
		if err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusInternalServerError, "cannot read results")
			return
		}
		result, parseErr := metrics.ParseResult(line)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		if (endpoint == "" || result.EndpointKey == endpoint) &&
			(target == "" || result.Target == target) &&
			(trial == 0 || int64(result.Trial) == trial) {
			if int64(len(page.Items)) == limit {
				next := offset
				page.NextCursor = &next
				break
			}
			result.ResponseJSON = nil // bodies belong to the detail view
			page.Items = append(page.Items, result)
		}
		offset += int64(len(line))
	}
	writeJSON(w, http.StatusOK, page)
}

func eachResult(dir string, fn func(metrics.Result) error) error {
	file, err := regularFile(filepath.Join(dir, "results.jsonl"))
	if err != nil {
		return err
	}
	defer file.Close()
	return metrics.ReadResults(file, fn)
}

// RequestDetail is one logical request in one trial, on both targets. Request is
// null for runs that predate trace.jsonl; Classification is empty unless both
// targets produced a result.
type RequestDetail struct {
	RequestID      string                `json:"request_id"`
	Trial          int                   `json:"trial"`
	Request        *trace.Record         `json:"request"`
	Baseline       *metrics.Result       `json:"baseline"`
	Candidate      *metrics.Result       `json:"candidate"`
	Classification report.Classification `json:"classification"`
	DifferingPaths []string              `json:"differing_paths"`
	IgnoredPaths   []string              `json:"ignored_paths"`
}

func (s *Server) getRequest(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.dirOr404(w, r)
	if !ok {
		return
	}
	trial, err := intParam(r, "trial", 1)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id := r.PathValue("request")
	detail := RequestDetail{RequestID: id, Trial: int(trial), DifferingPaths: []string{}, IgnoredPaths: []string{}}
	err = eachResult(dir, func(result metrics.Result) error {
		if result.RequestID == id && int64(result.Trial) == trial {
			if result.Target == "baseline" {
				detail.Baseline = &result
			} else {
				detail.Candidate = &result
			}
		}
		return nil
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "this run has no readable results")
		return
	}
	if detail.Baseline == nil && detail.Candidate == nil {
		writeError(w, http.StatusNotFound, "request not found in that trial")
		return
	}
	detail.Request = findRecord(dir, id)
	if built, err := readReport(dir); err == nil && built != nil {
		detail.IgnoredPaths = built.IgnoredJSONPaths
	}
	if detail.Baseline != nil && detail.Candidate != nil {
		canonicalizer, err := compare.NewCanonicalizer(detail.IgnoredPaths)
		if err == nil {
			class, paths, classifyErr := analysis.Classify(*detail.Baseline, *detail.Candidate, canonicalizer)
			if classifyErr == nil {
				detail.Classification, detail.DifferingPaths = class, paths
			}
		}
	}
	writeJSON(w, http.StatusOK, detail)
}

var errFound = errors.New("found")

func findRecord(dir, id string) *trace.Record {
	file, err := regularFile(filepath.Join(dir, "trace.jsonl"))
	if err != nil {
		return nil
	}
	defer file.Close()
	var found *trace.Record
	_ = trace.Read(file, func(record trace.Record) error {
		if record.RequestID == id {
			found = &record
			return errFound
		}
		return nil
	})
	return found
}

// TimelineItem is one attempt's position in time. The bar runs from
// scheduled_offset_ms through dependency wait and scheduling delay to the end of
// duration_ms. Requests never sent have zero duration.
type TimelineItem struct {
	RequestID           string                `json:"request_id"`
	EndpointKey         string                `json:"endpoint_key"`
	Status              int                   `json:"status"`
	ErrorCategory       metrics.ErrorCategory `json:"error_category"`
	ScheduledOffsetMS   float64               `json:"scheduled_offset_ms"`
	DependencyWaitMS    float64               `json:"dependency_wait_ms"`
	SchedulingDelayMS   float64               `json:"scheduling_delay_ms"`
	ActualStartOffsetMS float64               `json:"actual_start_offset_ms"`
	DurationMS          float64               `json:"duration_ms"`
}

// Timeline is a window of one target's attempts in one trial, by due time.
type Timeline struct {
	Trial  int            `json:"trial"`
	Target string         `json:"target"`
	Total  int            `json:"total"`
	Offset int            `json:"offset"`
	Items  []TimelineItem `json:"items"`
}

func (s *Server) getTimeline(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.dirOr404(w, r)
	if !ok {
		return
	}
	trial, err := intParam(r, "trial", 1)
	var offset, limit int64
	if err == nil {
		offset, err = intParam(r, "offset", 0)
	}
	if err == nil {
		limit, err = intParam(r, "limit", 200)
	}
	if err == nil && (limit < 1 || limit > maxTimeline) {
		err = fmt.Errorf("limit must be between 1 and %d", maxTimeline)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	target := r.URL.Query().Get("target")
	if target == "" {
		target = "baseline"
	}
	if target != "baseline" && target != "candidate" {
		writeError(w, http.StatusBadRequest, "target must be baseline or candidate")
		return
	}
	endpoint := r.URL.Query().Get("endpoint")
	items := []TimelineItem{}
	err = eachResult(dir, func(result metrics.Result) error {
		if result.Target == target && int64(result.Trial) == trial && (endpoint == "" || result.EndpointKey == endpoint) {
			items = append(items, TimelineItem{
				RequestID: result.RequestID, EndpointKey: result.EndpointKey, Status: result.Status,
				ErrorCategory: result.ErrorCategory, ScheduledOffsetMS: result.ScheduledOffsetMS,
				DependencyWaitMS: result.DependencyWaitMS, SchedulingDelayMS: result.SchedulingDelayMS,
				ActualStartOffsetMS: result.ActualStartOffsetMS, DurationMS: result.DurationMS,
			})
		}
		return nil
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "this run has no readable results")
		return
	}
	slices.SortFunc(items, func(a, b TimelineItem) int {
		return cmp.Or(cmp.Compare(a.ScheduledOffsetMS, b.ScheduledOffsetMS), cmp.Compare(a.RequestID, b.RequestID))
	})
	timeline := Timeline{Trial: int(trial), Target: target, Total: len(items), Offset: int(offset), Items: []TimelineItem{}}
	if int(offset) < len(items) {
		timeline.Items = items[offset:min(int(offset+limit), len(items))]
	}
	writeJSON(w, http.StatusOK, timeline)
}
