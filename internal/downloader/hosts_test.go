package downloader

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// IdentifyHostInURL
// ---------------------------------------------------------------------------

func TestIdentifyHostInURL_MajorHosts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		url  string
		want string
	}{
		{"https://mega.nz/file/abc123", "mega"},
		{"https://mega.co.nz/file/abc123", "mega"},
		{"https://pixeldrain.com/u/abc123", "pixeldrain"},
		{"https://buzzheavier.com/file", "buzzheavier"},
		{"https://gofile.io/d/abc123", "gofile"},
		{"https://vikingfile.com/file", "vikingfile"},
		{"https://www.mediafire.com/file/abc123", "mediafire"},
		{"https://workupload.com/file/abc123", "workupload"},
		{"https://bunkrr.su/file/abc123", "bunkrr"},
		{"https://bunkr.su/file/abc123", "bunkrr"},
		{"https://krakenfiles.com/view/abc123", "krakenfiles"},
		{"https://uploadhaven.com/download/abc123", "uploadhaven"},
		{"https://wetransfer.com/downloads/abc123", "wetransfer"},
		{"https://sendgb.com/file/abc123", "sendgb"},
		{"https://1cloudfile.com/file/abc123", "1cloudfile"},
		{"https://akirabox.com/file/abc123", "akirabox"},
		{"https://anontransfer.com/file/abc123", "anontransfer"},
		{"https://anonymfile.com/file/abc123", "anonymfile"},
		{"https://apkadmin.com/file/abc123", "apkadmin"},
		{"https://bowfile.com/file/abc123", "bowfile"},
		{"https://catbox.moe/file/abc123", "catbox"},
		{"https://cyberfile.me/file/abc123", "cyberfile"},
		{"https://datanodes.to/file/abc123", "datanodes"},
		{"https://delafil.com/file/abc123", "delafil"},
		{"https://download.gg/file/abc123", "downloadgg"},
		{"https://dropmefiles.com/file/abc123", "dropmefiles"},
		{"https://easyupload.io/file/abc123", "easyupload"},
		{"https://filemail.com/file/abc123", "filemail"},
		{"https://files.dp.ua/file/abc123", "filesdpua"},
		{"https://files.fm/file/abc123", "filesfm"},
		{"https://fromsmash.com/file/abc123", "fromsmash"},
		{"https://drive.google.com/file/d/abc123", "google drive"},
		{"https://hexload.com/file/abc123", "hexload"},
		{"https://mixdrop.co/file/abc123", "mixdrop"},
		{"https://qu.ax/file/abc123", "quax"},
		{"https://transfer.sh/file/abc123", "transfersh"},
		{"https://uploadnow.io/file/abc123", "uploadnow"},
		{"https://wdho.ru/file/abc123", "wdho"},
		{"https://yourfilestore.com/file/abc123", "yourfilestore"},
		{"https://keep2share.cc/file/abc123", "keep2share"},
		{"https://k2s.cc/file/abc123", "keep2share"},
		{"https://uploaded.net/file/abc123", "uploaded"},
		{"https://ul.to/file/abc123", "uploaded"},
		{"https://dropbox.com/s/abc123/file", "dropbox"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := IdentifyHostInURL(tt.url)
			if got != tt.want {
				t.Errorf("IdentifyHostInURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestIdentifyHostInURL_Unknown(t *testing.T) {
	t.Parallel()
	tests := []string{
		"https://example.com/file.zip",
		"https://unknown-host.net/download",
		"",
		"not-a-url",
	}
	for _, url := range tests {
		t.Run(url, func(t *testing.T) {
			got := IdentifyHostInURL(url)
			if got != "unknown" {
				t.Errorf("IdentifyHostInURL(%q) = %q, want %q", url, got, "unknown")
			}
		})
	}
}

func TestIdentifyHostInURL_CaseInsensitive(t *testing.T) {
	t.Parallel()
	tests := []struct {
		url  string
		want string
	}{
		{"https://MEGA.NZ/file/abc123", "mega"},
		{"https://PIXELDRAIN.COM/u/abc123", "pixeldrain"},
		{"https://BuzzHeavier.com/file", "buzzheavier"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := IdentifyHostInURL(tt.url)
			if got != tt.want {
				t.Errorf("IdentifyHostInURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// NewHostResolver
// ---------------------------------------------------------------------------

func TestNewHostResolver(t *testing.T) {
	t.Parallel()
	r := NewHostResolver()
	if r == nil {
		t.Fatal("NewHostResolver() returned nil")
	}
	if r.client == nil {
		t.Fatal("NewHostResolver() returned resolver with nil client")
	}
}

// ---------------------------------------------------------------------------
// HostResolver.Resolve - Pixeldrain
// ---------------------------------------------------------------------------

func TestResolvePixeldrain_ValidURL(t *testing.T) {
	t.Parallel()
	r := NewHostResolver()
	result, err := r.Resolve("https://pixeldrain.com/u/abc123", "pixeldrain")
	if err != nil {
		t.Fatalf("Resolve pixeldrain failed: %v", err)
	}
	if result == nil {
		t.Fatal("Resolve pixeldrain returned nil result")
	}
	expectedURL := "https://pixeldrain.com/api/file/abc123"
	if result.URL != expectedURL {
		t.Errorf("expected URL %q, got %q", expectedURL, result.URL)
	}
	if result.Headers == nil {
		t.Fatal("expected non-nil headers")
	}
	if ua, ok := result.Headers["User-Agent"]; !ok || ua == "" {
		t.Errorf("expected User-Agent header, got %q", ua)
	}
}

func TestResolvePixeldrain_APIFormat(t *testing.T) {
	t.Parallel()
	r := NewHostResolver()
	result, err := r.Resolve("https://pixeldrain.com/api/file/xyz789", "pixeldrain")
	if err != nil {
		t.Fatalf("Resolve pixeldrain API format failed: %v", err)
	}
	expectedURL := "https://pixeldrain.com/api/file/xyz789"
	if result.URL != expectedURL {
		t.Errorf("expected URL %q, got %q", expectedURL, result.URL)
	}
}

func TestResolvePixeldrain_InvalidURL(t *testing.T) {
	t.Parallel()
	r := NewHostResolver()
	_, err := r.Resolve("https://example.com/file", "pixeldrain")
	if err == nil {
		t.Fatal("expected error for invalid pixeldrain URL, got nil")
	}
	if !strings.Contains(err.Error(), "could not extract Pixeldrain file ID") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// ---------------------------------------------------------------------------
// HostResolver.Resolve - Mega
// ---------------------------------------------------------------------------

func TestResolveMega_ReturnsError(t *testing.T) {
	t.Parallel()
	r := NewHostResolver()
	result, err := r.Resolve("https://mega.nz/file/abc123", "mega")
	if err == nil {
		t.Fatal("expected error for mega URL, got nil")
	}
	if result != nil {
		t.Errorf("expected nil result for mega, got %+v", result)
	}
	if !strings.Contains(err.Error(), "megatools") {
		t.Errorf("expected error to mention megatools, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// HostResolver.Resolve - VikingFile (direct pass-through)
// ---------------------------------------------------------------------------

func TestResolveVikingFile(t *testing.T) {
	t.Parallel()
	r := NewHostResolver()
	url := "https://vikingfile.com/file/abc123"
	result, err := r.Resolve(url, "vikingfile")
	if err != nil {
		t.Fatalf("Resolve vikingfile failed: %v", err)
	}
	if result.URL != url {
		t.Errorf("expected URL %q, got %q", url, result.URL)
	}
	if result.Headers == nil {
		t.Fatal("expected non-nil headers")
	}
}

// ---------------------------------------------------------------------------
// HostResolver.Resolve - Unknown host (generic pass-through)
// ---------------------------------------------------------------------------

func TestResolveUnknownHost(t *testing.T) {
	t.Parallel()
	r := NewHostResolver()
	url := "https://example.com/file.zip"
	result, err := r.Resolve(url, "unknown")
	if err != nil {
		t.Fatalf("Resolve unknown host failed: %v", err)
	}
	if result.URL != url {
		t.Errorf("expected URL %q, got %q", url, result.URL)
	}
	if result.Headers == nil {
		t.Fatal("expected non-nil headers map")
	}
	if len(result.Headers) != 0 {
		t.Errorf("expected empty headers for unknown host, got %v", result.Headers)
	}
}
