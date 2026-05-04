package db

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// marshalStoreLinks / unmarshalStoreLinks
// ---------------------------------------------------------------------------

func TestMarshalUnmarshalStoreLinks_EmptyMap(t *testing.T) {
	t.Parallel()

	links := map[string]string{}
	got, err := marshalStoreLinks(links)
	if err != nil {
		t.Fatalf("marshalStoreLinks({}) failed: %v", err)
	}
	if got != "{}" {
		t.Errorf("marshalStoreLinks({}) = %q, want %q", got, "{}")
	}

	unmarshaled, err := unmarshalStoreLinks(got)
	if err != nil {
		t.Fatalf("unmarshalStoreLinks(%q) failed: %v", got, err)
	}
	if len(unmarshaled) != 0 {
		t.Errorf("unmarshalStoreLinks returned %d entries, want 0", len(unmarshaled))
	}
}

func TestMarshalUnmarshalStoreLinks_SingleEntry(t *testing.T) {
	t.Parallel()

	links := map[string]string{"steam": "https://store.steampowered.com/app/12345/"}
	got, err := marshalStoreLinks(links)
	if err != nil {
		t.Fatalf("marshalStoreLinks failed: %v", err)
	}
	if got != `{"steam":"https://store.steampowered.com/app/12345/"}` {
		t.Errorf("marshalStoreLinks = %q", got)
	}

	unmarshaled, err := unmarshalStoreLinks(got)
	if err != nil {
		t.Fatalf("unmarshalStoreLinks failed: %v", err)
	}
	if len(unmarshaled) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(unmarshaled))
	}
	if unmarshaled["steam"] != "https://store.steampowered.com/app/12345/" {
		t.Errorf(`unmarshaled["steam"] = %q, want %q`,
			unmarshaled["steam"], "https://store.steampowered.com/app/12345/")
	}
}

func TestMarshalUnmarshalStoreLinks_MultipleEntries(t *testing.T) {
	t.Parallel()

	links := map[string]string{
		"steam":  "https://store.steampowered.com/app/12345/",
		"itch":   "https://some-creator.itch.io/game-name",
		"dlsite": "https://www.dlsite.com/work/abc123/",
	}
	got, err := marshalStoreLinks(links)
	if err != nil {
		t.Fatalf("marshalStoreLinks failed: %v", err)
	}

	unmarshaled, err := unmarshalStoreLinks(got)
	if err != nil {
		t.Fatalf("unmarshalStoreLinks failed: %v", err)
	}
	if len(unmarshaled) != len(links) {
		t.Fatalf("expected %d entries, got %d", len(links), len(unmarshaled))
	}
	for k, v := range links {
		if unmarshaled[k] != v {
			t.Errorf("unmarshaled[%q] = %q, want %q", k, unmarshaled[k], v)
		}
	}
}

func TestMarshalUnmarshalStoreLinks_NilInput(t *testing.T) {
	t.Parallel()

	// marshalStoreLinks treats nil the same as empty
	got, err := marshalStoreLinks(nil)
	if err != nil {
		t.Fatalf("marshalStoreLinks(nil) failed: %v", err)
	}
	if got != "{}" {
		t.Errorf("marshalStoreLinks(nil) = %q, want %q", got, "{}")
	}
}

func TestUnmarshalStoreLinks_EmptyString(t *testing.T) {
	t.Parallel()

	links, err := unmarshalStoreLinks("")
	if err != nil {
		t.Fatalf("unmarshalStoreLinks(\"\") failed: %v", err)
	}
	if links == nil {
		t.Fatal("unmarshalStoreLinks(\"\") returned nil, expected empty map")
	}
	if len(links) != 0 {
		t.Errorf("expected empty map, got %d entries", len(links))
	}
}

func TestUnmarshalStoreLinks_NullString(t *testing.T) {
	t.Parallel()

	links, err := unmarshalStoreLinks("null")
	if err != nil {
		t.Fatalf("unmarshalStoreLinks(\"null\") failed: %v", err)
	}
	if links == nil {
		t.Fatal("unmarshalStoreLinks(\"null\") returned nil, expected empty map")
	}
	if len(links) != 0 {
		t.Errorf("expected empty map, got %d entries", len(links))
	}
}

