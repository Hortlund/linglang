# linglang benchmarks

For an installed self-hosted compiler, OTP alone can identify hot functions:

```sh
mkdir -p _build/profiles
escript benchmarks/profile.escript --compiler bin/linglang-selfhost \
  --source bootstrap/lexer > _build/profiles/lexer.json
```

This profiles checking through lowering, not application execution or OTP's
BEAM compilation. Version 1 JSON contains the archive SHA-256, source path,
instrumented elapsed microseconds, managed counters, and per-function call counts
and call time, sorted by descending time. `source_function` decodes Linglang
function names. Only the compiler worker is traced. `--no-opt` changes the target
lowering, not the supplied compiler's own lowering; `--timeout SECONDS` defaults
to 120. Failed checks/timeouts fail the command, and successful reports require
zero final managed cells and roots. The tool uses OTP 29 and no Go/Python/erlc.

Tracing has substantial overhead. Use these profiles to select work, then measure
uninstrumented archives against the same frozen source bytes and absolute paths:

```sh
python3 benchmarks/bootstrap.py \
  --compiler before=/absolute/path/to/old-compiler \
  --compiler after=/absolute/path/to/verified-new-compiler \
  --corpus /absolute/path/to/frozen-corpus \
  --samples 5 --warmup 1 --out _build/profiles/comparison.json
```

That harness alternates archive order, excludes VM startup, retains source and
artifact hashes, and checks deterministic emission and final managed cleanup.
Do not run other builds/tests alongside latency measurements. A compiler profile
or one small corpus cannot establish application throughput or production scale.

## Application runtime benchmarks

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

### Native list traversal — 2026-10-05

The next change emits native list patterns in the Go seed's optimized range
header. Fresh unboxed values bind directly to the matched head; the private
cursor advances on that edge, with no more/first/tail helper calls. Assignment
targets and addressed iteration variables preserve their original semantics.

Interleaved previous/new/Erlang programs, one scheduler, three warmups and 21
samples per variant, on the same Apple M4 Pro / macOS ARM64 / OTP 29 JIT setup:

| Workload | Previous (ms) | Native range (ms) | Erlang (ms) |
| --- | ---: | ---: | ---: |
| lists | 1.664 | 1.355 | 0.934 |
| strings | 7.032 | 6.047 | 42.143 |

Lists improved 18.6%, with 548,680 -> 388,678 reductions (29.2% fewer). Strings
improved 14.0%, with 1,664,598 -> 1,322,275 reductions (20.6% fewer); this workload
also ranges over lists. The string reference still uses different library
implementations. Neither workload allocates managed cells. All answers and final
root/cell cleanup checks passed; no tests or other benchmarks ran concurrently.

The separate full suite used 21 samples and three warmups across all four
languages. All 672 measured runs passed. Median milliseconds:

| Workload | Linglang | Erlang | Elixir | Go |
| --- | ---: | ---: | ---: | ---: |
| arithmetic | 5.133 | 4.168 | 4.137 | 0.030 |
| calls | 5.082 | 4.051 | 3.966 | 0.024 |
| structs | 8.346 | 7.287 | 7.176 | 0.026 |
| pointers | 79.508 | 4.764 | 5.035 | 0.026 |
| lists | 1.446 | 0.987 | 0.981 | 0.131 |
| maps | 4.179 | 3.260 | 3.226 | 0.349 |
| strings | 6.263 | 42.110 | 41.932 | 0.740 |
| messages | 25.445 | 16.430 | 16.474 | 1.503 |

These application results use the Go-seeded optimized backend. Small changes in
unaffected workloads are not attributed to range lowering. Raw reports and
generated-source hashes are in `_build/range-native/applications.json`,
`paired-lists.json`, and `paired-strings.json`. The local paired_applications.py
reuses the standard measurement body with preserved before/after modules.

### Noncollecting calls and native helpers — 2026-10-05

The seed now omits redundant managed-GC checks in pointer-free callers of proven
noncollecting leaf functions and an audited set of native collection/text
helpers. Rooted callers, recursion, unknown calls, and blocking helpers retain
the conservative protocol. Native BEAM allocation and GC still operate normally.

On Apple M4 Pro / macOS ARM64, OTP 29 / ERTS 17.1 JIT, old and new generated
programs were interleaved with the Erlang reference in one VM using one scheduler,
three warmups and 21 samples per variant. Each sample ran in a fresh process and
checked its answer and final managed-root/cell cleanup. No tests or other
repository benchmarks ran concurrently. Median milliseconds:

| Workload | Before | After | Erlang | Elapsed-time reduction |
| --- | ---: | ---: | ---: | ---: |
| calls | 11.418 | 4.993 | 3.976 | 56.3% |
| lists | 4.087 | 1.644 | 0.920 | 59.8% |
| maps | 6.064 | 4.103 | 3.236 | 32.3% |
| strings | 12.760 | 7.091 | 41.906 | 44.4% |

