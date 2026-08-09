package downloader

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	// v1.8.2 declares `package tls` — alias to utls for readability.
	utls "github.com/refraction-networking/utls"
)

// UseUTLSTransport opts the download transport into browser-grade TLS
// impersonation (JA3/JA4) so cf_clearance cookies from the user's browser
// pass Cloudflare managed-challenge zones. Enabled via MOXIE_UTLS=1 or
// SetUTLSTransport. The default stdlib transport remains the primary path.
var UseUTLSTransport = os.Getenv("MOXIE_UTLS") == "1"

// ErrCFChallengeStale signals a Cloudflare challenge despite browser-grade
// TLS: the cf_clearance cookie is stale, bound to a different UA, or the
// zone demands more than a fingerprint match. The caller should re-import
// cookies or fall back to a browser path.
var ErrCFChallengeStale = errors.New("Cloudflare challenge received (cf_clearance stale or TLS fingerprint rejected) — re-import browser cookies or use the browser fallback")

// utlsProfileKey carries the impersonation profile through the request
// context to the dialer.
type utlsProfileKey struct{}

// sharedUTLSTransport is lazily built once UseUTLSTransport is on.
var sharedUTLSTransport *http.Transport

// SetUTLSTransport flips the transport choice at runtime (tests, wiring).
func SetUTLSTransport(on bool) {
	UseUTLSTransport = on
}

// testTransportOverride, when set, replaces the shared transport in
// downloadWithHeaders (tests only — lets a test client dial an httptest TLS
// server through a public-looking hostname).
var testTransportOverride *http.Transport

// downloadTransport returns the active shared transport: the uTLS-backed
// one when opted in, else the stdlib sharedDownloadTransport.
func downloadTransport() *http.Transport {
	if testTransportOverride != nil {
		return testTransportOverride
	}
	if UseUTLSTransport {
		if sharedUTLSTransport == nil {
			sharedUTLSTransport = newUTLSTransport()
		}
		return sharedUTLSTransport
	}
	return sharedDownloadTransport
}

// newUTLSTransport builds an http.Transport that dials with a browser-grade
// TLS fingerprint via uTLS, keeping the shared transport's settings.
func newUTLSTransport() *http.Transport {
	return &http.Transport{
		ResponseHeaderTimeout: 30 * time.Second,
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          8,
		IdleConnTimeout:       90 * time.Second,
		DialTLSContext:        utlsDialTLSContext,
	}
}

// utlsRoundTripper injects the UA-derived impersonation profile into the
// request context before delegating to the underlying transport.
type utlsRoundTripper struct {
	transport *http.Transport
}

var _ http.RoundTripper = (*utlsRoundTripper)(nil)

func (r *utlsRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	profile := uaProfile(req.UserAgent())
	ctx := context.WithValue(req.Context(), utlsProfileKey{}, profile)
	return r.transport.RoundTrip(req.WithContext(ctx))
}

// uaProfile maps a User-Agent to the uTLS impersonation profile. The
// fingerprint must match the cookie-minting browser, so the mapping is
// derived from the same UA the cookies were minted under.
func uaProfile(ua string) utls.ClientHelloID {
	switch {
	case strings.Contains(ua, "Firefox"):
		return utls.HelloFirefox_Auto
	default:
		// Chrome/Edge/Chromium and unknown UAs impersonate Chrome.
		return utls.HelloChrome_Auto
	}
}

// utlsRootCAs allows tests to supply a custom CA pool; nil = system roots.
var utlsRootCAs *x509.CertPool

// utlsDialTLSContext dials addr and performs a uTLS handshake with the
// profile from the context (default Chrome). h2 is stripped from ALPN:
// Go's http.Transport only wires h2 when the dialed conn is a *tls.Conn,
// which a *utls.UConn never is — advertising h2 would break the request.
func utlsDialTLSContext(ctx context.Context, network, addr string) (net.Conn, error) {
	profile, ok := ctx.Value(utlsProfileKey{}).(utls.ClientHelloID)
	if !ok {
		profile = utls.HelloChrome_Auto
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("utls dial: split host/port: %w", err)
	}
	dialer := &net.Dialer{Timeout: 30 * time.Second}
	rawConn, err := dialer.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	uconn := utls.UClient(rawConn, &utls.Config{ServerName: host, RootCAs: utlsRootCAs}, profile)
	if err := uconn.BuildHandshakeState(); err != nil {
		rawConn.Close()
		return nil, fmt.Errorf("utls build handshake: %w", err)
	}
	uconn.HandshakeState.Hello.AlpnProtocols = []string{"http/1.1"}
	if err := uconn.HandshakeContext(ctx); err != nil {
		rawConn.Close()
		return nil, fmt.Errorf("utls handshake: %w", err)
	}
	return uconn, nil
}

// cfChallengeDetected reports whether resp is a Cloudflare challenge wall:
// a Cf-Mitigated header, or a 403 whose body carries CF challenge markers.
func cfChallengeDetected(resp *http.Response) bool {
	if v := resp.Header.Get("Cf-Mitigated"); v != "" {
		switch strings.ToLower(v) {
		case "challenge", "block", "javascript", "managed_challenge":
			return true
		}
	}
	if resp.StatusCode != http.StatusForbidden {
		return false
	}
	sniff := make([]byte, 1024)
	n, _ := resp.Body.Read(sniff)
	if n > 0 {
		body := strings.ToLower(string(sniff[:n]))
		if strings.Contains(body, "cf-chl-") || strings.Contains(body, "managed_challenge") {
			return true
		}
	}
	return false
}
