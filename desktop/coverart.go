package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"log/slog"
	"os"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/mili/moxie/internal/config"
	"github.com/mili/moxie/internal/coverart"
	"github.com/mili/moxie/internal/db"
)

// CoverArtSettings is the Settings → Cover art state.
type CoverArtSettings struct {
	Steam bool `json:"steam"`
	VNDB  bool `json:"vndb"`
	// SGDBKeySet / SGDBKeyHint: whether a SteamGridDB key is configured and
	// its last 4 characters — the key itself never reaches the frontend.
	SGDBKeySet  bool   `json:"sgdbKeySet"`
	SGDBKeyHint string `json:"sgdbKeyHint"`
	SGDBFromEnv bool   `json:"sgdbFromEnv"`
}

// GetCoverArtSettings returns which cover sources are enabled.
func (a *App) GetCoverArtSettings() CoverArtSettings {
	o := coverart.OptionsFromConfig()
	s := CoverArtSettings{Steam: o.Steam, VNDB: o.VNDB, SGDBKeySet: o.SGDBKey != ""}
	if k := o.SGDBKey; len(k) >= 4 {
		s.SGDBKeyHint = k[len(k)-4:]
	}
	s.SGDBFromEnv = os.Getenv("STEAMGRIDDB_KEY") != ""
	return s
}

// SetCoverSources toggles the keyless sources.
func (a *App) SetCoverSources(steam, vndb bool) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		cfg = &config.Config{}
	}
	cfg.Set(coverart.KeySteam, fmt.Sprint(steam))
	cfg.Set(coverart.KeyVNDB, fmt.Sprint(vndb))
	return config.WriteConfig(cfg)
}

// SetSteamGridDBKey stores (or with "" clears) the SteamGridDB API key in
// the config file shared with the CLI.
func (a *App) SetSteamGridDBKey(key string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		cfg = &config.Config{}
	}
	cfg.Set(coverart.KeySGDB, strings.TrimSpace(key))
	return config.WriteConfig(cfg)
}

// CoverUpgradeResult summarises an UpgradeCovers run.
type CoverUpgradeResult struct {
	Checked  int      `json:"checked"`
	Replaced int      `json:"replaced"`
	Banners  int      `json:"banners"` // landscape banners retained for the wide view
	Failed   int      `json:"failed"`
	Errors   []string `json:"errors,omitempty"` // first few source errors
}

// UpgradeCovers looks up portrait art for every installed game whose cover
// is missing, landscape or under 600px (and not locked), and replaces the
// cover when coverart.Better finds portrait art at least as sharp. Shares
// the cover-fetch single-flight guard. Events:
//
//	covers:upgrade-progress {current, total, title}
//	covers:upgrade-complete CoverUpgradeResult
//	covers:error            {error}
func (a *App) UpgradeCovers() error {
	if a.db == nil {
		return fmt.Errorf("database not initialized")
	}
	opts := coverart.OptionsFromConfig()
	if !opts.Steam && opts.SGDBKey == "" && !opts.VNDB {
		return fmt.Errorf("no cover sources enabled — turn one on in Settings → Cover art")
	}
	if !a.coverRunning.CompareAndSwap(false, true) {
		return fmt.Errorf("cover fetch already running")
	}
	a.goBackground("cover-upgrade", func(ctx context.Context) {
		defer a.coverRunning.Store(false)
		res, err := a.upgradeCoversRun(ctx, opts)
		if err != nil {
			runtime.EventsEmit(a.ctx, "covers:error", map[string]string{"error": err.Error()})
			return
		}
		runtime.EventsEmit(a.ctx, "covers:upgrade-complete", res)
	})
	return nil
}

