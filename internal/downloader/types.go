package downloader

import "time"

type Status int

const (
	StatusPending Status = iota
	StatusDownloading
	StatusPaused
	StatusCompleted
	StatusFailed
	StatusCancelled
)

func (s Status) String() string {
	switch s {
	case StatusPending:
		return "pending"
	case StatusDownloading:
		return "downloading"
	case StatusPaused:
		return "paused"
	case StatusCompleted:
		return "completed"
	case StatusFailed:
		return "failed"
	case StatusCancelled:
		return "cancelled"
	default:
		return "unknown"
	}
}

type Progress struct {
	BytesDownloaded int64
	TotalBytes      int64
	SpeedBytesPerSec float64
	Percent         float64
}

type Job struct {
	ID        string
	GameID    int64
	URL       string
	Host      string
	DestPath  string
	Status    Status
	Progress  Progress
	Error     string
	CreatedAt time.Time
	StartedAt time.Time
	CompletedAt time.Time
}

type ProgressEvent struct {
	JobID    string
	GameID   int64
	Status   Status
	Progress Progress
	Error    string
}