Calls dropped from 3,905,024 to 1,500,296 reductions; lists from 1,674,366 to
548,680; maps from 1,312,028 to 550,471; strings from 3,912,242 to 1,664,598.
These workloads allocate no managed cells in either version. String reference
library differences still apply; this is not a general language-speed claim.

The complete four-language run used 21 samples and three warmups too:

| Workload | Linglang | Erlang | Elixir | Go |
| --- | ---: | ---: | ---: | ---: |
| arithmetic | 5.303 | 4.207 | 4.196 | 0.028 |
| calls | 5.412 | 4.147 | 4.263 | 0.027 |
| structs | 8.285 | 7.356 | 7.157 | 0.024 |
| pointers | 79.444 | 4.626 | 4.952 | 0.028 |
| lists | 1.678 | 0.938 | 0.938 | 0.128 |
| maps | 4.157 | 3.264 | 3.260 | 0.329 |
| strings | 7.239 | 41.901 | 41.473 | 0.741 |
| messages | 25.439 | 16.524 | 16.505 | 1.499 |

All 672 measured runs passed. Go used GOMAXPROCS=1; runtime versions are saved
in the report. The pointer workload remains roughly 17 times the Erlang
value-threading reference. Application results concern the Go-seeded optimized
backend; the bootstrap emitter still uses cells for mutable control-flow state.

Raw full-suite reports: `_build/benchmarks/current-2026-10-05.json` (before) and
`optimized-2026-10-05.json` (after). Interleaved comparisons and generated-source
hashes: `_build/bootstrap-readonly/paired-{calls,lists,maps,strings}.json`.
The local `paired_applications.py` reuses the repository benchmark's measurement
body against preserved before/after generated modules. These files are ignored.

### Previous four-language local results — 2026-10-04

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

### Application baseline — 2026-10-11

macOS ARM64, OTP 29.1.1, one scheduler, seven samples and two warmups, with no
concurrent repository tests. These are median microseconds for the fixed workloads
above, emitted by the Go seed's two lowerings; compilation and VM startup are
excluded. These are not measurements of programs emitted by the self-built CLI.

| Workload | Cells | Optimized | Erlang reference |
| --- | ---: | ---: | ---: |
| arithmetic | 217641 | 5708 | 4524 |
| calls | 435302 | 5540 | 4452 |
| structs | 254901 | 9674 | 8153 |
| pointers | 316589 | 81731 | 5196 |
| lists | 79433 | 1534 | 1084 |
| maps | 67935 | 4944 | 3884 |
| strings | 212958 | 6947 | 51181 |
| messages | 87274 | 30829 | 17748 |

All answers and final managed cleanup checks passed. The reference differences
described above matter: pointer identity, message validation, and string-library
choices differ. Pointer-heavy application code is a useful next profiling target;
these measurements do not establish multi-node throughput or fault-recovery
behavior. The compiler rope optimization below targets compiler work, not these
application workloads. Raw report:
`_build/perf-2026-10-11/runtime-before.json`.

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

To measure actual packed, self-built compilers on the same frozen source corpus:

```sh
python3 benchmarks/bootstrap.py --compiler before=_build/compiler-before --compiler after=bin/linglang-bootstrap --corpus _build/frozen-lexer --samples 3 --warmup 1 --out _build/benchmarks/selfbuilt-lexer.json
```

Preserve the before executable and dereference source symlinks when freezing the
corpus. Keep its absolute path unchanged between runs. The script uses an
OTP-only PATH, extracts the embedded modules, interleaves compiler order, and
runs each sample in a fresh VM with one scheduler. Timing and reductions cover
the compiler's `main` call, including discovery, reading, parsing, checking,
emission, output, and managed heap cleanup; VM startup and unpacking are outside
the interval. It records source and compiler hashes, output bytes/hashes, managed
allocations, peak live cells, and GC counts, and requires zero live cells and root
entries/frames after every run. Outputs must be deterministic within a compiler;
different lowerings can produce different output. Use `--no-opt` with compilers
that support the original cell lowering. A compiler-source corpus measures a
rebuild's emission cost; OTP compilation and executable packaging remain outside
this measurement. Avoid running tests or other benchmarks concurrently.

### Pointer-free constant metadata — 2026-10-11

Profiling the actual self-built compiler found 16,437,995 calls to the collector's
`references/2` while checking the lexer corpus. A pointer inside the constant
string representation made otherwise immutable checker/emitter metadata require
tracing, including values with no rope at all.

Large constant strings now use integer links into a package-local node table.
Checked results retain that table. Shared-prefix comparisons still skip identical
nodes, and byte materialization looks nodes up explicitly. Keeping the table flat
also bounds generic scans in the cell backend: nested native trees would let a
generic root walk repeatedly expand shared subtrees.

