package browserresolve

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
)

// TestLooksLikeVerification covers the phrasing Cloudflare actually serves,
// plus the F95Zone /dl/ check page that must NOT be treated as a wall (the
// click flow handles it; gating it would deadlock the solver).
func TestLooksLikeVerification(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		title string
		body  string
		want  bool
	}{
		{
			name:  "cloudflare title",
			title: "Just a moment...",
			want:  true,
		},
		{
			name: "cloudflare managed challenge body",
			body: "f95zone.to Performing security verification This website uses a security service to protect against malicious bots. " +
				"This page is displayed while the website verifies you are not a bot. Ray ID: a4fc6bc",
			want: true,
		},
		{
			name: "cloudflare bot-protection wording",
			body: "This website uses a security service to protect against malicious bots.",
			want: true,
		},
		{
			name: "hcaptcha human wording is not a wall",
			body: "Please verify you are human by completing the action below.",
			want: false,
		},
		{
			name:  "browser check wording",
			title: "Checking your browser before accessing f95zone.to",
			want:  true,
		},
		{
			name:  "f95zone quick check page",
			title: "Just a quick check",
			body:  "Just a quick check This is just a quick check to make sure you are not a robot. Continue",
			want:  false,
		},
		{
			name:  "normal host page",
			title: "Pixeldrain",
			body:  "Game_v1.0.zip 1.2 GB Download",
			want:  false,
		},
		{name: "empty", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := looksLikeVerification(tt.title, tt.body); got != tt.want {
				t.Errorf("looksLikeVerification(%q, %q) = %v, want %v", tt.title, tt.body, got, tt.want)
			}
		})
	}
}

func TestRetryHeadful_HeadfulSessionDoesNotRetry(t *testing.T) {
	t.Parallel()
	cause := errors.New("verification")
	called := false
	_, err := retryHeadful(context.Background(), Options{Headful: true}, cause, func(context.Context, Options) (string, error) {
		called = true
		return "dest", nil
	})
	if called {
		t.Fatal("headful session must not retry headful")
	}
	if !errors.Is(err, cause) {
		t.Errorf("err = %v, want the original cause", err)
	}
}

func TestRetryHeadful_RetriesVisibleAndKeepsOtherOptions(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Setenv("DISPLAY", ":0")
		t.Setenv("WAYLAND_DISPLAY", "")
	}
	var got Options
	dest, err := retryHeadful(context.Background(), Options{MinSize: 42}, errors.New("verification"), func(_ context.Context, o Options) (string, error) {
		got = o
		return "https://pixeldrain.com/u/abc", nil
	})
	if err != nil {
		t.Fatalf("retryHeadful: %v", err)
	}
	if dest != "https://pixeldrain.com/u/abc" {
		t.Errorf("dest = %q", dest)
	}
	if !got.Headful {
		t.Error("retry must set Headful")
	}
	if got.MinSize != 42 {
		t.Errorf("MinSize = %d, want 42 (options must be carried over)", got.MinSize)
	}
}

func TestRetryHeadful_NoDisplayFailsWithHint(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("display detection only gates Linux")
	}
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	cause := errors.New("verification")
	called := false
	_, err := retryHeadful(context.Background(), Options{}, cause, func(context.Context, Options) (string, error) {
		called = true
		return "dest", nil
	})
	if called {
		t.Fatal("must not launch a hidden window when no display exists")
	}
	if !errors.Is(err, cause) {
		t.Errorf("err = %v, want the original cause wrapped", err)
	}
	if !strings.Contains(err.Error(), "--open") {
		t.Errorf("err = %v, want the manual `--open` hint", err)
	}
}
