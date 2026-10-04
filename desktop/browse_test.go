package main

import (
	"testing"

	"github.com/mili/moxie/internal/scraper"
)

// TestBuildBrowsePage: the Discover feed's scraper rows are mapped to the
// desktop binding shape, including the full-resolution cover rewrite and the
// pagination metadata.
func TestBuildBrowsePage(t *testing.T) {
	t.Parallel()

	lp := &scraper.LatestPage{
		Results: []scraper.LatestSearchResult{
			{
				Title:    "Eternum",
				URL:      "https://f95zone.to/threads/93340/",
				ThreadID: 93340,
				Version:  "v0.9.5",
				Creator:  "Caribdis",
				CoverURL: "https://preview.f95zone.to/2024/01/cover.jpg",
				Rating:   4.82,
				Views:    28661376,
				Likes:    6670,
				Date:     "9 months",
			},
		},
		Page:       2,
		TotalPages: 917,
		TotalCount: 27503,
	}

	got := buildBrowsePage(lp)
	if got.Page != 2 || got.TotalPages != 917 || got.TotalCount != 27503 {
		t.Errorf("pagination = page %d / %d / %d, want 2 / 917 / 27503",
			got.Page, got.TotalPages, got.TotalCount)
	}
	if len(got.Results) != 1 {
		t.Fatalf("got %d results, want 1", len(got.Results))
	}
	r := got.Results[0]
	if r.Title != "Eternum" || r.ThreadID != 93340 || r.Rating != 4.82 ||
		r.Views != 28661376 || r.Likes != 6670 || r.Date != "9 months" || r.Creator != "Caribdis" {
		t.Errorf("unexpected mapped result: %+v", r)
	}
	// preview.f95zone.to is rewritten to the full-resolution attachments host.
	if r.CoverURL != "https://attachments.f95zone.to/2024/01/cover.jpg" {
		t.Errorf("CoverURL = %q, want the full-res attachments host", r.CoverURL)
	}
}

// buildBrowsePage must never return a nil Results slice — the binding
// serialises it straight to JSON and the frontend iterates it directly.
func TestBuildBrowsePage_Empty(t *testing.T) {
	t.Parallel()

	got := buildBrowsePage(&scraper.LatestPage{})
	if got.Results == nil {
		t.Error("Results is nil; want an empty slice so JSON emits [] not null")
	}
}
