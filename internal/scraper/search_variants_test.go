package scraper

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"testing"
)

func TestCamelSplitWords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{"CyanBrain", "Cyan Brain"},
		{"SelobusArena", "Selobus Arena"},
		{"FreeEmberDoors", "Free Ember Doors"},
		{"KunoichiSekiren", "Kunoichi Sekiren"},
		{"SiNiSistar2", "Si Ni Sistar2"},
		// All-caps and mixed tokens must stay untouched.
		{"RPGM", "RPGM"},
		{"M.U.G.E.N", "M.U.G.E.N"},
		{"HTML5", "HTML5"},
		{"v0.1.7", "v0.1.7"},
		{"Summertime Saga", "Summertime Saga"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := camelSplitWords(tt.in); got != tt.want {
			t.Errorf("camelSplitWords(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestComputeMatchScore_CamelCase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		game   string
		thread string
		want   float64
	}{
		// CamelCase local names must score against spaced thread titles.
		{"CyanBrain", "Cyan Brain", 1.0},
		{"FreeEmberDoors", "Ember Doors", 0.85},
		{"SelobusArena", "Selobus Arena", 1.0},
		{"SiNiSistar2", "SiNiSistar 2", 0.5},
		// Existing guards must hold under the camel-aware tokenizer.
		{"Aurelia", "Aurelian Nostrum", 0.0},
		{"Summer's Gone", "Summer's Gone Deluxe", 0.85},
		{"Summer's Gone", "Summer's Gone 2", 0.25},
	}
	for _, tt := range tests {
		got := ComputeMatchScore(tt.game, tt.thread)
		if got != tt.want {
			t.Errorf("ComputeMatchScore(%q, %q) = %.2f, want %.2f", tt.game, tt.thread, got, tt.want)
		}
	}
}

func TestSearchQueryVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "trailing noise dropped repeatedly",
			in:   "Monster Black Market w DLC w Uncen",
			want: []string{"Monster Black Market"},
		},
		{
			name: "version token dropped",
			in:   "Island SAGA v5",
			want: []string{"Island SAGA"},
		},
		{
			name: "date digits dropped",
			in:   "Sana 0824",
			want: []string{"Sana"},
		},
		{
			name: "Eng suffix dropped",
			in:   "Luna Kurokami's Revenge Eng",
			want: []string{"Luna Kurokami Revenge"},
		},
		{
			name: "trailing digits stripped from last token",
			in:   "SiNiSistar2",
			want: []string{"SiNiSistar"},
		},
		{
			name: "camel-case split",
			in:   "CyanBrain",
			want: []string{"Cyan Brain"},
		},
		{
			name: "camel split then front drops reach the name",
			in:   "FreeEmberDoors",
			want: []string{"Free Ember Doors", "Ember Doors"},
		},
		{
			name: "plain title has no variants",
			in:   "Barely Working",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := SearchQueryVariants(tt.in)
			// The full variant list is an implementation detail; assert the
			// expected variants appear, in order (order-preserving subset).
			idx := 0
			for _, want := range tt.want {
				found := false
				for idx < len(got) {
					if got[idx] == want {
						found = true
						idx++
						break
					}
					idx++
				}
				if !found {
					t.Errorf("SearchQueryVariants(%q) = %v, missing %q (in order)", tt.in, got, want)
				}
			}
			if len(tt.want) == 0 && len(got) != 0 {
				t.Errorf("SearchQueryVariants(%q) = %v, want no variants", tt.in, got)
			}
		})
	}
}

