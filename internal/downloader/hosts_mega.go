package downloader

import (
	"fmt"

	"github.com/mili/moxie/internal/log"
)

// --- Mega ---
// Mega uses a proprietary encrypted protocol that plain HTTP cannot handle.
// When the megatools binary is installed, resolveMega signals the download
// flow to delegate to 'megatools dl' via ErrMegaNeedsMegatools (intercepted
// by DownloadWithContext). Without megatools the user gets install
// instructions instead of a silent failure.
func (r *HostResolver) resolveMega(url string) (*ResolveResult, error) {
	if _, err := findMegatools(); err != nil {
		log.Info("mega link skipped (megatools not installed)", "url", redactedURL(url))
		return nil, fmt.Errorf(
			"Mega uses encrypted protocol - use megatools CLI:\n"+
				"  megatools dl --path <dest> '%s'\n"+
				"  Install: brew install megatools / apt install megatools",
			url,
		)
	}
	log.Debug("mega link delegating to megatools subprocess", "url", redactedURL(url))
	return nil, fmt.Errorf(
		"%w: megatools installed — the download flow will run 'megatools dl --path <dest> '%s'",
		ErrMegaNeedsMegatools, url,
	)
}
