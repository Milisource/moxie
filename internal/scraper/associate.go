package scraper

import (
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"strings"
)

// MatchResult is a potential thread match for a game.
type MatchResult struct {
	ScrapeInput ScrapeInput    `json:"scrape_input"`
	Candidates  []SearchResult `json:"candidates"`
	BestMatch   *SearchResult  `json:"best_match,omitempty"`
}

// AssociateOptions controls auto-association behavior.
type AssociateOptions struct {
	Client *Client

	// AllGames is the list of ScrapeInputs to search for.
	// The caller is responsible for pre-filtering (e.g. games that already
	// have an F95URL should not be passed here).
	AllGames []ScrapeInput

	// URLMap is an optional JSON file path mapping game paths to thread URLs.
	// Format: {"path/to/game": "https://f95zone.to/threads/slug.12345/"}
	URLMap string

	// Interactive is true when running inside a TUI (affects how we present results).
	Interactive bool
}

// FindMatches searches F95Zone for each provided ScrapeInput and returns
// potential thread matches sorted by confidence (best match first).
// The caller is responsible for pre-filtering inputs (e.g. games that already
// have an F95URL should be filtered out before calling this function).
func FindMatches(opts AssociateOptions) ([]MatchResult, error) {
	// Load URL map if provided.
	urlMap := make(map[string]string)
	if opts.URLMap != "" {
		data, err := os.ReadFile(opts.URLMap)
		if err != nil {
			return nil, err
		}
		var rawMap map[string]string
		if err := json.Unmarshal(data, &rawMap); err != nil {
			return nil, err
		}
		urlMap = rawMap
	}

	var results []MatchResult
	for _, input := range opts.AllGames {
		mr := MatchResult{ScrapeInput: input}

		// Check URL map first (no HTTP call needed).
		if url, ok := urlMap[input.Path]; ok {
			mr.Candidates = []SearchResult{{
				Title: input.Title,
				URL:   url,
			}}
			mr.BestMatch = &mr.Candidates[0]
			results = append(results, mr)
			continue
		}

		// Search F95Zone.
		sanitized := SanitizeTitle(input.Title)
		candidates, err := opts.Client.SearchF95Zone(sanitized)
		if err != nil {
			// If search fails, still add the result with no candidates.
			candidates = nil
		}
		mr.Candidates = candidates

		// Find best match among candidates (skipping non-game threads).
		var best *SearchResult
		var bestScore float64
		for i := range candidates {
			if IsNonGameThread(candidates[i].Title) {
				continue // skip non-game threads
			}
			score := ComputeMatchScore(input.Title, candidates[i].Title)
			if score > bestScore {
				bestScore = score
				best = &candidates[i]
			}
		}
		mr.BestMatch = best

		results = append(results, mr)
	}

	// Sort results: best matches first (by best match score), no matches last.
	sort.SliceStable(results, func(i, j int) bool {
		scoreI := 0.0
		scoreJ := 0.0
		if results[i].BestMatch != nil {
			scoreI = ComputeMatchScore(results[i].ScrapeInput.Title, results[i].BestMatch.Title)
		}
		if results[j].BestMatch != nil {
			scoreJ = ComputeMatchScore(results[j].ScrapeInput.Title, results[j].BestMatch.Title)
		}
		return scoreI > scoreJ
	})

	return results, nil
}

