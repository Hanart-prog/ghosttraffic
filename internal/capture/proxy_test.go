package capture

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hanart-prog/ghosttraffic/internal/trace"
)

type memorySink struct {
	mu      sync.Mutex
	records []trace.Record
	err     error
}

func (s *memorySink) Write(r trace.Record) error {
	if err := r.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.records = append(s.records, r)
	return nil
}

func (s *memorySink) all() []trace.Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]trace.Record(nil), s.records...)
}

type fixture struct {
	proxy *Proxy
	url   string
	sink  *memorySink
}

func start(t *testing.T, upstream http.Handler, change func(*Options)) *fixture {
	t.Helper()
	backend := httptest.NewServer(upstream)
	t.Cleanup(backend.Close)
	target, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatal(err)
	}
	sink := &memorySink{}
	opts := Options{
		Upstream:       target,
		Transport:      &http.Transport{},
		Sink:           sink,
		AllowedHeaders: []string{"Accept", "Content-Type"},
		MaxBodyBytes:   1 << 10,
		SessionHeader:  "X-GhostTraffic-Session",
	}
	if change != nil {
		change(&opts)
	}
	proxy := New(opts)
	front := httptest.NewServer(proxy)
	t.Cleanup(front.Close)
	return &fixture{proxy: proxy, url: front.URL, sink: sink}
}

func (f *fixture) only(t *testing.T) trace.Record {
	t.Helper()
	// The handler's deferred write can land just after the client has its response.
	deadline := time.Now().Add(2 * time.Second)
	for len(f.sink.all()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	records := f.sink.all()
	if len(records) != 1 {
		t.Fatalf("got %d records", len(records))
	}
	return records[0]
}

func post(t *testing.T, target, contentType string, body io.Reader) *http.Response {
	t.Helper()
	resp, err := http.Post(target, contentType, body)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestRecordsASanitizedRequest(t *testing.T) {
	var got struct {
		auth, cookie, body, query string
	}
	f := start(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		got.auth, got.cookie, got.body, got.query = r.Header.Get("Authorization"), r.Header.Get("Cookie"), string(data), r.URL.RawQuery
		w.WriteHeader(http.StatusCreated)
	}), func(o *Options) { o.RedactJSONPaths = []string{"$.customer.email"} })

	const body = `{"customer":{"email":"a@example.test","password":"hunter2"},"qty":3}`
	req, err := http.NewRequest("POST", f.url+"/api/x?q=shoe&access_token=abc", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sekrit")
	req.Header.Set("Cookie", "sid=sekrit")
	req.Header.Set("X-GhostTraffic-Session", "user-session-value")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if got.auth != "Bearer sekrit" || got.cookie != "sid=sekrit" || got.body != body || got.query != "q=shoe&access_token=abc" {
		t.Errorf("upstream must see the original request: %+v", got)
	}
	rec := f.only(t)
	if rec.RequestID != "req_000001" || rec.ArrivalOffsetMS != 0 || rec.Method != "POST" || rec.Capture.Status != 201 || !rec.Replayable {
		t.Errorf("record = %+v", rec)
	}
	if rec.Path != "/api/x?q=shoe&access_token=[REDACTED]" {
		t.Errorf("path = %q", rec.Path)
	}
	if want := `{"customer":{"email":"[REDACTED]","password":"[REDACTED]"},"qty":3}`; string(rec.BodyJSON) != want {
		t.Errorf("body = %s", rec.BodyJSON)
	}
	if !strings.HasPrefix(rec.SessionID, "s_") {
		t.Errorf("session = %q", rec.SessionID)
	}
	line, err := trace.MarshalLine(rec)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"sekrit", "hunter2", "a@example.test", "user-session-value", "abc"} {
		if bytes.Contains(line, []byte(secret)) {
			t.Errorf("trace contains %q: %s", secret, line)
		}
	}
}

func TestResponseIsPassedThroughUnchanged(t *testing.T) {
	payload := bytes.Repeat([]byte{0, 1, 2, 255}, 100_000)
	f := start(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Upstream", "yes")
		w.Header().Add("Set-Cookie", "a=1")
		w.Header().Add("Set-Cookie", "b=2")
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write(payload)
	}), nil)
	resp, err := http.Get(f.url + "/x")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusTeapot || resp.Header.Get("X-Upstream") != "yes" || len(resp.Header["Set-Cookie"]) != 2 {
		t.Errorf("status %d, headers %v", resp.StatusCode, resp.Header)
	}
	if sha256.Sum256(got) != sha256.Sum256(payload) {
		t.Error("response body altered")
	}
	if rec := f.only(t); rec.Capture.Status != http.StatusTeapot || rec.BodyJSON != nil {
		t.Errorf("record = %+v", rec)
	}
}

