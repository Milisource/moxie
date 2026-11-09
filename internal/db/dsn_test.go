package db

import (
	"strings"
	"testing"
)

// TestDSNForPath pins the DSN shape that broke Windows: a drive-qualified
// path must stay in the path component, never become a URI authority
// ("file://C:/..."), or the WASM VFS resolves the wrong path and Open fails.
func TestDSNForPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string // prefix the DSN must start with
	}{
		{
			name: "posix absolute",
			path: "/home/u/.config/moxie/games.db",
			want: "file:/home/u/.config/moxie/games.db?",
		},
		{
			// Windows filepath.ToSlash produces this form before the DSN is built.
			name: "windows drive path",
			path: "C:/users/u/AppData/Roaming/moxie/games.db",
			want: "file:C:/users/u/AppData/Roaming/moxie/games.db?",
		},
		{
			name: "spaces are escaped",
			path: "C:/Program Files/moxie/games.db",
			want: "file:C:/Program%20Files/moxie/games.db?",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dsnForPath(tt.path)
			if !strings.HasPrefix(got, tt.want) {
				t.Errorf("dsnForPath(%q) = %q, want prefix %q", tt.path, got, tt.want)
			}
			// The regression: a double slash turns the drive letter into an
			// authority. A bare "file:" prefix must never be followed by "//".
			if strings.HasPrefix(got, "file://") {
				t.Errorf("dsnForPath(%q) = %q: drive path became a URI authority", tt.path, got)
			}
			if !strings.Contains(got, "_pragma=foreign_keys(1)") ||
				!strings.Contains(got, "_pragma=busy_timeout(5000)") {
				t.Errorf("dsnForPath(%q) = %q: missing per-connection pragmas", tt.path, got)
			}
		})
	}
}