func (a *App) upgradeCoversRun(ctx context.Context, opts coverart.Options) (CoverUpgradeResult, error) {
	var res CoverUpgradeResult
	games, err := a.db.ListActiveGames("", "")
	if err != nil {
		return res, fmt.Errorf("failed to load games: %w", err)
	}
	var todo []db.Game
	for _, g := range games {
		if db.IsVirtualPath(g.Path) {
			continue
		}
		p := coverPathFor(g.ID)
		m, _ := readCoverMeta(p)
		w, h := coverart.CurrentSize(p)
		if coverart.NeedsUpgrade(w, h, m.Locked) || coverNeedsBanner(p) {
			todo = append(todo, g)
		}
	}
	finder := coverart.NewFinder(opts)
	slog.Info("cover upgrade started", "candidates", len(todo), "steam", opts.Steam, "sgdb", opts.SGDBKey != "", "vndb", opts.VNDB)
	for i, g := range todo {
		if ctx.Err() != nil {
			return res, fmt.Errorf("cover upgrade cancelled")
		}
		runtime.EventsEmit(a.ctx, "covers:upgrade-progress", CoverProgress{Current: i + 1, Total: len(todo), Title: g.Title, Phase: "searching"})
		res.Checked++
		p := coverPathFor(g.ID)
		w, h := coverart.CurrentSize(p)
		m, _ := readCoverMeta(p)
		gameKey := coverart.Game{Title: g.Title, SteamAppID: g.SteamAppID}

		// Portrait primary cover.
		if coverart.NeedsUpgrade(w, h, m.Locked) {
			found := finder.Find(ctx, gameKey)
			for _, e := range found.Errors {
				if len(res.Errors) < 5 {
					res.Errors = append(res.Errors, g.Title+": "+e)
				}
			}
			if pick, ok := coverart.Better(found.Candidates, w, h); ok {
				if err := a.storeCoverCandidate(ctx, g.ID, pick, false); err != nil {
					slog.Warn("cover upgrade: store failed", "game_id", g.ID, "url", pick.URL, "error", err)
					res.Failed++
				} else {
					if found.SteamAppID != 0 {
						_ = a.db.SetGameSteamAppIDIfEmpty(g.ID, found.SteamAppID)
					}
					slog.Info("cover upgraded", "game_id", g.ID, "title", g.Title, "from", fmt.Sprintf("%dx%d", w, h), "to", fmt.Sprintf("%dx%d", pick.W, pick.H), "source", pick.Source)
					res.Replaced++
				}
			}
		}

		// Landscape banner for the wide view. Skipped when replacing the
		// primary already retained its landscape art as the banner.
		if coverNeedsBanner(p) {
			if ban, ok := finder.FindBanner(ctx, gameKey); ok {
				if err := a.storeCoverBanner(ctx, g.ID, ban); err != nil {
					slog.Warn("banner fetch: store failed", "game_id", g.ID, "url", ban.URL, "error", err)
				} else {
					slog.Info("banner fetched", "game_id", g.ID, "title", g.Title, "size", fmt.Sprintf("%dx%d", ban.W, ban.H), "source", ban.Source)
					res.Banners++
				}
			}
		}
	}
	if res.Replaced > 0 {
		releaseDecoderMemory()
	}
	slog.Info("cover upgrade complete", "checked", res.Checked, "replaced", res.Replaced, "failed", res.Failed)
	return res, nil
}

// storeCoverCandidate downloads c and installs it as gameID's cover, then
// rebuilds the thumbnail.
func (a *App) storeCoverCandidate(ctx context.Context, gameID int64, c coverart.Candidate, lock bool) error {
	data, cfg, err := coverart.Fetch(ctx, c.URL)
	if err != nil {
		return err
	}
	return a.storeCoverData(gameID, data, cfg, c, lock)
}

// storeCoverData writes already-decoded image data as gameID's cover,
// rebuilding the renditions and invalidating the cover-set cache. lock pins the
// cover against syncs and upgrades. A landscape cover being replaced is kept as
// the wide-view banner; a landscape incoming cover becomes its own banner.
func (a *App) storeCoverData(gameID int64, data []byte, cfg image.Config, c coverart.Candidate, lock bool) error {
	if err := os.MkdirAll(config.CoverDir(), 0o700); err != nil {
		return err
	}
	p := coverPathFor(gameID)
	captureCoverBanner(p)
	if err := coverart.Store(p, data, cfg, c, lock); err != nil {
		return err
	}
	if isLandscape(cfg.Width, cfg.Height) {
		coverart.RemoveBanner(p)
	}
	writeCoverRenditions(p)
	invalidateCoverSetCache()
	return nil
}

// storeCoverBanner downloads a landscape candidate and retains it as gameID's
// wide-view banner, rebuilding the wide rendition. It leaves the primary cover
// untouched.
func (a *App) storeCoverBanner(ctx context.Context, gameID int64, c coverart.Candidate) error {
	data, _, err := coverart.Fetch(ctx, c.URL)
	if err != nil {
		return err
	}
	p := coverPathFor(gameID)
	ok, err := coverart.StoreBanner(p, data, c.Source, c.URL)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("image is not landscape")
	}
	writeCoverWide(p)
	invalidateCoverSetCache()
	return nil
}

// FindCoverCandidates lists cover art from every enabled source for the
// detail view's "Choose cover" picker.
func (a *App) FindCoverCandidates(gameID int64) ([]coverart.Candidate, error) {
	if a.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	g, err := a.db.GetGame(gameID)
	if err != nil || g == nil {
		return nil, fmt.Errorf("game %d not found", gameID)
	}
	opts := coverart.OptionsFromConfig()
	res := coverart.NewFinder(opts).Find(a.ctx, coverart.Game{Title: g.Title, SteamAppID: g.SteamAppID})
	if len(res.Candidates) == 0 && len(res.Errors) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(res.Errors, "; "))
	}
	if res.SteamAppID != 0 {
		_ = a.db.SetGameSteamAppIDIfEmpty(gameID, res.SteamAppID)
	}
	if res.Candidates == nil {
		res.Candidates = []coverart.Candidate{}
	}
	return res.Candidates, nil
}

