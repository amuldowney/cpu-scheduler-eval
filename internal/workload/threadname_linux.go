//go:build linux

package workload

import (
	"runtime"
	"syscall"
	"unsafe"
)

func setThreadComm(name string) error {
	buffer := append([]byte(name), 0)
	_, _, errno := syscall.RawSyscall6(uintptr(syscall.SYS_PRCTL), uintptr(syscall.PR_SET_NAME), uintptr(unsafe.Pointer(&buffer[0])), 0, 0, 0, 0)
	runtime.KeepAlive(buffer)
	if errno != 0 {
		return errno
	}
	return nil
}
