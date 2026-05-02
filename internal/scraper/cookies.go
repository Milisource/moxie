package scraper

import "strings"

// CookiePair represents a single name=value pair from a Cookie header.
type CookiePair struct {
	Name  string
	Value string
}

// ParseCookies parses a raw Cookie header string into key-value pairs.
// It splits on "; " (semicolon followed by space) and then on "=" to
// extract each cookie name and value. Malformed entries (e.g. no "=")
// are silently skipped.
func ParseCookies(cookieStr string) []CookiePair {
	if cookieStr == "" {
		return nil
	}

	parts := strings.Split(cookieStr, "; ")
	var result []CookiePair

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		idx := strings.IndexByte(part, '=')
		if idx == -1 {
			// Malformed cookie pair — skip.
			continue
		}
		name := strings.TrimSpace(part[:idx])
		value := strings.TrimSpace(part[idx+1:])
		if name == "" {
			continue
		}
		result = append(result, CookiePair{Name: name, Value: value})
	}

	return result
}
