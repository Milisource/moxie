package scraper

// ScrapeInput is the input to a scrape/association operation.
// It carries the minimum info needed to look up a game on F95Zone:
// its directory name (Title) and filesystem path (Path).
type ScrapeInput struct {
	Title string `json:"title"`
	Path  string `json:"path"`
}

// ThreadData contains metadata extracted from a XenForo thread page.
type ThreadData struct {
	Title         string         `json:"title"`
	Version       string         `json:"version,omitempty"`
	Developer     string         `json:"developer,omitempty"`
	Tags          []string       `json:"tags"`
	Overview      string         `json:"overview,omitempty"`
	CoverURL      string         `json:"cover_url,omitempty"`
	ThreadID      int64          `json:"thread_id"`
	DownloadLinks []DownloadLink `json:"download_links,omitempty"`
	StoreLinks    map[string]string `json:"store_links,omitempty"`
	Status        string            `json:"status,omitempty"` // completed, abandoned, on_hold
}

// DownloadLink represents a download link found in the thread.
type DownloadLink struct {
	URL  string `json:"url"`
	Host string `json:"host"` // mega, keep2share, uploaded, mediafire, workupload, googledrive, dropbox, other
	Name string `json:"name"` // link text
}
