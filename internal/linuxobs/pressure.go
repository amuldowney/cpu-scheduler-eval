// Package linuxobs provides parsers and optional Linux scheduler observations.
// It does not change workload configuration or cgroup/scheduler state.
package linuxobs

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// PressureLine is one line from Linux's pressure-stall-information interface.
type PressureLine struct {
	Avg10     float64
	Avg60     float64
	Avg300    float64
	TotalUsec uint64
}

// Pressure retains whether the optional full line was actually present.
type Pressure struct {
	Some PressureLine
	Full *PressureLine
}

// CPUStat contains the counters reported by cgroup v2 cpu.stat. The map keeps
// newer kernel counters without forcing callers to update this package.
type CPUStat map[string]uint64

// Measurement distinguishes an unavailable observation from a measured zero.
type Measurement[T any] struct {
	Value     T
	Available bool
	Reason    string
}

// Snapshot contains independent host and current-cgroup observations. Each
// measurement can be unavailable without making zero-valued data look real.
type Snapshot struct {
	HostPressure   Measurement[Pressure]
	CgroupPressure Measurement[Pressure]
	CPUStat        Measurement[CPUStat]
}

// ParsePressure parses /proc/pressure/cpu or a cgroup v2 cpu.pressure file.
// The required "some" line and optional "full" line are retained separately.
func ParsePressure(data []byte) (Pressure, error) {
	var result Pressure
	seenSome, seenFull := false, false
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		if fields[0] != "some" && fields[0] != "full" {
			return Pressure{}, fmt.Errorf("line %d: unknown pressure line %q", lineNumber, fields[0])
		}
		if len(fields) != 5 {
			return Pressure{}, fmt.Errorf("line %d: expected pressure kind and four fields", lineNumber)
		}

		line := PressureLine{}
		seen := make(map[string]bool, 4)
		for _, field := range fields[1:] {
			key, value, ok := strings.Cut(field, "=")
			if !ok || seen[key] {
				return Pressure{}, fmt.Errorf("line %d: malformed or duplicate field %q", lineNumber, field)
			}
			seen[key] = true
			switch key {
			case "avg10", "avg60", "avg300":
				parsed, err := strconv.ParseFloat(value, 64)
				if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed < 0 {
					return Pressure{}, fmt.Errorf("line %d: invalid %s value %q", lineNumber, key, value)
				}
				switch key {
				case "avg10":
					line.Avg10 = parsed
				case "avg60":
					line.Avg60 = parsed
				case "avg300":
					line.Avg300 = parsed
				}
			case "total":
				parsed, err := strconv.ParseUint(value, 10, 64)
				if err != nil {
					return Pressure{}, fmt.Errorf("line %d: invalid total value %q", lineNumber, value)
				}
				line.TotalUsec = parsed
			default:
				return Pressure{}, fmt.Errorf("line %d: unknown pressure field %q", lineNumber, key)
			}
		}
		if len(seen) != 4 {
			return Pressure{}, fmt.Errorf("line %d: missing pressure field", lineNumber)
		}

		switch fields[0] {
		case "some":
			if seenSome {
				return Pressure{}, errors.New("duplicate some pressure line")
			}
			result.Some, seenSome = line, true
		case "full":
			if seenFull {
				return Pressure{}, errors.New("duplicate full pressure line")
			}
			result.Full, seenFull = &line, true
		}
	}
	if err := scanner.Err(); err != nil {
		return Pressure{}, fmt.Errorf("read pressure data: %w", err)
	}
	if !seenSome {
		return Pressure{}, errors.New("missing some pressure line")
	}
	return result, nil
}

// ParseCPUStat parses cgroup v2 cpu.stat while preserving unknown counters.
func ParseCPUStat(data []byte) (CPUStat, error) {
	result := make(CPUStat)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 2 || strings.ContainsAny(fields[0], "=\t") {
			return nil, fmt.Errorf("line %d: expected counter name and value", lineNumber)
		}
		if _, exists := result[fields[0]]; exists {
			return nil, fmt.Errorf("line %d: duplicate counter %q", lineNumber, fields[0])
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid %s value %q", lineNumber, fields[0], fields[1])
		}
		result[fields[0]] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read cpu.stat: %w", err)
	}
	if _, ok := result["usage_usec"]; !ok {
		return nil, errors.New("missing usage_usec counter")
	}
	return result, nil
}