Actual verified before/after archives, an unchanged frozen lexer corpus and path,
macOS ARM64 / OTP 29.1.1, one scheduler, seven measured samples and two warmups
per compiler, with interleaved order and no concurrent builds/tests:

| Metric | Before | After |
| --- | ---: | ---: |
| Median milliseconds | 861.306 | 521.400 |
| Median BEAM reductions | 160654476 | 36176181 |
| Managed allocations | 242214 | 242493 |
| Peak live cells | 384 | 398 |
| Managed collections | 946 | 947 |

Median latency fell 39.5% and reductions 77.5%. Allocations did not fall: this
change removes traversal cost. A separate instrumented profile counted 77,790
`references/2` calls afterward (99.5% fewer). Its timings are not benchmark
timings; the table above comes from uninstrumented self-built archives.

Every measured run emitted the same 82,614 bytes, SHA-256
`fcd8de8ed8519c69e43a046be529c9e84f7589588aee65f8d66b2cef7685f4f0`,
and finished with zero managed cells and roots. These small local samples are
not a production throughput claim or a CI performance threshold.

The new archive also emitted the complete current `bootstrap/emitter` corpus in
13.706 seconds median (three isolated samples plus one warmup; range
13.609–13.865 seconds). Each run allocated 4,768,870 managed cells, peaked at 421,
and finished with zero cells/roots. This is emission only: OTP compilation,
packaging, and acceptance suites are excluded. It is an after-only measurement,
not a controlled full-rebuild speedup comparison. The report is
`compiler-rebuild-after.json` in the directory below.

The Go-free proof completed from the previous verified compiler, with byte-identical
B/C emission of 1,860,236 bytes, SHA-256
`c5dee594d6da01727025ed51c0553d8e556aab159f716258a03d50dfbae1bf8c`.
All 70 C acceptance test executions passed, including forced GC and both lowerings
for the concurrency/language suites. The independent constant oracle and materialized-string
emission test passed in both compiler modes; the Go-seeded cell checker suite and
separate constant-result lifetime test passed under forced GC as well.

Reports and verified archives are under `_build/perf-2026-10-11/`:
`compiler-comparison.json`, `profile-before.json`, `profile-after.json`, and
`selfhost-proof.log`. These ignored local artifacts are not included in Git.

### Native bootstrap range cursors — 2026-10-05

The bootstrap emitter now uses a native cursor/index through list_range/4,
retaining scoped iteration bindings and typed managed roots. The cell emitter
remains available with --no-opt. Actual packed previous/new self-built compilers
were compared on the same frozen absolute corpus paths, with an OTP-only PATH,
one scheduler, and no concurrent tests or repository benchmarks.

Seven lexer samples and two warmups per compiler, interleaved, gave:

| Metric | Previous | Native range |
| --- | ---: | ---: |
| Median milliseconds | 743.973 | 691.085 |
| Reductions | 167,846,992 | 152,737,285 |
| Managed allocations | 248,376 | 236,126 |
| Peak live cells | 409 | 382 |
| Collections | 970 | 922 |

Lexer elapsed time fell 7.1%, reductions 9.0%, and allocations 4.9%. One isolated
full-compiler pair took 134.887 -> 124.201 seconds (7.9% less elapsed time), with
23,422,348,274 -> 21,560,696,105 reductions (8.0% fewer),
3,205,379 -> 3,061,894 allocations (4.5% fewer), 12,513 -> 11,952 collections,
and 440 -> 433 peak cells. The full-corpus timing is a single paired observation.
All final managed cells and root entries/frames were zero.

Reports: `_build/range-native/lexer-comparison.json` and `rebuild-comparison.json`.
Output differs across lowerings but is deterministic within each compiler.
The separate range-step B/C/D proof agrees on 1,225,743 bytes, SHA-256
`0139b0480d30f8e205ddae79847edaf54df73d732bc0b5da7ec7370a09280ece`,
with actual-C execution, diagnostics, CLI lifecycle and GC cleanup checks.
The final Go seed reproduces that output. Refreshed D also reproduces C's frozen
lexer output and passes collection execution under forced GC with clean final
cells/roots. The --no-opt frozen lexer output still
matches the previous compiler byte for byte; `cell-oracle.json` records its hash.

### Read-only locals in complex functions — 2026-10-05

Read-only primitive locals, including range declarations, now use native BEAM
values inside branches and loops. Mutable/addressed bindings retain cells.
Actual packed self-built compilers were compared on the unchanged absolute
frozen corpus paths under `_build/bootstrap-opt/corpus`, with one scheduler,
an OTP-only PATH, and no concurrent tests or repository benchmarks.

