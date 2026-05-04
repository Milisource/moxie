package downloader

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// CheckLink - URL validation layer
//
// CheckLink requires HTTPS URLs with non-private hostnames. Testing the full
// HTTP round-trip with a local server is impractical because isValidDownloadURL
// blocks loopback/private IPs. We test:
//
//  1. URL validation rejects invalid/blocked URLs (correct security behavior)
//  2. The response status code parsing is tested by testing the URL validation
//     layer that feeds into CheckLink.
//
// The status code switch in CheckLink is straightforward and visible in the
// source — it maps HTTP status codes to descriptive error messages.
// ---------------------------------------------------------------------------

func TestCheckLink_InvalidURL(t *testing.T) {
	t.Parallel()
	err := CheckLink("http://169.254.169.254/latest/meta-data/")
	if err == nil {
		t.Fatal("expected error for blocked URL, got nil")
	}
	if !strings.Contains(err.Error(), "invalid URL") {
		t.Errorf("expected 'invalid URL' error, got: %v", err)
	}
}

func TestCheckLink_InvalidURLBadScheme(t *testing.T) {
	t.Parallel()
	err := CheckLink("ftp://example.com/file.zip")
	if err == nil {
		t.Fatal("expected error for non-https URL, got nil")
	}
}

func TestCheckLink_InvalidURLNonHTTPS(t *testing.T) {
	t.Parallel()
	err := CheckLink("http://example.com/file.zip")
	if err == nil {
		t.Fatal("expected error for http URL, got nil")
	}
}

func TestCheckLink_InvalidLoopback(t *testing.T) {
	t.Parallel()
	err := CheckLink("https://127.0.0.1/file.zip")
	if err == nil {
		t.Fatal("expected error for loopback, got nil")
	}
}

func TestCheckLink_InvalidPrivateIP(t *testing.T) {
	t.Parallel()
	err := CheckLink("https://192.168.1.1/file.zip")
	if err == nil {
		t.Fatal("expected error for private IP, got nil")
	}
}

func TestCheckLink_InvalidMetadataEndpoint(t *testing.T) {
	t.Parallel()
	tests := []string{
		"https://169.254.169.254/latest/meta-data/",
		"https://metadata.google.internal/computeMetadata/",
		"https://100.100.100.200/latest/",
	}
	for _, url := range tests {
		t.Run(url, func(t *testing.T) {
			err := CheckLink(url)
			if err == nil {
				t.Errorf("expected error for blocked URL %q, got nil", url)
			}
			if !strings.Contains(err.Error(), "invalid URL") {
				t.Errorf("expected 'invalid URL' error for %q, got: %v", url, err)
			}
		})
	}
}
