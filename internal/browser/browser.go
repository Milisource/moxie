// Package browser provides cross-browser cookie extraction for authenticating
// with web services. Uses github.com/browserutils/kooky which reads SQLite
// files at the binary level (B-tree parser), avoiding WAL/locking issues
// that plague SQL drivers when reading live browser databases.
package browser

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/browserutils/kooky"
	_ "github.com/browserutils/kooky/browser/firefox"
)

// GetF95Cookies extracts f95zone.to cookies from installed browsers (Firefox,
// Chrome, etc.). Returns a Cookie header string suitable for HTTP requests,
// e.g. "xf_session=abc; cf_clearance=def; xf_user=ghi"
func GetF95Cookies() (string, error) {
	cookies, err := kooky.ReadCookies(
		context.Background(),
		kooky.Valid,
		kooky.Domain("f95zone.to"),
	)
	if err != nil {
		return "", fmt.Errorf("reading browser cookies: %w", err)
	}

	var f95 []*kooky.Cookie
	for _, c := range cookies {
		if c.Domain == "f95zone.to" || strings.HasSuffix(c.Domain, ".f95zone.to") {
			f95 = append(f95, c)
		}
	}

	if len(f95) == 0 {
		return "", fmt.Errorf("no f95zone.to cookies found in any browser\n" +
			"Make sure you're logged into f95zone.to in Firefox or Chrome")
	}

	sort.Slice(f95, func(i, j int) bool {
		return f95[i].Name < f95[j].Name
	})

	return buildCookieHeader(f95), nil
}

// buildCookieHeader constructs a Cookie header string from cookie pairs.
func buildCookieHeader(cookies []*kooky.Cookie) string {
	pairs := make([]string, len(cookies))
	for i, c := range cookies {
		pairs[i] = c.Name + "=" + c.Value
	}
	return strings.Join(pairs, "; ")
}
