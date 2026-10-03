package main

import (
	"context"
	"fmt"
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
		if coverart.NeedsUpgrade(w, h, m.Locked) {
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
		found := finder.Find(ctx, coverart.Game{Title: g.Title, SteamAppID: g.SteamAppID})
		for _, e := range found.Errors {
			if len(res.Errors) < 5 {
				res.Errors = append(res.Errors, g.Title+": "+e)
			}
		}
		pick, ok := coverart.Better(found.Candidates, w, h)
		if !ok {
			continue
		}
		if err := a.storeCoverCandidate(ctx, g.ID, pick, false); err != nil {
			slog.Warn("cover upgrade: store failed", "game_id", g.ID, "url", pick.URL, "error", err)
			res.Failed++
			continue
		}
		if found.SteamAppID != 0 {
			_ = a.db.SetGameSteamAppIDIfEmpty(g.ID, found.SteamAppID)
		}
		slog.Info("cover upgraded", "game_id", g.ID, "title", g.Title, "from", fmt.Sprintf("%dx%d", w, h), "to", fmt.Sprintf("%dx%d", pick.W, pick.H), "source", pick.Source)
		res.Replaced++
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
	if err := os.MkdirAll(config.CoverDir(), 0o700); err != nil {
		return err
	}
	p := coverPathFor(gameID)
	if err := coverart.Store(p, data, cfg, c, lock); err != nil {
		return err
	}
	writeCoverThumb(p)
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

// SetGameCover installs a picked candidate as the cover and locks it so
// syncs and upgrades leave it alone.
func (a *App) SetGameCover(gameID int64, url, source string) error {
	if !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("cover URL must be https")
	}
	return a.storeCoverCandidate(a.ctx, gameID, coverart.Candidate{URL: url, Source: source}, true)
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
	writeCoverThumb(p)
	invalidateCoverSetCache()
	return nil
}
