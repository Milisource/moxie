package downloader

import (
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	utls "github.com/refraction-networking/utls"
)

func TestUaProfile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		ua   string
		want utls.ClientHelloID
	}{
		{"Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0", utls.HelloFirefox_Auto},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:130.0) Gecko/20100101 Firefox/130.0", utls.HelloFirefox_Auto},
		{"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36", utls.HelloChrome_Auto},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0", utls.HelloChrome_Auto},
		{"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chromium/130.0.0.0 Safari/537.36", utls.HelloChrome_Auto},
		{"", utls.HelloChrome_Auto},
		{"weird-custom-client/1.0", utls.HelloChrome_Auto},
	}
	for _, tt := range tests {
		if got := uaProfile(tt.ua); got != tt.want {
			t.Errorf("uaProfile(%q) = %v, want %v", tt.ua, got, tt.want)
		}
	}
}

func TestCfChallengeDetected(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		status     int
		cfHeader   string
		body       string
		want       bool
	}{
		{"challenge header", http.StatusOK, "challenge", "ok", true},
		{"block header", http.StatusOK, "block", "ok", true},
		{"javascript header", http.StatusOK, "javascript", "ok", true},
		{"managed_challenge header", http.StatusOK, "managed_challenge", "ok", true},
		{"no header 200", http.StatusOK, "", "ok", false},
		{"no header 403 clean", http.StatusForbidden, "", "Forbidden", false},
		{"403 with cf marker", http.StatusForbidden, "", "<html><script src=\"/cf-chl-javascript\">", true},
		{"403 with managed marker", http.StatusForbidden, "", "managed_challenge", true},
		{"404 clean", http.StatusNotFound, "", "not found", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &http.Response{
				StatusCode: tt.status,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(tt.body)),
			}
			if tt.cfHeader != "" {
				rec.Header.Set("Cf-Mitigated", tt.cfHeader)
			}
			if got := cfChallengeDetected(rec); got != tt.want {
				t.Errorf("cfChallengeDetected = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewUTLSTransport(t *testing.T) {
	t.Parallel()
	tr := newUTLSTransport()
	if tr.DialTLSContext == nil {
		t.Fatal("DialTLSContext not set")
	}
	if tr.ResponseHeaderTimeout == 0 {
		t.Error("ResponseHeaderTimeout lost")
	}
	if tr.MaxIdleConns == 0 {
		t.Error("MaxIdleConns lost")
	}
	if tr.IdleConnTimeout == 0 {
		t.Error("IdleConnTimeout lost")
	}
}

// TestUTLSRoundTrip drives a full request through the uTLS dialer against a
// local TLS server, proving the ClientHello handshake path works end to end.
func TestUTLSRoundTrip(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("utls-ok"))
	}))
	defer srv.Close()

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())

	oldRoots := utlsRootCAs
	utlsRootCAs = pool
	defer func() { utlsRootCAs = oldRoots }()

	tr := newUTLSTransport()
	client := &http.Client{Transport: &utlsRoundTripper{transport: tr}}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("round trip through uTLS dialer: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "utls-ok" {
		t.Fatalf("response = %d %q, want 200 utls-ok", resp.StatusCode, body)
	}
}