func TestVersionMatchBonus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		local  string
		thread string
		want   float64
	}{
		{"1.3.0", "v1.3.1", 0.3},
		{"0.1.10", "v0.1.10", 0.3},
		{"1.0", "v1.0.3", 0.3},
		{"1.3", "1.3", 0.3},
		// Different release lines get no bonus.
		{"1.3.0", "3.0.1", 0},
		{"1.3", "1.4", 0},
		// Unusable versions get no bonus.
		{"", "v1.0", 0},
		{"Final", "v1.0", 0},
		{"2024-08-17", "v1.5a", 0},
		{"Steam", "1.0", 0},
		{"5", "v1.0.5", 0},
	}
	for _, tt := range tests {
		if got := VersionMatchBonus(tt.local, tt.thread); got != tt.want {
			t.Errorf("VersionMatchBonus(%q, %q) = %.1f, want %.1f", tt.local, tt.thread, got, tt.want)
		}
	}
}

// TestSearchTitleFirstHit: the primary query returns nothing; the variant
// query must be tried and its results returned.
func TestSearchTitleFirstHit(t *testing.T) {
	t.Parallel()

	var queries []string
	api, _ := newTestPublicAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Query().Get("search"))
		if r.URL.Query().Get("search") == "Cyan Brain" {
			fmt.Fprint(w, `{"status":"ok","msg":{"data":[
				{"thread_id":210467,"title":"Cyan Brain","creator":"Dev","version":"v1.1.4","prefixes":[19]}
			]}}`)
			return
		}
		fmt.Fprint(w, `{"status":"ok","msg":{"data":[]}}`)
	}))

	results, err := api.SearchTitleFirstHit(context.Background(), "CyanBrain")
	if err != nil {
		t.Fatalf("SearchTitleFirstHit failed: %v", err)
	}
	if len(results) != 1 || results[0].ThreadID != 210467 {
		t.Fatalf("results = %+v, want the Cyan Brain thread", results)
	}
	if len(queries) != 2 || queries[0] != "CyanBrain" || queries[1] != "Cyan Brain" {
		t.Errorf("queries = %v, want [CyanBrain Cyan Brain]", queries)
	}
}

// TestSearchTitleFirstHit_AllEmpty: every variant comes up empty — returns
// nil results (callers fall back to the cookie search) and stops.
func TestSearchTitleFirstHit_AllEmpty(t *testing.T) {
	t.Parallel()

	var queries []string
	api, _ := newTestPublicAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, queryParam(r.URL.RawQuery, "search"))
		fmt.Fprint(w, `{"status":"ok","msg":{"data":[]}}`)
	}))

	results, err := api.SearchTitleFirstHit(context.Background(), "Totally Unfindable Game")
	if err != nil {
		t.Fatalf("SearchTitleFirstHit failed: %v", err)
	}
	if results != nil {
		t.Errorf("results = %+v, want nil", results)
	}
	if len(queries) < 2 {
		t.Errorf("queries = %v, want the variant ladder walked", queries)
	}
}

// TestSearchTitleFirstHit_PrimaryWins: the primary query already returns
// results — no variant requests may be made.
func TestSearchTitleFirstHit_PrimaryWins(t *testing.T) {
	t.Parallel()

	var queries []string
	api, _ := newTestPublicAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Query().Get("search"))
		fmt.Fprint(w, `{"status":"ok","msg":{"data":[
			{"thread_id":1,"title":"Barely Working","creator":"Dev","version":"v1.0","prefixes":[]}
		]}}`)
	}))

	results, err := api.SearchTitleFirstHit(context.Background(), "Barely Working")
	if err != nil {
		t.Fatalf("SearchTitleFirstHit failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %+v, want 1", results)
	}
	if !reflect.DeepEqual(queries, []string{"Barely Working"}) {
		t.Errorf("queries = %v, want only the primary query", queries)
	}
}

// TestSearchTitleFirstHit_BlockAborts: a block on any variant must abort
// (never degrade to silent emptiness) — mirror of the SearchTitle behavior.
func TestSearchTitleFirstHit_BlockAborts(t *testing.T) {
	t.Parallel()

	api, _ := newTestPublicAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "Cloudflare challenge")
	}))

	_, err := api.SearchTitleFirstHit(context.Background(), "Anything At All")
	if err == nil {
		t.Fatal("expected error for blocked response, got nil")
	}
}
