// Package config loads GhostTraffic configuration and enforces its safety
// defaults. A missing setting always resolves to the safer behavior.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Hanart-prog/ghosttraffic/internal/capture"
	"github.com/Hanart-prog/ghosttraffic/internal/trace"
)

// Config is the on-disk YAML configuration. Unknown keys are rejected so a
// misspelled safety setting cannot be silently ignored.
type Config struct {
	Capture Capture `yaml:"capture"`
	Replay  Replay  `yaml:"replay"`
}

// Capture controls what the recorder may persist.
type Capture struct {
	// AllowedHeaders is an allowlist: headers not named here are never stored.
	AllowedHeaders  []string `yaml:"allowed_headers"`
	MaxBodyBytes    int64    `yaml:"max_body_bytes"`
	RedactJSONPaths []string `yaml:"redact_json_paths"`
	ExcludePaths    []string `yaml:"exclude_paths"`
}

// Replay controls how a trace is sent to targets.
type Replay struct {
	AllowHosts []string      `yaml:"allow_hosts"`
	Timeout    time.Duration `yaml:"timeout"`
	QueueLimit int           `yaml:"queue_limit"`
}

// Default returns the safe configuration used when no file is given.
func Default() Config {
	return Config{
		Capture: Capture{
			AllowedHeaders: []string{"Accept", "Content-Type"},
			MaxBodyBytes:   1 << 20,
		},
		Replay: Replay{
			Timeout:    30 * time.Second,
			QueueLimit: 10000,
		},
	}
}

// Load reads path over the defaults. An empty path returns the defaults.
func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("invalid %s: %w", path, err)
	}
	return cfg, nil
}

// Validate rejects settings that are unsafe or cannot work.
func (c Config) Validate() error {
	for _, h := range c.Capture.AllowedHeaders {
		if trace.IsSensitiveHeader(h) {
			return fmt.Errorf("capture.allowed_headers: %q is a credential or routing header and cannot be captured", h)
		}
	}
	for _, path := range c.Capture.RedactJSONPaths {
		if _, err := capture.ParsePath(path); err != nil {
			return fmt.Errorf("capture.redact_json_paths: %w", err)
		}
	}
	for _, path := range c.Capture.ExcludePaths {
		if !strings.HasPrefix(path, "/") {
			return fmt.Errorf("capture.exclude_paths: %q must start with /", path)
		}
	}
	if c.Capture.MaxBodyBytes <= 0 {
		return errors.New("capture.max_body_bytes must be positive")
	}
	if c.Replay.Timeout <= 0 {
		return errors.New("replay.timeout must be positive")
	}
	if c.Replay.QueueLimit <= 0 {
		return errors.New("replay.queue_limit must be positive")
	}
	return nil
}

// composeService matches a single-label hostname as used on a Compose network.
// The leading letter excludes numeric forms such as 134744072 or 0x08080808,
// which some resolvers accept as IPv4 addresses.
var composeService = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// Target is a validated replay target.
type Target struct {
	URL  *url.URL
	Host string
	// External is true when the host passed only because it was allowlisted.
	// Callers must warn before sending traffic to it.
	External bool
}

// SameBackend reports whether t and other address the same listener, treating
// all loopback spellings as one host. It cannot see two listeners sharing a
// database; ADR 0003 covers that.
func (t Target) SameBackend(other Target) bool {
	return t.backendKey() == other.backendKey()
}

func (t Target) backendKey() string {
	host := t.Host
	if addr, err := netip.ParseAddr(host); host == "localhost" || (err == nil && addr.Unmap().IsLoopback()) {
		host = "loopback"
	}
	port := t.URL.Port()
	if port == "" {
		port = map[string]string{"http": "80", "https": "443"}[t.URL.Scheme]
	}
	return net.JoinHostPort(host, port) + strings.TrimRight(t.URL.Path, "/")
}

// CheckTarget validates a replay target URL. Loopback addresses and
// single-label Compose service names pass by default; anything else must be
// named exactly in allowHosts. This check is on the name only: internal/egress
// enforces the resolved address and refuses redirects (ADR 0005).
func CheckTarget(raw string, allowHosts []string) (Target, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Target{}, fmt.Errorf("invalid target URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return Target{}, fmt.Errorf("target %q must use http or https", raw)
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return Target{}, fmt.Errorf("target %q has no host", raw)
	}
	if u.User != nil {
		return Target{}, errors.New("target URL must not contain credentials")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return Target{}, errors.New("target URL must not contain a query or fragment")
	}
	t := Target{URL: u, Host: host}
	if isLocal(host) {
		return t, nil
	}
	for _, allowed := range allowHosts {
		if strings.EqualFold(allowed, host) {
			t.External = true
			return t, nil
		}
	}
	return Target{}, fmt.Errorf("target host %q is not local; pass --allow-host %s only if you own it and it is not production", host, host)
}

func isLocal(host string) bool {
	if addr, err := netip.ParseAddr(host); err == nil {
		return addr.Unmap().IsLoopback()
	}
	return IsComposeService(host)
}

// IsComposeService reports whether host has the shape of a Compose service name.
func IsComposeService(host string) bool {
	return composeService.MatchString(host)
}