// SanitizeTitle cleans a game directory name into a searchable title.
// Handles underscores, version numbers, platform suffixes, HG prefixes,
// bracketed tags, and other noise common in download directory names.
//
// Examples:
//
//	"SiNiSistar2_v1_0_6-WIN"     → "SiNiSistar2"
//	"SummertimeSaga-0-20-16-pc"  → "SummertimeSaga"
//	"HG755_Inn, Tavern and Halberd" → "Inn, Tavern and Halberd"
//	"BootyHunter-0.8-pc"         → "BootyHunter"
//	"Latex_Dungeon_V1.5.7-WIN"   → "Latex Dungeon"
func SanitizeTitle(title string) string {
	s := title

	// 1. Strip HG/numeric prefixes (e.g. "HG755_", "HG2496_")
	s = hgPrefix.ReplaceAllString(s, "")

	// 2. Remove bracketed tags: [tag], (tag)
	s = bracketed.ReplaceAllString(s, "")

	// 3. Strip dash-separated version numbers BEFORE underscore-to-space,
	//    e.g. "Game-0-20-16-pc" → "Game-pc"
	s = dashVersion.ReplaceAllString(s, "")

	// 4. Strip version patterns with underscore/dot delimiters,
	//    e.g. "v1_0_6", "v1.2.3", "V1.5.7"
	s = versionPattern.ReplaceAllString(s, " ")

	// 4. Replace underscores with spaces.
	s = strings.ReplaceAll(s, "_", " ")

	// 5. Strip trailing platform/archive suffixes: -pc, -win, -linux, -mac, -WIN
	s = platformSuffix.ReplaceAllString(s, "")

	// 5b. Strip standalone platform words (e.g., "Game Windows" → "Game").
	s = standalonePlatform.ReplaceAllString(s, " ")

	// 6. Replace remaining hyphens with spaces (they're now word separators).
	s = strings.ReplaceAll(s, "-", " ")

	// 7. Remove trailing dots and stray characters.
	s = trailingDots.ReplaceAllString(s, " ")

	// 8. Collapse multiple spaces and trim.
	s = multiSpace.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// Pre-compiled regex patterns for SanitizeTitle.
var (
	hgPrefix       = regexp.MustCompile(`(?i)^HG\d+[_\s]*`)
	bracketed      = regexp.MustCompile(`\s*[\[\(][^\]\)]*[\]\)]`)
	dashVersion    = regexp.MustCompile(`(?i)\s*-\d+(?:[._-]\d+)*\b`)
	versionPattern = regexp.MustCompile(`(?i)\s*[vV]?\d+[._]\d+(?:[._]\d+)*(?:\w*)`)
	platformSuffix = regexp.MustCompile(`(?i)\s*-\s*(pc|win|windows|linux|mac|macos)\b`)
	// standalonePlatform matches platform words that appear as standalone
	// tokens (not dash-separated — those are handled by platformSuffix).
	standalonePlatform = regexp.MustCompile(`(?i)\b(pc|win|windows|linux|mac|macos)\b`)
	trailingDots       = regexp.MustCompile(`\.{2,}`)
	multiSpace         = regexp.MustCompile(`\s{2,}`)
)

// ComputeMatchScore returns a confidence score (0.0-1.0) for how well a
// search result title matches a game title.
func ComputeMatchScore(gameTitle, resultTitle string) float64 {
	a := strings.ToLower(SanitizeTitle(gameTitle))
	b := strings.ToLower(SanitizeTitle(resultTitle))
	if a == "" || b == "" {
		return 0.0
	}
	if a == b {
		return 1.0
	}
	if strings.Contains(b, a) {
		// Result contains game title — check if the extra words are
		// meaningful (sequel, remaster, etc.) or just noise (version, studio).
		if hasMeaningfulDiff(a, b) {
			return 0.25 // below the 0.3 auto-accept threshold
		}
		return 0.85
	}
	if strings.Contains(a, b) {
		// Game title contains result — always a good match
		// (e.g., "Corruption of Champions II" matching "Corruption of Champions").
		return 0.85
	}
	// Word overlap: count shared words / total unique words.
	wordsA := strings.Fields(a)
	wordsB := strings.Fields(b)
	shared := 0
	for _, wa := range wordsA {
		for _, wb := range wordsB {
			if wa == wb {
				shared++
				break
			}
		}
	}
	if len(wordsA) > 0 && len(wordsB) > 0 {
		return float64(shared) / float64(max(len(wordsA), len(wordsB)))
	}
	return 0.0
}

// meaningfulDiffWords are words that, when found in the longer title but not
// the shorter one, suggest the result is a different game (sequel, remaster,
// etc.) rather than the same game with extra noise.
var meaningfulDiffWords = []string{
	"ii", "iii", "iv", "v", "vi",
	"2", "3", "4", "5",
	"remastered", "remaster", "remake",
	"definitive", "enhanced", "director",
	"dlc", "expansion", "episode",
	"chapter", "season", "part",
	"redux", "reloaded", "rebirth",
}

// hasMeaningfulDiff returns true when the longer title contains extra words
// that suggest it is a different game (sequel, remaster, etc.) rather than
// the same game with noisy extra words like version numbers or studio names.
func hasMeaningfulDiff(a, b string) bool {
	longer := a
	shorter := b
	if len(b) > len(a) {
		longer = b
		shorter = a
	}

	diff := strings.TrimSpace(strings.Replace(longer, shorter, "", 1))
	if diff == "" {
		return false
	}

	words := strings.Fields(strings.ToLower(diff))
	for _, w := range words {
		for _, mw := range meaningfulDiffWords {
			if w == mw {
				return true
			}
		}
	}
	return false
}
