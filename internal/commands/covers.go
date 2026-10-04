package commands

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mili/moxie/internal/config"
	"github.com/mili/moxie/internal/coverart"
	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/util"
)

// Covers dispatches `moxie covers <sub>`.
func Covers(args []string) {
	if len(args) == 0 || args[0] != "upgrade" {
		fmt.Fprintf(os.Stderr, "Usage: moxie covers upgrade [--dry-run] [--vndb] [--no-steam] [--limit N] [--game ID]\n")
		os.Exit(2)
	}
	CoversUpgrade(args[1:])
}

// CoversUpgrade replaces landscape / low-resolution covers with portrait art
// from Steam, SteamGridDB (when a key is configured) and VNDB.
func CoversUpgrade(args []string) {
	fs := flag.NewFlagSet("covers upgrade", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "List proposed replacements without downloading")
	vndb := fs.Bool("vndb", false, "Also search VNDB (overrides config cover-vndb)")
	noSteam := fs.Bool("no-steam", false, "Skip Steam")
	limit := fs.Int("limit", 0, "Stop after N games checked (0 = all)")
	only := fs.Int64("game", 0, "Only this game ID")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: moxie covers upgrade [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Look up portrait cover art for games whose cover is missing, landscape or\nunder 600px, and replace it when the new art is portrait and at least as sharp.\nLocked covers are skipped. The replaced cover is kept as <id>.prev and its\nlandscape art as the wide-view banner.\n\nSources: Steam (keyless), SteamGridDB (with steamgriddb-key), VNDB (opt-in).\n\nFlags:\n")
		fs.PrintDefaults()
	}
	fs.Parse(hoistFlags(args, nil))

	opts := coverart.OptionsFromConfig()
	if *vndb {
		opts.VNDB = true
	}
	if *noSteam {
		opts.Steam = false
	}
	if !opts.Steam && opts.SGDBKey == "" && !opts.VNDB {
		fmt.Fprintln(os.Stderr, "No cover sources enabled.")
		os.Exit(1)
	}
	var sources []string
	if opts.Steam {
		sources = append(sources, "steam")
	}
	if opts.SGDBKey != "" {
		sources = append(sources, "steamgriddb")
	}
	if opts.VNDB {
		sources = append(sources, "vndb")
	}
	fmt.Fprintf(os.Stderr, "Sources: %s\n", strings.Join(sources, ", "))

	database := OpenDB()
	defer database.Close()
	games, err := database.ListActiveGames("", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading games: %v\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	finder := coverart.NewFinder(opts)
	dir := config.CoverDir()
	if !*dryRun {
		_ = os.MkdirAll(dir, 0o700)
	}

	checked, replaced := 0, 0
	for _, g := range games {
		if ctx.Err() != nil {
			break
		}
		if *only != 0 && g.ID != *only {
			continue
		}
		if db.IsVirtualPath(g.Path) && *only == 0 {
			continue
		}
		coverPath := filepath.Join(dir, strconv.FormatInt(g.ID, 10))
		m, _ := coverart.ReadMeta(coverPath)
		w, h := coverart.CurrentSize(coverPath)
		if !coverart.NeedsUpgrade(w, h, m.Locked) && *only == 0 {
			continue
		}
		if *limit > 0 && checked >= *limit {
			break
		}
		checked++
		res := finder.Find(ctx, coverart.Game{Title: g.Title, SteamAppID: g.SteamAppID})
		cur := "none"
		if w > 0 {
			cur = fmt.Sprintf("%dx%d", w, h)
		}
		pick, ok := coverart.Better(res.Candidates, w, h)
		for _, e := range res.Errors {
			fmt.Fprintf(os.Stderr, "  ! %s: %s\n", util.Truncate(g.Title, 40), e)
		}
		if !ok {
			fmt.Fprintf(os.Stderr, "  %-4d %-44s %-10s keep (%d candidates)\n", g.ID, util.Truncate(g.Title, 42), cur, len(res.Candidates))
			continue
		}
		fmt.Fprintf(os.Stderr, "  %-4d %-44s %-10s → %dx%d %s\n", g.ID, util.Truncate(g.Title, 42), cur, pick.W, pick.H, pick.Source)
		if *dryRun {
			replaced++
			continue
		}
		data, cfg, err := coverart.Fetch(ctx, pick.URL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "       download failed: %v\n", err)
			continue
		}
		if cfg.Height <= cfg.Width {
			fmt.Fprintf(os.Stderr, "       skipped: image is %dx%d, not portrait\n", cfg.Width, cfg.Height)
			continue
		}
		// Keep the outgoing landscape cover as the wide-view banner before it
		// is replaced (the desktop app renders the .wide rendition on demand).
		if !coverart.HasBanner(coverPath) {
			if old, rerr := os.ReadFile(coverPath); rerr == nil {
				_, _ = coverart.StoreBanner(coverPath, old, m.Source, m.URL)
			}
		}
		if err := coverart.Store(coverPath, data, cfg, pick, false); err != nil {
			fmt.Fprintf(os.Stderr, "       store failed: %v\n", err)
			continue
		}
		if res.SteamAppID != 0 {
			_ = database.SetGameSteamAppIDIfEmpty(g.ID, res.SteamAppID)
		}
		replaced++
	}
	verb := "replaced"
	if *dryRun {
		verb = "would be replaced"
	}
	fmt.Fprintf(os.Stderr, "\n%d checked, %d %s.\n", checked, replaced, verb)
}
