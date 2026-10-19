package workload

import (
	"runtime"
	"testing"
	"time"
)

func TestThreadCPUClockExcludesSleep(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	before, err := threadCPUTime()
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	after, err := threadCPUTime()
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := after - before; elapsed >= 5*time.Millisecond {
		t.Fatalf("thread CPU clock advanced by %s while sleeping", elapsed)
	}
}

func TestBusyCPUForConsumesRequestedThreadCPU(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	before, err := threadCPUTime()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := busyCPUFor(5 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	after, err := threadCPUTime()
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := after - before; elapsed < 5*time.Millisecond || elapsed > 30*time.Millisecond {
		t.Fatalf("busy workload used %s thread CPU, want near 5ms", elapsed)
	}
}
