# linglang benchmarks

Run from the repository root with Go, `erl`, and `erlc` available:

```sh
go run ./cmd/bench
go run ./cmd/bench -samples 21 -warmup 3 -out _build/benchmarks/baseline.json
go run ./cmd/bench -samples 21 -workload messages -out _build/benchmarks/messages.json
go run ./cmd/bench -compare -samples 21 -warmup 3 -out _build/benchmarks/four-languages.json
```

`-samples` accepts 1–100 measured runs, `-warmup` accepts 0–100 unmeasured runs,
and `-workload` selects a name below or `all` (the default). The defaults are
seven samples and two warmups per variant. Use different `-out` paths to preserve
before/after reports; the default report is replaced on each successful run.

`-compare` selects optimized linglang, Erlang, Elixir, and native Go instead of
the default linglang-backend comparison. It also requires `elixir` on `PATH`.
Elixir modules and the standalone Go executable are compiled before measurement.

| Workload | Work per run |
| --- | --- |
| arithmetic | Sum 100,000 integers with a mutable loop accumulator. |
| calls | Sum 100,000 integers through a named `add` function. |
| structs | Update two fields of a local struct 100,000 times. |
| pointers | Increment a struct through a pointer in 100,000 function calls. |
| lists | Prepend 20,000 integers, then sum them with range. |
| maps | Insert 10,000 integer keys, look up each value, and remove half. |
| strings | Split and trim 10,000 config lines, parsing 40,000 decimal fields. |
| messages | Exchange 10,000 integer requests and replies with a monitored worker. |

Each workload runs as optimized linglang, the original cell backend (`--no-opt`),
and handwritten Erlang. Every implementation checks its result. A crash or wrong
answer fails the benchmark. The harness also checks that managed cells and root
frames can be reclaimed after each run, outside the timed interval.

Compilation and BEAM startup happen before timing. Each workload gets its own
VM with one scheduler (`+S 1`); each sample gets a fresh monitored process. Warmups
load the code, and the measured variant order rotates each round. These are
single-scheduler runtime microbenchmarks. The message workload includes spawning,
waiting for, and stopping its worker; it measures 10,000 complete round trips,
not 10,000 individual messages. It does not measure multicore throughput or OTP
supervisor restart latency.

The console shows median and nearest-rank p95 elapsed microseconds. With only
seven samples, p95 is the slowest sample; more samples make it more useful.
Allocation, peak-cell, reduction, and collection counters come from the sample
nearest the median. JSON preserves all samples, summaries, environment metadata,
and a SHA-256 fingerprint of the generated Erlang, runtime sources, and selected
Go/Elixir reference sources. Times are recorded in whole microseconds.

Managed allocations and peak cells count linglang's pointer/local cells, not all
BEAM allocations or memory bytes. Zero cells still means Erlang terms are being
allocated. Reductions are BEAM scheduling work units, not CPU instructions. These
counters cover the main benchmark process; the message worker contributes to
elapsed time but its counters are not included. Cell GCs count linglang's managed
collector, not BEAM's heap collector. GC stress is disabled.

The Erlang references are comparison implementations, not universal performance
limits. The pointer reference threads a value explicitly instead of implementing
general pointer identity. The message reference uses native messages without
linglang's schema checks. The string reference uses OTP's `string:trim` and
`binary_to_integer`, while linglang has specialized ASCII trimming and bounded
integer parsing. Arithmetic wraps to signed 64 bits in every variant.

## Comparing four languages

In `-compare` mode, all three BEAM implementations run in the same Elixir-launched
OTP VM per workload. The shared measurement harness rotates their order. Go runs
separately immediately afterward, with `GOMAXPROCS=1`. Each Go sample uses a fresh
goroutine and forces GC before timing, just as the BEAM sample process does.
Both harnesses enforce a 30-second deadline per sample. All warmups, startup,
compilation, counter reads, and sample-process/goroutine creation are excluded
from elapsed time. Worker creation inside the message workload is included.

These references perform the same fixed-size tasks and validate the same answers;
their implementations use each language's native facilities:

- Go uses signed `int64` values, native structs and pointers, a singly linked
  list, and a mutable map. BEAM struct fields and map updates create immutable
  snapshots. The map test does not retain an old snapshot, so Go can update in
  place. It is not emulating persistent maps.
- Elixir uses maps for the struct workloads and passes updated counter values
  through recursion, like the Erlang reference. These do not implement linglang's
  general pointer aliasing. Its string test uses the same OTP functions as Erlang.
