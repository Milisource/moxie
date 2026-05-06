package steam

import (
	"regexp"
	"strconv"
)

var steamAppIDRe = regexp.MustCompile(`/app/(\d+)(?:/|$)`)

// ExtractSteamAppID extracts a Steam App ID from a store URL.
// Example: "https://store.steampowered.com/app/12345/GameName/" → (12345, true)
func ExtractSteamAppID(storeURL string) (int, bool) {
	matches := steamAppIDRe.FindStringSubmatch(storeURL)
	if len(matches) < 2 {
		return 0, false
	}
	id, err := strconv.Atoi(matches[1])
	return id, err == nil
}
