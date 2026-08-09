package downloader

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Test helpers (fake megatools binary)
// ---------------------------------------------------------------------------

// stubFindMegatools replaces the package-level findMegatools var and returns
// a restore function. Tests that call it must NOT run in parallel — the var
// is global state.
func stubFindMegatools(path string, err error) func() {
	orig := findMegatools
	findMegatools = func() (string, error) { return path, err }
	return func() { findMegatools = orig }
}

// writeFakeMegatools writes an executable fake megatools script to a temp dir
// and returns its path. The script must be self-contained shell; no real
// megatools downloads ever run in tests.
func writeFakeMegatools(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "megatools")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake megatools script: %v", err)
	}
	return path
}

// fakeMegatoolsSuccessScript mimics megatools dl: records the args it was
// invoked with, streams progress lines to stderr, and writes a file into the
// --path destination.
const fakeMegatoolsSuccessScript = `#!/bin/sh
outdir=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "--path" ]; then outdir="$arg"; fi
  prev="$arg"
done
printf '%s\n' "$*" > "$outdir/args.txt"
printf '%s\n' "game.zip: 0.00% - 0 bytes of 9.5MiB" >&2
printf '%s\n' "game.zip: 42.10% - 4.0MiB (4194304 bytes) of 9.5MiB (1.5MiB/s)" >&2
printf '%s\n' "game.zip: 100.00% - 9.5MiB (9961472 bytes) of 9.5MiB (2.0MiB/s)" >&2
printf '%s\n' 'fake mega archive content' > "$outdir/game.zip"
printf '%s\n' "Downloaded game.zip" >&2
exit 0
`

// fakeMegatoolsFailureScript exits non-zero with a rate-limit-style error on
// stderr.
const fakeMegatoolsFailureScript = `#!/bin/sh
printf '%s\n' "game.zip: 12.00% - 1.0MiB (1048576 bytes) of 9.5MiB" >&2
printf '%s\n' "ERROR: Download failed for 'https://mega.nz/file/abc123#key': Too many requests" >&2
exit 1
`

// fakeMegatoolsSleepScript hangs until killed — used for cancellation and
// timeout tests. exec replaces the shell so the kill lands on the sleeper
// itself.
const fakeMegatoolsSleepScript = `#!/bin/sh
exec sleep 60
`

// ---------------------------------------------------------------------------
// extractMegaFileID
// ---------------------------------------------------------------------------

