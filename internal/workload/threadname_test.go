package workload

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestSetThreadCommLabelsCurrentThread(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := setThreadComm("victim"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("/proc/thread-self/comm")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != "victim" {
		t.Fatalf("thread comm = %q, want victim", got)
	}
}
