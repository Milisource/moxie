package browserresolve

import (
	"net/url"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/mili/moxie/internal/browser"
	"github.com/mili/moxie/internal/log"
)

// injectCookiesForURL seeds the browser session with the user's cookies for
// the target URL's host before navigation — extracted from EVERY browser
// store (kooky), so a Chromium session can carry cookies that live in the
// user's Firefox (or vice versa). This is what lets the rod engine pass the
// F95Zone masked-URL wall on machines whose session cookies are not in a
// Chrome-family profile.
func injectCookiesForURL(b *rod.Browser, rawURL string) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return
	}
	params := browser.GetCookieParamsForHost(u.Hostname())
	if len(params) == 0 {
		return
	}
	cookies := make([]*proto.NetworkCookieParam, 0, len(params))
	for _, c := range params {
		cookies = append(cookies, &proto.NetworkCookieParam{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Secure:   c.Secure,
			HTTPOnly: c.HTTPOnly,
			Expires:  proto.TimeSinceEpoch(c.Expires),
		})
	}
	if err := b.SetCookies(cookies); err != nil {
		log.Debug("browserresolve: cookie injection failed", "host", u.Hostname(), "error", err)
		return
	}
	log.Info("browserresolve: injected session cookies", "host", u.Hostname(), "count", len(cookies))
}
