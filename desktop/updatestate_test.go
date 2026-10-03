package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mili/moxie/internal/db"
)

func TestGameUpdateState(t *testing.T) {
	tests := []struct {
		latest, installed, want string
	}{
		{"", "1.0", ""},
		{"", "", ""},
		{"v1.03", "", updateUnknown},
		{"Final", "", ""},
		{"v1.03", "1.03", updateCurrent}, // prefix-only difference is no update
		{"v0.4", "0.4", updateCurrent},
		{"v1.1", "1.0", updateAvailable},
		{"v1.0.5", "5", updateCurrent}, // installed compares newer
		{"Ep. 4", "Ep. 3", updateAvailable},
	}
	for _, tt := range tests {
		if got := gameUpdateState(tt.latest, tt.installed); got != tt.want {
			t.Errorf("gameUpdateState(%q, %q) = %q, want %q", tt.latest, tt.installed, got, tt.want)
		}
	}
}

// Unknown-version games must reach the Updates view (the old SQL dropped
// them: NULL != x is NULL), while phantom prefix-only "updates" must not, and
// the sidebar count covers confirmed updates only.
func TestGetUpdatableGamesClassifies(t *testing.T) {
	a := newTestApp(t)
	add := func(title, ver, latest string) int64 {
		id, err := a.db.InsertGame(&db.Game{Title: title, Path: "/g/" + title, Engine: "RenPy", Status: "active", Version: ver, LatestVersion: latest})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	avail := add("Avail", "1.0", "v1.1")
	unknown := add("Unknown", "", "v2.0")
	add("Phantom", "0.4", "v0.4")
	add("FinalUnknown", "", "Final")
	add("NoLatest", "1.0", "")

	got, err := a.GetUpdatableGames()
	if err != nil {
		t.Fatal(err)
	}
	states := map[int64]string{}
	for _, g := range got {
		states[g.ID] = g.UpdateState
	}
	if len(got) != 2 || states[avail] != updateAvailable || states[unknown] != updateUnknown {
		t.Errorf("GetUpdatableGames = %+v, want only Avail (available) and Unknown (unknown)", states)
	}
	n, err := a.GetUpdatableCount()
	if err != nil || n != 1 {
		t.Errorf("GetUpdatableCount = %d, %v; want 1", n, err)
	}
}

func TestRankDownloadLinksOrdersAndFilters(t *testing.T) {
	links := []DesktopDownloadLink{
		{ID: 1, Host: "unknownhost", Platform: "all"},
		{ID: 2, Host: "pixeldrain", Platform: "all"},
		{ID: 3, Host: "pixeldrain", Platform: "all", IsDead: true},
		{ID: 4, Host: "gofile", Platform: "all"},
	}
	ranked, err := rankDownloadLinks(links)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range ranked {
		if l.IsDead {
			t.Errorf("dead link %d ranked", l.ID)
		}
	}
	if len(ranked) != 3 {
		t.Fatalf("ranked %d links, want 3", len(ranked))
	}
	best, _ := selectDownloadLink(links)
	if best.ID != ranked[0].ID {
		t.Errorf("selectDownloadLink = %d, want ranked[0] = %d", best.ID, ranked[0].ID)
	}
}

func TestTryDownloadLinksFallsBack(t *testing.T) {
	links := []db.DownloadLink{{Host: "a"}, {Host: "b"}, {Host: "c"}, {Host: "d"}}
	ctx := context.Background()

	t.Run("second link succeeds", func(t *testing.T) {
		var tried []string
		var lastFlags []bool
		var retries []string
		path, link, err := tryDownloadLinks(ctx, links,
			func(l db.DownloadLink, last bool) (string, error) {
				tried = append(tried, l.Host)
				lastFlags = append(lastFlags, last)
				if l.Host == "a" {
					return "", errors.New("host a blocked")
				}
				return "/work/game.zip", nil
			},
			func(failed, next db.DownloadLink, err error) { retries = append(retries, failed.Host+">"+next.Host) })
		if err != nil || path != "/work/game.zip" || link.Host != "b" {
			t.Fatalf("got (%q, %q, %v), want (/work/game.zip, b, nil)", path, link.Host, err)
		}
		if strings.Join(tried, ",") != "a,b" || strings.Join(retries, ",") != "a>b" {
			t.Errorf("tried=%v retries=%v", tried, retries)
		}
		if lastFlags[0] || lastFlags[1] {
			t.Errorf("last flags = %v; neither of the first 2 of 3 attempts is last", lastFlags)
		}
	})

	t.Run("caps attempts and reports last error", func(t *testing.T) {
		var tried []string
		_, link, err := tryDownloadLinks(ctx, links,
			func(l db.DownloadLink, last bool) (string, error) {
				tried = append(tried, l.Host)
				if last != (l.Host == "c") {
					t.Errorf("host %s last=%v", l.Host, last)
				}
				return "", errors.New("fail " + l.Host)
			},
			func(db.DownloadLink, db.DownloadLink, error) {})
		if len(tried) != maxDownloadFallbackLinks || link.Host != "c" || err == nil || err.Error() != "fail c" {
			t.Fatalf("tried=%v link=%s err=%v", tried, link.Host, err)
		}
	})

	t.Run("missing cookies stops immediately", func(t *testing.T) {
		calls := 0
		_, _, err := tryDownloadLinks(ctx, links,
			func(db.DownloadLink, bool) (string, error) { calls++; return "", errNoF95Cookies },
			func(db.DownloadLink, db.DownloadLink, error) { t.Error("unexpected retry") })
		if calls != 1 || !errors.Is(err, errNoF95Cookies) {
			t.Fatalf("calls=%d err=%v", calls, err)
		}
	})

	t.Run("no links", func(t *testing.T) {
		if _, _, err := tryDownloadLinks(ctx, nil, nil, nil); err == nil {
			t.Fatal("expected error for empty link list")
		}
	})
}

func TestUpdatableGate(t *testing.T) {
	if !updatable("v0.5", "") {
		t.Error("unknown installed version must be updatable on request")
	}
	if !updatable("v0.5", "0.4") {
		t.Error("newer latest must be updatable")
	}
	if updatable("v0.4", "0.4") || updatable("", "0.4") {
		t.Error("same/absent latest must not be updatable")
	}
}

// Game 74 (My Hentai Fantasy, Windows install) picked the Linux .tar.bz2
// and failed extraction. With install-aware ranking only Win links remain,
// and Android links (stored as "unknown") are never candidates.
func TestRankForInstall_Game74(t *testing.T) {
	var links []DesktopDownloadLink
	id := int64(1)
	for _, plat := range []string{"Android", "Win", "Linux", "Mac"} {
		for _, host := range []string{"buzzheavier", "datanodes", "pixeldrain", "vikingfile", "mega"} {
			links = append(links, DesktopDownloadLink{ID: id, Host: host, Name: "DOWNLOAD · " + plat, URL: "https://f95zone.to/masked/" + host + ".com/1/2/x"})
			id++
		}
	}
	opts := updateRankOpts(&db.Game{ExePath: "/g/My Hentai Fantasy/My_hentai_fantasy.exe", Version: "0.11", LatestVersion: "0.18.1"})
	ranked, err := rankDownloadLinksFor(links, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(ranked) != 5 {
		t.Fatalf("got %d candidates, want the 5 Win links", len(ranked))
	}
	for _, l := range ranked {
		if l.Name != "DOWNLOAD · Win" {
			t.Errorf("non-Win candidate %q (%s)", l.Name, l.Host)
		}
	}
	if ranked[0].Host != "pixeldrain" {
		t.Errorf("best host = %s, want pixeldrain", ranked[0].Host)
	}
}

func TestRankPatchLinks(t *testing.T) {
	links := []DesktopDownloadLink{
		{ID: 1, Host: "pixeldrain", Name: "All · Win"},
		{ID: 2, Host: "pixeldrain", Name: "Update Only (v0.17 -> v0.18) · Win"},
		{ID: 3, Host: "pixeldrain", Name: "Unofficial Mod · Win"},
	}
	g := &db.Game{ExePath: "/g/B/Bunker.exe", Version: "0.17", LatestVersion: "0.18"}
	ranked, err := rankDownloadLinksFor(links, updateRankOpts(g))
	if err != nil {
		t.Fatal(err)
	}
	if len(ranked) != 2 || ranked[0].ID != 2 || ranked[1].ID != 1 {
		t.Fatalf("ranked = %+v, want patch first then full build, no mod", ranked)
	}

	// Installed 0.16: the 0.17→0.18 patch doesn't apply.
	g.Version = "0.16"
	ranked, _ = rankDownloadLinksFor(links, updateRankOpts(g))
	if len(ranked) != 1 || ranked[0].ID != 1 {
		t.Fatalf("ranked = %+v, want only the full build", ranked)
	}

	// Unknown installed version: never patch.
	g.Version = ""
	ranked, _ = rankDownloadLinksFor(links, updateRankOpts(g))
	if len(ranked) != 1 || ranked[0].ID != 1 {
		t.Fatalf("ranked = %+v, want only the full build", ranked)
	}
}

func TestUpdateDownloadPageURL(t *testing.T) {
	masked := "https://f95zone.to/masked/pixeldrain.com/1/2/x"
	links := []db.DownloadLink{{URL: masked}, {URL: "https://other"}}
	cache := func(u string) (string, bool) {
		if u == masked {
			return "https://pixeldrain.com/u/aQiB1niF", true
		}
		return "", false
	}
	none := func(string) (string, bool) { return "", false }
	thread := "https://f95zone.to/threads/x.1/"

	if got := updateDownloadPageURL(links, cache, thread); got != "https://pixeldrain.com/u/aQiB1niF" {
		t.Errorf("resolved: got %s", got)
	}
	if got := updateDownloadPageURL(links, none, thread); got != masked {
		t.Errorf("masked fallback: got %s", got)
	}
	if got := updateDownloadPageURL(nil, none, thread); got != thread {
		t.Errorf("thread fallback: got %s", got)
	}
}
