// Command ghosttraffic records HTTP traffic and replays it against a baseline
// and a candidate backend to find behavioral and latency regressions.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/Hanart-prog/ghosttraffic/internal/capture"
	"github.com/Hanart-prog/ghosttraffic/internal/egress"
	"github.com/Hanart-prog/ghosttraffic/internal/metrics"
	"github.com/Hanart-prog/ghosttraffic/internal/replay"
	"github.com/Hanart-prog/ghosttraffic/internal/report"
	"github.com/Hanart-prog/ghosttraffic/internal/trace"
	"github.com/Hanart-prog/ghosttraffic/pkg/config"
)

// Set by the Makefile through -ldflags.
var (
	version = "dev"
	commit  = "unknown"
)

const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

const shutdownTimeout = 5 * time.Second

// maxConcurrency is a sanity bound: file descriptors run out long before it.
const maxConcurrency = 10000

const usage = `ghosttraffic records HTTP traffic and compares replays against two backends.

Usage:
  ghosttraffic <command> [flags]

Commands:
  capture   Record traffic through a reverse proxy into a trace file
  replay    Replay a trace against a baseline and a candidate
  validate  Check a trace file and summarize it
  report    Build a report from a replay run
  serve     Serve the local read-only API
  version   Print build information

Run "ghosttraffic <command> --help" for command flags.
`

type command func(ctx context.Context, args []string, stdout, stderr io.Writer) int

var commands = map[string]command{
	"capture":  runCapture,
	"replay":   runReplay,
	"validate": runValidate,
	"report":   runReport,
	"serve":    runServe,
	"version":  runVersion,
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}
	switch args[0] {
	case "help", "-h", "-help", "--help":
		fmt.Fprint(stdout, usage)
		return exitOK
	}
	cmd, ok := commands[args[0]]
	if !ok {
		fmt.Fprintf(stderr, "ghosttraffic: unknown command %q\n\n%s", args[0], usage)
		return exitUsage
	}
	return cmd(ctx, args[1:], stdout, stderr)
}

// newFlags returns a FlagSet whose help goes to stdout and whose parse errors
// go to stderr, as parse reports them.
func newFlags(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {} // parse prints help itself, to the right stream
	return fs
}

// parse handles --help and bad flags. done is true when the command must return
// code without running.
func parse(fs *flag.FlagSet, args []string, stdout io.Writer) (code int, done bool) {
	err := fs.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		fs.SetOutput(stdout)
		fmt.Fprintf(stdout, "Usage: ghosttraffic %s [flags]\n\n", fs.Name())
		fs.PrintDefaults()
		return exitOK, true
	}
	if err != nil {
		fmt.Fprintf(fs.Output(), "Run \"ghosttraffic %s --help\" for usage.\n", fs.Name())
		return exitUsage, true
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(fs.Output(), "ghosttraffic %s: unexpected argument %q\n", fs.Name(), fs.Arg(0))
		return exitUsage, true
	}
	return exitOK, false
}

func usageError(stderr io.Writer, name, format string, a ...any) int {
	fmt.Fprintf(stderr, "ghosttraffic %s: %s\n", name, fmt.Sprintf(format, a...))
	return exitUsage
}

// notImplemented ends a command whose flags are validated but whose engine has
// not been built. It exits nonzero so no script mistakes it for success.
func notImplemented(stderr io.Writer, name, phase string) int {
	fmt.Fprintf(stderr, "ghosttraffic %s: not implemented yet (planned for %s)\n", name, phase)
	return exitFailure
}

func validateListen(addr string) error {
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return fmt.Errorf("invalid --listen %q: %w", addr, err)
	}
	return nil
}

