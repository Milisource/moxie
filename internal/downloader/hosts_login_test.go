package downloader

import "testing"

func TestIsLoginRedirect(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		url  string
		want bool
	}{
		{"https://f95zone.to/login", true},
		{"https://www.f95zone.to/login/", true},
		{"https://sub.f95zone.to/login/password", true},
		{"https://F95ZONE.TO/LOGIN", true},
		{"https://f95zone.to/threads/1/?_xfRedirect=/login", true},
		{"https://f95zone.to/_XFREDIRECT/result", true},
		{"https://f95zone.to/Login.zip", false},
		{"https://f95zone.to/Login_Patch.zip", false},
		{"https://f95zone.to/downloads/login/file.zip", false},
		{"https://host.example/Login_Patch.zip", false},
		{"https://host.example/login", false},
		{"https://host.example/?_xfRedirect=/login", false},
		{"https://f95zone.to.evil.example/login", false},
		{"https://evilf95zone.to/login", false},
		{"https://f95zone.to@host.example/login", false},
		{"/login", false},
		{"https://[bad/login", false},
	} {
		t.Run(tc.url, func(t *testing.T) {
			if got := isLoginRedirect(tc.url); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
