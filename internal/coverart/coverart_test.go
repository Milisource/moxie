package coverart

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/time/rate"
)

func TestNormalizeTitle(t *testing.T) {
	cases := map[string]string{
		"Summertime Saga [v21.0.0]":     "summertimesaga",
		"Being a DIK (Season 2)":        "beingadik",
		"Fort of Chains":                "fortofchains",
		"Monster Girl Island: Prologue": "monstergirlislandprologue",
		"Eternum Remastered":            "eternum",
		"  Treasure of Nadia  ":         "treasureofnadia",
		"アニヴァーサリー ～夏休みの想い出～":            "アニヴァーサリー夏休みの想い出",
	}
	for in, want := range cases {
		if got := NormalizeTitle(in); got != want {
			t.Errorf("NormalizeTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBetter(t *testing.T) {
	steam := Candidate{URL: "s", W: 600, H: 900, Source: "steam"}
	small := Candidate{URL: "v", W: 256, H: 359, Source: "vndb"}
	wide := Candidate{URL: "w", W: 920, H: 430, Source: "steamgriddb"}
	cases := []struct {
		name       string
		cs         []Candidate
		curW, curH int
		want       string
	}{
		{"no cover takes any portrait", []Candidate{wide, small}, 0, 0, "v"},
		{"1080p banner → steam capsule", []Candidate{steam}, 1920, 1080, "s"},
		{"1080p banner keeps over small vndb", []Candidate{small}, 1920, 1080, ""},
		{"dlsite 560x420 → vndb 256x359 is softer", []Candidate{small}, 560, 420, ""},
		{"landscape candidates never win", []Candidate{wide}, 560, 420, ""},
		{"sharper portrait kept", []Candidate{steam}, 1200, 1800, ""},
		{"small portrait upgraded", []Candidate{steam}, 300, 450, "s"},
	}
	for _, c := range cases {
		got, ok := Better(c.cs, c.curW, c.curH)
		if (c.want == "") == ok || (ok && got.URL != c.want) {
			t.Errorf("%s: got %q ok=%v, want %q", c.name, got.URL, ok, c.want)
		}
	}
}

func testFinder(opts Options, h http.Handler) (*Finder, func()) {
	srv := httptest.NewServer(h)
	f := NewFinder(opts)
	f.steamStoreBase, f.steamAPIBase, f.sgdbBase, f.vndbBase = srv.URL, srv.URL, srv.URL, srv.URL
	f.steamCDNBase = "https://cdn.test/"
	f.steamLimit, f.sgdbLimit, f.vndbLimit = rate.NewLimiter(rate.Inf, 1), rate.NewLimiter(rate.Inf, 1), rate.NewLimiter(rate.Inf, 1)
	return f, srv.Close
}

func TestFindAllSources(t *testing.T) {
	var sawAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/storesearch/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("term") != "Monster Girl Island" {
			t.Errorf("storesearch term = %q", r.URL.Query().Get("term"))
		}
		io.WriteString(w, `{"items":[{"type":"app","name":"Monster Girl Island: Prologue","id":943700},{"type":"app","name":"Monster Girl Island","id":1000}]}`)
	})
	mux.HandleFunc("/IStoreBrowseService/GetItems/v1/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Query().Get("input_json"), `"appid":1000`) {
			t.Errorf("GetItems input = %s", r.URL.Query().Get("input_json"))
		}
		io.WriteString(w, `{"response":{"store_items":[{"name":"Monster Girl Island","assets":{"asset_url_format":"steam/apps/1000/${FILENAME}?t=1","library_capsule":"abc/library_capsule.jpg","library_capsule_2x":"abc/library_capsule_2x.jpg"}}]}}`)
	})
	mux.HandleFunc("/grids/steam/1000", func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		if r.URL.Query().Get("nsfw") != "any" {
			t.Errorf("sgdb nsfw = %q", r.URL.Query().Get("nsfw"))
		}
		io.WriteString(w, `{"success":true,"data":[{"url":"https://cdn2.steamgriddb.com/grid/x.png","thumb":"https://cdn2.steamgriddb.com/thumb/x.jpg","width":600,"height":900,"score":3}]}`)
	})
	mux.HandleFunc("/vn", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		io.WriteString(w, `{"results":[{"id":"v1","title":"Monster Girl Island","image":{"url":"https://t.vndb.org/cv/1.jpg","dims":[256,360]}},{"id":"v2","title":"Something Else","image":{"url":"https://t.vndb.org/cv/2.jpg","dims":[256,360]}}]}`)
	})
	f, done := testFinder(Options{Steam: true, SGDBKey: "k", VNDB: true}, mux)
	defer done()

	res := f.Find(context.Background(), Game{Title: "Monster Girl Island [v0.5]"})
	if len(res.Errors) > 0 {
		t.Fatalf("errors: %v", res.Errors)
	}
	if res.SteamAppID != 1000 {
		t.Errorf("SteamAppID = %d, want exact-title match 1000 (not the Prologue)", res.SteamAppID)
	}
	if sawAuth != "Bearer k" {
		t.Errorf("sgdb auth = %q", sawAuth)
	}
	var got []string
	for _, c := range res.Candidates {
		got = append(got, c.Source+":"+c.URL)
	}
	want := []string{
		"steam:https://cdn.test/steam/apps/1000/abc/library_capsule_2x.jpg?t=1",
		"steamgriddb:https://cdn2.steamgriddb.com/grid/x.png",
		"vndb:https://t.vndb.org/cv/1.jpg",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("candidates:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestSGDBAutocompleteRequiresExactTitle(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/search/autocomplete/", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"success":true,"data":[{"id":5,"name":"Fort of Chains 2"}]}`)
	})
	mux.HandleFunc("/grids/game/", func(w http.ResponseWriter, r *http.Request) {
		t.Error("grids fetched for a non-matching title")
	})
	f, done := testFinder(Options{SGDBKey: "k"}, mux)
	defer done()
	res := f.Find(context.Background(), Game{Title: "Fort of Chains"})
	if len(res.Candidates) != 0 {
		t.Errorf("candidates = %v, want none", res.Candidates)
	}
}

func TestBestLandscape(t *testing.T) {
	hero16 := Candidate{URL: "16", W: 1920, H: 1080, Source: "steamgriddb"}
	ultrawide := Candidate{URL: "3", W: 3840, H: 1240, Source: "steamgriddb"}
	small := Candidate{URL: "small", W: 560, H: 315, Source: "f95"}
	big := Candidate{URL: "big", W: 1920, H: 1080, Source: "f95"}
	portrait := Candidate{URL: "p", W: 600, H: 900, Source: "steam"}
	cases := []struct {
		name string
		cs   []Candidate
		want string
	}{
		{"empty", nil, ""},
		{"portrait ignored", []Candidate{portrait}, ""},
		{"16:9 beats a larger ultrawide", []Candidate{ultrawide, hero16}, "16"},
		{"higher resolution 16:9 wins", []Candidate{small, big}, "big"},
	}
	for _, c := range cases {
		got, ok := BestLandscape(c.cs)
		if (c.want == "") == ok || (ok && got.URL != c.want) {
			t.Errorf("%s: got %q ok=%v, want %q", c.name, got.URL, ok, c.want)
		}
	}
}

func testJPEGBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestStoreBanner(t *testing.T) {
	p := filepath.Join(t.TempDir(), "7")
	if ok, err := StoreBanner(p, testJPEGBytes(t, 1920, 1080), "f95", "u"); err != nil || !ok {
		t.Fatalf("landscape store: ok=%v err=%v", ok, err)
	}
	if !HasBanner(p) {
		t.Fatal("HasBanner = false after storing a landscape banner")
	}
	if m, _ := ReadMeta(p); m.BannerW != 1920 || m.BannerH != 1080 || m.BannerURL != "u" || m.BannerSource != "f95" {
		t.Errorf("meta = %+v", m)
	}
	// A portrait image must not clobber a retained banner.
	if ok, err := StoreBanner(p, testJPEGBytes(t, 600, 900), "steam", "u2"); err != nil || ok {
		t.Errorf("portrait store: ok=%v err=%v, want false", ok, err)
	}
	if m, _ := ReadMeta(p); m.BannerW != 1920 || m.BannerURL != "u" {
		t.Errorf("banner clobbered by portrait: %+v", m)
	}

	RemoveBanner(p)
	if HasBanner(p) {
		t.Error("HasBanner = true after RemoveBanner")
	}
	if m, _ := ReadMeta(p); m.BannerW != 0 || m.BannerURL != "" || m.BannerSource != "" {
		t.Errorf("banner fields not cleared: %+v", m)
	}
}

func TestStoreCarriesBanner(t *testing.T) {
	p := filepath.Join(t.TempDir(), "7")
	if _, err := StoreBanner(p, testJPEGBytes(t, 1920, 1080), "f95", "u"); err != nil {
		t.Fatal(err)
	}
	if err := Store(p, testJPEGBytes(t, 600, 900), image.Config{Width: 600, Height: 900}, Candidate{URL: "x", Source: "steam"}, true); err != nil {
		t.Fatal(err)
	}
	m, ok := ReadMeta(p)
	if !ok || m.BannerW != 1920 || m.BannerURL != "u" {
		t.Errorf("banner fields lost across Store: %+v (ok=%v)", m, ok)
	}
	if !HasBanner(p) {
		t.Error("banner file lost across Store")
	}
}