func runCapture(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlags("capture", stderr)
	listen := fs.String("listen", "127.0.0.1:9000", "address to accept traffic on")
	upstream := fs.String("upstream", "", "backend to forward traffic to (required)")
	out := fs.String("out", "", "trace file to write (required)")
	configPath := fs.String("config", "", "YAML config file (default: built-in safe defaults)")
	if code, done := parse(fs, args, stdout); done {
		return code
	}

	if err := validateListen(*listen); err != nil {
		return usageError(stderr, "capture", "%v", err)
	}
	if *upstream == "" {
		return usageError(stderr, "capture", "--upstream is required")
	}
	if *out == "" {
		return usageError(stderr, "capture", "--out is required")
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return usageError(stderr, "capture", "%v", err)
	}
	target, err := config.CheckTarget(*upstream, cfg.Replay.AllowHosts)
	if err != nil {
		return usageError(stderr, "capture", "%v", err)
	}
	if target.External {
		fmt.Fprintf(stderr, "WARNING: capturing traffic from non-local host %s. Never capture production traffic.\n", target.Host)
	}
	writer, err := trace.Create(*out)
	if err != nil {
		fmt.Fprintf(stderr, "ghosttraffic capture: %v\n", err)
		return exitFailure
	}
	defer writer.Close()

	proxy := capture.New(capture.Options{
		Upstream: target.URL,
		Transport: egress.NewClient(egress.Options{
			AllowHosts: cfg.Replay.AllowHosts,
			Timeout:    cfg.Replay.Timeout,
		}).Transport,
		Sink:            writer,
		AllowedHeaders:  cfg.Capture.AllowedHeaders,
		MaxBodyBytes:    cfg.Capture.MaxBodyBytes,
		RedactJSONPaths: cfg.Capture.RedactJSONPaths,
		ExcludePaths:    cfg.Capture.ExcludePaths,
		SessionHeader:   cfg.Capture.SessionHeader,
		Log:             stderr,
	})
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		fmt.Fprintf(stderr, "ghosttraffic capture: %v\n", err)
		return exitFailure
	}
	server := &http.Server{Handler: proxy, ReadHeaderTimeout: 10 * time.Second, ErrorLog: log.New(stderr, "", 0)}
	fmt.Fprintf(stderr, "capturing %s -> %s into %s; press Ctrl-C to stop\n", listener.Addr(), *upstream, *out)

	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	select {
	case err = <-served:
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		// A request still running at the deadline is cut off and recorded as aborted.
		if err = server.Shutdown(shutdown); err != nil {
			err = errors.Join(err, server.Close())
		}
	}
	stats := proxy.Stats()
	fmt.Fprintf(stderr, "recorded %d replayable, %d unreplayable; %d excluded, %d failed to record\n",
		stats.Recorded, stats.Unreplayable, stats.Excluded, stats.Failed)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(stderr, "ghosttraffic capture: %v\n", err)
		return exitFailure
	}
	if stats.Failed > 0 {
		return exitFailure
	}
	return exitOK
}

func runValidate(_ context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlags("validate", stderr)
	tracePath := fs.String("trace", "", "trace file to check (required)")
	if code, done := parse(fs, args, stdout); done {
		return code
	}
	if *tracePath == "" {
		return usageError(stderr, "validate", "--trace is required")
	}
	file, err := os.Open(*tracePath)
	if err != nil {
		fmt.Fprintf(stderr, "ghosttraffic validate: %v\n", err)
		return exitFailure
	}
	defer file.Close()
	var total, replayable int
	err = trace.Read(file, func(r trace.Record) error {
		total++
		if r.Replayable {
			replayable++
		}
		return nil
	})
	if err != nil {
		fmt.Fprintf(stderr, "ghosttraffic validate: %s: %v\n", *tracePath, err)
		return exitFailure
	}
	fmt.Fprintf(stdout, "%s: %d records valid, %d replayable\n", *tracePath, total, replayable)
	return exitOK
}

type stringList []string

func (l *stringList) String() string     { return strings.Join(*l, ",") }
func (l *stringList) Set(v string) error { *l = append(*l, v); return nil }

