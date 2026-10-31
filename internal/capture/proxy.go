package capture

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Hanart-prog/ghosttraffic/internal/trace"
)

// Sink receives finished records. It must be safe for concurrent use.
type Sink interface {
	Write(trace.Record) error
}

// Options configures a Proxy.
type Options struct {
	Upstream        *url.URL
	Transport       http.RoundTripper // must come from internal/egress
	Sink            Sink
	AllowedHeaders  []string
	MaxBodyBytes    int64
	RedactJSONPaths []string
	ExcludePaths    []string
	SessionHeader   string
	Log             io.Writer
}

// Stats counts what happened to proxied requests. Every request lands in
// exactly one field, so nothing leaves the trace without being counted.
type Stats struct {
	Recorded, Unreplayable, Excluded, Failed int64
}

// Proxy is a streaming reverse proxy that records what passes through it. The
// client's exchange with the upstream is never altered by recording: failing to
// record a request is counted and logged, and does not fail the request.
type Proxy struct {
	opts    Options
	reverse *httputil.ReverseProxy

	mu    sync.Mutex
	start time.Time // first arrival, the zero of arrival_offset_ms
	next  int

	recorded, unreplayable, excluded, failed atomic.Int64
}

// New returns a Proxy. Options must already be validated.
func New(opts Options) *Proxy {
	p := &Proxy{opts: opts}
	p.reverse = &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(opts.Upstream)
			// Rewrite strips parameters it cannot parse; forward them unchanged.
			pr.Out.URL.RawQuery = pr.In.URL.RawQuery
		},
		Transport:     opts.Transport,
		FlushInterval: -1, // stream: never hold a response back
		ErrorHandler:  upstreamFailed,
	}
	return p
}

// Stats returns the current counts.
func (p *Proxy) Stats() Stats {
	return Stats{p.recorded.Load(), p.unreplayable.Load(), p.excluded.Load(), p.failed.Load()}
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if Excluded(r.URL.Path, p.opts.ExcludePaths) {
		p.excluded.Add(1)
		p.reverse.ServeHTTP(w, r)
		return
	}

	id, offset := p.arrive()
	began := time.Now()
	record := trace.Record{
		SchemaVersion:   trace.SchemaVersion,
		RequestID:       id,
		SessionID:       sessionID(r.Header.Get(p.opts.SessionHeader)),
		ArrivalOffsetMS: offset.Milliseconds(),
		Method:          r.Method,
		Path:            SanitizeQuery(r.URL.RequestURI()),
		Headers:         FilterHeaders(r.Header, p.opts.AllowedHeaders),
	}
	contentType := r.Header.Get("Content-Type")
	declaredLength := r.ContentLength
	var tee *CappedTee
	if r.Body != nil && r.Body != http.NoBody {
		tee = NewCappedTee(r.Body, p.opts.MaxBodyBytes)
		r.Body = tee
	}
	status := &statusWriter{ResponseWriter: w}

	// Deferred so that a client abort, which panics through the proxy, is
	// recorded too.
	defer func() {
		record.Capture = trace.Capture{
			Status:     status.upstreamStatus(),
			DurationMS: float64(time.Since(began)) / float64(time.Millisecond),
		}
		p.applyBody(&record, tee, contentType, declaredLength)
		p.write(record)
	}()
	p.reverse.ServeHTTP(status, r)
}

// arrive assigns ID and offset together, so IDs follow arrival order.
func (p *Proxy) arrive() (string, time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	if p.next == 0 {
		p.start = now
	}
	p.next++
	return fmt.Sprintf("req_%06d", p.next), now.Sub(p.start)
}

// applyBody decides what of a request body may enter the trace. Everything that
// is not known-complete, sanitized JSON is stored as metadata only.
func (p *Proxy) applyBody(record *trace.Record, tee *CappedTee, contentType string, declaredLength int64) {
	record.Replayable = true
	if tee == nil {
		return
	}
	body, truncated := tee.Retained()
	unreplayable := func(reason string) {
		record.Replayable = false
		record.UnreplayableReason = reason
	}
	switch {
	case truncated || declaredLength > p.opts.MaxBodyBytes:
		record.BodyTruncated = true
		unreplayable("body exceeded max_body_bytes")
	case declaredLength > 0 && int64(len(body)) < declaredLength:
		unreplayable("upstream did not read the whole body")
	case len(body) == 0:
	case !isJSON(contentType):
		unreplayable("body is not JSON")
	default:
		redacted, err := RedactBody(body, p.opts.RedactJSONPaths)
		if err != nil {
			unreplayable("body is not valid JSON")
			return
		}
		record.BodyJSON = json.RawMessage(redacted)
	}
}

func (p *Proxy) write(record trace.Record) {
	if err := p.opts.Sink.Write(record); err != nil {
		p.failed.Add(1)
		if p.opts.Log != nil {
			fmt.Fprintf(p.opts.Log, "ghosttraffic capture: %s not recorded: %v\n", record.RequestID, err)
		}
		return
	}
	if record.Replayable {
		p.recorded.Add(1)
	} else {
		p.unreplayable.Add(1)
	}
}

func isJSON(contentType string) bool {
	media, _, err := mime.ParseMediaType(contentType)
	return err == nil && (media == "application/json" || strings.HasSuffix(media, "+json"))
}

// sessionID hashes the header value: it groups requests without storing what
// may be a real session identifier.
func sessionID(value string) string {
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return "s_" + hex.EncodeToString(sum[:6])
}

// upstreamFailed answers 502 and marks the writer, so the trace records that the
// upstream gave no response rather than that it returned a 502.
func upstreamFailed(w http.ResponseWriter, _ *http.Request, _ error) {
	if s, ok := w.(*statusWriter); ok {
		s.failed = true
	}
	w.WriteHeader(http.StatusBadGateway)
}

type statusWriter struct {
	http.ResponseWriter
	status int
	failed bool
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 && code >= 200 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach Flush and Hijack underneath.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *statusWriter) upstreamStatus() int {
	if w.failed {
		return 0
	}
	return w.status
}
