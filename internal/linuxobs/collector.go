package linuxobs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrUnavailable marks a Linux observation that could not be collected.
var ErrUnavailable = errors.New("Linux observation unavailable")

// UnavailableError explains why an optional Linux observation could not run.
type UnavailableError struct {
	Reason string
}

func (e *UnavailableError) Error() string {
	return fmt.Sprintf("%v: %s", ErrUnavailable, e.Reason)
}

func (e *UnavailableError) Unwrap() error { return ErrUnavailable }

// Role is the comm name that the scheduler collector targets. Workload threads
// must be named exactly "borrower" or "victim" for events to be included.
type Role string

const (
	RoleBorrower Role = "borrower"
	RoleVictim   Role = "victim"
)

// Sample is one exact wakeup-to-running delay printed by bpftrace, in ns.
type Sample struct {
	Role    Role
	DelayNS uint64
}

// SamplesByRole retains the raw delay values without estimating percentiles.
type SamplesByRole struct {
	Borrower []uint64
	Victim   []uint64
}

// ParseDelayLine parses one line of the collector's bpftrace output protocol.
func ParseDelayLine(line string) (Sample, error) {
	fields := strings.Fields(line)
	if len(fields) != 3 || fields[0] != "linuxobs_delay" {
		return Sample{}, fmt.Errorf("invalid delay event line %q", line)
	}
	role := Role(fields[1])
	if role != RoleBorrower && role != RoleVictim {
		return Sample{}, fmt.Errorf("unknown delay event role %q", fields[1])
	}
	delay, err := strconv.ParseUint(fields[2], 10, 64)
	if err != nil {
		return Sample{}, fmt.Errorf("invalid delay in event line %q: %w", line, err)
	}
	return Sample{Role: role, DelayNS: delay}, nil
}

// GroupByRole returns exact samples separated by workload role.
func GroupByRole(samples []Sample) SamplesByRole {
	var grouped SamplesByRole
	for _, sample := range samples {
		switch sample.Role {
		case RoleBorrower:
			grouped.Borrower = append(grouped.Borrower, sample.DelayNS)
		case RoleVictim:
			grouped.Victim = append(grouped.Victim, sample.DelayNS)
		}
	}
	return grouped
}

const traceProgram = `
BEGIN { printf("linuxobs_ready\n"); }
tracepoint:sched:sched_wakeup,tracepoint:sched:sched_wakeup_new
/str(args->comm) == "borrower" || str(args->comm) == "victim"/
{
  @linuxobs_wakeup[args->pid] = nsecs;
  @linuxobs_role[args->pid] = str(args->comm) == "borrower" ? 1 : 2;
}
tracepoint:sched:sched_switch
/@linuxobs_wakeup[args->next_pid]/
{
  $delay = nsecs - @linuxobs_wakeup[args->next_pid];
  if (@linuxobs_role[args->next_pid] == 1) {
    printf("linuxobs_delay borrower %llu\n", $delay);
  } else {
    printf("linuxobs_delay victim %llu\n", $delay);
  }
  delete(@linuxobs_wakeup[args->next_pid]);
  delete(@linuxobs_role[args->next_pid]);
}
`

const startupTimeout = 10 * time.Second

// Collector runs bpftrace until Stop or context cancellation and retains raw
// per-event samples. bpftrace must be installed and permitted to attach to the
// sched tracepoints; failure is reported as an UnavailableError.
type Collector struct {
	cmd       *exec.Cmd
	stdout    *eventWriter
	stderr    *boundedBuffer
	ready     chan struct{}
	done      chan struct{}
	readyOnce sync.Once
	stopOnce  sync.Once
	mu        sync.Mutex
	samples   []Sample
	parseErr  error
	waitErr   error
	stopping  bool
}

