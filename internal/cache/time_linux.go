//go:build linux

package cache

import (
	"syscall"
	"time"
)

// timeFromTimespec converts syscall.Timespec to time.Time
func timeFromTimespec(ts syscall.Timespec) time.Time {
	return time.Unix(ts.Sec, ts.Nsec)
}

// getCtime gets change time from file stat on Linux
func getCtime(stat *syscall.Stat_t) time.Time {
	return timeFromTimespec(stat.Ctim)
}

// isTimeEqualPlatform implements Linux-specific time comparison with tolerance
func isTimeEqualPlatform(t1, t2 time.Time) bool {
	if t1.Equal(t2) {
		return true
	}

	// Linux filesystems may have microsecond precision issues
	// Allow up to 1 microsecond tolerance
	diff := t1.Sub(t2)
	if diff < 0 {
		diff = -diff
	}

	return diff <= 1*time.Microsecond
}