// CurrentSnapshot reads host pressure and the current cgroup-v2 cpu.pressure
// and cpu.stat files. Unsupported/missing observations carry explicit reasons.
func CurrentSnapshot() Snapshot {
	cgroupDir, err := currentCgroupV2Dir("/proc/self/cgroup", "/proc/self/mountinfo")
	if err != nil {
		return Snapshot{
			HostPressure:   readPressure("/proc/pressure/cpu"),
			CgroupPressure: Measurement[Pressure]{Reason: err.Error()},
			CPUStat:        Measurement[CPUStat]{Reason: err.Error()},
		}
	}
	return readSnapshotAt("/proc/pressure/cpu", cgroupDir)
}

func readSnapshotAt(hostPressurePath, cgroupDir string) Snapshot {
	return Snapshot{
		HostPressure:   readPressure(hostPressurePath),
		CgroupPressure: readPressure(filepath.Join(cgroupDir, "cpu.pressure")),
		CPUStat:        readCPUStat(filepath.Join(cgroupDir, "cpu.stat")),
	}
}

func readPressure(path string) Measurement[Pressure] {
	data, err := os.ReadFile(path)
	if err != nil {
		return Measurement[Pressure]{Reason: fmt.Sprintf("read %s: %v", path, err)}
	}
	value, err := ParsePressure(data)
	if err != nil {
		return Measurement[Pressure]{Reason: fmt.Sprintf("parse %s: %v", path, err)}
	}
	return Measurement[Pressure]{Value: value, Available: true}
}

func readCPUStat(path string) Measurement[CPUStat] {
	data, err := os.ReadFile(path)
	if err != nil {
		return Measurement[CPUStat]{Reason: fmt.Sprintf("read %s: %v", path, err)}
	}
	value, err := ParseCPUStat(data)
	if err != nil {
		return Measurement[CPUStat]{Reason: fmt.Sprintf("parse %s: %v", path, err)}
	}
	return Measurement[CPUStat]{Value: value, Available: true}
}

func currentCgroupV2Dir(cgroupFile, mountInfoFile string) (string, error) {
	cgroupData, err := os.ReadFile(cgroupFile)
	if err != nil {
		return "", fmt.Errorf("read cgroup membership %s: %w", cgroupFile, err)
	}
	var membership string
	for _, line := range strings.Split(string(cgroupData), "\n") {
		if strings.HasPrefix(line, "0::") {
			membership = strings.TrimPrefix(line, "0::")
			break
		}
	}
	if membership == "" {
		return "", errors.New("current process has no cgroup v2 membership")
	}

	mountData, err := os.ReadFile(mountInfoFile)
	if err != nil {
		return "", fmt.Errorf("read cgroup mount info %s: %w", mountInfoFile, err)
	}
	for _, line := range strings.Split(string(mountData), "\n") {
		before, after, ok := strings.Cut(line, " - ")
		if !ok {
			continue
		}
		mountFields, fsFields := strings.Fields(before), strings.Fields(after)
		if len(mountFields) < 5 || len(fsFields) < 1 || fsFields[0] != "cgroup2" {
			continue
		}
		root := unescapeMountField(mountFields[3])
		mountPoint := unescapeMountField(mountFields[4])
		cleanMembership := filepath.Clean(membership)
		cleanRoot := filepath.Clean(root)
		if cleanRoot != "/" && cleanMembership != cleanRoot && !strings.HasPrefix(cleanMembership, cleanRoot+string(os.PathSeparator)) {
			continue
		}
		relative := strings.TrimPrefix(strings.TrimPrefix(cleanMembership, cleanRoot), string(os.PathSeparator))
		return filepath.Join(mountPoint, relative), nil
	}
	return "", errors.New("no cgroup v2 mount contains the current process")
}

func unescapeMountField(value string) string {
	return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(value)
}
