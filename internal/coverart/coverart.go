// Package coverart finds portrait cover art for games from external
// catalogues: Steam (keyless store APIs), SteamGridDB (API key) and VNDB
// (keyless). F95Zone covers are mostly landscape banners, while the library
// grid is 3:4 — these sources carry real box/capsule art.
//
// Matching is deliberately strict: a catalogue entry is only used when its
// normalised title equals the game's (or the game already has a Steam AppID),
// because a wrong cover is worse than a blurry one.
package coverart

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"

	"golang.org/x/time/rate"
)

// Game is what the finders need to know about a library entry.
type Game struct {
	Title      string
	SteamAppID int64 // 0 when unknown
}

// Candidate is one image a source offers.
type Candidate struct {
	URL    string `json:"url"`
	Thumb  string `json:"thumb,omitempty"` // smaller preview when the source has one
	W      int    `json:"w"`
	H      int    `json:"h"`
	Source string `json:"source"` // steam, steamgriddb, vndb
	Note   string `json:"note,omitempty"`
}

// Portrait reports whether the image is taller than wide.
func (c Candidate) Portrait() bool { return c.H > c.W }

// ShortEdge is min(W, H).
func (c Candidate) ShortEdge() int { return min(c.W, c.H) }

// Options selects sources. Zero value = Steam only.
type Options struct {
	Steam   bool
	SGDBKey string // SteamGridDB API key; "" disables it
	VNDB    bool
}

// Finder queries the enabled sources. Safe for concurrent use; requests are
// paced per source.
type Finder struct {
	opts Options
	http *http.Client

	steamStoreBase string // store.steampowered.com
	steamAPIBase   string // api.steampowered.com
	steamCDNBase   string // asset host prefix
	sgdbBase       string
	vndbBase       string

	steamLimit *rate.Limiter
	sgdbLimit  *rate.Limiter
	vndbLimit  *rate.Limiter
}

// NewFinder builds a Finder for opts.
func NewFinder(opts Options) *Finder {
	return &Finder{
		opts:           opts,
		http:           &http.Client{Timeout: 20 * time.Second},
		steamStoreBase: "https://store.steampowered.com",
		steamAPIBase:   "https://api.steampowered.com",
		steamCDNBase:   "https://shared.akamai.steamstatic.com/store_item_assets/",
		sgdbBase:       "https://www.steamgriddb.com/api/v2",
		vndbBase:       "https://api.vndb.org/kana",
		// Steam's store endpoints tolerate ~200 req / 5 min; SGDB's free tier
		// is 1 req/s; VNDB allows 200 req / 5 min.
		steamLimit: rate.NewLimiter(rate.Every(time.Second), 2),
		sgdbLimit:  rate.NewLimiter(rate.Every(1100*time.Millisecond), 1),
		vndbLimit:  rate.NewLimiter(rate.Every(2*time.Second), 1),
	}
}

// Result is what Find returns.
type Result struct {
	Candidates []Candidate // best first
	SteamAppID int64       // AppID matched by title (0 when none / already known)
	Errors     []string    // per-source failures (non-fatal)
}

// Find queries every enabled source and returns candidates ordered best
// first: portrait before landscape, then by source preference (Steam,
// SteamGridDB, VNDB), then by resolution.
func (f *Finder) Find(ctx context.Context, g Game) Result {
	var res Result
	appID := g.SteamAppID
	if f.opts.Steam {
		if appID == 0 {
			id, err := f.steamSearch(ctx, g.Title)
			if err != nil {
				res.Errors = append(res.Errors, "steam search: "+err.Error())
			} else if id != 0 {
				appID = id
				res.SteamAppID = id
			}
		}
		if appID != 0 {
			cs, err := f.steamCapsules(ctx, appID)
			if err != nil {
				res.Errors = append(res.Errors, "steam: "+err.Error())
			}
			res.Candidates = append(res.Candidates, cs...)
		}
	}
	if f.opts.SGDBKey != "" {
		cs, err := f.sgdbGrids(ctx, g.Title, appID)
		if err != nil {
			res.Errors = append(res.Errors, "steamgriddb: "+err.Error())
		}
		res.Candidates = append(res.Candidates, cs...)
	}
	if f.opts.VNDB {
		cs, err := f.vndbCovers(ctx, g.Title)
		if err != nil {
			res.Errors = append(res.Errors, "vndb: "+err.Error())
		}
		res.Candidates = append(res.Candidates, cs...)
	}
	SortCandidates(res.Candidates)
	return res
}

var sourceRank = map[string]int{"steam": 0, "steamgriddb": 1, "vndb": 2}

// SortCandidates orders candidates best first (see Find).
func SortCandidates(cs []Candidate) {
	sort.SliceStable(cs, func(i, j int) bool {
		a, b := cs[i], cs[j]
		if a.Portrait() != b.Portrait() {
			return a.Portrait()
		}
		if ra, rb := sourceRank[a.Source], sourceRank[b.Source]; ra != rb {
			return ra < rb
		}
		return a.ShortEdge() > b.ShortEdge()
	})
}

// Better returns the first candidate worth replacing the current cover
// (curW×curH; zeros = no cover) with: portrait, and at least as sharp as
// the current cover on its short edge. A current cover that is itself
// portrait and sharper is kept.
func Better(cs []Candidate, curW, curH int) (Candidate, bool) {
	curShort := min(curW, curH)
	curPortrait := curH > curW
	for _, c := range cs {
		if !c.Portrait() || c.URL == "" {
			continue
		}
		if curW == 0 || curH == 0 {
			return c, true
		}
		if curPortrait && c.ShortEdge() <= curShort {
			continue
		}
		if c.ShortEdge() >= min(curShort, 600) {
			return c, true
		}
	}
	return Candidate{}, false
}

// NormalizeTitle folds a title for exact matching: lower case, letters and
// digits only, with F95 decorations ("[v0.5]", "(Ch. 2)", edition words)
// and a trailing version dropped.
func NormalizeTitle(s string) string {
	s = strings.ToLower(s)
	s = stripBracketed(s)
	for _, suffix := range []string{
		" remastered", " remake", " definitive edition", " complete edition",
		" deluxe edition", " director's cut", " directors cut",
	} {
		s = strings.TrimSuffix(strings.TrimSpace(s), suffix)
	}
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// stripBracketed removes [..] and (..) groups.
func stripBracketed(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '[', '(':
			depth++
			continue
		case ']', ')':
			if depth > 0 {
				depth--
			}
			continue
		}
		if depth == 0 {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// SearchTerm is the title as sent to search endpoints: brackets removed.
func SearchTerm(title string) string {
	return strings.Join(strings.Fields(stripBracketed(title)), " ")
}

// titleMatches reports an exact normalised match between the game title and
// any of names.
func titleMatches(title string, names ...string) bool {
	want := NormalizeTitle(title)
	if want == "" {
		return false
	}
	for _, n := range names {
		if NormalizeTitle(n) == want {
			return true
		}
	}
	return false
}
