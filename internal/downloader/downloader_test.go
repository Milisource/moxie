package downloader

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// isValidDownloadURL
// ---------------------------------------------------------------------------

func TestIsValidDownloadURL_Valid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		url  string
		want bool
	}{
		{"https://example.com/file.zip", true},
		{"https://mega.nz/file/abc123", true},
		{"https://pixeldrain.com/u/abc123", true},
		{"https://buzzheavier.com/file", true},
		{"https://cdn.example.com/path/to/file.exe?download=1", true},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := isValidDownloadURL(tt.url)
			if got != tt.want {
				t.Errorf("isValidDownloadURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestIsValidDownloadURL_Invalid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		url  string
		want bool
	}{
		{"", false},
		{"not-a-url", false},
		{"http://example.com/file.zip", false},
		{"ftp://example.com/file.zip", false},
		{"/relative/path/file.zip", false},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := isValidDownloadURL(tt.url)
			if got != tt.want {
				t.Errorf("isValidDownloadURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestIsValidDownloadURL_SSRFProtection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"loopback IPv4", "https://127.0.0.1/latest/meta-data/", false},
		{"loopback IPv6", "https://[::1]/latest/meta-data/", false},
		{"private 10.x", "https://10.0.0.1/secrets", false},
		{"private 172.16.x", "https://172.16.0.1/secrets", false},
		{"private 192.168.x", "https://192.168.1.1/admin", false},
		{"link-local", "https://169.254.169.254/latest/meta-data/", false},
		{"AWS metadata", "https://169.254.169.254/latest/", false},
		{"GCP metadata host", "https://metadata.google.internal/computeMetadata/", false},
		{"Aliyun metadata", "https://100.100.100.200/latest/", false},
		{"public IP unaffected", "https://93.184.216.34/path", true},
		{"public hostname unaffected", "https://example.com/path", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidDownloadURL(tt.url)
			if got != tt.want {
				t.Errorf("isValidDownloadURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestIsValidDownloadURL_HostWithPort(t *testing.T) {
	t.Parallel()
	got := isValidDownloadURL("https://example.com:8080/file.zip")
	if !got {
		t.Errorf("expected valid URL with port, got false")
	}
	got2 := isValidDownloadURL("https://127.0.0.1:8080/file.zip")
	if got2 {
		t.Errorf("expected loopback with port to be invalid, got true")
	}
}

func TestIsValidDownloadURL_HostnameBlocklist(t *testing.T) {
	t.Parallel()
	tests := []string{
		"https://169.254.169.254/",
		"https://metadata.google.internal/",
		"https://100.100.100.200/",
	}
	for _, url := range tests {
		t.Run(url, func(t *testing.T) {
			got := isValidDownloadURL(url)
			if got {
				t.Errorf("expected %q to be blocked", url)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// downloadWithHeaders - URL validation
// ---------------------------------------------------------------------------

func TestDownloadWithHeaders_InvalidURL(t *testing.T) {
	t.Parallel()
	err := downloadWithHeaders("http://127.0.0.1/secret", nil, t.TempDir(), 0, nil)
	if err == nil {
		t.Fatal("expected error for blocked URL, got nil")
	}
	if !strings.Contains(err.Error(), "invalid or blocked") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Download - rejects non-HTTPS URLs (correct security behavior)
// ---------------------------------------------------------------------------

func TestDownload_RejectsNonHTTPS(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	destDir := t.TempDir()
	err := Download(srv.URL, destDir, 0, nil, "")
	if err == nil {
		t.Fatal("expected error for non-https URL, got nil")
	}
	if !strings.Contains(err.Error(), "invalid or blocked") {
		t.Errorf("expected 'invalid or blocked' error, got: %v", err)
	}
}

func TestDownload_InvalidURL(t *testing.T) {
	t.Parallel()
	err := Download("", t.TempDir(), 0, nil, "")
	if err == nil {
		t.Fatal("expected error for empty URL, got nil")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Errorf("expected error mentioning 'invalid', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// writeCounter
// ---------------------------------------------------------------------------

func TestWriteCounter_Write(t *testing.T) {
	t.Parallel()
	calls := 0
	wc := &writeCounter{
		prevTime: time.Now(),
		onProgress: func(p Progress) {
			calls++
		},
	}

	n, err := wc.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != 5 {
		t.Errorf("expected 5 bytes written, got %d", n)
	}
	if wc.total != 5 {
		t.Errorf("expected total 5, got %d", wc.total)
	}
}

func TestWriteCounter_AccumulatesTotal(t *testing.T) {
	t.Parallel()
	wc := &writeCounter{
		prevTime: time.Now(),
		onProgress: func(p Progress) {
		},
	}

	wc.Write([]byte("one"))
	wc.Write([]byte("two"))
	wc.Write([]byte("three"))

	if wc.total != 11 {
		t.Errorf("expected total 11, got %d", wc.total)
	}
}

func TestWriteCounter_MultipleWrites(t *testing.T) {
	t.Parallel()
	wc := &writeCounter{
		prevTime: time.Now().Add(-time.Second),
		onProgress: func(p Progress) {},
	}

	data := make([]byte, 32768)
	for i := 0; i < 10; i++ {
		wc.Write(data)
	}

	expected := int64(len(data) * 10)
	if wc.total != expected {
		t.Errorf("expected total %d, got %d", expected, wc.total)
	}
}

func TestWriteCounter_ProgressOnElapsed(t *testing.T) {
	t.Parallel()
	progressCalled := false
	wc := &writeCounter{
		prevTotal: 0,
		total:     0,
		prevTime:  time.Now().Add(-10 * time.Second),
		onProgress: func(p Progress) {
			progressCalled = true
			if p.BytesDownloaded <= 0 {
				t.Errorf("expected positive BytesDownloaded, got %d", p.BytesDownloaded)
			}
			if p.SpeedBytesPerSec <= 0 {
				t.Errorf("expected positive SpeedBytesPerSec, got %f", p.SpeedBytesPerSec)
			}
		},
	}

	wc.Write([]byte("some data to trigger progress"))
	if !progressCalled {
		t.Error("expected progress callback to be called")
	}
}

// ---------------------------------------------------------------------------
// File operations (simulating download behavior)
// ---------------------------------------------------------------------------

func Test_FilePartRename(t *testing.T) {
	t.Parallel()
	destDir := t.TempDir()
	partPath := filepath.Join(destDir, "test.dat.part")
	finalPath := filepath.Join(destDir, "test.dat")

	content := []byte("downloaded content")
	if err := os.WriteFile(partPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	// Simulate downloadWithHeaders renaming on success
	if err := os.Rename(partPath, finalPath); err != nil {
		t.Fatalf("rename failed: %v", err)
	}

	// Verify final file
	data, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(content) {
		t.Errorf("expected %q, got %q", string(content), string(data))
	}

	// .part should be gone
	if _, err := os.Stat(partPath); !os.IsNotExist(err) {
		t.Errorf("expected .part file to be removed")
	}
}

func Test_FileWriteMode(t *testing.T) {
	t.Parallel()
	destDir := t.TempDir()
	path := filepath.Join(destDir, "output.bin")

	// O_CREATE | O_WRONLY | O_TRUNC (same flags as downloadWithHeaders)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("first write"))
	f.Close()

	f2, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		t.Fatal(err)
	}
	f2.Write([]byte("second write"))
	f2.Close()

	// Verify content is from second write (truncated)
	data, _ := os.ReadFile(path)
	if string(data) != "second write" {
		t.Errorf("expected 'second write', got %q", string(data))
	}
}

func Test_FileAppendMode(t *testing.T) {
	t.Parallel()
	destDir := t.TempDir()
	path := filepath.Join(destDir, "append.bin")

	// Create initial partial content (simulating .part with existing data)
	os.WriteFile(path, []byte("partial "), 0644)

	// Append mode (for resume support)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("append content"))
	f.Close()

	data, _ := os.ReadFile(path)
	if string(data) != "partial append content" {
		t.Errorf("expected 'partial append content', got %q", string(data))
	}
}
