package coverart

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/time/rate"
)

const maxBody = 4 << 20

func (f *Finder) getJSON(ctx context.Context, lim *rate.Limiter, req *http.Request, out any) error {
	if err := lim.Wait(ctx); err != nil {
		return err
	}
	req = req.WithContext(ctx)
	req.Header.Set("User-Agent", "moxie (cover art)")
	req.Header.Set("Accept", "application/json")
	resp, err := f.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.Unmarshal(body, out)
}

// ── Steam ─────────────────────────────────────────────────────────────

// steamSearch resolves a title to an AppID through the store search,
// accepting only an exact normalised title match on an app (not DLC).
func (f *Finder) steamSearch(ctx context.Context, title string) (int64, error) {
	term := SearchTerm(title)
	if term == "" {
		return 0, nil
	}
	q := url.Values{"term": {term}, "cc": {"us"}, "l": {"english"}}
	req, _ := http.NewRequest("GET", f.steamStoreBase+"/api/storesearch/?"+q.Encode(), nil)
	var out struct {
		Items []struct {
			Type string `json:"type"`
			Name string `json:"name"`
			ID   int64  `json:"id"`
		} `json:"items"`
	}
	if err := f.getJSON(ctx, f.steamLimit, req, &out); err != nil {
		return 0, err
	}
	for _, it := range out.Items {
		if it.Type == "app" && titleMatches(title, it.Name) {
			return it.ID, nil
		}
	}
	return 0, nil
}

// steamCapsules returns the library capsule art (600x900 / 300x450) for
// appID. The asset filenames are hashed for newer apps, so they come from
// IStoreBrowseService/GetItems rather than a guessed CDN path.
func (f *Finder) steamCapsules(ctx context.Context, appID int64) ([]Candidate, error) {
	input := fmt.Sprintf(`{"ids":[{"appid":%d}],"context":{"language":"english","country_code":"US"},"data_request":{"include_assets":true}}`, appID)
	req, _ := http.NewRequest("GET", f.steamAPIBase+"/IStoreBrowseService/GetItems/v1/?input_json="+url.QueryEscape(input), nil)
	var out struct {
		Response struct {
			StoreItems []struct {
				Name   string `json:"name"`
				Assets struct {
					Format      string `json:"asset_url_format"`
					Capsule     string `json:"library_capsule"`
					Capsule2x   string `json:"library_capsule_2x"`
					MainCapsule string `json:"main_capsule"`
				} `json:"assets"`
			} `json:"store_items"`
		} `json:"response"`
	}
	if err := f.getJSON(ctx, f.steamLimit, req, &out); err != nil {
		return nil, err
	}
	if len(out.Response.StoreItems) == 0 {
		return nil, nil
	}
	it := out.Response.StoreItems[0]
	asset := func(name string) string {
		if name == "" || it.Assets.Format == "" {
			return ""
		}
		return f.steamCDNBase + strings.ReplaceAll(it.Assets.Format, "${FILENAME}", name)
	}
	var cs []Candidate
	note := "Steam library capsule · " + it.Name
	if u := asset(it.Assets.Capsule2x); u != "" {
		cs = append(cs, Candidate{URL: u, Thumb: asset(it.Assets.Capsule), W: 600, H: 900, Source: "steam", Note: note})
	} else if u := asset(it.Assets.Capsule); u != "" {
		cs = append(cs, Candidate{URL: u, W: 300, H: 450, Source: "steam", Note: note})
	}
	return cs, nil
}

// steamLandscape returns the landscape banner art (header / library hero) for
// appID, used by the wide library view. The asset filenames come from
// IStoreBrowseService/GetItems for the same reason as steamCapsules. The
// declared sizes are Steam's standard dimensions; the real ones are measured
// when the image is fetched.
func (f *Finder) steamLandscape(ctx context.Context, appID int64) ([]Candidate, error) {
	input := fmt.Sprintf(`{"ids":[{"appid":%d}],"context":{"language":"english","country_code":"US"},"data_request":{"include_assets":true}}`, appID)
	req, _ := http.NewRequest("GET", f.steamAPIBase+"/IStoreBrowseService/GetItems/v1/?input_json="+url.QueryEscape(input), nil)
	var out struct {
		Response struct {
			StoreItems []struct {
				Name   string `json:"name"`
				Assets struct {
					Format      string `json:"asset_url_format"`
					Header      string `json:"header"`
					LibraryHero string `json:"library_hero"`
				} `json:"assets"`
			} `json:"store_items"`
		} `json:"response"`
	}
	if err := f.getJSON(ctx, f.steamLimit, req, &out); err != nil {
		return nil, err
	}
	if len(out.Response.StoreItems) == 0 {
		return nil, nil
	}
	it := out.Response.StoreItems[0]
	asset := func(name string) string {
		if name == "" || it.Assets.Format == "" {
			return ""
		}
		return f.steamCDNBase + strings.ReplaceAll(it.Assets.Format, "${FILENAME}", name)
	}
	var cs []Candidate
	if u := asset(it.Assets.LibraryHero); u != "" {
		cs = append(cs, Candidate{URL: u, W: 1920, H: 620, Source: "steam", Note: "Steam hero · " + it.Name})
	}
	if u := asset(it.Assets.Header); u != "" {
		cs = append(cs, Candidate{URL: u, W: 460, H: 215, Source: "steam", Note: "Steam header · " + it.Name})
	}
	return cs, nil
}

// ── SteamGridDB ───────────────────────────────────────────────────────

