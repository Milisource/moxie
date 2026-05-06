package scraper

import "strings"

// Known engine/status/category prefix words that F95Zone threads
// often have at the start of their titles.
var prefixWords = map[string]bool{
	"unity": true, "ren'py": true, "renpy": true, "rpgm": true,
	"vn": true, "html": true, "flash": true, "java": true,
	"godot": true, "electron": true, "unreal": true, "others": true, "html5": true,
	"completed": true, "abandoned": true, "onhold": true,
	"collection": true, "video": true, "mod": true, "cheat": true,
	"tool": true, "daz": true, "update": true, "req": true,
	"request": true, "seeking": true, "announcement": true,
}

// StripThreadPrefix removes known engine/status/category prefix words
// from an F95Zone thread title.
func StripThreadPrefix(title string) string {
	words := strings.Fields(title)
	for len(words) > 0 && prefixWords[strings.ToLower(strings.TrimRight(words[0], "•"))] {
		words = words[1:]
	}

	result := strings.TrimSpace(strings.Join(words, " "))
	if result == "" {
		return title
	}
	return result
}
