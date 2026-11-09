package downloader

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// withGoogleDriveServer points the resolver at a local server for both the
// uc endpoint and the usercontent host.
func withGoogleDriveServer(t *testing.T, handler http.HandlerFunc) *HostResolver {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	oldBase, oldUC := googleDriveUCBase, googleDriveUserContent
	googleDriveUCBase = srv.URL
	googleDriveUserContent = srv.URL + "/download"
	t.Cleanup(func() { googleDriveUCBase, googleDriveUserContent = oldBase, oldUC })

	r := NewHostResolver()
	r.client = srv.Client()
	return r
}

// The current warning page carries a form; its hidden inputs build the real
// download URL. The old resolver returned the uc URL and the download then
// failed with "server returned HTML instead of file".
func TestResolveGoogleDrive_UserContentForm(t *testing.T) {
	const page = `<html><body>
<form id="download-form" action="https://drive.usercontent.google.com/download" method="get">
  <input type="hidden" name="id" value="FILEID123">
  <input type="hidden" name="export" value="download">
  <input type="hidden" name="confirm" value="t">
  <input type="hidden" name="uuid" value="abcd-1234">
</form>
</body></html>`

	r := withGoogleDriveServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, page)
	})

	res, err := r.resolveGoogleDrive("https://drive.google.com/file/d/FILEID123/view")
	if err != nil {
		t.Fatalf("resolveGoogleDrive: %v", err)
	}
	for _, want := range []string{"id=FILEID123", "export=download", "confirm=t", "uuid=abcd-1234"} {
		if !strings.Contains(res.URL, want) {
			t.Errorf("URL %q missing %q", res.URL, want)
		}
	}
}

// Some pages embed the usercontent link directly, with HTML-escaped ampersands.
func TestResolveGoogleDrive_UserContentLink(t *testing.T) {
	const page = `<html><body><a href="https://drive.usercontent.google.com/download?id=FILEID123&amp;export=download&amp;confirm=t&amp;uuid=xyz">Download</a></body></html>`
	r := withGoogleDriveServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page)
	})

	res, err := r.resolveGoogleDrive("https://drive.google.com/file/d/FILEID123/view")
	if err != nil {
		t.Fatalf("resolveGoogleDrive: %v", err)
	}
	if !strings.Contains(res.URL, "drive.usercontent.google.com/download?") {
		t.Errorf("URL = %q, want the usercontent endpoint", res.URL)
	}
	if strings.Contains(res.URL, "&amp;") {
		t.Errorf("URL still HTML-escaped: %q", res.URL)
	}
}

// The legacy confirm-token page must keep working.
func TestResolveGoogleDrive_LegacyConfirm(t *testing.T) {
	const page = `<html><body><a class="uc-download-link" href="/uc?export=download&amp;confirm=TOKEN9&amp;id=FILEID123">Download anyway</a></body></html>`
	r := withGoogleDriveServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page)
	})

	res, err := r.resolveGoogleDrive("https://drive.google.com/file/d/FILEID123/view")
	if err != nil {
		t.Fatalf("resolveGoogleDrive: %v", err)
	}
	if !strings.Contains(res.URL, "confirm=TOKEN9") {
		t.Errorf("URL = %q, want the legacy confirm URL", res.URL)
	}
}

// An unparseable page keeps the historical fallback to the uc URL.
func TestResolveGoogleDrive_UnknownPageFallsBackToUC(t *testing.T) {
	r := withGoogleDriveServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html><body>Sorry, you can't view this file.</body></html>")
	})

	res, err := r.resolveGoogleDrive("https://drive.google.com/file/d/FILEID123/view")
	if err != nil {
		t.Fatalf("resolveGoogleDrive: %v", err)
	}
	if !strings.Contains(res.URL, "/uc?export=download&id=FILEID123") {
		t.Errorf("URL = %q, want the uc fallback", res.URL)
	}
}

// A binary response is a direct download and must pass through untouched.
func TestResolveGoogleDrive_DirectBinary(t *testing.T) {
	r := withGoogleDriveServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write([]byte("PK\x03\x04"))
	})

	res, err := r.resolveGoogleDrive("https://drive.google.com/file/d/FILEID123/view")
	if err != nil {
		t.Fatalf("resolveGoogleDrive: %v", err)
	}
	if !strings.Contains(res.URL, "/uc?export=download&id=FILEID123") {
		t.Errorf("URL = %q, want the uc download URL", res.URL)
	}
}