func runReplay(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlags("replay", stderr)
	tracePath := fs.String("trace", "", "trace file to replay (required)")
	baseline := fs.String("baseline", "", "baseline target URL (required)")
	candidate := fs.String("candidate", "", "candidate target URL (required)")
	concurrency := fs.Int("concurrency", 50, "maximum in-flight requests")
	speed := fs.Float64("speed", 1, "time scale: 2 replays twice as fast as recorded")
	out := fs.String("out", "", "run directory to create (required)")
	configPath := fs.String("config", "", "YAML config file (default: built-in safe defaults)")
	captureBodies := fs.Bool("capture-bodies", false, "store sanitized response JSON in results")
	allowExternalMutation := fs.Bool("allow-external-mutation", false, "permit non-GET requests to a non-local target")
	var allowHosts stringList
	fs.Var(&allowHosts, "allow-host", "permit a non-local target host; repeatable")
	if code, done := parse(fs, args, stdout); done {
		return code
	}

	if *tracePath == "" {
		return usageError(stderr, "replay", "--trace is required")
	}
	if *out == "" {
		return usageError(stderr, "replay", "--out is required")
	}
	if *concurrency < 1 || *concurrency > maxConcurrency {
		return usageError(stderr, "replay", "--concurrency must be between 1 and %d", maxConcurrency)
	}
	// The negated form also rejects NaN.
	if !(*speed > 0) {
		return usageError(stderr, "replay", "--speed must be positive")
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return usageError(stderr, "replay", "%v", err)
	}
	allowed := slices.Concat(cfg.Replay.AllowHosts, []string(allowHosts))
	var targets []config.Target
	for _, flag := range []struct{ name, raw string }{{"--baseline", *baseline}, {"--candidate", *candidate}} {
		if flag.raw == "" {
			return usageError(stderr, "replay", "%s is required", flag.name)
		}
		target, err := config.CheckTarget(flag.raw, allowed)
		if err != nil {
			return usageError(stderr, "replay", "%s: %v", flag.name, err)
		}
		targets = append(targets, target)
	}
	if targets[0].SameBackend(targets[1]) {
		return usageError(stderr, "replay", "--baseline and --candidate must be different backends")
	}
	for _, target := range targets {
		if target.External {
			fmt.Fprintf(stderr, "WARNING: recorded traffic will be sent to non-local host %s. Never point replay at production.\n", target.Host)
		}
	}
	workload, err := replay.LoadWorkload(*tracePath)
	if err != nil {
		fmt.Fprintf(stderr, "ghosttraffic replay: %v\n", err)
		return exitFailure
	}
	router, err := metrics.NewRouter(cfg.Replay.Routes)
	if err != nil {
		return usageError(stderr, "replay", "%v", err)
	}
	client := egress.NewClient(egress.Options{AllowHosts: allowed, Timeout: cfg.Replay.Timeout, MaxConns: *concurrency})
	outcome, err := replay.Execute(ctx, replay.Options{
		Workload:              workload,
		Baseline:              targets[0],
		Candidate:             targets[1],
		Client:                client,
		Router:                router,
		OutDir:                *out,
		Concurrency:           *concurrency,
		QueueLimit:            cfg.Replay.QueueLimit,
		Speed:                 *speed,
		Timeout:               cfg.Replay.Timeout,
		MaxResponseBytes:      cfg.Replay.MaxResponseBytes,
		CaptureBodies:         *captureBodies,
		AllowExternalMutation: *allowExternalMutation,
		Build:                 report.Build{Version: version, Commit: commit},
	})
	printOutcome(stdout, stderr, outcome)
	if err != nil {
		fmt.Fprintf(stderr, "ghosttraffic replay: %v\n", err)
		return exitFailure
	}
	return exitOK
}

func printOutcome(stdout, stderr io.Writer, outcome replay.Outcome) {
	for _, warning := range outcome.Run.Warnings {
		fmt.Fprintf(stderr, "WARNING: %s\n", warning)
	}
	for _, name := range []string{"baseline", "candidate"} {
		stats, ok := outcome.Stats[name]
		if !ok {
			continue
		}
		fmt.Fprintf(stdout, "%-9s %d requests: %d ok, %d errors (%d transport, %d never sent); response time p50 %s p95 %s; %.1f req/s wall-clock\n",
			name, stats.Total, stats.Successes, stats.Errors, stats.TransportErrors, stats.Dropped,
			formatMS(stats.ResponseTime.P50MS), formatMS(stats.ResponseTime.P95MS), stats.ThroughputRPS)
	}
}

func formatMS(v *float64) string {
	if v == nil {
		return "n/a"
	}
	return fmt.Sprintf("%.2fms", *v)
}

func runReport(_ context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlags("report", stderr)
	runDir := fs.String("run", "", "run directory to read (required)")
	format := fs.String("format", "html,json", "comma-separated output formats: html, json")
	if code, done := parse(fs, args, stdout); done {
		return code
	}

	if *runDir == "" {
		return usageError(stderr, "report", "--run is required")
	}
	for _, f := range strings.Split(*format, ",") {
		if f != "html" && f != "json" {
			return usageError(stderr, "report", "unknown format %q (want html, json)", f)
		}
	}
	return notImplemented(stderr, "report", "Phase 3")
}

func runServe(_ context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlags("serve", stderr)
	runsDir := fs.String("runs-dir", "runs", "directory containing run directories")
	listen := fs.String("listen", "127.0.0.1:7070", "address to serve on")
	if code, done := parse(fs, args, stdout); done {
		return code
	}

	if *runsDir == "" {
		return usageError(stderr, "serve", "--runs-dir must not be empty")
	}
	if err := validateListen(*listen); err != nil {
		return usageError(stderr, "serve", "%v", err)
	}
	return notImplemented(stderr, "serve", "Phase 5")
}

func runVersion(_ context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlags("version", stderr)
	if code, done := parse(fs, args, stdout); done {
		return code
	}
	fmt.Fprintf(stdout, "ghosttraffic %s (commit %s, %s, %s/%s)\n",
		version, commit, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	return exitOK
}
