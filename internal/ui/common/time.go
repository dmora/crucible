package common

import (
	"fmt"
	"time"
)

// FormatDuration returns a compact elapsed string like "0:42" or "12:05" for a duration.
func FormatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	mins := int(d.Minutes())
	secs := int(d.Seconds()) % 60
	return fmt.Sprintf("%d:%02d", mins, secs)
}

// FormatElapsed returns a compact elapsed time string like "0:42" or "12:05".
// Returns "" if started is zero.
func FormatElapsed(started time.Time) string {
	if started.IsZero() {
		return ""
	}
	return FormatDuration(time.Since(started))
}