func TestMarshalUnmarshalStoreLinks_RoundTrip(t *testing.T) {
	t.Parallel()

	cases := []map[string]string{
		nil,
		{},
		{"steam": "https://store.steampowered.com/app/99999/"},
		{
			"steam":  "https://store.steampowered.com/app/111/",
			"itch":   "https://dev.itch.io/game",
		},
		{
			"steam":  "https://store.steampowered.com/app/222/",
			"dlsite": "https://www.dlsite.com/pro/work/=/product_id/abc123.html",
		},
	}

	for i, original := range cases {
		t.Run("", func(t *testing.T) {
			if original == nil {
				got, err := marshalStoreLinks(nil)
				if err != nil {
					t.Fatal(err)
				}
				unmarshaled, err := unmarshalStoreLinks(got)
				if err != nil {
					t.Fatal(err)
				}
				if len(unmarshaled) != 0 {
					t.Errorf("case %d: expected empty map, got %d entries", i, len(unmarshaled))
				}
			} else {
				got, err := marshalStoreLinks(original)
				if err != nil {
					t.Fatal(err)
				}
				unmarshaled, err := unmarshalStoreLinks(got)
				if err != nil {
					t.Fatal(err)
				}
				if len(unmarshaled) != len(original) {
					t.Fatalf("case %d: expected %d entries, got %d", i, len(original), len(unmarshaled))
				}
				for k, v := range original {
					if unmarshaled[k] != v {
						t.Errorf("case %d: [%q] = %q, want %q", i, k, unmarshaled[k], v)
					}
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// DownloadStatus values
// ---------------------------------------------------------------------------

func TestDownloadStatus_Values(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status DownloadStatus
		want   string
	}{
		{DownloadStatusPending, "pending"},
		{DownloadStatusDownloading, "downloading"},
		{DownloadStatusPaused, "paused"},
		{DownloadStatusCompleted, "completed"},
		{DownloadStatusFailed, "failed"},
		{DownloadStatusCancelled, "cancelled"},
		{DownloadStatusExtracting, "extracting"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if string(tt.status) != tt.want {
				t.Errorf("DownloadStatus(%q) = %q, want %q", tt.want, string(tt.status), tt.want)
			}
		})
	}
}

func TestDownloadStatus_Comparisons(t *testing.T) {
	t.Parallel()
	// Status comparison should work as string comparison
	if DownloadStatusPending != "pending" {
		t.Error("DownloadStatusPending should equal 'pending'")
	}
	if DownloadStatusCompleted == DownloadStatusFailed {
		t.Error("completed and failed should be different")
	}
}

// ---------------------------------------------------------------------------
// Platform values
// ---------------------------------------------------------------------------

func TestPlatformValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		platform Platform
		want     string
	}{
		{PlatformLinux, "linux"},
		{PlatformWindows, "windows"},
		{PlatformMacOS, "macos"},
		{PlatformAll, "all"},
		{PlatformUnknown, "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if string(tt.platform) != tt.want {
				t.Errorf("Platform(%q) = %q, want %q", tt.want, string(tt.platform), tt.want)
			}
		})
	}
}

func TestPlatformConversions(t *testing.T) {
	t.Parallel()
	// Platform is just a string type; verify type compatibility
	var p Platform = "linux"
	if p != PlatformLinux {
		t.Errorf("expected %q, got %q", PlatformLinux, p)
	}
	p = "windows"
	if p != PlatformWindows {
		t.Errorf("expected %q, got %q", PlatformWindows, p)
	}
}

// ---------------------------------------------------------------------------
// timeToRFC3339
// ---------------------------------------------------------------------------

func TestTimeToRFC3339_Zero(t *testing.T) {
	t.Parallel()
	got := timeToRFC3339(time.Time{})
	if got != "" {
		t.Errorf("expected empty string for zero time, got %q", got)
	}
}

func TestTimeToRFC3339_Valid(t *testing.T) {
	t.Parallel()
	now := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
	got := timeToRFC3339(now)
	expected := "2024-06-15T10:30:00Z"
	if got != expected {
		t.Errorf("timeToRFC3339(%v) = %q, want %q", now, got, expected)
	}
}

func TestTimeToRFC3339_WithLocation(t *testing.T) {
	t.Parallel()
	// Should output UTC regardless of input location
	loc := time.FixedZone("EST", -5*60*60)
	tm := time.Date(2024, 6, 15, 5, 30, 0, 0, loc)
	got := timeToRFC3339(tm)
	expected := "2024-06-15T10:30:00Z" // 5:30 EST = 10:30 UTC
	if got != expected {
		t.Errorf("timeToRFC3339(%v) = %q, want %q", tm, got, expected)
	}
}