// SetGameCover installs a picked or user-supplied image URL as the cover and
// locks it so syncs and upgrades leave it alone.
func (a *App) SetGameCover(gameID int64, url, source string) error {
	url = strings.TrimSpace(url)
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		return fmt.Errorf("cover URL must be an http(s) URL")
	}
	if source == "" {
		source = "manual"
	}
	return a.storeCoverCandidate(a.ctx, gameID, coverart.Candidate{URL: url, Source: source}, true)
}

// SetGameCoverFromFile installs a local image file as the cover and locks it so
// syncs and upgrades leave it alone. The file must be a format the app can
// display.
func (a *App) SetGameCoverFromFile(gameID int64, path string) error {
	if a.db == nil {
		return fmt.Errorf("database not initialized")
	}
	if g, err := a.db.GetGame(gameID); err != nil || g == nil {
		return fmt.Errorf("game %d not found", gameID)
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("no cover file chosen")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading cover file: %w", err)
	}
	img, err := decodeCoverImage(data)
	if err != nil {
		return fmt.Errorf("not a decodable image (png, jpg, webp, gif, avif)")
	}
	b := img.Bounds()
	cfg := image.Config{Width: b.Dx(), Height: b.Dy()}
	return a.storeCoverData(gameID, data, cfg, coverart.Candidate{Source: "manual"}, true)
}

// PickCoverImage opens a native file picker filtered to image files and
// returns the chosen path, or "" when the user cancels.
func (a *App) PickCoverImage() string {
	if a.ctx == nil {
		return ""
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose Cover Image",
		Filters: []runtime.FileFilter{
			{DisplayName: "Images", Pattern: "*.png;*.jpg;*.jpeg;*.webp;*.gif;*.avif"},
			{DisplayName: "All Files", Pattern: "*.*"},
		},
	})
	if err != nil {
		slog.Error("cover image picker failed", "error", err)
		return ""
	}
	return path
}

// maxCoverPreviewBytes caps the image PreviewCoverFile inlines as a data URL.
const maxCoverPreviewBytes = 8 << 20

// PreviewCoverFile reads a local image and returns it as a data URL so the
// Edit Game dialog can show the chosen file before the cover is installed. The
// file must be a format the app can display and at most maxCoverPreviewBytes.
func (a *App) PreviewCoverFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading image: %w", err)
	}
	if len(data) > maxCoverPreviewBytes {
		return "", fmt.Errorf("image is too large to preview (%d MiB max)", maxCoverPreviewBytes>>20)
	}
	if !knownImageFormat(data) {
		return "", fmt.Errorf("not a supported image (png, jpg, webp, gif, avif)")
	}
	return "data:image/" + imageMimeFromPrefix(data) + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

// SetCoverLocked pins (or unpins) the current cover against automatic
// replacement.
func (a *App) SetCoverLocked(gameID int64, locked bool) error {
	p := coverPathFor(gameID)
	if _, err := os.Stat(p); err != nil {
		return fmt.Errorf("game has no cached cover")
	}
	updateCoverMeta(p, func(m *coverMeta) { m.Locked = locked })
	return nil
}

// RevertCover restores the cover replaced by the last upgrade/pick.
func (a *App) RevertCover(gameID int64) error {
	p := coverPathFor(gameID)
	if _, err := os.Stat(p + ".prev"); err != nil {
		return fmt.Errorf("no previous cover to restore")
	}
	if err := os.Rename(p+".prev", p); err != nil {
		return err
	}
	if b, err := os.ReadFile(p + ".prev.meta.json"); err == nil {
		_ = os.WriteFile(p+".meta.json", b, 0o644)
		_ = os.Remove(p + ".prev.meta.json")
	}
	// The user chose the old cover over the upgrade: lock it so the next
	// upgrade run doesn't swap it straight back.
	updateCoverMeta(p, func(m *coverMeta) { m.Locked = true })
	if m, ok := readCoverMeta(p); ok && m.URL != "" {
		_ = os.WriteFile(p+".url", []byte(m.URL), 0o644)
	}
	// The restored cover may itself be landscape: then it is its own banner.
	if w, h := coverart.CurrentSize(p); isLandscape(w, h) {
		coverart.RemoveBanner(p)
	}
	writeCoverRenditions(p)
	invalidateCoverSetCache()
	return nil
}
