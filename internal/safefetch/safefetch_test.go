package safefetch

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fixedResolver returns the configured IPs regardless of hostname —
// lets tests assert what the SSRF guard does for any IP class without
// touching real DNS.
func fixedResolver(ips ...string) func(ctx context.Context, host string) ([]net.IP, error) {
	parsed := make([]net.IP, len(ips))
	for i, s := range ips {
		parsed[i] = net.ParseIP(s)
	}
	return func(_ context.Context, _ string) ([]net.IP, error) { return parsed, nil }
}

func TestFetchRejectsNonHTTPSchemes(t *testing.T) {
	for _, u := range []string{"file:///etc/passwd", "gopher://x.com", "ftp://x.com"} {
		_, _, err := Fetch(context.Background(), u, Options{})
		if !errors.Is(err, ErrUnsupportedScheme) {
			t.Errorf("%s: expected ErrUnsupportedScheme, got %v", u, err)
		}
	}
}

func TestFetchRejectsPrivateIPs(t *testing.T) {
	cases := []string{
		"10.0.0.1", "172.16.0.1", "192.168.1.1", // RFC1918
		"127.0.0.1",       // loopback
		"169.254.169.254", // AWS metadata / link-local
		"0.0.0.0",         // unspecified
		"::1",             // IPv6 loopback
	}
	for _, ip := range cases {
		opts := Options{Resolver: fixedResolver(ip)}
		_, _, err := Fetch(context.Background(), "https://example.com", opts)
		if !errors.Is(err, ErrUnsafeURL) {
			t.Errorf("%s: expected ErrUnsafeURL, got %v", ip, err)
		}
	}
}

func TestFetchAllowsPublicIPs(t *testing.T) {
	// Set up an httptest server, point the resolver at a public-looking
	// IP, but redirect the actual HTTP transport at the test server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "hello world")
	}))
	defer srv.Close()
	// httptest binds to 127.0.0.1 which would normally fail. Bypass
	// the resolver check by pointing the resolver at a public IP for
	// the request URL, then hit the test server directly via its URL
	// (which the resolver also gets called on — chain the fixed
	// resolver to either return 8.8.8.8 or fall through to default).
	// Easier: just hit srv.URL with a public-IP resolver since the
	// validateHost call uses the resolver, not direct dialing.
	_, _, err := Fetch(context.Background(), srv.URL, Options{Resolver: fixedResolver("8.8.8.8")})
	if err != nil {
		t.Fatalf("public IP should pass: %v", err)
	}
}

func TestFetchRespectsAllowlist(t *testing.T) {
	// Disallowed bare host: allowlist gate fires before any HTTP.
	_, _, err := Fetch(context.Background(), "https://example.com/x", Options{
		Resolver:  fixedResolver("8.8.8.8"),
		Allowlist: []string{"github.com", "arxiv.org"},
	})
	if err == nil || !strings.Contains(err.Error(), "not in allowlist") {
		t.Errorf("disallowed host should error: %v", err)
	}

	// Suffix-allowed: api.github.com is under github.com. Use an
	// httptest server to satisfy the actual fetch — we're only
	// testing that the gate doesn't reject "api.github.com" given
	// allowlist=["github.com"]. The URL host is what gets checked,
	// not what the resolver returns. We avoid a real DNS hit by
	// using a synthetic URL whose host directly matches.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()
	// The httptest URL is http://127.0.0.1:PORT. We can't make the
	// allowlist accept "127.0.0.1", and we don't want to weaken the
	// allowlist check. Instead just verify the allowlist function
	// directly on the validator level.
	if err := validateURL("https://api.github.com/repos/x/y", []string{"github.com"}); err != nil {
		t.Errorf("api.github.com should suffix-match github.com: %v", err)
	}
	if err := validateURL("https://evil.com/x", []string{"github.com"}); err == nil {
		t.Errorf("evil.com should not match github.com allowlist")
	}
}

func TestFetchEnforcesSizeCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, 5000)) // 5KB
	}))
	defer srv.Close()
	opts := Options{
		Resolver: fixedResolver("8.8.8.8"),
		MaxBytes: 100, // way under
	}
	_, _, err := Fetch(context.Background(), srv.URL, opts)
	if err == nil || !strings.Contains(err.Error(), "cap") {
		t.Fatalf("oversize response should be rejected: %v", err)
	}
}

func TestFetchBlocksRedirectToPrivateIP(t *testing.T) {
	// A server can redirect to an internal address to bypass the
	// initial check. CheckRedirect re-runs the IP guard.
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "should not reach")
	}))
	defer target.Close()
	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Redirect to a different hostname; resolver below will
		// pretend that hostname resolves to 192.168.x.x → blocked.
		http.Redirect(w, r, "http://internal.example/", http.StatusFound)
	}))
	defer redir.Close()
	// Resolver: initial hop public, redirect hop private.
	resolver := func(_ context.Context, host string) ([]net.IP, error) {
		if host == "internal.example" {
			return []net.IP{net.ParseIP("192.168.1.1")}, nil
		}
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	}
	_, _, err := Fetch(context.Background(), redir.URL, Options{Resolver: resolver})
	if err == nil {
		t.Fatal("redirect to private IP should be blocked")
	}
	if !strings.Contains(err.Error(), "blocked") && !errors.Is(err, ErrUnsafeURL) {
		t.Errorf("error should mention block: %v", err)
	}
}

func TestFetchHTTPErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	_, _, err := Fetch(context.Background(), srv.URL, Options{Resolver: fixedResolver("8.8.8.8")})
	if err == nil {
		t.Fatal("4xx should be an error")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("status code should appear: %v", err)
	}
}
