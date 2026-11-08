package downloader

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func workuploadTestClient(server *httptest.Server) *http.Client {
	return &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			testURL := server.URL + req.URL.Path
			if req.URL.RawQuery != "" {
				testURL += "?" + req.URL.RawQuery
			}
			newReq, err := http.NewRequest(req.Method, testURL, req.Body)
			if err != nil {
				return nil, err
			}
			newReq.Header = req.Header.Clone()
			return http.DefaultTransport.RoundTrip(newReq)
		}),
	}
}

func TestSolveWorkuploadPuzzle(t *testing.T) {
	t.Parallel()
	puzzle := "test-puzzle-123"
	want := []int{3, 7}
	find := make([]string, len(want))
	for i, n := range want {
		h := sha256.Sum256([]byte(puzzle + fmt.Sprint(n)))
		find[i] = hex.EncodeToString(h[:])
	}
	gotCaptcha := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/puzzle":
			fmt.Fprintf(w, `{"success":true,"data":{"puzzle":%q,"range":100,"find":[%q,%q]}}`, puzzle, find[1], find[0])
		case "/captcha":
			_ = r.ParseForm()
			gotCaptcha = r.Form.Get("captcha")
			fmt.Fprint(w, "ok")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := solveWorkuploadPuzzle(workuploadTestClient(server), "ua"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotCaptcha != "3 7 " {
		t.Errorf("captcha = %q, want %q", gotCaptcha, "3 7 ")
	}
}

func TestResolveWorkupload_PoWThenLink(t *testing.T) {
	t.Parallel()
	puzzle := "p"
	h := sha256.Sum256([]byte(puzzle + "42"))
	find := hex.EncodeToString(h[:])
	solved := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/file/abc":
			if !solved {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, `<html><title>workupload - Are you a human?</title><script>url: "/puzzle"</script></html>`)
				return
			}
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<a id="directDownload" href="https://workupload.com/file/abc/download">Download</a>`)
		case "/puzzle":
			fmt.Fprintf(w, `{"success":true,"data":{"puzzle":%q,"range":100,"find":[%q]}}`, puzzle, find)
		case "/captcha":
			solved = true
			fmt.Fprint(w, "ok")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	r := NewHostResolver()
	r.client = workuploadTestClient(server)

	got, err := r.resolveWorkupload("https://workupload.com/file/abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.URL != "https://workupload.com/file/abc/download" {
		t.Errorf("got %q", got.URL)
	}
}

func TestResolveWorkupload_NotFound(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><title>File not found</title></html>`)
	}))
	defer server.Close()

	r := NewHostResolver()
	r.client = workuploadTestClient(server)
	_, err := r.resolveWorkupload("https://workupload.com/file/abc")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveWorkupload_NoLink(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html>page with no download link</html>`)
	}))
	defer server.Close()

	r := NewHostResolver()
	r.client = workuploadTestClient(server)
	if _, err := r.resolveWorkupload("https://workupload.com/file/abc"); err == nil {
		t.Fatal("expected error")
	}
}
