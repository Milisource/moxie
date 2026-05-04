package db

import (
	"encoding/json"
	"time"
)

// Game represents a game in the user's local library.
type Game struct {
	ID          int64             `json:"id"`
	Title       string            `json:"title"`
	Engine      string            `json:"engine"` // Unity, RenPy, RPGMakerMV, RPGMakerMZ, RPGMakerVXAce, RPGMaker, Godot, Unreal, Electron, HTML, Unknown
	Path        string            `json:"path"`   // absolute directory path (unique)
	ExePath     string            `json:"exe_path,omitempty"`
	Version     string            `json:"version,omitempty"`
	SizeBytes   int64             `json:"size_bytes"`
	F95URL      string            `json:"f95_url,omitempty"`
	F95ThreadID int64             `json:"f95_thread_id,omitempty"`
	Tags        []string          `json:"tags"`   // stored as JSON array string in SQLite
	Status      string            `json:"status"` // active, completed, abandoned, on_hold, unknown
	Notes       string            `json:"notes,omitempty"`
	StoreLinks  map[string]string `json:"store_links,omitempty"` // stored as JSON in SQLite
	SteamAppID  int64             `json:"steam_app_id,omitempty"`
	LatestVersion  string         `json:"latest_version,omitempty"`
	VersionCheckedAt time.Time    `json:"version_checked_at,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// ScrapedMeta holds F95Zone scraped metadata.
type ScrapedMeta struct {
	GameID      int64     `json:"game_id"`
	Developer   string    `json:"developer,omitempty"`
	Overview    string    `json:"overview,omitempty"`
	CoverURL    string    `json:"cover_url,omitempty"`
	LastScraped time.Time `json:"last_scraped"`
}

// DownloadStatus represents the state of a download job.
type DownloadStatus string

const (
	DownloadStatusPending     DownloadStatus = "pending"
	DownloadStatusDownloading DownloadStatus = "downloading"
	DownloadStatusPaused      DownloadStatus = "paused"
	DownloadStatusCompleted   DownloadStatus = "completed"
	DownloadStatusFailed      DownloadStatus = "failed"
	DownloadStatusCancelled   DownloadStatus = "cancelled"
	DownloadStatusExtracting  DownloadStatus = "extracting"
)

// Download represents a game download job in the database.
type Download struct {
	ID               int64          `json:"id"`
	GameID           int64          `json:"game_id"`
	URL              string         `json:"url"`
	Host             string         `json:"host"`
	Filename         string         `json:"filename"`
	DestPath         string         `json:"dest_path"`
	Status           DownloadStatus `json:"status"`
	BytesDownloaded  int64          `json:"bytes_downloaded"`
	TotalBytes       int64          `json:"total_bytes"`
	SpeedBytesPerSec float64        `json:"speed_bytes_per_sec"`
	PercentComplete  float64        `json:"percent_complete"`
	Error            string         `json:"error,omitempty"`
	StartedAt        time.Time      `json:"started_at,omitempty"`
	CompletedAt      time.Time      `json:"completed_at,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
}

// Platform represents the target operating system for a download.
type Platform string

const (
	PlatformLinux   Platform = "linux"
	PlatformWindows Platform = "windows"
	PlatformMacOS   Platform = "macos"
	PlatformAll     Platform = "all"     // Cross-platform (e.g., web/HTML)
	PlatformUnknown Platform = "unknown" // Could not determine
)

// DownloadLink represents a scraped download link stored in the database.
type DownloadLink struct {
	ID          int64     `json:"id"`
	GameID      int64     `json:"game_id"`
	URL         string    `json:"url"`
	Host        string    `json:"host"`
	Name        string    `json:"name"`
	Platform    Platform  `json:"platform"`
	IsDead      bool      `json:"is_dead"`
	DeadReason  string    `json:"dead_reason,omitempty"`
	LastChecked time.Time `json:"last_checked,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// marshalTags serializes a string slice to a JSON string for SQLite storage.
func marshalTags(tags []string) (string, error) {
	if len(tags) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(tags)
	return string(b), err
}

// unmarshalTags deserializes a JSON string from SQLite back to a string slice.
func unmarshalTags(s string) ([]string, error) {
	if s == "" || s == "null" {
		return []string{}, nil
	}
	var tags []string
	err := json.Unmarshal([]byte(s), &tags)
	return tags, err
}

// marshalStoreLinks serializes a map to a JSON string for SQLite storage.
func marshalStoreLinks(links map[string]string) (string, error) {
	if len(links) == 0 {
		return "{}", nil
	}
	b, err := json.Marshal(links)
	return string(b), err
}

// unmarshalStoreLinks deserializes a JSON string from SQLite back to a map.
func unmarshalStoreLinks(s string) (map[string]string, error) {
	if s == "" || s == "null" {
		return map[string]string{}, nil
	}
	var links map[string]string
	err := json.Unmarshal([]byte(s), &links)
	return links, err
}

// timeToRFC3339 formats a time.Time as an RFC3339 string for SQLite storage.
func timeToRFC3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// parseTime parses a time string, trying RFC3339 first, then SQLite's
// native datetime format as a fallback.
func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err == nil {
		return t
	}
	// SQLite datetime('now') returns "2006-01-02 15:04:05"
	t, err = time.Parse("2006-01-02 15:04:05", s)
	if err == nil {
		return t
	}
	return time.Time{}
}
