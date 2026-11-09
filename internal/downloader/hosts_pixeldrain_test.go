package downloader

import "testing"

// ---------------------------------------------------------------------------
// HostResolver.Resolve - Pixeldrain Referer
// ---------------------------------------------------------------------------
// Track D research (2026-08-09): pixeldrain serves reliably only when the
// API request carries Referer https://pixeldrain.com/u/<ID> (the original
// page URL). /api/file/<ID> inputs have no page URL available, so the
// resolver falls back to the bare origin referer.

func TestResolvePixeldrain_RefererHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		wantURL     string
		wantReferer string
	}{
		{
			name:        "page URL sets Referer to page URL",
			input:       "https://pixeldrain.com/u/abc123",
			wantURL:     "https://pixeldrain.com/api/file/abc123",
			wantReferer: "https://pixeldrain.com/u/abc123",
		},
		{
			name:        "API URL falls back to bare origin referer",
			input:       "https://pixeldrain.com/api/file/xyz789",
			wantURL:     "https://pixeldrain.com/api/file/xyz789",
			wantReferer: "https://pixeldrain.com/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := NewHostResolver()
			result, err := r.Resolve(tt.input, "pixeldrain")
			if err != nil {
				t.Fatalf("Resolve pixeldrain failed: %v", err)
			}
			if result == nil {
				t.Fatal("Resolve pixeldrain returned nil result")
			}
			if result.URL != tt.wantURL {
				t.Errorf("expected URL %q, got %q", tt.wantURL, result.URL)
			}
			if result.Headers == nil {
				t.Fatal("expected non-nil headers")
			}
			if got := result.Headers["Referer"]; got != tt.wantReferer {
				t.Errorf("expected Referer %q, got %q", tt.wantReferer, got)
			}
			if ua := result.Headers["User-Agent"]; ua == "" {
				t.Error("expected User-Agent header in resolved result")
			}
		})
	}
}
