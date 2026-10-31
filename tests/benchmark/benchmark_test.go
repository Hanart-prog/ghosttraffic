//go:build benchmark

// Package benchmark measures the built binary from the outside. Numbers are for one
// host and are written up, with their method, in docs/benchmarks.md.
package benchmark

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Hanart-prog/ghosttraffic/internal/metrics"
)

const (
	binary      = "../../bin/ghosttraffic"
	repetitions = 3
)

func target(delay time.Duration) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if delay > 0 {
			time.Sleep(delay)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	}))
}

// quantile uses the product's own nearest-rank method, so numbers compare with reports.
func quantile(sorted []float64, perMille int) float64 {
	if value := metrics.Quantile(sorted, perMille); value != nil {
		return *value
	}
	return 0
}

func median(values []float64) float64 {
	sorted := slices.Sorted(slices.Values(values))
	return sorted[len(sorted)/2]
}

func writeTrace(t *testing.T, path string, rate, seconds int) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	out := bufio.NewWriter(file)
	defer out.Flush()
	for i := range rate * seconds {
		fmt.Fprintf(out, `{"schema_version":1,"request_id":"req_%06d","session_id":"s_%012x","arrival_offset_ms":%d,"method":"GET","path":"/item/%d","headers":{"accept":"application/json"},"body_json":null,"capture":{"status":200,"duration_ms":1},"replayable":true}`+"\n",
			i+1, i%64, i*1000/rate, i%100)
	}
}

type pass struct {
	rate, p50, p99, max, errors float64
}

func replay(t *testing.T, dir string, rate int) pass {
	t.Helper()
	base, cand := target(time.Millisecond), target(time.Millisecond)
	defer base.Close()
	defer cand.Close()
	trace, run := filepath.Join(dir, "t.jsonl"), filepath.Join(dir, "run")
	os.RemoveAll(run)
	writeTrace(t, trace, rate, 5)
	cmd := exec.Command(binary, "replay", "--trace", trace, "--baseline", base.URL, "--candidate", cand.URL,
		"--concurrency", "500", "--out", run)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("replay: %v\n%s", err, out)
	}
	file, err := os.Open(filepath.Join(run, "results.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var delays []float64
	var last float64
	var errors float64
	scanner := bufio.NewScanner(file)
	scanner.Buffer(nil, 1<<24)
	for scanner.Scan() {
		var r struct {
			Target string  `json:"target"`
			Start  float64 `json:"actual_start_offset_ms"`
			Delay  float64 `json:"scheduling_delay_ms"`
			Error  string  `json:"error_category"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		if r.Target != "baseline" {
			continue
		}
		delays = append(delays, r.Delay)
		last = max(last, r.Start)
		if r.Error != "none" {
			errors++
		}
	}
	slices.Sort(delays)
	return pass{
		rate:   float64(len(delays)) / (last / 1000),
		p50:    quantile(delays, 500),
		p99:    quantile(delays, 990),
		max:    delays[len(delays)-1],
		errors: errors,
	}
}

func TestReplayPacing(t *testing.T) {
	dir := t.TempDir()
	fmt.Printf("\n### Replay pacing\n\n| Offered req/s | Achieved req/s | Delay p50 ms | Delay p99 ms | Delay max ms | Errors |\n| --- | --- | --- | --- | --- | --- |\n")
	for _, rate := range []int{500, 1000, 2000, 5000, 10000} {
		var rates, p50s, p99s, maxes, errors []float64
		for range repetitions {
			p := replay(t, dir, rate)
			rates, p50s, p99s = append(rates, p.rate), append(p50s, p.p50), append(p99s, p.p99)
			maxes, errors = append(maxes, p.max), append(errors, p.errors)
		}
		fmt.Printf("| %d | %.0f | %.2f | %.2f | %.1f | %.0f |\n", rate, median(rates), median(p50s), median(p99s), median(maxes), median(errors))
	}
}

// load sends total requests over workers connections and returns sorted milliseconds.
func load(t *testing.T, url string, workers, total int) ([]float64, float64) {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: workers, DisableCompression: true}}
	defer client.CloseIdleConnections()
	timings := make([][]float64, workers)
	var wg sync.WaitGroup
	start := time.Now()
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range total / workers {
				began := time.Now()
				resp, err := client.Get(url)
				if err != nil {
					t.Error(err)
					return
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				timings[w] = append(timings[w], float64(time.Since(began).Microseconds())/1000)
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start).Seconds()
	all := slices.Concat(timings...)
	slices.Sort(all)
	return all, float64(len(all)) / elapsed
}

func TestCaptureOverhead(t *testing.T) {
	const total = 40000
	fmt.Printf("\n### Capture overhead\n\n| Connections | Path | p50 ms | p99 ms | req/s |\n| --- | --- | --- | --- | --- |\n")
	for _, workers := range []int{1, 16} {
		for _, name := range []string{"direct", "through capture"} {
			var p50s, p99s, rates []float64
			for i := range repetitions {
				upstream := target(0)
				url := upstream.URL + "/item/1"
				var proxy *exec.Cmd
				if name != "direct" {
					listener, err := net.Listen("tcp", "127.0.0.1:0")
					if err != nil {
						t.Fatal(err)
					}
					addr := listener.Addr().String()
					listener.Close()
					proxy = exec.Command(binary, "capture", "--listen", addr, "--upstream", upstream.URL,
						"--out", filepath.Join(t.TempDir(), fmt.Sprintf("c%d.jsonl", i)))
					if err := proxy.Start(); err != nil {
						t.Fatal(err)
					}
					for range 200 {
						conn, err := net.Dial("tcp", addr)
						if err == nil {
							conn.Close()
							break
						}
						time.Sleep(5 * time.Millisecond)
					}
					url = "http://" + addr + "/item/1"
				}
				timings, rate := load(t, url, workers, total)
				p50s, p99s, rates = append(p50s, quantile(timings, 500)), append(p99s, quantile(timings, 990)), append(rates, rate)
				if proxy != nil {
					proxy.Process.Signal(syscall.SIGTERM)
					proxy.Wait()
				}
				upstream.Close()
			}
			fmt.Printf("| %d | %s | %.3f | %.3f | %.0f |\n", workers, name, median(p50s), median(p99s), median(rates))
		}
	}
}

func TestReportCost(t *testing.T) {
	dir := t.TempDir()
	replay(t, dir, 10000)
	run := filepath.Join(dir, "run")
	info, err := os.Stat(filepath.Join(run, "results.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines, err := os.ReadFile(filepath.Join(run, "results.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var seconds, rss []float64
	for range repetitions {
		cmd := exec.Command(binary, "report", "--run", run, "--format", "html,json")
		start := time.Now()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("report: %v\n%s", err, out)
		}
		seconds = append(seconds, time.Since(start).Seconds())
		peak := float64(cmd.ProcessState.SysUsage().(*syscall.Rusage).Maxrss)
		if runtime.GOOS == "linux" {
			peak *= 1024
		}
		rss = append(rss, peak/(1<<20))
	}
	fmt.Printf("\n### Report cost\n\n| Results | File MB | Seconds | Peak RSS MB |\n| --- | --- | --- | --- |\n| %d | %.1f | %.2f | %.0f |\n",
		strings.Count(string(lines), "\n"), float64(info.Size())/(1<<20), median(seconds), median(rss))
}
