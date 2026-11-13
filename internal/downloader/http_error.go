package downloader

import "fmt"

// HTTPStatusError preserves a server status for callers classifying failures.
// Message retains the context displayed by the download or validation path.
type HTTPStatusError struct {
	StatusCode int
	Message    string
}

func (e *HTTPStatusError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("HTTP %d", e.StatusCode)
}