func TestRedirectIsReturnedNotFollowed(t *testing.T) {
	f := start(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.invalid/", http.StatusFound)
	}), nil)
	client := http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(f.url + "/x")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestStreamingResponsesAreNotBuffered(t *testing.T) {
	release := make(chan struct{})
	f := start(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("first\n"))
		w.(http.Flusher).Flush()
		<-release
		_, _ = w.Write([]byte("second\n"))
	}), nil)
	resp, err := http.Get(f.url + "/x")
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// Headers and the first chunk arrive while the handler is still blocked.
	buf := make([]byte, 6)
	if _, err := io.ReadFull(resp.Body, buf); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	if string(buf) != "first\n" {
		t.Fatalf("got %q", buf)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
}

func echoBody(w http.ResponseWriter, r *http.Request) {
	data, _ := io.ReadAll(r.Body)
	_, _ = w.Write(data)
}

func TestOversizedBodyIsForwardedButNotKept(t *testing.T) {
	f := start(t, http.HandlerFunc(echoBody), nil)
	body := strings.Repeat("x", 1<<20)
	resp := post(t, f.url+"/x", "application/json", strings.NewReader(`"`+body+`"`))
	echoed, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(echoed) != len(body)+2 {
		t.Errorf("upstream received %d bytes", len(echoed))
	}
	rec := f.only(t)
	if rec.Replayable || !rec.BodyTruncated || rec.BodyJSON != nil {
		t.Errorf("record = %+v", rec)
	}
}

func TestChunkedOversizedBodyIsTruncated(t *testing.T) {
	f := start(t, http.HandlerFunc(echoBody), nil)
	reader, writer := io.Pipe()
	go func() {
		_, _ = writer.Write(bytes.Repeat([]byte("x"), 1<<12))
		writer.Close()
	}()
	post(t, f.url+"/x", "application/json", reader)
	if rec := f.only(t); !rec.BodyTruncated || rec.Replayable {
		t.Errorf("record = %+v", rec)
	}
}

func TestUnreplayableBodies(t *testing.T) {
	tests := []struct {
		name, contentType, body, want string
	}{
		{"multipart", "multipart/form-data; boundary=x", "--x--", "not JSON"},
		{"binary", "application/octet-stream", "\x00\x01", "not JSON"},
		{"form", "application/x-www-form-urlencoded", "password=hunter2", "not JSON"},
		{"no content type", "", "{}", "not JSON"},
		{"malformed json", "application/json", `{"password":`, "valid JSON"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := start(t, http.HandlerFunc(echoBody), nil)
			post(t, f.url+"/x", tt.contentType, strings.NewReader(tt.body))
			rec := f.only(t)
			if rec.Replayable || !strings.Contains(rec.UnreplayableReason, tt.want) || rec.BodyJSON != nil {
				t.Errorf("record = %+v", rec)
			}
		})
	}
}

func TestJSONVariantsAreRecorded(t *testing.T) {
	f := start(t, http.HandlerFunc(echoBody), nil)
	post(t, f.url+"/x", "application/vnd.api+json; charset=utf-8", strings.NewReader(`{"a":1}`))
	if rec := f.only(t); !rec.Replayable || string(rec.BodyJSON) != `{"a":1}` {
		t.Errorf("record = %+v", rec)
	}
}

func TestPartiallyReadBodyIsUnreplayable(t *testing.T) {
	f := start(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.CopyN(io.Discard, r.Body, 1)
		w.WriteHeader(http.StatusRequestEntityTooLarge)
	}), func(o *Options) { o.MaxBodyBytes = 1 << 25 })
	// Far past what the upstream's server drains and sockets buffer: near those
	// limits the whole body sometimes gets through, making this a race.
	post(t, f.url+"/x", "application/json", strings.NewReader(`{"a":"`+strings.Repeat("x", 1<<24)+`"}`))
	rec := f.only(t)
	if rec.Replayable || rec.UnreplayableReason != "upstream did not read the whole body" {
		t.Errorf("replayable = %v, reason = %q", rec.Replayable, rec.UnreplayableReason)
	}
}