- Go's normal compiler optimizations are enabled, including inlining `add` and
  `increment`. Go may therefore eliminate function-call or pointer indirection
  overhead that remains in the emitted BEAM code.
- Go's message test uses an unbuffered request channel, reply channel, and worker
  completion channel. BEAM uses asynchronous mailboxes and a process monitor.
  The BEAM references also set a five-second timeout on each receive; Go uses
  the outer sample watchdog. This is a local round-trip comparison, not a test
  of equivalent failure semantics or network communication.

Go allocation counts and bytes cover the timed workload across its process,
including its message worker; harness setup is excluded. They measure actual
Go heap allocations and are different from linglang's managed-cell counters.
The console shows dashes for Go's unavailable BEAM counters and for unavailable
Go counters on BEAM. In JSON, ignore BEAM counter fields for `variant: "go"`;
use `go_allocations` and `go_allocated_bytes` instead. Runtime versions and
`go_max_procs` are saved with the report. Comparison tests skip when Elixir is
unavailable; running the command with `-compare` requires it and fails clearly
if it is missing.

### Four-language local results

Apple M4 Pro, macOS ARM64, OTP 29 / ERTS 17.1 with the JIT, Elixir 1.20.4,
and Go 1.27.1. One BEAM scheduler and `GOMAXPROCS=1`. Run timestamp:
`2026-10-03T23:10:12Z`. Each implementation had three warmups and 21 measured
runs; all 672 measured runs passed. Median elapsed milliseconds:

| Workload | linglang (optimized) | Erlang | Go | Elixir |
| --- | ---: | ---: | ---: | ---: |
| arithmetic | 5.523 | 4.320 | 0.025 | 4.365 |
| calls | 12.677 | 4.594 | 0.024 | 4.581 |
| structs | 9.170 | 7.790 | 0.027 | 7.782 |
| pointers | 138.133 | 5.051 | 0.027 | 5.546 |
| lists | 4.512 | 1.062 | 0.139 | 1.055 |
| maps | 6.691 | 3.502 | 0.355 | 3.529 |
| strings | 14.395 | 46.266 | 0.806 | 46.118 |
| messages | 29.180 | 17.744 | 1.641 | 18.256 |

This run predates the leaf pointer optimization measured below.

Go's native machine code and inlining make a large difference in these small
loops. Inspecting the Go executable confirmed that its arithmetic and pointer
loops still execute; the workload has not been replaced with a constant answer.
Erlang and Elixir produce similar results, as expected from these closely
matched implementations on the same VM. Linglang's runtime pointer handling
remains its largest gap here. String results reflect the different library
implementations described above. These numbers measure these programs on this
machine, not overall language speed or production service capacity.

The raw samples, p95 values, counters, versions, and source fingerprint are in
`_build/benchmarks/four-languages.json` (ignored by Git). Rerun with the command
above and preserve a separate report when measuring changes.

## Local baseline

### Leaf pointer optimization

A later change lets proven noncollecting leaf helpers borrow their caller's roots,
avoiding repeated root frames and safe points inside small pointer operations.
Functions with loops, managed allocations, unknown calls, or blocking operations
retain the existing protocol. Pointer ownership and nil checks remain in place.

On the same machine, 21 samples with three warmups of the pointer workload gave:

| Metric | Before | After |
| --- | ---: | ---: |
| linglang median milliseconds | 137.470 | 85.551 |
| linglang p95 milliseconds | 144.797 | 90.691 |
| Main-process reductions near the median | 15,530,432 | 10,571,104 |
| Managed cells allocated | 1 | 1 |
| Erlang reference median milliseconds | 5.193 | 5.139 |

That is about 38% less elapsed time, or 1.61× faster, for this workload. The
reference stayed within roughly 1%, providing a useful check on machine drift.
It still leaves a substantial gap to explicit value threading. The raw reports
are `_build/benchmarks/pointers-before.json` and `pointers-after.json`. Reproduce
the measurement on a particular revision with:

```sh
go run ./cmd/bench -compare -samples 21 -warmup 3 -workload pointers -out _build/benchmarks/pointers.json
```

The older full-suite results below predate this optimization.

### Original backend baseline

Measured on an Apple M4 Pro, macOS ARM64, OTP 29 / ERTS 17.1 with the JIT,
Go 1.27.1, and one BEAM scheduler. Run timestamp: `2026-10-03T23:03:55Z`.
There were 21 measured samples and three warmups for each variant; all 504
measured runs passed their correctness and cleanup checks.

Median execution times in milliseconds:

