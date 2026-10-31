package egress

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCheck(t *testing.T) {
	tests := []struct {
		name  string
		host  string
		addr  string
		allow []string
		ok    bool
	}{
		{name: "loopback", host: "localhost", addr: "127.0.0.1", ok: true},
		{name: "mapped loopback", host: "localhost", addr: "::ffff:127.0.0.1", ok: true},
		{name: "ipv6 loopback", host: "localhost", addr: "::1", ok: true},
		{name: "compose bridge", host: "baseline", addr: "172.18.0.2", ok: true},
		{name: "compose user pool", host: "baseline", addr: "192.168.32.4", ok: true},
		{name: "allowlisted", host: "staging.example.com", addr: "8.8.8.8", allow: []string{"STAGING.example.com"}, ok: true},

		{name: "short name to public", host: "prod", addr: "8.8.8.8"},
		{name: "short name to corporate", host: "prod", addr: "10.1.2.3"},
		{name: "localhost rebound", host: "localhost", addr: "172.18.0.2"},
		{name: "dotted name to docker range", host: "svc.example.com", addr: "172.18.0.2"},
		{name: "metadata", host: "baseline", addr: "169.254.169.254"},
		{name: "metadata allowlisted", host: "meta", addr: "169.254.169.254", allow: []string{"meta"}},
		{name: "unspecified", host: "baseline", addr: "0.0.0.0"},
		{name: "ipv6 link-local", host: "baseline", addr: "fe80::1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Check(tt.host, netip.MustParseAddr(tt.addr), tt.allow)
			if (err == nil) != tt.ok {
				t.Fatalf("Check = %v, want ok=%v", err, tt.ok)
			}
		})
	}
}

func client() *http.Client {
	return NewClient(Options{Timeout: 2 * time.Second, MaxConns: 4})
}

func TestClientDoesNotFollowRedirects(t *testing.T) {
	var followed atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { followed.Store(true) }))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirector.Close()

	resp, err := client().Get(redirector.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want the 302 itself", resp.StatusCode)
	}
	if followed.Load() {
		t.Fatal("redirect was followed")
	}
}

func TestClientIgnoresProxyEnvironment(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://proxy.invalid:1")
	if client().Transport.(*http.Transport).Proxy != nil {
		t.Fatal("transport must not use a proxy")
	}
}

// 0.0.0.0 reaches the local machine on many systems, so the dial check itself
// must stop it; this proves Control runs on a real connection attempt.
func TestClientRefusesForbiddenAddressAtDial(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	url := strings.Replace(srv.URL, "127.0.0.1", "0.0.0.0", 1)
	_, err := client().Get(url)
	if err == nil || !strings.Contains(err.Error(), "forbidden address") {
		t.Fatalf("got %v", err)
	}
}
