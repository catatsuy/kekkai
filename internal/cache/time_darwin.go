//go:build darwin

package cache

import (
	"syscall"
	"time"
)

// timeFromTimespec converts syscall.Timespec to time.Time
func timeFromTimespec(ts syscall.Timespec) time.Time {
	return time.Unix(ts.Sec, ts.Nsec)
}

// getCtime gets change time from file stat on Darwin
func getCtime(stat *syscall.Stat_t) time.Time {
	return timeFromTimespec(stat.Ctimespec)
}
