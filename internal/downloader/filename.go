package downloader

import (
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"
)

// safeDestName decodes before removing path components on either platform.
func safeDestName(rawURL, contentDisposition string) (string, error) {
	raw := contentDispositionFilename(contentDisposition)
	fromDisposition := raw != ""
	if raw == "" {
		u, err := url.Parse(rawURL)
		if err != nil {
			return "", fmt.Errorf("invalid download filename: %w", err)
		}
		raw = u.EscapedPath()
	}
	decoded := raw
	// mime.ParseMediaType already decodes RFC 5987 filename*. Plain
	// disposition names may contain literal percent signs as well as escapes.
	extended := strings.Contains(strings.ToLower(strings.Join(strings.Fields(contentDisposition), "")), "filename*=")
	if !fromDisposition || !extended {
		unescaped, err := url.PathUnescape(raw)
		if err != nil && !fromDisposition {
			return "", fmt.Errorf("invalid download filename: %w", err)
		}
		if err == nil {
			decoded = unescaped
		}
	}
	name := path.Base(strings.ReplaceAll(decoded, "\\", "/"))
	if decoded == "" || strings.HasSuffix(decoded, "/") || strings.HasSuffix(decoded, "\\") || name == "" || name == "." || name == ".." || strings.HasPrefix(name, ".") || strings.ContainsAny(name, "/\\\x00") {
		return "", fmt.Errorf("unsafe download filename %q", name)
	}
	// ':' is legal on Linux/macOS but not on Windows, so neutralise it rather
	// than failing a download whose bytes already arrived ("Game: Remake.zip").
	name = strings.ReplaceAll(name, ":", "_")
	prefix := strings.ToUpper(strings.SplitN(strings.TrimRight(name, " ."), ".", 2)[0])
	if prefix == "CON" || prefix == "PRN" || prefix == "AUX" || prefix == "NUL" || len(prefix) == 4 && (strings.HasPrefix(prefix, "COM") || strings.HasPrefix(prefix, "LPT")) && prefix[3] >= '1' && prefix[3] <= '9' {
		return "", fmt.Errorf("reserved download filename %q", name)
	}
	return name, nil
}

func safeDestPath(destDir, name string) (string, error) {
	dest := filepath.Join(destDir, name)
	rel, err := filepath.Rel(destDir, dest)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("download filename escapes destination")
	}
	return dest, nil
}
