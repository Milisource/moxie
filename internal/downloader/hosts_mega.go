package downloader

import (
	"fmt"
	"os/exec"

	"github.com/mili/moxie/internal/log"
)

// --- Mega ---
// Mega uses a proprietary encrypted protocol, so plain HTTP cannot fetch the
// file. When megatools is installed we hand the link to it; megatools performs
// the key exchange and AES-CTR decryption itself and supports resume.
//
// Install: apt install megatools / brew install megatools
func (r *HostResolver) resolveMega(url string) (*ResolveResult, error) {
	path, err := exec.LookPath("megatools")
	if err != nil {
		log.Info("mega link skipped: megatools not installed", "url", url)
		return nil, fmt.Errorf(
			"Mega needs megatools (not installed). Install it with:\n" +
				"  apt install megatools   (Debian/Ubuntu)\n" +
				"  brew install megatools  (macOS)\n" +
				"  pacman -S megatools     (Arch)\n" +
				"then retry the download",
		)
	}

	log.Debug("mega link delegated to megatools", "binary", path)
	return &ResolveResult{
		URL:             url,
		Headers:         map[string]string{},
		ExternalCommand: path,
	}, nil
}
