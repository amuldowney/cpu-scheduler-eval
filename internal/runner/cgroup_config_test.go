package runner

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConfigRequiresPlacementWindowForCgroupMoves(t *testing.T) {
	config := DefaultConfig()
	config.BorrowerCgroup = "/sys/fs/cgroup/bench/borrower"
	config.StartDelay = 0
	if err := config.Validate(); err == nil {
		t.Fatal("expected cgroup-attached runs to require a process placement window")
	}
	config.StartDelay = 100 * time.Millisecond
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestBorrowerAndVictimRequireDistinctCgroups(t *testing.T) {
	for _, paths := range [][2]string{
		{"/sys/fs/cgroup/bench/role", "/sys/fs/cgroup/bench/./role"},
		{"/sys/fs/cgroup/bench/role", "/sys/fs/cgroup/bench/role/nested"},
	} {
		config := DefaultConfig()
		config.BorrowerCgroup, config.VictimCgroup = paths[0], paths[1]
		if err := config.Validate(); err == nil {
			t.Fatalf("expected overlapping role cgroups to be rejected: %v", paths)
		}
	}
}

func TestCgroupMustBeEmptyBeforeBenchmarkAttach(t *testing.T) {
	group := t.TempDir()
	procs := filepath.Join(group, "cgroup.procs")
	if err := os.WriteFile(procs, []byte("123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := requireEmptyCgroup(group); err == nil {
		t.Fatal("expected nonempty cgroup to be rejected")
	}
	if err := os.WriteFile(procs, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := requireEmptyCgroup(group); err != nil {
		t.Fatalf("empty cgroup rejected: %v", err)
	}
}