// ---------------------------------------------------------------------------
// parseTime
// ---------------------------------------------------------------------------

func TestParseTime_EmptyString(t *testing.T) {
	t.Parallel()
	got := parseTime("")
	if !got.IsZero() {
		t.Errorf("expected zero time for empty string, got %v", got)
	}
}

func TestParseTime_RFC3339(t *testing.T) {
	t.Parallel()
	got := parseTime("2024-06-15T10:30:00Z")
	if got.IsZero() {
		t.Fatal("expected non-zero time")
	}
	if got.Year() != 2024 || got.Month() != 6 || got.Day() != 15 {
		t.Errorf("unexpected date: %v", got)
	}
	if got.Hour() != 10 || got.Minute() != 30 {
		t.Errorf("unexpected time: %v", got)
	}
}

func TestParseTime_SQLiteFormat(t *testing.T) {
	t.Parallel()
	got := parseTime("2024-06-15 10:30:00")
	if got.IsZero() {
		t.Fatal("expected non-zero time")
	}
	if got.Year() != 2024 || got.Month() != 6 || got.Day() != 15 {
		t.Errorf("unexpected date: %v", got)
	}
}

func TestParseTime_InvalidFormat(t *testing.T) {
	t.Parallel()
	got := parseTime("not a date")
	if !got.IsZero() {
		t.Errorf("expected zero time for invalid string, got %v", got)
	}
}

func TestParseTime_RFC3339Nano(t *testing.T) {
	t.Parallel()
	got := parseTime("2024-06-15T10:30:00.123456Z")
	if got.IsZero() {
		t.Fatal("expected non-zero time")
	}
}

// ---------------------------------------------------------------------------
// marshalTags / unmarshalTags
// ---------------------------------------------------------------------------

func TestMarshalUnmarshalTags_Nil(t *testing.T) {
	t.Parallel()
	got, err := marshalTags(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "[]" {
		t.Errorf("marshalTags(nil) = %q, want %q", got, "[]")
	}
	unmarshaled, err := unmarshalTags(got)
	if err != nil {
		t.Fatal(err)
	}
	if len(unmarshaled) != 0 {
		t.Errorf("expected empty slice, got %d", len(unmarshaled))
	}
}

func TestMarshalUnmarshalTags_Empty(t *testing.T) {
	t.Parallel()
	got, err := marshalTags([]string{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "[]" {
		t.Errorf("marshalTags([]) = %q, want %q", got, "[]")
	}
}

func TestMarshalUnmarshalTags_Values(t *testing.T) {
	t.Parallel()
	tags := []string{"adult", "rpg", "fantasy"}
	got, err := marshalTags(tags)
	if err != nil {
		t.Fatal(err)
	}
	unmarshaled, err := unmarshalTags(got)
	if err != nil {
		t.Fatal(err)
	}
	if len(unmarshaled) != 3 {
		t.Fatalf("expected 3 tags, got %d", len(unmarshaled))
	}
	for i, tag := range tags {
		if unmarshaled[i] != tag {
			t.Errorf("tag[%d] = %q, want %q", i, unmarshaled[i], tag)
		}
	}
}

func TestUnmarshalTags_EmptyString(t *testing.T) {
	t.Parallel()
	tags, err := unmarshalTags("")
	if err != nil {
		t.Fatal(err)
	}
	if tags == nil {
		t.Fatal("expected non-nil slice")
	}
	if len(tags) != 0 {
		t.Errorf("expected empty slice, got %d", len(tags))
	}
}

func TestUnmarshalTags_NullString(t *testing.T) {
	t.Parallel()
	tags, err := unmarshalTags("null")
	if err != nil {
		t.Fatal(err)
	}
	if tags == nil {
		t.Fatal("expected non-nil slice")
	}
	if len(tags) != 0 {
		t.Errorf("expected empty slice, got %d", len(tags))
	}
}

// ---------------------------------------------------------------------------
// marshalStoreLinks with edge cases
// ---------------------------------------------------------------------------

func TestMarshalStoreLinks_NilSafety(t *testing.T) {
	t.Parallel()
	got, err := marshalStoreLinks(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "{}" {
		t.Errorf("marshalStoreLinks(nil) = %q, want %q", got, "{}")
	}
}

func TestUnmarshalStoreLinks_EmptyObject(t *testing.T) {
	t.Parallel()
	links, err := unmarshalStoreLinks("{}")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 0 {
		t.Errorf("expected empty map, got %d entries", len(links))
	}
}
