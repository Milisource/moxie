package browserresolve

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
	"github.com/go-rod/rod/lib/proto"
	"github.com/go-rod/stealth"
)

// TestMaskedPageStateLive launches the real masked page and dumps the
// browser's view of it over time: current URL, host_link presence,
// reCAPTCHA widget state, login markers, CF challenge markers. This shows
// what the masked solver actually sees on a real F95Zone session.
func TestMaskedPageStateLive(t *testing.T) {
	if os.Getenv("MOXIE_BROWSERRESOLVE_LIVE") != "1" || os.Getenv("MOXIE_LIVE") == "" {
		t.Skip("set MOXIE_BROWSERRESOLVE_LIVE=1 and MOXIE_LIVE=1")
	}
	bin := os.Getenv("MOXIE_CHROME_BIN")
	if bin == "" {
		bin = findPlaywrightChromium()
	}
	if bin == "" {
		t.Skip("no Chromium binary found")
	}
	const masked = "https://f95zone.to/masked/pixeldrain.com/6004/6265512/w0QsrGbXtWLejRyJd2K_9f3Qn94/1TuWkfbn_zSTdTuHu0eM5Q/mifP5JR4lIKvNQsat27MNlzAaXuDtdhleLdtNSqJ_EhFo05L8ZEyzb1ClXjc7Qwm"

	profile, err := discoverProfileDir("")
	if err != nil {
		t.Fatalf("profile: %v", err)
	}
	copyDir, err := copyProfileForSession(profile)
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	defer removeDir("probe profile copy", copyDir)

	l := launcher.New().Bin(bin).UserDataDir(copyDir)
	l.Delete(flags.Flag("enable-automation"))
	u, err := l.Launch()
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	defer l.Cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	b := rod.New().ControlURL(u).Context(ctx).MustConnect()
	defer b.MustClose()
	p := b.MustPage("about:blank")
	remove, _ := p.EvalOnNewDocument(stealth.JS)
	defer remove()

	state := func(tag string) {
		info, _ := p.Info()
		url := "?"
		if info != nil {
			url = info.URL
		}
		probe, _ := p.Eval(`() => JSON.stringify({
			hostLink: !!document.querySelector('a.host_link'),
			checkbox: !!document.querySelector('#recaptcha-anchor'),
			checkboxChecked: (document.querySelector('#recaptcha-anchor')||{}).getAttribute && (document.querySelector('#recaptcha-anchor')||{}).getAttribute('aria-checked'),
			captchaDiv: !!document.querySelector('#captcha'),
			captchaVisible: !!document.querySelector('#captcha') && document.querySelector('#captcha').offsetParent !== null,
			loginLink: !!document.querySelector('a[href*="login/login"]'),
			cfChallenge: document.body && document.body.innerText.includes('Just a moment'),
			title: document.title
		})`)
		val := ""
		if probe != nil {
			val = probe.Value.Str()
		}
		t.Logf("[%s] url=%s state=%s", tag, url, val)
	}

	// Inject exactly like the engine does, then dump the jar BEFORE any
	// navigation — if the session cookies are already missing, SetCookies
	// itself dropped them.
	injectCookiesForURL(b, masked)
	if res, err2 := (proto.NetworkGetCookies{Urls: []string{"https://f95zone.to/"}}).Call(p); err2 == nil {
		for _, c := range res.Cookies {
			t.Logf("jar-after-inject: name=%q domain=%q path=%q secure=%v httponly=%v value=%q",
				c.Name, c.Domain, c.Path, c.Secure, c.HTTPOnly, c.Value)
		}
	} else {
		t.Logf("getCookies after inject: %v", err2)
	}

	p.MustNavigate(masked)
	state("t+1s")
	time.Sleep(2 * time.Second)

	// Dump the browser's actual cookie jar for f95zone.to (CDP ground
	// truth — document.cookie hides HttpOnly).
	res, err := proto.NetworkGetCookies{Urls: []string{"https://f95zone.to/"}}.Call(p)
	if err == nil {
		for _, c := range res.Cookies {
			t.Logf("browser cookie: name=%q domain=%q path=%q secure=%v httponly=%v value=%q",
				c.Name, c.Domain, c.Path, c.Secure, c.HTTPOnly, c.Value)
		}
	} else {
		t.Logf("getCookies: %v", err)
	}
	time.Sleep(4 * time.Second)
	state("t+5s")
	time.Sleep(6 * time.Second)
	state("t+11s")

	// Click the Continue link if present.
	if el, err := p.Timeout(3 * time.Second).ElementX(maskedHostLinkXPath); err == nil {
		t.Log("clicking host_link")
		_ = el.Click(proto.InputMouseButtonLeft, 1)
		time.Sleep(3 * time.Second)
		state("after-continue")
	}
	// Click the checkbox if present and unchecked.
	if el, err := p.Timeout(3 * time.Second).ElementX(`//*[@id='recaptcha-anchor' and @aria-checked='false']`); err == nil {
		t.Log("clicking recaptcha checkbox")
		_ = el.Click(proto.InputMouseButtonLeft, 1)
		time.Sleep(5 * time.Second)
		state("after-checkbox")
	}
	// Final state.
	time.Sleep(3 * time.Second)
	state("final")
	_ = fmt.Sprint()
}
