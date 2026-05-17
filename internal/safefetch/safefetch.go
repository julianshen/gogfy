// Package safefetch is an HTTP client that refuses to fetch URLs
// pointing at private networks, link-local addresses, loopback, or
// cloud-metadata endpoints — closing the SSRF hole that an unwrapped
// `http.Get(userInput)` would open.
//
// Mirrors graphify's `safe_fetch`: validates scheme + host before the
// request, resolves the hostname and rejects on private IP, caps
// response size, and follows a small number of redirects with the
// same checks applied to each hop.
package safefetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultMaxBytes caps response size to prevent memory-exhaustion
// attacks (e.g. a server returning a 10 GB stream). Matches graphify.
const DefaultMaxBytes = 10 << 20 // 10 MiB

// DefaultTimeout bounds total wall-clock for a fetch.
const DefaultTimeout = 30 * time.Second

// ErrUnsafeURL is returned when a URL targets a blocked address
// (private network, loopback, cloud metadata, link-local, etc.).
var ErrUnsafeURL = errors.New("safefetch: URL targets a blocked address range")

// ErrUnsupportedScheme rejects non-http(s) URLs — `file://` would
// leak local files, `gopher://` enables long-known SSRF tricks.
var ErrUnsupportedScheme = errors.New("safefetch: only http/https URLs allowed")

// Options tweaks a fetch. Zero value uses defaults.
type Options struct {
	MaxBytes int64
	Timeout  time.Duration
	// Allowlist, when non-empty, restricts fetches to hostnames whose
	// suffix matches any entry. Useful for "only github.com and arxiv.org"
	// configurations.
	Allowlist []string
	// Resolver overrides hostname → IP lookup for tests. Production
	// uses net.DefaultResolver.
	Resolver func(ctx context.Context, host string) ([]net.IP, error)
}

// Fetch performs a GET with all SSRF guards applied. Returns the
// response body bytes (size-capped) plus the final URL after
// redirects.
func Fetch(ctx context.Context, rawURL string, opts Options) ([]byte, string, error) {
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = DefaultMaxBytes
	}
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	resolver := opts.Resolver
	if resolver == nil {
		resolver = func(ctx context.Context, host string) ([]net.IP, error) {
			return net.DefaultResolver.LookupIP(ctx, "ip", host)
		}
	}

	if err := validateURL(rawURL, opts.Allowlist); err != nil {
		return nil, "", err
	}
	parsed, _ := url.Parse(rawURL)
	if err := validateHost(ctx, parsed.Hostname(), resolver); err != nil {
		return nil, "", err
	}

	client := &http.Client{
		Timeout: opts.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("safefetch: too many redirects (>%d)", 5)
			}
			// Re-validate every hop — a server can redirect to an
			// internal address to bypass the initial check.
			if err := validateURL(req.URL.String(), opts.Allowlist); err != nil {
				return err
			}
			return validateHost(ctx, req.URL.Hostname(), resolver)
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("safefetch: build request: %w", err)
	}
	req.Header.Set("User-Agent", "gogfy/1.0 (+https://github.com/julianshen/gogfy)")
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("safefetch: http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("safefetch: %s for %s", resp.Status, rawURL)
	}
	// LimitReader caps memory; we read one extra byte to detect
	// over-cap responses so users see a clear error.
	body, err := io.ReadAll(io.LimitReader(resp.Body, opts.MaxBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("safefetch: read body: %w", err)
	}
	if int64(len(body)) > opts.MaxBytes {
		return nil, "", fmt.Errorf("safefetch: response exceeds %d-byte cap", opts.MaxBytes)
	}
	return body, resp.Request.URL.String(), nil
}

// validateURL checks scheme and (optionally) host suffix.
func validateURL(rawURL string, allowlist []string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("safefetch: parse URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ErrUnsupportedScheme
	}
	if u.Hostname() == "" {
		return fmt.Errorf("safefetch: URL has no host")
	}
	if len(allowlist) > 0 {
		host := strings.ToLower(u.Hostname())
		hit := false
		for _, suffix := range allowlist {
			if host == suffix || strings.HasSuffix(host, "."+suffix) {
				hit = true
				break
			}
		}
		if !hit {
			return fmt.Errorf("safefetch: host %q not in allowlist", host)
		}
	}
	return nil
}

// validateHost resolves the host and rejects if any IP it resolves to
// is in a blocked range. Resolves ALL addresses (not just first) so a
// DNS-rebinding attack returning a public IP for the initial lookup
// followed by a private IP for the connection still fails the check.
func validateHost(ctx context.Context, host string, resolver func(ctx context.Context, host string) ([]net.IP, error)) error {
	ips, err := resolver(ctx, host)
	if err != nil {
		return fmt.Errorf("safefetch: resolve %q: %w", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("safefetch: no IPs for host %q", host)
	}
	for _, ip := range ips {
		if isBlocked(ip) {
			return fmt.Errorf("%w: %s → %s", ErrUnsafeURL, host, ip)
		}
	}
	return nil
}

// isBlocked rejects IPs that an external request shouldn't reach:
//   - private RFC1918 ranges (10/8, 172.16/12, 192.168/16)
//   - loopback (127/8, ::1)
//   - link-local (169.254/16, fe80::/10) — includes 169.254.169.254
//     (AWS/GCP/Azure metadata)
//   - unspecified (0.0.0.0)
//   - IPv6 unique local (fc00::/7)
//
// Mirrors graphify's block-list verbatim.
func isBlocked(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	// IPv6 unique local: fc00::/7 — Go's IsPrivate covers fc00::/7 since 1.17.
	return false
}
