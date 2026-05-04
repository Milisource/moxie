package db

import (
	"testing"
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
