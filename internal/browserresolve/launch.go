package browserresolve

// launchFlags returns the Chrome command-line flags required for a stealth
// download session launched on a COPIED profile:
//
//   - --user-data-dir: always the copied profile — Chrome >= 136 ignores
//     --remote-debugging-port against the real profile dir, and the live
//     profile is hard-locked by SingletonLock
//   - --no-first-run: suppress first-run dialogs that would break a freshly
//     copied profile
//   - --headless=new (unless headful): the modern headless mode; the
//     challenge engine treats it like a real browser
//
// navigator.webdriver is neutralized by script injection in open(), not by
// a launch flag — see the note in applyLaunchFlags.
//
// rod's own launcher defaults (random --remote-debugging-port, leakless
// process-group teardown, ...) are layered on top by applyLaunchFlags.
func launchFlags(profileDir string, headful bool) []string {
	args := []string{
		"--user-data-dir=" + profileDir,
		"--no-first-run",
	}
	if !headful {
		args = append(args, "--headless=new")
	}
	return args
}