// StartCollector starts the optional bpftrace-backed scheduler observation.
// Linux workload threads must set their comm names exactly to "borrower" or
// "victim". The caller should call Stop; ctx cancellation also interrupts and
// reaps the bpftrace process.
func StartCollector(ctx context.Context) (*Collector, error) {
	if ctx == nil {
		return nil, errors.New("collector context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := exec.LookPath("bpftrace")
	if err != nil {
		return nil, &UnavailableError{Reason: "bpftrace executable not found in PATH"}
	}
	return startCollectorAt(ctx, path)
}

func startCollectorAt(ctx context.Context, executable string) (*Collector, error) {
	if err := checkTracepointAvailability(ctx, executable); err != nil {
		return nil, err
	}
	cmd := exec.Command(executable, "-e", traceProgram)
	c := &Collector{
		cmd:    cmd,
		ready:  make(chan struct{}),
		done:   make(chan struct{}),
		stderr: &boundedBuffer{limit: 32 * 1024},
	}
	c.stdout = &eventWriter{collector: c}
	cmd.Stdout = c.stdout
	cmd.Stderr = c.stderr
	if err := cmd.Start(); err != nil {
		return nil, &UnavailableError{Reason: fmt.Sprintf("start bpftrace: %v", err)}
	}
	go func() {
		err := cmd.Wait()
		c.mu.Lock()
		c.waitErr = err
		c.mu.Unlock()
		close(c.done)
	}()

	timer := time.NewTimer(startupTimeout)
	defer timer.Stop()
	select {
	case <-c.ready:
		go func() {
			select {
			case <-ctx.Done():
				_, _ = c.Stop()
			case <-c.done:
			}
		}()
		return c, nil
	case <-c.done:
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, c.unavailableExitError("bpftrace exited before the tracepoints became ready")
	case <-ctx.Done():
		_, _ = c.Stop()
		return nil, ctx.Err()
	case <-timer.C:
		_, _ = c.Stop()
		return nil, &UnavailableError{Reason: "timed out waiting for bpftrace to attach scheduler tracepoints"}
	}
}

// Stop interrupts bpftrace, waits for it to exit, and returns all exact samples
// collected so far. It is safe to call Stop more than once.
func (c *Collector) Stop() ([]Sample, error) {
	c.stopOnce.Do(func() {
		select {
		case <-c.done:
		default:
			if c.cmd.Process != nil && c.cmd.Process.Signal(os.Interrupt) == nil {
				c.mu.Lock()
				c.stopping = true
				c.mu.Unlock()
			}
		}
	})
	<-c.done

	c.mu.Lock()
	defer c.mu.Unlock()
	samples := append([]Sample(nil), c.samples...)
	if c.parseErr != nil {
		return samples, c.parseErr
	}
	if c.waitErr != nil && !c.stopping {
		return samples, c.unavailableExitErrorLocked("bpftrace exited unexpectedly")
	}
	return samples, nil
}

func checkTracepointAvailability(ctx context.Context, executable string) error {
	cmd := exec.CommandContext(ctx, executable, "--dry-run", "-e", traceProgram)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	reason := "bpftrace could not attach scheduler tracepoints"
	if text := strings.TrimSpace(string(output)); text != "" {
		reason += ": " + text
	}
	reason += ": " + err.Error()
	return &UnavailableError{Reason: reason}
}

func (c *Collector) unavailableExitError(prefix string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.unavailableExitErrorLocked(prefix)
}

func (c *Collector) unavailableExitErrorLocked(prefix string) error {
	reason := prefix
	if output := strings.TrimSpace(c.stderr.String()); output != "" {
		reason += ": " + output
	}
	if c.waitErr != nil {
		reason += ": " + c.waitErr.Error()
	}
	return &UnavailableError{Reason: reason}
}

type eventWriter struct {
	collector *Collector
	pending   []byte
}

func (w *eventWriter) Write(data []byte) (int, error) {
	originalLength := len(data)
	for len(data) > 0 {
		index := bytes.IndexByte(data, '\n')
		if index < 0 {
			w.pending = append(w.pending, data...)
			break
		}
		w.pending = append(w.pending, data[:index]...)
		w.handleLine(string(w.pending))
		w.pending = w.pending[:0]
		data = data[index+1:]
	}
	return originalLength, nil
}

func (w *eventWriter) handleLine(line string) {
	if line == "linuxobs_ready" {
		w.collector.readyOnce.Do(func() { close(w.collector.ready) })
		return
	}
	if !strings.HasPrefix(line, "linuxobs_delay ") {
		return
	}
	sample, err := ParseDelayLine(line)
	w.collector.mu.Lock()
	defer w.collector.mu.Unlock()
	if err != nil {
		if w.collector.parseErr == nil {
			w.collector.parseErr = err
		}
		return
	}
	w.collector.samples = append(w.collector.samples, sample)
}

type boundedBuffer struct {
	mu    sync.Mutex
	value strings.Builder
	limit int
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	originalLength := len(data)
	remaining := b.limit - b.value.Len()
	if remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
		}
		_, _ = b.value.Write(data)
	}
	return originalLength, nil
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.value.String()
}
