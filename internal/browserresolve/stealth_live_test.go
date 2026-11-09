package browserresolve

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
	"github.com/go-rod/stealth"
)

// stealthJS is the full go-rod/stealth evasion bundle (patches
// navigator.webdriver with a native-looking getter, plugins, languages,
// chrome object, canvas...). Its en-US/en languages hardcode is fine for
// F95Zone (an English site) — the rod#1208 locale breakage applies to
// non-English locales.
var stealthJS = stealth.JS

// TestStealthJSInjectionLive A/Bs the navigator.webdriver patch against
// bot.sannysoft.com with a REAL Chromium:
//   - baseline: rod defaults with --enable-automation removed (status quo)
//   - injected: same + the EvalOnNewDocument webdriver patch (what open()
//     does)
//
// The page's JS detection table reports the WebDriver row; the raw
// navigator.webdriver value is the ground truth. (The
// --disable-blink-features=AutomationControlled launch flag is NOT tested:
// live-verified 2026-08-09 that it crashes Playwright Chromium builds on
// profiles with conflicting blink-feature prefs — Brave core-dumps at
// startup.)
func TestStealthJSInjectionLive(t *testing.T) {
	if os.Getenv(liveEnvVar) != "1" {
		t.Skipf("set %s=1 to run the live browser test", liveEnvVar)
	}
	bin := os.Getenv("MOXIE_CHROME_BIN")
	if bin == "" {
		bin = findPlaywrightChromium()
	}
	if bin == "" {
		if p, err := detectBrowserBinary(); err == nil {
			bin = p
		}
	}
	if bin == "" {
		t.Skip("no Chromium binary found")
	}

	check := func(label string, inject bool) {
		profile := buildProfileFixture(t)
		// The fixture writes SingletonLock as a plain file (copy-behavior
		// tests); Chrome expects a symlink — strip it for a real launch.
		os.Remove(filepath.Join(profile, "SingletonLock"))
		os.Remove(filepath.Join(profile, "SingletonSocket"))
		l := launcher.New().Bin(bin).UserDataDir(profile)
		l.Delete(flags.Flag("enable-automation"))
		u, err := l.Launch()
		if err != nil {
			t.Fatalf("%s: launch: %v", label, err)
		}
		defer l.Cleanup()

		b := rod.New().ControlURL(u).MustConnect()
		defer b.MustClose()
		p := b.MustPage("about:blank")
		if inject {
			remove, err := p.EvalOnNewDocument(stealthJS)
			if err != nil {
				t.Fatalf("%s: injection: %v", label, err)
			}
			defer remove()
		}
		p.MustNavigate("https://bot.sannysoft.com/")
		p.MustWaitLoad()
		p.MustElement("body")
		time.Sleep(2500 * time.Millisecond)

		vals, err := p.Eval(`() => JSON.stringify({
			webdriver: navigator.webdriver,
			headlessUA: navigator.userAgent.includes('HeadlessChrome'),
			plugins: navigator.plugins.length
		})`)
		if err != nil {
			t.Fatalf("%s: eval: %v", label, err)
		}
		var raw struct {
			Webdriver any    `json:"webdriver"`
			Headless  bool   `json:"headlessUA"`
			Plugins   int    `json:"plugins"`
		}
		_ = json.Unmarshal([]byte(vals.Value.Str()), &raw)
		t.Logf("%s: navigator.webdriver=%v headlessUA=%v plugins=%d",
			label, raw.Webdriver, raw.Headless, raw.Plugins)

		rows, err := p.Eval(`() => JSON.stringify(Array.from(document.querySelectorAll('table tr')).map(r => {
			const tds = r.querySelectorAll('td');
			if (tds.length < 2) return null;
			return tds[0].textContent.trim() + '|' + (tds[1].className || '');
		}).filter(x => x))`)
		if err == nil {
			var list []string
			_ = json.Unmarshal([]byte(rows.Value.Str()), &list)
			passed, failed := 0, 0
			for _, s := range list {
				t.Logf("  %s", s)
				if strings.HasSuffix(s, "passed") {
					passed++
				} else if strings.HasSuffix(s, "failed") {
					failed++
				}
			}
			t.Logf("%s: detection table passed=%d failed=%d", label, passed, failed)
		}
		p.MustClose()
	}

	check("baseline", false)
	check("injected", true)
}
