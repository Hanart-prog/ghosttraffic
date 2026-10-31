// Package egress builds the only HTTP client replay may use. It enforces target
// policy on resolved addresses at dial time, because a name check alone cannot
// see where DNS sends a connection (ADR 0005).
package egress

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"syscall"
	"time"

	"github.com/Hanart-prog/ghosttraffic/pkg/config"
)

// dockerRanges are Docker's default address pools. 10.0.0.0/8 is left out: it
// is the usual corporate range, where a short name like "prod" would resolve.
var dockerRanges = []netip.Prefix{
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
}

// Check reports whether a connection for host may reach addr. Loopback is always
// permitted. Link-local (including cloud metadata) and unspecified addresses are
// never permitted, even for allowlisted hosts.
func Check(host string, addr netip.Addr, allowHosts []string) error {
	addr = addr.Unmap()
	if addr.IsLoopback() {
		return nil
	}
	if addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsUnspecified() || addr.IsMulticast() {
		return fmt.Errorf("egress: %s resolved to forbidden address %s", host, addr)
	}
	for _, allowed := range allowHosts {
		if strings.EqualFold(allowed, host) {
			return nil
		}
	}
	if host != "localhost" && config.IsComposeService(host) {
		for _, prefix := range dockerRanges {
			if prefix.Contains(addr) {
				return nil
			}
		}
	}
	return fmt.Errorf("egress: %s resolved to non-local address %s", host, addr)
}

// Options configures NewClient.
type Options struct {
	AllowHosts []string
	Timeout    time.Duration
	MaxConns   int
}

// NewClient returns a client that checks every dialed address, does not follow
// redirects, and ignores proxy environment variables, which would otherwise
// carry traffic past the dial check. Compression is off so that bodies and
// timings reflect the target, not client decoding.
func NewClient(opts Options) *http.Client {
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		d := net.Dialer{
			Timeout: opts.Timeout,
			Control: func(_, resolved string, _ syscall.RawConn) error {
				ap, err := netip.ParseAddrPort(resolved)
				if err != nil {
					return fmt.Errorf("egress: unparseable address %q: %w", resolved, err)
				}
				return Check(host, ap.Addr(), opts.AllowHosts)
			},
		}
		return d.DialContext(ctx, network, address)
	}
	return &http.Client{
		Timeout: opts.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			Proxy:               nil,
			DialContext:         dial,
			MaxConnsPerHost:     opts.MaxConns,
			MaxIdleConnsPerHost: opts.MaxConns,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  true,
		},
	}
}
