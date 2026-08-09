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
)

// TestStealthFlagsLive A/Bs the rod launcher flag sets against
// bot.sannysoft.com with a REAL Chromium:
//   - current: rod defaults with --enable-automation removed (status quo)
//   - hardened: + --disable-blink-features=AutomationControlled
//
// The page's JS detection table reports navigator.webdriver and headless
// markers; the row class is "passed" or "failed". Also reads the raw JS
// values as ground truth.
func TestStealthFlagsLive(t *testing.T) {
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
	check := func(label string, hardened bool) {
		profile := buildProfileFixture(t)
		// The fixture writes SingletonLock as a plain file (copy-behavior
		// tests); Chrome expects a symlink — strip it for a real launch.
		os.Remove(filepath.Join(profile, "SingletonLock"))
		os.Remove(filepath.Join(profile, "SingletonSocket"))
		l := launcher.New().Bin(bin).UserDataDir(profile)
		l.Delete(flags.Flag("enable-automation"))
		if hardened {
			l.Set(flags.Flag("disable-blink-features"), "AutomationControlled")
		}
		u, err := l.Launch()
		if err != nil {
			t.Fatalf("%s: launch: %v", label, err)
		}
		defer l.Cleanup()

		b := rod.New().ControlURL(u).MustConnect()
		defer b.MustClose()
		p := b.MustPage("https://bot.sannysoft.com/")
		p.MustWaitLoad()
		p.MustElement("body")
		time.Sleep(2500 * time.Millisecond)

		vals, err := p.Eval(`() => JSON.stringify({
			webdriver: navigator.webdriver,
			chrome: typeof window.chrome,
			headlessUA: navigator.userAgent.includes('HeadlessChrome'),
			plugins: navigator.plugins.length,
			languages: navigator.languages.join(',')
		})`)
		if err != nil {
			t.Fatalf("%s: eval: %v", label, err)
		}
		var raw struct {
			Webdriver bool   `json:"webdriver"`
			Chrome    string `json:"chrome"`
			Headless  bool   `json:"headlessUA"`
			Plugins   int    `json:"plugins"`
			Languages string `json:"languages"`
		}
		_ = json.Unmarshal([]byte(vals.Value.Str()), &raw)
		t.Logf("%s: navigator.webdriver=%v window.chrome=%q headlessUA=%v plugins=%d languages=%q",
			label, raw.Webdriver, raw.Chrome, raw.Headless, raw.Plugins, raw.Languages)

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

	check("current", false)
	check("hardened", true)
}
