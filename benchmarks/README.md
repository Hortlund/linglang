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

### Current four-language local results — 2026-10-04

The current working tree, including leaf-call optimization and type-aware cell
tracing, was measured twice on the same Apple M4 Pro / macOS ARM64 machine,
OTP 29 / ERTS 17.1 with the JIT, Elixir 1.20.4, and Go 1.27.1. Each suite used
one BEAM scheduler, `GOMAXPROCS=1`, three warmups, and 21 measured samples per
workload/language. All 1,344 measured runs passed result and applicable managed
cleanup checks. Both reports have source fingerprint
`6b9c59dfbf4626b3e603c2032788de1ceca02d631bf72734531183abe1a0c80d`.

The second suite (`2026-10-04T15:11:49Z`) produced these median elapsed milliseconds:

| Workload | linglang (optimized) | Erlang | Elixir | Go |
| --- | ---: | ---: | ---: | ---: |
| arithmetic | 5.353 | 4.218 | 4.224 | 0.027 |
| calls | 11.275 | 3.946 | 3.891 | 0.023 |
| structs | 8.634 | 7.539 | 7.337 | 0.022 |
| pointers | 82.046 | 4.898 | 5.258 | 0.023 |
| lists | 4.147 | 0.972 | 0.971 | 0.132 |
| maps | 6.210 | 3.304 | 3.291 | 0.355 |
| strings | 13.068 | 43.227 | 42.992 | 0.791 |
| messages | 26.663 | 16.565 | 16.540 | 1.496 |

The first suite (`2026-10-04T15:11:26Z`) measured linglang pointers at 81.274 ms;
the repeat was within 1%. Arithmetic varied more: linglang 6.578 -> 5.353 ms,
Erlang 4.817 -> 4.218 ms, and Elixir 4.825 -> 4.224 ms. Background desktop/system
activity was present, although no tests or other repository benchmarks ran
concurrently. Treat small differences from historical runs cautiously; this is
local repeatability evidence, not a controlled attribution experiment.

The pointer median is about 41% below the older full-suite result of 138.133 ms,
which predates leaf-call optimization. The earlier isolated post-leaf result was
85.551 ms. These new runs do not isolate the effect of tracing; its compiler-phase
measurements are recorded below. Pointers still take roughly 17 times the Erlang
value-threading reference. Other reference implementation differences, including
native Go inlining/mutable maps and specialized linglang string helpers, still
apply. Only the pointer workload allocates a managed cell (one per run).

Raw samples, p95 values, counters and environment metadata are preserved in
`_build/benchmarks/four-languages-tracing-2026-10-04.json` and
`_build/benchmarks/four-languages-tracing-2026-10-04-repeat.json` (ignored by Git).
The original `four-languages.json` remains unchanged. Reproduce with:

```sh
go run ./cmd/bench -compare -samples 21 -warmup 3 -out _build/benchmarks/four-languages-next.json
```

### Previous four-language local results

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

### Lexer operator recognition

Lex/parse call-time profiles found 48,062 calls to `startsWith` on the lexer
corpus: operator recognition tried every prefix, repeatedly reading scanner
fields. It now caches the current byte and tests each candidate's first byte
before checking its full prefix. The longest-first ordering is unchanged.
The follow-up profile recorded 4,547 `startsWith` calls.

Uninstrumented macOS ARM64 / OTP 29 runs used three samples and one warmup per
backend, with no concurrent heavy tests:

| Entry point | Optimized before / after (ms) | Cells before / after (ms) |
| --- | ---: | ---: |
| lex | 115.108 / 105.903 | 588.261 / 379.644 |
| parse | 142.094 / 135.904 | 830.949 / 573.439 |

Optimized lex reductions fell from 15.83 million to 14.84 million; cell lex
allocations fell from 291,259 to 164,202. Parse includes lexing, so these are
overlapping improvements. Corpus paths were unchanged; the lexer corpus itself
includes the small source edit. Fingerprints record both revisions. These local
samples support this narrow optimization, not a global speedup or CI threshold.

Reports: `_build/benchmarks/frontend-operators-before.json` and
`frontend-operators-after.json`. Separate tracing reports are
`profile-lex-before.txt`, `profile-lex-after.txt`, `profile-parse-before.txt`, and
`profile-parse-after.txt` in the same directory.


### Type-aware managed-cell tracing and compiler rebuild

The collector previously revisited pointer-free AST and metadata values held
in cells and temporary root frames. Both compilers now mark pointer-free cell
contents and list the pointer-containing fields of structs. Cell identities
remain rooted; writes and loop cloning preserve the tracing descriptor. The
bootstrap emitter also omits pointer-free temporary roots. Descriptors live
separately from values so the ordinary cell read/write paths stay unchanged.

Uninstrumented macOS ARM64 / OTP 29 runs used the same corpus paths and bytes,
three samples and one warmup per backend, with no concurrent heavy tests:

| Entry point | Optimized before / after (ms) | Cells before / after (ms) |
| --- | ---: | ---: |
| lex | 105.668 / 107.070 | 366.700 / 322.595 |
| parse | 134.205 / 133.841 | 549.397 / 427.087 |
| resolve | 10.597 / 10.801 | 383.848 / 98.532 |
| check | 43.295 / 44.898 | 1818.222 / 504.858 |
| emit | 9.979 / 9.343 | 49.698 / 41.165 |

Cell-backend resolve and check improved about 74% and 72%. Checker reductions
fell from 832.63M to 177.20M, with the same 96,764 managed allocations, 428 peak
cells, and 377 collections. This targets traversal cost rather than collector
frequency or cell allocation. Emission now performs additional static type
classification (17,756 -> 18,205 cell allocations in this corpus). Optimized
latencies were mostly flat; these small local samples do not establish a global
speedup or a performance threshold.

The opt-in A -> B -> C proof now passes: B rebuilt C in 185.729 seconds and emitted
exactly the same 1,202,577 bytes. C was compiled and checked against seed execution
under forced GC and against A's source diagnostics, with clean final roots/cells.
The rebuild timing is one local run with other correctness checks running during
part of it; it is separate from the isolated frontend measurements above. The
previous ten-minute deadline was exceeded, so there is no completed before-run
rebuild timing to compare.

Reports: `_build/benchmarks/frontend-tracing-before.json` and
`frontend-tracing-final.json`. An intermediate paired-metadata layout is preserved
in `frontend-tracing-paired.json`; the final layout restored the cell read/write
fast path. The successful full proof log is `_build/selfhost/proof-tracing-final.txt`.
