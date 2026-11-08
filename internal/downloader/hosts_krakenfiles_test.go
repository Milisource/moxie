package downloader

import (
	"strings"
	"testing"
)

func TestParseKrakenFilesResponse_OK(t *testing.T) {
	got, err := parseKrakenFilesResponse(
		"https://krakenfiles.com/view/abc/file.html",
		`{"status":"ok","url":"https://cdn.krakenfiles.com/file/abc.zip","msg":""}`,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.URL != "https://cdn.krakenfiles.com/file/abc.zip" {
		t.Errorf("got %q", got.URL)
	}
}

func TestParseKrakenFilesResponse_ServerError(t *testing.T) {
	_, err := parseKrakenFilesResponse(
		"https://krakenfiles.com/view/abc/file.html",
		`{"status":"error","url":"","msg":"Server error"}`,
	)
	if err == nil || !strings.Contains(err.Error(), "Server error") {
		t.Fatalf("expected server error, got %v", err)
	}
}

func TestParseKrakenFilesResponse_Malformed(t *testing.T) {
	if _, err := parseKrakenFilesResponse("https://krakenfiles.com/view/abc/file.html", "<html>"); err == nil {
		t.Fatal("expected error for non-JSON response")
	}
}

func TestParseKrakenFilesResponse_InvalidURL(t *testing.T) {
	if _, err := parseKrakenFilesResponse(
		"https://krakenfiles.com/view/abc/file.html",
		`{"status":"ok","url":"http://insecure.example/file.zip"}`,
	); err == nil {
		t.Fatal("expected error for non-HTTPS URL")
	}
}
