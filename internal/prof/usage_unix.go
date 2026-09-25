//go:build unix

package prof

import (
	"runtime"
	"syscall"
	"time"
)

func readUsage() usage {
	var ru syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &ru) != nil {
		return usage{}
	}
	// Darwin reports the peak in bytes, every other unix in kilobytes
	peak := int64(ru.Maxrss)
	if runtime.GOOS != "darwin" {
		peak *= 1024
	}
	return usage{
		user:        time.Duration(ru.Utime.Nano()),
		sys:         time.Duration(ru.Stime.Nano()),
		peakRSS:     peak,
		readOps:     int64(ru.Inblock),
		writeOps:    int64(ru.Oublock),
		switches:    int64(ru.Nvcsw),
		preemptions: int64(ru.Nivcsw),
	}
}
