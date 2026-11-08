package downloader

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractGoogleDriveUserContentURL(t *testing.T) {
	t.Parallel()
	body := []byte(`<form id="download-form" action="https://drive.usercontent.google.com/download" method="get">
		<input type="hidden" name="id" value="FILE123">
		<input type="hidden" name="export" value="download">
		<input type="hidden" name="confirm" value="t">
		<input type="hidden" name="uuid" value="uuid-456">
	</form>`)
	got := extractGoogleDriveUserContentURL(body, "FILE123")
	want := "https://drive.usercontent.google.com/download?id=FILE123&export=download&confirm=t&uuid=uuid-456"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExtractGoogleDriveUserContentURL_Absent(t *testing.T) {
	t.Parallel()
	if got := extractGoogleDriveUserContentURL([]byte(`<html>no form</html>`), "x"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestResolveGoogleDrive_UserContentForm(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<form id="download-form" action="https://drive.usercontent.google.com/download" method="get">
			<input type="hidden" name="id" value="FILE123">
			<input type="hidden" name="export" value="download">
			<input type="hidden" name="confirm" value="t">
			<input type="hidden" name="uuid" value="uuid-456">
		</form>`)
	}))
	defer server.Close()

	r := NewHostResolver()
	toTestServer(r, server)
	got, err := r.resolveGoogleDrive("https://drive.google.com/file/d/FILE123/view")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got.URL, "drive.usercontent.google.com/download?id=FILE123") ||
		!strings.Contains(got.URL, "uuid=uuid-456") {
		t.Errorf("unexpected URL: %s", got.URL)
	}
}

func TestContentDispositionFilename(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
	}{
		{`attachment; filename="game.zip"`, "game.zip"},
		{`inline; filename*=UTF-8''my%20game.zip`, "my game.zip"},
		{``, ""},
		{`attachment`, ""},
	}
	for _, tt := range tests {
		if got := contentDispositionFilename(tt.in); got != tt.want {
			t.Errorf("contentDispositionFilename(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestIsValidGameFile_HTMLGames(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	small := filepath.Join(dir, "interstitial.html")
	if err := os.WriteFile(small, []byte("<html>captcha</html>"), 0600); err != nil {
		t.Fatal(err)
	}
	if IsValidGameFile(small) {
		t.Error("small HTML page should be rejected")
	}

	large := filepath.Join(dir, "game.html")
	if err := os.WriteFile(large, make([]byte, 64*1024+1), 0600); err != nil {
		t.Fatal(err)
	}
	if !IsValidGameFile(large) {
		t.Error("large HTML game should be accepted")
	}
}
