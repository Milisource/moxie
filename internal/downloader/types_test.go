package downloader

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Status.String
// ---------------------------------------------------------------------------

func TestStatusString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status Status
		want   string
	}{
		{StatusPending, "pending"},
		{StatusDownloading, "downloading"},
		{StatusPaused, "paused"},
		{StatusCompleted, "completed"},
		{StatusFailed, "failed"},
		{StatusCancelled, "cancelled"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.status.String()
			if got != tt.want {
				t.Errorf("Status(%d).String() = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestStatusString_Unknown(t *testing.T) {
	t.Parallel()
	unknown := Status(999)
	got := unknown.String()
	if got != "unknown" {
		t.Errorf("Status(999).String() = %q, want %q", got, "unknown")
	}
}

func TestStatusString_Negative(t *testing.T) {
	t.Parallel()
	neg := Status(-1)
	got := neg.String()
	if got != "unknown" {
		t.Errorf("Status(-1).String() = %q, want %q", got, "unknown")
	}
}

// ---------------------------------------------------------------------------
// Progress struct
// ---------------------------------------------------------------------------

func TestProgress_ZeroValue(t *testing.T) {
	t.Parallel()
	var p Progress
	if p.BytesDownloaded != 0 {
		t.Errorf("expected BytesDownloaded to be 0, got %d", p.BytesDownloaded)
	}
	if p.TotalBytes != 0 {
		t.Errorf("expected TotalBytes to be 0, got %d", p.TotalBytes)
	}
	if p.SpeedBytesPerSec != 0 {
		t.Errorf("expected SpeedBytesPerSec to be 0, got %f", p.SpeedBytesPerSec)
	}
	if p.Percent != 0 {
		t.Errorf("expected Percent to be 0, got %f", p.Percent)
	}
}

func TestProgress_Complete(t *testing.T) {
	t.Parallel()
	p := Progress{
		BytesDownloaded:  1024,
		TotalBytes:       1024,
		SpeedBytesPerSec: 512.5,
		Percent:          100.0,
	}
	if p.BytesDownloaded != 1024 {
		t.Errorf("BytesDownloaded = %d, want 1024", p.BytesDownloaded)
	}
	if p.Percent != 100.0 {
		t.Errorf("Percent = %f, want 100.0", p.Percent)
	}
}

func TestProgress_Partial(t *testing.T) {
	t.Parallel()
	p := Progress{
		BytesDownloaded:  512,
		TotalBytes:       1024,
		SpeedBytesPerSec: 256.0,
		Percent:          50.0,
	}
	if p.Percent != 50.0 {
		t.Errorf("Percent = %f, want 50.0", p.Percent)
	}
}

// ---------------------------------------------------------------------------
// Job struct (basic sanity)
// ---------------------------------------------------------------------------

func TestJob_DefaultValues(t *testing.T) {
	t.Parallel()
	var job Job
	if job.Status != StatusPending {
		t.Errorf("expected StatusPending (0), got %d", job.Status)
	}
	if job.ID != "" {
		t.Errorf("expected empty ID, got %q", job.ID)
	}
}

// ---------------------------------------------------------------------------
// ProgressEvent struct (basic sanity)
// ---------------------------------------------------------------------------

func TestProgressEvent_DefaultValues(t *testing.T) {
	t.Parallel()
	var ev ProgressEvent
	if ev.JobID != "" {
		t.Errorf("expected empty JobID, got %q", ev.JobID)
	}
	if ev.Error != "" {
		t.Errorf("expected empty Error, got %q", ev.Error)
	}
}
