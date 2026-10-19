# Linux CPU scheduler evaluation

A single-node Linux tool for comparing CPU scheduling under borrower/victim
contention. It includes CPU-time-based Go workloads, a repeatable trial runner,
optional Linux telemetry, and a local results dashboard.

This repository contains the evaluation tool and its tests only. It does not
include measured benchmark results or a scheduler implementation.

## Build and test

Requires Linux and Go 1.23 or later. Cgroup telemetry requires cgroup v2;
optional scheduler tracing requires `bpftrace` and permission to attach probes.

```sh
git clone https://github.com/amuldowney/cpu-scheduler-eval.git
cd cpu-scheduler-eval
go test ./...
go build -buildvcs=false -o bin/workload ./cmd/workload
go build -buildvcs=false -o bin/bench ./cmd/bench
go build -buildvcs=false -o bin/viewer ./cmd/viewer
```

## What it measures

- **Application response time:** scheduled release to completion of each work
  item that actually starts. Work consumes configured OS-thread CPU time, so
  preemption extends response time rather than shortening the work.
- **Missed releases and throughput:** overdue releases are skipped, not queued
  indefinitely. Skipped releases are counted separately and are **not included
  in latency percentiles**. Always inspect both latency and missed demand.
- **Go scheduling latency:** process-local `/sched/latencies:seconds` histogram
  deltas, using bucket-midpoint estimates.
- **Linux wakeup-to-running delay:** optional tracepoint samples, separate from
  Go scheduling latency and not a complete measure of preemption-related wait.
- **CPU accounting and pressure:** cgroup CPU usage, CPU weight, quota
  throttling, per-role CPU PSI, and host CPU PSI when available.
- **Run metadata:** kernel, architecture, CPU requests, offered demand,
  effective GOMAXPROCS, scheduler revision, and workload parameters.

Unavailable telemetry carries an explicit reason; it is never treated as zero.

## Establish contention before comparing policies

Configured demand alone is not evidence of contention. Before using a scenario:

1. Run the victim alone on the intended CPUs and cgroup. Check actual CPU use,
   release delivery, mean latency, and tail latency.
2. Run the borrower alone on those same CPUs. Check actual CPU consumption and
   runnable backlog rather than only the requested duty cycle.
3. Run both together. Confirm increased runnable pressure or scheduling delay,
   and distinguish CPU competition from quota throttling.
4. Repeat controls and combined trials before changing scheduler policy.

Choose a work period the host can deliver reliably in the solo control. The
short default period is not guaranteed to work reliably on every host; increase
both period and work duration proportionally if needed. Set GOMAXPROCS explicitly
when you need OS-runnable workers rather than Go-internal queuing.

The standalone workload can be used for control runs; see `./bin/workload -help`.
It does not place itself in a cgroup. Its raw workload JSON is not a full trial
artifact for the dashboard.

## Run a trial

**This command starts CPU load.** Review the host, CPU placement, and cgroups
before passing `-confirm-load-test`.

The example below assumes two CPUs dedicated to the test, preconfigured
borrower/victim cgroups constrained to those CPUs, and policy-appropriate weights.
CPU requests total 2000m and nominal victim demand is 450m. The configuration is
an example, not a performance claim; calibrate it with the controls above.

```sh
sudo ./bin/bench \
  -confirm-load-test \
  -workload-bin ./bin/workload \
  -policy native-weight \
  -scheduler-revision '<record-the-exact-kernel-or-scheduler-build>' \
  -scenario borrower-victim \
  -allocatable-mcpu 2000 \
  -borrower-request-mcpu 1500 \
  -victim-request-mcpu 500 \
  -borrower-workers 4 \
  -borrower-gomaxprocs 4 \
  -borrower-period 10ms \
  -borrower-cpu-time 9500us \
  -victim-workers 1 \
  -victim-gomaxprocs 2 \
  -victim-period 10ms \
  -victim-cpu-time 4500us \
  -borrower-cgroup /sys/fs/cgroup/bench/borrower \
  -victim-cgroup /sys/fs/cgroup/bench/victim \
  -duration 30s \
  -repeat 5 \
  -output runs
```

The runner validates that requests fit declared allocatable CPU, with a minimum
500m request per role. These are admission checks and metadata: **the runner does
not enforce CPU reservations or configure the scheduler.**

The cgroups must already exist, be empty, and be dedicated to the trial. The
runner moves each workload into its supplied cgroup after launch; `-start-delay`
allows placement and tracer startup before measurement. It does not create
cgroups, change weights, pin CPUs, or load/switch scheduler policies. Configure
those separately. Declared allocatable capacity must match the CPUs actually
available to the workloads, not merely the machine's advertised CPU count.

Omitting both cgroup paths runs an ungrouped harness smoke test; it is not a
substitute for an isolated policy comparison. Repeat trials with identical
kernel, CPU placement, cgroups, workload settings, and measurement windows.

### Optional tracing limitation

The current `bpftrace` collector uses `str(args->comm)` for role attribution.
Versions that already type this tracepoint field as a string can reject that
expression. Missing tools, attachment failures, and script errors mark Linux
scheduling latency unavailable while allowing other measurements to complete.
Do not claim Linux delay was measured unless the artifact reports it available
and contains actual samples. This publication does not change the collector.

## View your own results locally

```sh
./bin/viewer -listen 127.0.0.1:8080 -runs runs
```

Open <http://127.0.0.1:8080>. The dashboard compares application, Go scheduling,
and available Linux scheduling latency, with expanded metadata and pressure
metrics. You can also load a trial JSON file directly in the browser.

The dashboard is local-only by default. Keep run artifacts private: they may
contain host metadata and scheduler revision identifiers. Generated results,
binaries, logs, and profiles are excluded by `.gitignore`; no measured results
are distributed here.

## Interpretation and safety

- Application percentiles are nearest-rank estimates over sampled operations;
  inspect missed releases separately. Go scheduler summaries are histogram
  estimates, not exact raw-event percentiles.
- A successful build or smoke test proves the harness runs, not that a scheduler
  improves performance. Use controls, repeated comparisons, sufficient tail
  samples, and working telemetry to support policy claims.
- Containers share their host kernel. A container alone cannot add kernel
  scheduling features that the host lacks.
- Loading a scheduler policy can affect the entire host. This tool does not
  provision hosts, install kernels, load policies, or manage Kubernetes placement.