type sgdbImage struct {
	URL    string `json:"url"`
	Thumb  string `json:"thumb"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Score  int    `json:"score"`
	Style  string `json:"style"`
}

func (f *Finder) sgdbGet(ctx context.Context, path string, out any) error {
	req, _ := http.NewRequest("GET", f.sgdbBase+path, nil)
	req.Header.Set("Authorization", "Bearer "+f.opts.SGDBKey)
	return f.getJSON(ctx, f.sgdbLimit, req, out)
}

// sgdbGrids returns portrait grids: by Steam AppID when known, else by an
// exact title match from autocomplete.
func (f *Finder) sgdbGrids(ctx context.Context, title string, appID int64) ([]Candidate, error) {
	params := url.Values{
		"dimensions": {"600x900,660x930,342x482"},
		"types":      {"static"},
		"nsfw":       {"any"},
		"humor":      {"false"},
	}.Encode()
	var path string
	if appID != 0 {
		path = fmt.Sprintf("/grids/steam/%d?%s", appID, params)
	} else {
		var ac struct {
			Success bool `json:"success"`
			Data    []struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
			} `json:"data"`
		}
		term := SearchTerm(title)
		if term == "" {
			return nil, nil
		}
		if err := f.sgdbGet(ctx, "/search/autocomplete/"+url.PathEscape(term), &ac); err != nil {
			return nil, err
		}
		id := 0
		for _, g := range ac.Data {
			if titleMatches(title, g.Name) {
				id = g.ID
				break
			}
		}
		if id == 0 {
			return nil, nil
		}
		path = fmt.Sprintf("/grids/game/%d?%s", id, params)
	}
	var out struct {
		Success bool        `json:"success"`
		Data    []sgdbImage `json:"data"`
	}
	if err := f.sgdbGet(ctx, path, &out); err != nil {
		return nil, err
	}
	var cs []Candidate
	for _, im := range out.Data {
		if im.URL == "" || strings.HasSuffix(im.URL, ".svg") {
			continue
		}
		cs = append(cs, Candidate{
			URL: im.URL, Thumb: im.Thumb, W: im.Width, H: im.Height, Source: "steamgriddb",
			Note: fmt.Sprintf("SteamGridDB grid · score %d", im.Score),
		})
		if len(cs) == 6 {
			break
		}
	}
	return cs, nil
}

// sgdbHeroes returns SteamGridDB hero art (wide banners) by AppID when known,
// else by exact title match. Heroes are unbounded in size, so their dimensions
// come from the API.
func (f *Finder) sgdbHeroes(ctx context.Context, title string, appID int64) ([]Candidate, error) {
	var path string
	if appID != 0 {
		path = fmt.Sprintf("/heroes/steam/%d", appID)
	} else {
		var ac struct {
			Success bool `json:"success"`
			Data    []struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
			} `json:"data"`
		}
		term := SearchTerm(title)
		if term == "" {
			return nil, nil
		}
		if err := f.sgdbGet(ctx, "/search/autocomplete/"+url.PathEscape(term), &ac); err != nil {
			return nil, err
		}
		id := 0
		for _, g := range ac.Data {
			if titleMatches(title, g.Name) {
				id = g.ID
				break
			}
		}
		if id == 0 {
			return nil, nil
		}
		path = fmt.Sprintf("/heroes/game/%d", id)
	}
	var out struct {
		Success bool        `json:"success"`
		Data    []sgdbImage `json:"data"`
	}
	if err := f.sgdbGet(ctx, path, &out); err != nil {
		return nil, err
	}
	var cs []Candidate
	for _, im := range out.Data {
		if im.URL == "" || strings.HasSuffix(im.URL, ".svg") || im.Width <= im.Height {
			continue
		}
		cs = append(cs, Candidate{
			URL: im.URL, Thumb: im.Thumb, W: im.Width, H: im.Height, Source: "steamgriddb",
			Note: fmt.Sprintf("SteamGridDB hero · score %d", im.Score),
		})
		if len(cs) == 4 {
			break
		}
	}
	return cs, nil
}

// ── VNDB ──────────────────────────────────────────────────────────────

func (f *Finder) vndbCovers(ctx context.Context, title string) ([]Candidate, error) {
	term := SearchTerm(title)
	if term == "" {
		return nil, nil
	}
	body, _ := json.Marshal(map[string]any{
		"filters": []any{"search", "=", term},
		"fields":  "title, alttitle, titles.title, image.url, image.dims",
		"results": 5,
	})
	req, _ := http.NewRequest("POST", f.vndbBase+"/vn", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	var out struct {
		Results []struct {
			ID       string `json:"id"`
			Title    string `json:"title"`
			AltTitle string `json:"alttitle"`
			Titles   []struct {
				Title string `json:"title"`
			} `json:"titles"`
			Image *struct {
				URL  string `json:"url"`
				Dims []int  `json:"dims"`
			} `json:"image"`
		} `json:"results"`
	}
	if err := f.getJSON(ctx, f.vndbLimit, req, &out); err != nil {
		return nil, err
	}
	var cs []Candidate
	for _, r := range out.Results {
		if r.Image == nil || r.Image.URL == "" || len(r.Image.Dims) != 2 {
			continue
		}
		names := []string{r.Title, r.AltTitle}
		for _, t := range r.Titles {
			names = append(names, t.Title)
		}
		if !titleMatches(title, names...) {
			continue
		}
		cs = append(cs, Candidate{
			URL: r.Image.URL, W: r.Image.Dims[0], H: r.Image.Dims[1], Source: "vndb",
			Note: "VNDB " + r.ID + " · " + r.Title,
		})
	}
	return cs, nil
}