| Workload | Cell backend | Optimized | Erlang reference | Cell / optimized speedup |
| --- | ---: | ---: | ---: | ---: |
| arithmetic | 189.84 | 5.92 | 4.69 | 32.1× |
| calls | 355.65 | 12.56 | 4.37 | 28.3× |
| structs | 227.34 | 9.16 | 8.23 | 24.8× |
| pointers | 272.33 | 135.77 | 5.15 | 2.0× |
| lists | 92.27 | 4.30 | 1.06 | 21.5× |
| maps | 81.09 | 7.24 | 3.84 | 11.2× |
| strings | 163.33 | 13.93 | 46.55 | 11.7× |
| messages | 78.11 | 27.28 | 16.79 | 2.9× |

The optimized backend allocated zero managed cells except for the pointer
workload, which allocated one. The old backend allocated 20,004–300,002 cells
per run. Arithmetic and struct updates are relatively close to these Erlang
references. Pointer-heavy function calls remain an optimization target: about
26× slower than explicit value threading here. Faster string results reflect
the specialized implementation discussed above, not a general claim that
linglang beats Erlang.

The raw local report is `_build/benchmarks/baseline.json` (ignored by Git).
Keep separate reports when comparing changes on the same machine. Close busy
apps and avoid running tests at the same time as a benchmark. CPU load, thermal
state, OTP version, and scheduler settings can change the numbers.

There are no timing thresholds in CI. `go test ./cmd/bench` runs the available
implementations and verifies correctness, root cleanup, report output, and
failure handling, including deliberately wrong Go and Elixir answers.

## Bootstrap compiler phases

```sh
go run ./cmd/bench -frontend -samples 7 -warmup 2 -out _build/benchmarks/frontend.json
```

This mode measures the Linglang-written compiler on both seed backends, using
one BEAM scheduler, rotated backend order, and a fresh process per sample. The
fixed corpus for lexing, parsing, resolving, and checking is `bootstrap/lexer`
without test files. Emission uses `examples/bootstrap_demo`, which fits the
current emitter subset. The report records each corpus's source paths, a source
fingerprint covering the corpus, compiler, driver and runtime, result counts,
median/p95 timings, reductions, managed allocations, peak cells, and collections.
Use the same checkout path for comparisons: source locations include absolute
filenames. `-frontend` cannot be combined with `-compare` or `-workload`.

Compilation, file reads, VM startup, and prerequisite parsing are outside the
timed interval. `check` requests expression/binding metadata, as emission needs;
it includes the resolver internally. `emit` includes resolution and checking
internally, as well as module generation. These are entry-point costs, not
disjoint passes that can be added together. Allocation/collection counters are
differences from the end of preparation; the peak counter is reset before timing.
Inputs are immutable pointer-free values. Managed-cell cleanup is checked after
preparation and after each measured or warmup run. Each run checks success and a
positive result count; measured counts must agree across samples and backends.
Semantic correctness remains covered by the separate seed oracle suites.

The initial local baseline is `_build/benchmarks/frontend-before.json`. It used
three measured samples and one warmup per backend on macOS ARM64 / OTP 29.
Optimized checking took a median 144.036 ms; cell-backend checking took
9,763.453 ms. This small baseline helps select profiling targets; it does not
establish a performance threshold. Preserve the report when measuring changes.

### Internal identity construction

A call-time profile of checker metadata construction found over 40,000 filename
byte visits in `quotedText` on the lexer corpus (relative source paths). Internal
keys now use a raw filename followed by `:` and a decimal offset/node ID. The
last colon separates the position unambiguously, even when filenames contain
colons, quotes, Unicode, or newlines. Diagnostic quoting is unchanged.

A fresh same-path comparison on macOS ARM64 / OTP 29, with three samples and one
warmup per backend, produced these medians:

| Entry point | Optimized before / after (ms) | Cells before / after (ms) |
| --- | ---: | ---: |
| check | 148.759 / 45.607 | 9,679.850 / 1,976.776 |
| emit | 24.251 / 9.806 | 232.579 / 62.410 |

Optimized checker reductions fell from about 22.19 million to 5.62 million;
cell checker allocations fell from 447,871 to 100,884. Lex/parse/resolve
reductions stayed essentially unchanged. The checker performs no emission, so
its improvement isolates the shared identity change; the emission comparison
also includes newly supported collections and range loops. These small local
samples show a substantial improvement on this corpus, not a general language
speedup or a CI timing threshold.

Raw reports: `_build/benchmarks/frontend-collections-before.json` and
`frontend-collections-after.json`. Call-time profiles are saved as
`profile-check-before.txt` and `profile-check-after.txt` in the same directory.
Profiling adds overhead; the timings above come from uninstrumented runs.