func TestExtractMegaFileID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		url  string
		want string
	}{
		// New style: /file/<ID>#<key>
		{"https://mega.nz/file/abc123#key", "abc123"},
		{"https://www.mega.nz/file/abc123#key", "abc123"},
		{"https://mega.nz/file/ABC-DEF_123#jFc2HL6rIoDVU9kECBpMEIAbcv2WQcz6le9kS_bb2gc", "ABC-DEF_123"},
		{"https://mega.co.nz/file/xyz789#k", "xyz789"},
		{"https://MEGA.NZ/file/UPPER123#k", "UPPER123"},
		// Legacy style: #!<ID>!<key>
		{"https://mega.nz/#!abc123!key", "abc123"},
		{"https://mega.nz/#!ABC!xyz", "ABC"},
		// Unsupported / malformed
		{"https://mega.nz/folder/xyz789#key", ""}, // folder links are rejected
		{"https://mega.nz/file/", ""},
		{"https://mega.nz/", ""},
		{"https://example.com/file/abc", ""}, // wrong host
		{"https://smega.com/file/abc", ""},   // hostname only smells like mega
		{"not-a-url", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			if got := extractMegaFileID(tt.url); got != tt.want {
				t.Errorf("extractMegaFileID(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// parseHumanSize
// ---------------------------------------------------------------------------

func TestParseHumanSize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want int64
	}{
		{"512B", 512},
		{"12.4KiB", 12697},  // 12.4 * 1024, truncated
		{"2.9MiB", 3040870}, // 2.9 * 1024 * 1024, truncated
		{"1.5MiB", 1572864},
		{"1GiB", 1 << 30},
		{"2TiB", 2 * (1 << 40)},
		{"KB", 0},
		{"", 0},
		{"abc", 0},
		{"-5MiB", 0},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := parseHumanSize(tt.in); got != tt.want {
				t.Errorf("parseHumanSize(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// parseMegatoolsProgress
// ---------------------------------------------------------------------------

func TestParseMegatoolsProgress(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		line     string
		wantDone int64
		wantTot  int64
		wantPct  float64
		wantSpd  int64
		wantOK   bool
	}{
		{
			name:     "new style full",
			line:     "game.zip: 50.00% - 4.0MiB (4194304 bytes) of 8.0MiB (1.5MiB/s)",
			wantDone: 4194304,
			wantTot:  8388608,
			wantPct:  50,
			wantSpd:  1572864,
			wantOK:   true,
		},
		{
			name:     "new style no speed",
			line:     "game.zip: 25.00% - 2.0MiB (2097152 bytes) of 8.0MiB",
			wantDone: 2097152,
			wantTot:  8388608,
			wantPct:  25,
			wantOK:   true,
		},
		{
			name:   "new style zero bytes first line",
			line:   "game.zip: 0.00% - 0 bytes of 9.5MiB",
			wantOK: true,
		},
		{
			name:     "new style name with parens and digits",
			line:     "Game (fixed) 2.zip: 75.00% - 6.0MiB (6291456 bytes) of 8.0MiB (2.0MiB/s)",
			wantDone: 6291456,
			wantTot:  8388608,
			wantPct:  75,
			wantSpd:  2097152,
			wantOK:   true,
		},
		{
			name:     "legacy full",
			line:     "Downloaded 123456 bytes of 987654 bytes (12.50%)",
			wantDone: 123456,
			wantTot:  987654,
			wantPct:  12.5,
			wantOK:   true,
		},
		{
			name:     "legacy no total",
			line:     "Downloaded 123456 bytes",
			wantDone: 123456,
			wantOK:   true,
		},
		{
			name:   "completion line is not progress",
			line:   "Downloaded game.zip",
			wantOK: false,
		},
		{
			name:   "error line is not progress",
			line:   "ERROR: Download failed for 'x': File not found",
			wantOK: false,
		},
		{
			name:   "unrelated output",
			line:   "some random megatools noise",
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, ok := parseMegatoolsProgress(tt.line)
			if ok != tt.wantOK {
				t.Fatalf("parseMegatoolsProgress(%q) ok = %v, want %v", tt.line, ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if p.done != tt.wantDone {
				t.Errorf("done = %d, want %d", p.done, tt.wantDone)
			}
			if p.total != tt.wantTot {
				t.Errorf("total = %d, want %d", p.total, tt.wantTot)
			}
			if p.percent != tt.wantPct {
				t.Errorf("percent = %f, want %f", p.percent, tt.wantPct)
			}
			if p.speedBytes != tt.wantSpd {
				t.Errorf("speedBytes = %d, want %d", p.speedBytes, tt.wantSpd)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// megatoolsErrSummary
// ---------------------------------------------------------------------------

func TestMegatoolsErrSummary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		tail string
		want string
	}{
		{"last ERROR line wins", "progress line\nERROR: Download failed: File not found\nnoise", "ERROR: Download failed: File not found"},
		{"multiple ERROR lines keep last", "ERROR: first\nERROR: second", "ERROR: second"},
		{"last non-empty fallback", "line1\nline2", "line2"},
		{"whitespace trimmed", "  \n  hello world  \n", "hello world"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := megatoolsErrSummary(tt.tail); got != tt.want {
				t.Errorf("megatoolsErrSummary(%q) = %q, want %q", tt.tail, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// runMegatoolsDownload
// ---------------------------------------------------------------------------

func TestRunMegatoolsDownload_InvalidURL(t *testing.T) {
	t.Parallel()
	err := runMegatoolsDownload(context.Background(), "https://example.com/not-mega", t.TempDir(), 0, nil)
	if err == nil {
		t.Fatal("expected error for non-mega URL, got nil")
	}
	if !strings.Contains(err.Error(), "could not extract Mega file ID") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunMegatoolsDownload_Success(t *testing.T) {
	// NOT parallel — replaces the findMegatools global.
	path := writeFakeMegatools(t, fakeMegatoolsSuccessScript)
	t.Cleanup(stubFindMegatools(path, nil))

	destDir := t.TempDir()
	url := "https://mega.nz/file/abc123#jFc2HL6rIoDVU9kECBpMEIAbcv2WQcz6le9kS_bb2gc"
	var samples []Progress
	err := runMegatoolsDownload(context.Background(), url, destDir, 0, func(p Progress) {
		samples = append(samples, p)
	})
	if err != nil {
		t.Fatalf("runMegatoolsDownload: %v", err)
	}

	// The fake binary wrote the downloaded file into destDir.
	content, err := os.ReadFile(filepath.Join(destDir, "game.zip"))
	if err != nil {
		t.Fatalf("downloaded file missing: %v", err)
	}
	if string(content) != "fake mega archive content\n" {
		t.Errorf("unexpected file content: %q", content)
	}

	// The full URL — including the #key fragment that carries the decryption
	// key — must reach megatools unchanged.
	args, err := os.ReadFile(filepath.Join(destDir, "args.txt"))
	if err != nil {
		t.Fatalf("args file missing: %v", err)
	}
	if !strings.Contains(string(args), "--path "+destDir) {
		t.Errorf("megatools args missing --path destDir: %s", args)
	}
	if !strings.Contains(string(args), url) {
		t.Errorf("megatools args missing url with key fragment: %s", args)
	}

	// Progress: 3 parsed samples (0% / 42.1% / 100%) + final completion report.
	if len(samples) < 4 {
		t.Fatalf("expected >= 4 progress samples, got %d: %+v", len(samples), samples)
	}
	mid := samples[len(samples)-2] // parsed "100.00%" line
	if mid.BytesDownloaded != 9961472 || mid.TotalBytes != 9961472 {
		t.Errorf("mid sample = %+v, want done/total 9961472", mid)
	}
	if mid.SpeedBytesPerSec != float64(2*1024*1024) {
		t.Errorf("mid SpeedBytesPerSec = %v, want %d", mid.SpeedBytesPerSec, 2*1024*1024)
	}
	final := samples[len(samples)-1] // completion report
	if final.Percent != 100 || final.BytesDownloaded != 9961472 || final.TotalBytes != 9961472 {
		t.Errorf("final sample = %+v, want 100%% @ 9961472 bytes", final)
	}
}

func TestRunMegatoolsDownload_MissingBinary(t *testing.T) {
	// NOT parallel — replaces the findMegatools global.
	t.Cleanup(stubFindMegatools("", errors.New("executable not found")))

	err := runMegatoolsDownload(context.Background(), "https://mega.nz/file/abc123#key", t.TempDir(), 0, nil)
	if err == nil {
		t.Fatal("expected error when megatools is missing, got nil")
	}
	if !strings.Contains(err.Error(), "megatools") {
		t.Errorf("expected error to mention megatools, got: %v", err)
	}
	if !strings.Contains(err.Error(), "apt install megatools") {
		t.Errorf("expected install hint in error, got: %v", err)
	}
}

func TestRunMegatoolsDownload_NonZeroExit(t *testing.T) {
	// NOT parallel — replaces the findMegatools global.
	path := writeFakeMegatools(t, fakeMegatoolsFailureScript)
	t.Cleanup(stubFindMegatools(path, nil))

	err := runMegatoolsDownload(context.Background(), "https://mega.nz/file/abc123#key", t.TempDir(), 0, nil)
	if err == nil {
		t.Fatal("expected error on non-zero exit, got nil")
	}
	if !strings.Contains(err.Error(), "megatools dl failed") {
		t.Errorf("expected 'megatools dl failed' prefix, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Too many requests") {
		t.Errorf("expected megatools stderr summary in error, got: %v", err)
	}
}

func TestRunMegatoolsDownload_Canceled(t *testing.T) {
	// NOT parallel — replaces the findMegatools global.
	path := writeFakeMegatools(t, fakeMegatoolsSleepScript)
	t.Cleanup(stubFindMegatools(path, nil))

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)
	err := runMegatoolsDownload(ctx, "https://mega.nz/file/abc123#key", t.TempDir(), 0, nil)
	if err == nil {
		t.Fatal("expected cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
	if !strings.Contains(err.Error(), "interrupted") {
		t.Errorf("expected interruption message, got: %v", err)
	}
}

func TestRunMegatoolsDownload_Timeout(t *testing.T) {
	// NOT parallel — replaces the findMegatools and megatoolsTimeout globals.
	path := writeFakeMegatools(t, fakeMegatoolsSleepScript)
	t.Cleanup(stubFindMegatools(path, nil))
	origTimeout := megatoolsTimeout
	megatoolsTimeout = 250 * time.Millisecond
	t.Cleanup(func() { megatoolsTimeout = origTimeout })

	err := runMegatoolsDownload(context.Background(), "https://mega.nz/file/abc123#key", t.TempDir(), 0, nil)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// HostResolver.Resolve - Mega (megatools present)
// ---------------------------------------------------------------------------

func TestResolveMega_DelegatesWhenMegatoolsPresent(t *testing.T) {
	// NOT parallel — replaces the findMegatools global.
	path := writeFakeMegatools(t, "#!/bin/sh\nexit 0\n")
	t.Cleanup(stubFindMegatools(path, nil))

	r := NewHostResolver()
	result, err := r.Resolve("https://mega.nz/file/abc123#key", "mega")
	if err == nil {
		t.Fatal("expected error for mega URL, got nil")
	}
	if result != nil {
		t.Errorf("expected nil result for mega, got %+v", result)
	}
	if !errors.Is(err, ErrMegaNeedsMegatools) {
		t.Errorf("expected ErrMegaNeedsMegatools, got: %v", err)
	}
	if !strings.Contains(err.Error(), "megatools") {
		t.Errorf("expected error to mention megatools, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// DownloadWithContext - Mega integration
// ---------------------------------------------------------------------------

func TestDownloadWithContext_MegaDelegatesToMegatools(t *testing.T) {
	// NOT parallel — replaces the findMegatools global.
	path := writeFakeMegatools(t, fakeMegatoolsSuccessScript)
	t.Cleanup(stubFindMegatools(path, nil))

	destDir := t.TempDir()
	err := DownloadWithContext(context.Background(), "https://mega.nz/file/abc123#key", "mega", destDir, 0, nil, "")
	if err != nil {
		t.Fatalf("DownloadWithContext mega: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "game.zip")); err != nil {
		t.Errorf("expected megatools output file in destDir: %v", err)
	}
}

func TestDownloadWithContext_MegaWithoutMegatools(t *testing.T) {
	// NOT parallel — replaces the findMegatools global.
	t.Cleanup(stubFindMegatools("", errors.New("executable not found")))

	err := DownloadWithContext(context.Background(), "https://mega.nz/file/abc123#key", "mega", t.TempDir(), 0, nil, "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "megatools") {
		t.Errorf("expected error to mention megatools, got: %v", err)
	}
	if !strings.Contains(err.Error(), "apt install megatools") {
		t.Errorf("expected install hint in error, got: %v", err)
	}
}