func TestUpstreamDownRecordsNoStatus(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dead := "http://" + listener.Addr().String()
	listener.Close()
	f := start(t, http.NotFoundHandler(), func(o *Options) { o.Upstream, _ = url.Parse(dead) })

	resp, err := http.Get(f.url + "/x")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d", resp.StatusCode)
	}
	if rec := f.only(t); rec.Capture.Status != 0 {
		t.Errorf("record = %+v", rec)
	}
}

func TestExcludedPathsAreForwardedButNotRecorded(t *testing.T) {
	f := start(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}), func(o *Options) { o.ExcludePaths = []string{"/admin"} })
	resp, err := http.Get(f.url + "/admin/users")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status = %d", resp.StatusCode)
	}
	if len(f.sink.all()) != 0 || f.proxy.Stats().Excluded != 1 {
		t.Errorf("records = %v, stats = %+v", f.sink.all(), f.proxy.Stats())
	}
}

func TestSinkFailureDoesNotFailTheRequest(t *testing.T) {
	f := start(t, http.NotFoundHandler(), func(o *Options) { o.Log = io.Discard })
	f.sink.err = errors.New("disk full")
	resp, err := http.Get(f.url + "/x")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d", resp.StatusCode)
	}
	deadline := time.Now().Add(2 * time.Second)
	for f.proxy.Stats().Failed == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if f.proxy.Stats().Failed != 1 {
		t.Errorf("stats = %+v", f.proxy.Stats())
	}
}

func TestClientAbortIsStillAccountedFor(t *testing.T) {
	entered := make(chan struct{})
	f := start(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("x"), 1<<16))
		w.(http.Flusher).Flush()
		close(entered)
		<-time.After(50 * time.Millisecond)
		panic(http.ErrAbortHandler)
	}), nil)
	resp, err := http.Get(f.url + "/x")
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if rec := f.only(t); rec.Capture.Status != http.StatusOK {
		t.Errorf("record = %+v", rec)
	}
}

func TestConcurrentRequestsGetUniqueOrderedRecords(t *testing.T) {
	f := start(t, http.HandlerFunc(echoBody), nil)
	const total = 200
	// macOS defaults to a listen backlog of 128, so a larger burst of new
	// connections is reset by the kernel before any server sees it.
	gate := make(chan struct{}, 64)
	var wg sync.WaitGroup
	for i := range total {
		wg.Add(1)
		go func() {
			defer wg.Done()
			gate <- struct{}{}
			defer func() { <-gate }()
			body := fmt.Sprintf(`{"n":%d}`, i)
			resp, err := http.Post(f.url+"/x", "application/json", strings.NewReader(body))
			if err != nil {
				t.Error(err)
				return
			}
			defer resp.Body.Close()
			if echoed, _ := io.ReadAll(resp.Body); string(echoed) != body {
				t.Errorf("echoed %q, want %q", echoed, body)
			}
		}()
	}
	wg.Wait()
	deadline := time.Now().Add(2 * time.Second)
	for len(f.sink.all()) < total && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	records := f.sink.all()
	if len(records) != total {
		t.Fatalf("got %d records", len(records))
	}
	ids, bodies := map[string]bool{}, map[string]bool{}
	for _, r := range records {
		ids[r.RequestID] = true
		var doc struct{ N int }
		if err := json.Unmarshal(r.BodyJSON, &doc); err != nil {
			t.Fatal(err)
		}
		bodies[string(r.BodyJSON)] = true
	}
	if len(ids) != total || len(bodies) != total {
		t.Errorf("%d unique IDs and %d unique bodies of %d", len(ids), len(bodies), total)
	}
}

func TestArrivalOffsetsMeasureRealGaps(t *testing.T) {
	f := start(t, http.NotFoundHandler(), nil)
	for range 2 {
		resp, err := http.Get(f.url + "/x")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		time.Sleep(30 * time.Millisecond)
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(f.sink.all()) < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	byID := map[string]int64{}
	for _, r := range f.sink.all() {
		byID[r.RequestID] = r.ArrivalOffsetMS
	}
	if byID["req_000001"] != 0 || byID["req_000002"] < 30 {
		t.Fatalf("offsets = %v", byID)
	}
}
