//go:build linux

package workload

import (
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

const clockThreadCPUTimeID = 3

type kernelTimespec struct {
	sec  int64
	nsec int64
}

// threadCPUTime reports CPU time consumed by the calling OS thread, excluding
// time it is descheduled or sleeping.
func threadCPUTime() (time.Duration, error) {
	var ts kernelTimespec
	_, _, errno := syscall.RawSyscall(syscall.SYS_CLOCK_GETTIME, clockThreadCPUTimeID, uintptr(unsafe.Pointer(&ts)), 0)
	runtime.KeepAlive(&ts)
	if errno != 0 {
		return 0, errno
	}
	return time.Duration(ts.sec)*time.Second + time.Duration(ts.nsec), nil
}

// busyCPUFor consumes at least budget of calling-thread CPU time. It checks the
// clock once per work chunk to bound syscall overhead and overshoot.
func busyCPUFor(budget time.Duration) (uint64, error) {
	start, err := threadCPUTime()
	if err != nil {
		return 0, err
	}
	value := uint64(0x9e3779b97f4a7c15)
	for {
		for i := 0; i < 512; i++ {
			value ^= value << 13
			value ^= value >> 7
			value ^= value << 17
		}
		now, err := threadCPUTime()
		if err != nil {
			return 0, err
		}
		if now-start >= budget {
			return value, nil
		}
	}
}
