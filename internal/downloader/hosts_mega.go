package downloader

import (
	"fmt"

	"github.com/mili/moxie/internal/log"
)

// --- Mega ---
// Mega uses proprietary encrypted protocol. Cannot be handled with simple HTTP.
// Inform user to use megatools CLI or download manually in browser.
func (r *HostResolver) resolveMega(url string) (*ResolveResult, error) {
	log.Info("mega link skipped (unsupported protocol)", "url", url)
	return nil, fmt.Errorf(
		"Mega uses encrypted protocol - use megatools CLI:\n"+
			"  megatools dl --path <dest> '%s'\n"+
			"  Install: brew install megatools / apt install megatools",
		url,
	)
}