Seven measured lexer samples plus two warmups per compiler, interleaved, gave:

| Metric | Before | After |
| --- | ---: | ---: |
| Median milliseconds | 854.194 | 756.005 |
| Reductions | 186,107,804 | 167,854,647 |
| Managed allocations | 334,120 | 248,376 |
| Peak live cells | 412 | 409 |
| Collections | 1,305 | 970 |

That is 11.5% less elapsed time and 25.7% fewer managed allocations. A single
isolated pair for the full frozen compiler corpus took 152.550 -> 134.986 seconds
(11.5% less time), with 4,360,235 -> 3,205,379 allocations (26.5% fewer),
26,698,917,605 -> 23,423,383,241 reductions (12.3% fewer), and
17,024 -> 12,513 collections. Peak live cells fell from 447 to 440. Treat the
full-corpus timing as one paired local observation, not a stable speed estimate.

Every sample finished with zero managed cells, root entries, and root frames.
Different native lowering produces different Erlang source; output hashes are
stable within each compiler. The current-source B/C proof separately verifies
byte-identical compiler output at 1,245,245 bytes, SHA-256
`3238308b078fd95ba1df74091b09494b720a6b1cf54186e3f6994a80d6a1bdc9`,
and actual-C execution/diagnostics/CLI lifecycle/GC cleanup. It is distinct from
the frozen-corpus performance measurement. Reports are
`_build/bootstrap-readonly/lexer-comparison.json` and `rebuild-comparison.json`;
the proof is `proof-final.txt`. The Go seed's separate noncollecting-call changes
do not change this bootstrap compiler output, verified by the final seed hash.
The old/new `--no-opt` frozen-lexer output also matches byte for byte: 87,217 bytes,
SHA-256 `679a8c115de83977334fbb8036f9d920a1ea59fe6746d19e79bcbf6cf706d2f1`.

### Earlier native-local measurements

The first native-local bootstrap slice was measured on macOS ARM64 / OTP 29,
using the pre-change compiler and lexer sources frozen from `ae37e05`. The lexer
comparison used seven measured samples and two warmups for each actual packed
compiler, with interleaved order and no concurrent tests. Median compilation
time fell from 975.400 ms to 933.292 ms (4.3%). Managed allocations fell from
401,595 to 341,711 (14.9%); collections fell from 1,567 to 1,335 and peak live
cells from 434 to 412. Reductions increased from 198,860,313 to 199,949,284
(0.55%). The new compiler includes conservative local write/address analysis.
Every sample finished with zero live cells and root entries/frames. These are
local measurements, with no CI timing threshold. Raw reports live in
`_build/bootstrap-opt/lexer-comparison.json`; the frozen corpus's absolute paths
and per-file hashes are included in the report.

For the full frozen compiler corpus, a single isolated pair took 172.255 s before
and 174.772 s after (1.5% longer). Managed allocations fell from 5,219,447 to
4,465,948 (14.4%), collections from 20,358 to 17,437, and peak live cells from
468 to 447. Reductions increased 1.5%. This slice establishes lower allocation
cost and a modest lexer improvement; the full rebuild measurement does not
establish a speedup. The report is `_build/bootstrap-opt/rebuild-comparison.json`.
New write/address analysis and binding lookup costs are targets for the next
compiler optimization, alongside native values across branches and loops.

The binding-analysis follow-up compares the reviewed `11c77c6` compiler with a
shared variable-identity index, direct reuse of resolved keys for boxed reads,
and an early exit for complex functions without eligible primitive parameters.
It preserves complete emitted source for both frozen corpora in both lowering
modes. Seven fresh measured lexer samples and two warmups per compiler gave
977.220 -> 948.840 ms median (2.9% faster), 199,957,516 -> 186,116,102 reductions
(6.9% fewer), and 341,711 -> 334,120 allocations (2.2% fewer). Peak live cells
remained 412; collections fell from 1,335 to 1,305. Comparisons use the paired
measurements in this run, rather than timings from the earlier session. The raw
report is `_build/bootstrap-analysis/lexer-comparison.json`.

A fresh single paired measurement of the full frozen compiler corpus took
192.178 -> 163.050 s (15.2% faster), with 29,224,055,073 -> 26,733,149,112
reductions (8.5% fewer), 4,465,948 -> 4,360,235 allocations (2.4% fewer), and
17,437 -> 17,024 collections. Peak live cells remained 447. Both compilers
emitted the same 1,215,233 bytes and SHA-256, and every sample finished with zero
live cells and root entries/frames. This is a local single-pair observation,
with no CI timing threshold; see `_build/bootstrap-analysis/rebuild-comparison.json`.

The phase harness below measures Go-seeded compiler programs, so its optimized
versus cell comparison concerns the seed's lowering of the compiler itself.

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
