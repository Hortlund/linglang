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

### Applications emitted by self-hosted compilers

Supply an actual compiler archive to add its optimized and cell-lowered programs
alongside the seed's `optimized`/`cells` variants and the Erlang reference:

```sh
go run ./cmd/bench -compiler bootstrap=bin/linglang-selfhost \
  -samples 21 -warmup 3 -out _build/benchmarks/selfhost-applications.json

go run ./cmd/bench \
  -compiler before=/absolute/path/to/verified-before \
  -compiler after=/absolute/path/to/verified-after \
  -samples 21 -warmup 3 -out _build/benchmarks/selfhost-comparison.json
```

Each label adds `<label>_optimized` and `<label>_cells`. Labels must be unique,
start with a lowercase ASCII letter, and contain at most 32 lowercase letters,
digits, or underscores. Combine `-compiler` with `-compare` to include Elixir and
Go alongside the seed, Erlang, and both self-hosted lowering modes:

```sh
go run ./cmd/bench -compare -compiler selfhost=bin/linglang-selfhost \
  -samples 21 -warmup 3 -out _build/benchmarks/selfhost-four-languages.json
```

Multiple archives can be included in this mode for before/after comparisons.
`-frontend` remains separate. Without archives, `-compare` retains its original
four variants. With archives, every BEAM variant rotates in the same VM;
Go still runs separately with `GOMAXPROCS=1`.
The harness still requires Go, OTP, and `erlc`, plus `escript` for archives.
It checks each archive's version-1 bootstrap discovery response, but does not
establish its provenance or fixed-point verification: retain the proof log and
use a verified archive. Compiler execution uses the archive's embedded runtime;
all emitted applications run against the same current checkout runtime, whose
sources are retained and hashed. This isolates lowering differences and requires
archives compatible with that runtime.

Before measurement, the harness copies the selected workload/reference bytes
and archives into a unique `applications-*` directory under `-build-dir`
(default `_build/benchmarks`). All compilers emit from those same snapshot paths.
Each archive emits each mode twice; differing output fails the run. Compilation,
archive startup, and the emission check are outside application timing. Every
workload uses one VM and scheduler; measured order rotates over **all BEAM** variants.
Result assertions, sample deadlines, and final managed-root/cell cleanup apply
to archive-generated programs too. No other builds/tests should run alongside
latency measurements.

Application JSON reports have `schemaVersion: 1`, compiler labels, original paths,
archive hashes and snapshot paths, original input paths/hashes and snapshot paths, generated
Erlang/runtime paths and hashes, and the retained `build_dir`. That directory
also contains input snapshots, compiled BEAMs, and each exact measurement script
as `<workload>-measurement.erl.txt`. `source_sha256` fingerprints generated/runtime
sources and measurement scripts (and Go/Elixir reference sources when selected).
Use `-source-dir` to select a frozen workload/reference corpus. Source paths may
appear in generated diagnostics, so paired archive comparisons within one run
provide the same paths as well as the same bytes. Keep the report and its build
directory together; use distinct `-out` paths to preserve reports between runs.

### Runtime measurement details

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
| prime_jobs | Four workers process 32 bounded prime-count jobs; verify every ID/count and join all workers. |

Each workload runs as optimized linglang, the original cell backend (`--no-opt`),
and handwritten Erlang. Every implementation checks its result. A crash or wrong
answer fails the benchmark. The harness also checks that managed cells and root
frames can be reclaimed after each run, outside the timed interval.

Compilation and BEAM startup happen before timing. Each workload gets its own
VM with one scheduler (`+S 1`); each sample gets a fresh monitored process. Warmups
load the code, and the measured variant order rotates each round. These are
single-scheduler runtime workloads. The message workload includes spawning,
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
counters cover the main benchmark process; message and prime-job workers contribute to
elapsed time but their counters are not included. Cell GCs count linglang's managed
collector, not BEAM's heap collector. GC stress is disabled.

The Erlang references are comparison implementations, not universal performance
limits. The pointer reference threads a value explicitly instead of implementing
general pointer identity. The message reference uses native messages without
linglang's schema checks. The string reference uses OTP's `string:trim` and
`binary_to_integer`, while linglang has specialized ASCII trimming and bounded
integer parsing. Arithmetic wraps to signed 64 bits in every variant.

## Comparing four languages

In `-compare` mode, the seed, Erlang, Elixir, and any requested self-hosted variants run in the same Elixir-launched
OTP VM per workload. The shared measurement harness rotates their order. Go runs
separately immediately afterward, with `GOMAXPROCS=1`. Each Go sample uses a fresh
goroutine and forces GC before timing, just as the BEAM sample process does.
Both harnesses enforce a 30-second deadline per sample. All warmups, startup,
compilation, counter reads, and sample-process/goroutine creation are excluded
from elapsed time. Worker creation and joins inside the message and prime-job workloads are included.

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

`prime_jobs` adapts the prime-job demo's computation and messaging into a bounded
application workload. Four monitored workers receive one job at a time; the
coordinator issues 32 jobs with limits cycling through 1,000, 2,000, 3,000, and
4,000. Every implementation uses trial division from 2 through the square root,
checks unique job IDs and counts (168/303/430/550), checks the total 11,608, and
waits for worker completion. All arithmetic stays within signed-64-bit bounds.
Go uses one buffered request channel per worker and a four-slot reply channel;
BEAM uses mailboxes. File I/O, fault injection, and supervisor restart behavior
are covered by demo/release tests rather than this benchmark. Worker managed
allocations/reductions are excluded from BEAM counters, so zero main-process
cells do not mean this workload allocates nothing.

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

### Native mixed-value loops — 2026-10-11

The bootstrap now lowers eligible mixed-value loop functions to native tail-call
edges. Ordinary bindings, including loop counters, travel as native values;
addressed locals retain stable cells. One managed scope owns the call, and each
block replaces its snapshot of live roots before its safepoint. Expression
temporaries remain rooted until the next block replaces that snapshot. Return
handoffs and scope exits cannot collect; exceptions unwind the function scope.
Addressed for-loop initializers, range, switch, and field/indirect assignment
targets retain the existing lowering. Straight-line and leaf paths are unchanged.
The independent seed already supports broader native control flow.

A paired run compared the previous leaf-scope archive with verified C, using
unchanged frozen inputs, three warmups, and 21 samples for each of nine workloads
and eight variants. All 1,512 executions passed result and cleanup checks. Only
the optimized `pointers`, `maps`, `messages`, and `prime_jobs` modules changed;
the other five optimized modules and all nine reference-cell modules are
byte-identical after normalizing module names. Timing variation in unchanged
programs is not attributed to this compiler change.

Median elapsed milliseconds:

| Workload | Self-hosted before | Self-hosted after | Seed | Erlang | Elixir | Go |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| arithmetic | 5.694 | 5.704 | 5.825 | 4.638 | 4.516 | 0.025 |
| calls | 11.877 | 11.931 | 5.524 | 4.331 | 4.462 | 0.025 |
| structs | 130.900 | 130.341 | 9.249 | 8.075 | 7.866 | 0.025 |
| pointers | 93.164 | 51.765 | 80.784 | 5.133 | 5.406 | 0.025 |
| lists | 27.962 | 27.621 | 1.527 | 1.028 | 1.034 | 0.143 |
| maps | 38.770 | 12.746 | 4.708 | 3.745 | 3.637 | 0.362 |
| strings | 94.095 | 93.635 | 6.711 | 48.241 | 48.088 | 0.815 |
| messages | 62.527 | 31.934 | 27.515 | 17.216 | 17.140 | 1.636 |
| prime_jobs | 131.300 | 85.496 | 72.644 | 3.087 | 3.038 | 0.355 |

Pointers improved 1.80× (44.4% less elapsed time), maps 3.04×, messages 1.96×,
and the bounded prime-worker demo 1.54×. Corresponding p95 times fell
97.343 → 56.632, 39.802 → 13.560, 69.789 → 34.911, and 146.781 → 92.762 ms.
The pointer case now outperforms the seed output on this host, while its managed
pointer semantics still differ from the native Erlang/Elixir value-threading and
Go implementations; the comparison limits above still apply.

Pointer allocations and peak live cells fall from two to one: the addressed
`Counter` remains, while the loop-counter cell disappears. Main-process managed
allocations fall 10,004 → 0 in both maps and messages. The prime demo's reported
main-process allocations remain 82; its workers gain native control flow while
the main function retains the fallback because it contains range. These counters
do not measure total BEAM allocation or sum worker-process managed allocations.

Separate pointer traces confirm the mechanism; instrumented timings are excluded
from the latency comparison:

| Runtime operation | Before | After |
| --- | ---: | ---: |
| Root scope (`scope/1`) | 500,007 | 1 |
| Root snapshot (`roots/1`) | 0 | 300,005 |
| Temporary root (`keep/1`) | 200,002 | 100,001 |
| Safepoint (`safepoint/0`) | 200,004 | 300,005 |
| Runtime state lookup (`state/2`) | 1,800,044 | 1,300,033 |
| Cell read (`read/1`) | 300,002 | 100,001 |
| Cell write (`write/2`) | 200,000 | 100,000 |
| Managed cell creation (`new_cell/2`) | 2 | 1 |

Both pointer variants perform one final collection and finish with zero live
cells/root frames/root entries. The new path retains a safepoint at every block,
including loop back edges; it wins despite more safepoints because repeated
scopes and counter-cell operations are gone. State lookups, safepoints, and root
snapshots now lead the pointer trace. Native struct-field updates are the next
candidate for the largest remaining absolute gap to the seed in this suite;
range lowering remains relevant to lists and strings. Either extension needs
its own profile, ownership argument, and forced-GC evidence.

Host: Apple M4 Pro, macOS 27.0.1 ARM64, OTP 29 / ERTS 17.1 JIT,
Elixir 1.20.4, Go 1.27.1; one BEAM scheduler and `GOMAXPROCS=1`.
Report timestamp: `2026-10-11T14:15:14Z`. No repository builds, tests, or profilers
ran concurrently with measurement. These are local results, not a compatibility
or performance claim for other OS/OTP configurations.

Verification passed the full differential suite across both emitter builds and
generated lowerings, the 26-test emitter suite under forced GC, and focused mixed
loop regressions. Cases cover boxed-local identity, interior pointers, cycles,
pointer-bearing collections, rebinding and parallel results, temporary roots
across collecting calls, escaping loop-body locals, returned parameter addresses,
break/continue, nil failures, and panic cleanup. A direct unrooted entry probe
runs 1,000 allocating iterations under forced GC and requires peak live cells
below 32 plus zero cells/roots after cleanup. Unnamed and blank parameters retain
the same lowering-time type validation as the fallback.

The Go-free proof passed all 85 acceptance executions with identical B/C emission:
2,014,355 bytes, SHA-256
`5a165d5a1b6b14071aa19aedae966267a02a3294a255c019801fe5d2d95f2c05`.
The full `TestSelfHostedFeatures` suite passed against published C under an
OTP-only PATH, including developer tools, discovery, releases, structured CLI,
cancellation/test runner, modules, multiple results, and typed I/O.

Archive SHA-256:

- Before: `21aca722eb6ca4fee797586af5f2b73577a288e251e459f02d377f62eb9a37a1`
- After: `fd6147bde6b79166c55f0e689d67a3685301b2b04e5985c36fea602dd6d647aa`

```sh
_build/leaf-scopes/compiler-verified build \
  -o _build/mixed-flow/updated-seed bootstrap/emitter
escript tools/bootstrap.escript --seed _build/mixed-flow/updated-seed \
  --output _build/mixed-flow/compiler-verified --timeout 1200
go run ./cmd/bench -compare \
  -compiler before=_build/leaf-scopes/compiler-verified \
  -compiler after=_build/mixed-flow/compiler-verified \
  -samples 21 -warmup 3 -build-dir _build/mixed-flow \
  -out _build/mixed-flow/application-comparison-next.json
```

Evidence is retained in `_build/mixed-flow/` (ignored by Git):
`application-comparison.json`, `verification.txt`, `verify-report.py`,
`proof.log`, `focused-tests.log`, `differential-tests.log`,
`emitter-tests-final.log`, `verified-features.log`, and
`pointers-{before,after}-profile.json` with `profile-workers.escript`.
All archive/input/generated-source hashes and 72 groups of 21 samples were
rechecked. Generated-source/measurement fingerprint:
`6a5930386f090bfa7977a37749d8b657ee33aacb6a0b18806e7c51887c69587e`.

### Noncollecting leaf scopes — 2026-10-11

The bootstrap now omits managed scopes, temporary keeps, and safepoints from
helpers whose complete lowering cannot allocate or collect managed cells. Named
parameters and locals must already use native bindings; runtime values are
limited to primitives, pointers, nil, and void. Loops, switches, address-taking,
user calls, I/O, process operations, and unknown calls retain the rooted path.
A small whitelist of noncollecting intrinsics is resolved by binding identity,
including in unreachable syntax. Pointer identity, evaluation order, and failure
behavior are unchanged. BEAM term allocation/GC is separate from managed-cell
collection. The seed already has its own broader leaf optimization.

The paired run compared the previous pointer-parameter archive with verified C,
using unchanged frozen inputs, three warmups, and 21 samples for each of nine
workloads and eight variants. All 1,512 measured executions passed result and
cleanup checks. Only the optimized `pointers` module changed; the other eight
optimized modules and all nine reference-cell modules are byte-identical after
normalizing module names. Timing differences in those unchanged programs are
measurement variation, not gains or regressions attributed to this change.

Median elapsed milliseconds:

| Workload | Self-hosted before | Self-hosted after | Seed | Erlang | Elixir | Go |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| arithmetic | 5.550 | 5.670 | 5.741 | 4.547 | 4.528 | 0.055 |
| calls | 12.574 | 12.549 | 5.750 | 4.406 | 4.553 | 0.026 |
| structs | 132.788 | 132.715 | 9.334 | 8.043 | 7.931 | 0.026 |
| pointers | 130.611 | 91.503 | 80.795 | 5.140 | 5.517 | 0.025 |
| lists | 28.292 | 28.390 | 1.545 | 1.051 | 1.049 | 0.140 |
| maps | 39.151 | 39.117 | 4.753 | 3.741 | 3.664 | 0.370 |
| strings | 94.614 | 96.636 | 6.918 | 48.960 | 48.945 | 0.799 |
| messages | 63.350 | 63.897 | 27.996 | 17.324 | 17.219 | 1.598 |
| prime_jobs | 135.649 | 140.630 | 77.168 | 3.123 | 3.153 | 0.353 |

Pointers improved 1.43× (29.9% less elapsed time); p95 fell from 146.103 to
99.661 ms. Managed allocations and peak live cells remain two, with one final
collection. The change removes root-management work around the 100,000
`increment` calls. Native Erlang/Elixir use value-threading and Go uses a different
runtime, so their pointer results still have the comparison limits described above.

Separate OTP call-time traces confirm the mechanism; instrumented timings are
excluded from the latency comparison:

| Runtime operation | Before | After |
| --- | ---: | ---: |
| Root scope (`scope/1`) | 800,007 | 500,007 |
| Temporary root (`keep/1`) | 400,002 | 200,002 |
| Safepoint (`safepoint/0`) | 300,004 | 200,004 |
| Runtime state lookup (`state/2`) | 2,900,044 | 1,800,044 |
| Cell read (`read/1`) | 300,002 | 300,002 |
| Cell write (`write/2`) | 200,000 | 200,000 |
| Managed cell creation (`new_cell/2`) | 2 | 2 |

Scopes and runtime-state lookups still lead the remaining trace. The next measured
target is native loop/control-flow lowering for mixed-value callers, preserving
roots across collecting calls and back-edge safepoints. That can also remove the
remaining loop-counter cell and its reads; this leaf change does not widen that
control-flow proof or infer safety transitively through user calls.

Host: Apple M4 Pro, macOS 27.0.1 ARM64, OTP 29 / ERTS 17.1 JIT,
Elixir 1.20.4, Go 1.27.1; one BEAM scheduler and `GOMAXPROCS=1`.
Report timestamp: `2026-10-11T13:38:30Z`. No repository builds, tests, or profilers
ran concurrently with measurement. These are local results; compatibility and
performance on other OS/OTP configurations remain unverified by this run.

Verification passed the full differential suite, focused unrooted-entry probes,
and emitter shape tests. Coverage includes aliases, interior writes, parallel
assignment, branches, early return, overflow, panic cleanup, and allocating
callees. One direct Erlang probe passes two pointers without caller roots under
GC stress; the leaf must not collect and the reference helper must root both.
Another calls an allocating user-defined `len` with an otherwise unrooted pointer,
ensuring that shadowing cannot bypass parameter rooting. Both require zero live
cells/root frames/root entries after cleanup.

The Go-free proof passed all 82 acceptance executions with identical B/C emission:
1,966,669 bytes, SHA-256
`95f0c4a1c09225db6b6be21de6f731d9c1e66aea8279e502ea74e721e3c6c541`.
The full `TestSelfHostedFeatures` suite passed against published C under an
OTP-only PATH, including developer tools, discovery, releases, structured CLI,
cancellation/test runner, modules, multiple results, and typed I/O.

Archive SHA-256:

- Before: `8fae7d1fb261e3c2273a7420243314eca311f12de6003f76f150b3123834dbac`
- After: `21aca722eb6ca4fee797586af5f2b73577a288e251e459f02d377f62eb9a37a1`

```sh
_build/pointer-parameters/compiler-verified build \
  -o _build/leaf-scopes/updated-seed bootstrap/emitter
escript tools/bootstrap.escript --seed _build/leaf-scopes/updated-seed \
  --output _build/leaf-scopes/compiler-verified --timeout 1200
go run ./cmd/bench -compare \
  -compiler before=_build/pointer-parameters/compiler-verified \
  -compiler after=_build/leaf-scopes/compiler-verified \
  -samples 21 -warmup 3 -build-dir _build/leaf-scopes \
  -out _build/leaf-scopes/application-comparison-next.json
```

Evidence is retained in `_build/leaf-scopes/` (ignored by Git):
`application-comparison.json`, `verification.txt`, `verify-report.py`,
`proof.log`, `shadow-entry-tests.log`, `differential-tests.log`,
`verified-features.log`, and `pointers-{before,after}-profile.json` with
`profile-workers.escript`. All archive/input/generated-source hashes and 72
groups of 21 samples were rechecked. Generated-source/measurement fingerprint:
`34fcf5c6a70029678424ef9ea6041c779a7eb0be4a528bac160199ac1439a6a8`.

### Rooted native pointer parameters — 2026-10-11

The bootstrap now keeps unchanged, unaddressed pointer parameters as native
values, explicitly rooted in the existing function scope before body safepoints.
It removes the parameter storage cell while preserving pointee identity and
mutation. Rebinding (including parallel/short/range assignment) or address-taking
anywhere in the function retains the cell path. The address scan is conservative
for interior-address expressions. Pointer locals and function scopes are unchanged;
the seed already has broader native binding support.

The paired run compared the prior verified native-flow archive with the new
verified C, using unchanged frozen inputs, three warmups, and 21 samples for each
of nine workloads and eight variants. All 1,512 measured executions passed.
Only the optimized `pointers` module changed; the other eight optimized modules
and all nine reference-cell modules are byte-identical after normalizing module
names. Small timing differences in those unchanged programs are not attributed
to this compiler change.

Median elapsed milliseconds:

| Workload | Self-hosted before | Self-hosted after | Seed | Erlang | Elixir | Go |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| arithmetic | 5.612 | 5.607 | 5.677 | 4.515 | 4.552 | 0.025 |
| calls | 11.976 | 11.889 | 5.534 | 4.321 | 4.333 | 0.025 |
| structs | 127.050 | 124.646 | 9.131 | 7.850 | 7.751 | 0.025 |
| pointers | 186.274 | 127.342 | 78.098 | 4.912 | 5.160 | 0.025 |
| lists | 27.426 | 27.298 | 1.417 | 0.992 | 0.996 | 0.134 |
| maps | 38.356 | 38.628 | 4.626 | 3.771 | 3.678 | 0.355 |
| strings | 90.451 | 90.004 | 6.651 | 45.519 | 45.081 | 0.823 |
| messages | 60.401 | 60.821 | 26.537 | 16.594 | 16.576 | 1.550 |
| prime_jobs | 126.556 | 127.243 | 69.312 | 2.747 | 2.835 | 0.355 |

Pointers improved 1.46× (31.6% less elapsed time); p95 fell from 192.428 to
129.818 ms. Each execution now allocates two managed cells, down from 100,002:
only the pointee and the fallback loop counter remain. Peak live cells fell
259 → 2 and collections 391 → 1. Result checks and final managed cleanup passed
for every sample. This does not make the pointer workload equivalent to the
native Erlang/Elixir value-threading or Go implementations; their differences
remain as described above.

Separate OTP call-time traces confirm the mechanism:

| Runtime operation | Before | After |
| --- | ---: | ---: |
| Managed cell creation (`new_cell/2`) | 100,002 | 2 |
| Cell read (`read/1`) | 400,002 | 300,002 |
| Cell write (`write/2`) | 200,000 | 200,000 |
| Root scope (`scope/1`) | 800,007 | 800,007 |
| Runtime state lookup (`state/2`) | 3,501,604 | 2,900,044 |

The 100,000 `increment` parameter cells and their reads are gone. Scope and
runtime-state operations lead the remaining trace. The next measured target is
redundant scopes/safepoints in proven nonallocating leaf helpers, followed by
native control flow for mixed-value functions. Any scope removal needs an
explicit allocation/collection argument and independent forced-GC tests; these
parameter roots must not simply be deleted. Instrumented timings are excluded
from the latency comparison.

Host: Apple M4 Pro, macOS 27.0.1 ARM64, OTP 29 / ERTS 17.1 JIT,
Elixir 1.20.4, Go 1.27.1; one BEAM scheduler and `GOMAXPROCS=1`.
Report timestamp: `2026-10-11T13:12:39Z`. No repository builds, tests, or profilers
ran concurrently with measurement. These are local results; other OS/OTP
configurations still require compatibility and performance evidence.

Verification passed the focused pointer fixtures and full differential suite
with both emitter builds and generated lowerings. A direct Erlang entry probe
passes a pointer with no caller root and forces collection inside the generated
callee, then checks zero live cells/root frames/root entries after return.
Language cases cover aliases, cycles, interior mutation, nil, recursion,
shadowing, rebinding, escaped parameter addresses, and failure cleanup.
The Go-free proof passed all 79 acceptance executions with identical B/C
emission: 1,951,513 bytes, SHA-256
`3b5ae5944f6a6b1168c59dbba44c3a0c8138afa972afa6068016c119b7833bf4`.
The full `TestSelfHostedFeatures` suite also passed against published C under an
OTP-only PATH, including developer tools, discovery, releases, structured CLI,
cancellation/test runner, modules, multiple results, and typed I/O.

Archive SHA-256:

- Before: `84f8a587c2196bab3f08491407b6f979b11f0babec875e4c5905e0d3cdce8749`
- After: `8fae7d1fb261e3c2273a7420243314eca311f12de6003f76f150b3123834dbac`

```sh
_build/native-flow/compiler-verified build \
  -o _build/pointer-parameters/updated-seed bootstrap/emitter
escript tools/bootstrap.escript --seed _build/pointer-parameters/updated-seed \
  --output _build/pointer-parameters/compiler-verified --timeout 1200
go run ./cmd/bench -compare \
  -compiler before=_build/native-flow/compiler-verified \
  -compiler after=_build/pointer-parameters/compiler-verified \
  -samples 21 -warmup 3 -build-dir _build/pointer-parameters \
  -out _build/pointer-parameters/application-comparison-next.json
```

Evidence is retained in `_build/pointer-parameters/` (ignored by Git):
`application-comparison.json`, `verification.txt`, `verify-report.py`,
`proof.log`, `focused-tests.log`, `entry-root-tests.log`,
`differential-tests.log`, `verified-features.log`, and
`pointers-{before,after}-profile.json` with `profile-workers.escript`.
All archive/input/generated-source hashes and 72 groups of 21 samples were
rechecked. Generated-source/measurement fingerprint:
`c99c831a3504cdd6d2b9b092bb3e9097e21d540f54e580d2e63c33a32c5539bd`.

### Self-hosted native control flow — 2026-10-11

The bootstrap now lowers primitive-only functions to native tail-call control
flow with live values passed between blocks. This removes managed local cells,
reads, root scopes, and break/continue/return throws from eligible ordinary loops
and branches. Managed values, address-taking, range, and switch retain the
existing lowering. Calls that may allocate keep safepoints; source semantics are
unchanged. The independent Go seed already supports broader native lowering.

A paired run used the prior verified archive and the new verified generation C,
the same frozen workload bytes, three warmups, and 21 samples per variant. All
1,512 measured runs passed (nine workloads × eight variants). No repository
builds, tests, or profilers ran during measurement. Host: Apple M4 Pro,
macOS 27.0.1 ARM64, OTP 29 / ERTS 17.1 JIT, Elixir 1.20.4, Go 1.27.1;
one BEAM scheduler and `GOMAXPROCS=1`. Report timestamp: `2026-10-11T12:48:58Z`.

Median elapsed milliseconds:

| Workload | Self-hosted before | Self-hosted after | Seed | Erlang | Elixir | Go |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| arithmetic | 94.296 | 5.870 | 5.799 | 4.677 | 4.606 | 0.025 |
| calls | 130.774 | 12.402 | 5.744 | 4.390 | 4.464 | 0.026 |
| structs | 128.661 | 126.731 | 9.184 | 8.048 | 7.900 | 0.025 |
| pointers | 187.783 | 186.983 | 78.471 | 5.054 | 5.321 | 0.026 |
| lists | 27.676 | 27.353 | 1.541 | 1.059 | 1.066 | 0.141 |
| maps | 38.229 | 37.994 | 4.600 | 3.684 | 3.608 | 0.364 |
| strings | 90.811 | 90.900 | 6.585 | 46.775 | 46.315 | 0.771 |
| messages | 61.678 | 61.798 | 26.874 | 16.580 | 16.602 | 1.574 |
| prime_jobs | 693.924 | 126.737 | 69.289 | 2.872 | 2.751 | 0.344 |

Arithmetic improved 16.06×, calls 10.54×, and the worker demo 5.48×. Their p95s
changed from 100.997 → 6.397 ms, 141.092 → 13.725 ms, and 713.568 → 132.471 ms.
Arithmetic and calls now allocate zero managed cells. Other workloads mostly
remain on the fallback and show no material change. These are local workload
results, not general language-speed claims; reference implementation differences
and native Go optimizations described above still apply.

Separate OTP `call_time` traces, including spawned workers, explain the prime
improvement. These counts are per complete demo execution:

| Runtime operation | Before | After |
| --- | ---: | ---: |
| Managed cell creation (`new_cell/2`) | 80,190 | 222 |
| Cell read (`read/1`) | 2,710,669 | 332,389 |
| Cell write (`write/2`) | 629,204 | 91,708 |
| Root scope (`scope/1`) | 3,796,612 | 424,244 |
| Runtime state lookup (`state/2`) | 13,451,924 | 1,458,996 |

That removes 99.7% of managed allocations and 87.7% of reads from this workload.
The ordinary benchmark counter remains 82 coordinator allocations in both
versions because it excludes workers. Instrumented timings are excluded from
the latency comparison. Primitive arithmetic dispatch now leads the remaining
prime trace; mixed-value worker control flow still incurs scopes and cell reads.

The largest remaining self-hosted workload time is pointers at 186.983 ms, with
100,002 managed allocations versus the seed's one. Its `increment(*Counter)`
helper still allocates a parameter cell on each of 100,000 calls. The next target
is native bindings and appropriate roots in functions containing managed values,
starting with pointer parameters whose own binding is never addressed. Preserve
pointer identity, interior mutation, failure cleanup, and forced-GC behavior
before extending the optimization boundary.

The full differential execution suite passed against both emitter builds and
both generated lowerings for local fixtures. New cases cover nested control
flow, assignment/evaluation order, allocating callees, and 220 live locals.
The Go-free proof passed all 76 acceptance executions, with identical B/C
emission: 1,984,535 bytes, SHA-256
`881affedad798bb967ce550dc436ca135ad840589c05575bafd901ccdfd8cdd1`.
Published C archive SHA-256:
`84f8a587c2196bab3f08491407b6f979b11f0babec875e4c5905e0d3cdce8749`.
CLI cancellation/test-runner, structured tooling, developer tools, discovery,
and release checks also passed against this C under an OTP-only PATH. The first
proof attempt exposed an obsolete loop-helper shape assertion; its corrected
form checks native optimized edges and reference-cell loop helpers separately.
No compiler implementation changed between that attempt and the passing rerun.

Reproduce the rebuild and paired benchmark from the preserved prior archive:

```sh
_build/selfhost-release/compiler-verified build \
  -o _build/native-flow/updated-seed bootstrap/emitter
escript tools/bootstrap.escript --seed _build/native-flow/updated-seed \
  --output _build/native-flow/compiler-verified --timeout 1200
go run ./cmd/bench -compare \
  -compiler before=_build/selfhost-release/compiler-verified \
  -compiler after=_build/native-flow/compiler-verified \
  -samples 21 -warmup 3 -build-dir _build/native-flow \
  -out _build/native-flow/application-comparison-next.json
```

Evidence is retained in `_build/native-flow/` (ignored by Git):
`application-comparison.json`, `verification.txt`, `proof.log`,
`proof-old-assertion.log`, `differential-tests.log`, `verified-features.log`,
`prime-before-profile.json`, `prime-after-profile.json`, `pointers-after-profile.json`, and the matching
`profile-workers.escript`. `verify-report.py` rechecks all artifact hashes,
unchanged benchmark inputs, exactly 21 samples in each of 72 groups, and identical
before/after reference-cell Erlang modules after normalizing module names.
Generated-source/measurement fingerprint:
`bf49e535ee5404edfbe4d70ad5a2a39e3bc33133cedc07208f6cf42f246d42a3`.
Cross-platform and other-OTP evidence is still needed before a stable release.

### Prior self-hosted cross-language baseline and worker demo — 2026-10-11

The combined `-compare -compiler` run includes nine workloads, six variants,
three warmups and 21 measured samples per variant (1,134 measured runs). Every
answer/root-cleanup check passed. Builds and tests were stopped during timing.
Host: Apple M4 Pro, macOS 27.0.1 ARM64, OTP 29 / ERTS 17.1 JIT, Elixir 1.20.4,
Go 1.27.1. BEAM uses one scheduler; Go uses `GOMAXPROCS=1`.

Median elapsed milliseconds (reference-cell results and p95s are in the JSON):

| Workload | Self-hosted optimized | Seed optimized | Erlang | Elixir | Go |
| --- | ---: | ---: | ---: | ---: | ---: |
| arithmetic | 93.061 | 5.321 | 4.207 | 4.241 | 0.048 |
| calls | 115.841 | 5.157 | 4.056 | 4.070 | 0.026 |
| structs | 128.004 | 8.825 | 7.735 | 7.430 | 0.024 |
| pointers | 185.467 | 79.642 | 4.783 | 5.077 | 0.025 |
| lists | 26.320 | 1.437 | 0.955 | 0.976 | 0.145 |
| maps | 37.433 | 4.285 | 3.461 | 3.439 | 0.353 |
| strings | 88.733 | 6.473 | 44.789 | 43.894 | 0.761 |
| messages | 59.758 | 26.077 | 16.556 | 16.559 | 1.548 |
| prime_jobs | 673.839 | 67.989 | 2.789 | 2.687 | 0.350 |

This is a baseline, not a before/after speedup claim. The original eight workload
sources are unchanged; `prime_jobs` adds a bounded application-style computation
and messaging workload. Reference implementations retain the semantic differences
and counter-scope limits described above. Go's tiny arithmetic/call timings also
reflect normal native inlining and compiler optimization.

The compiler was rebuilt without Go using `--upgrade-seed`, including the new
`releaseProgram` native bridge. B/C emission matched at 1,904,801 bytes,
SHA-256 `e2c6c360ffa794534b6925927e7c057a2c7c49b3a0f075e10154a0aac30cd78e`.
All 73 acceptance executions passed; release/discovery/developer-tool integration
checks then passed against the published C under an OTP-only PATH. Archive SHA-256:
`f2c6fc50dc53fd566a71da152062ed030045f1d9fec4ff4b4722ddb5b488a116`.

```sh
go run ./cmd/bench -compare \
  -compiler selfhost=_build/selfhost-release/compiler-verified \
  -samples 21 -warmup 3 -build-dir _build/selfhost-release \
  -out _build/selfhost-release/application-comparison-next.json
```

Recorded report: `_build/selfhost-release/application-comparison.json`.
Its compiler, input, and generated-source hashes were rechecked against retained
files, and all 54 workload/variant groups contain exactly 21 measured samples.
Generated-source/measurement fingerprint:
`1381b793a99abb118670aa1317f37a4a163384e24e8c818c27e1b325ab509869`.
Proof, integration logs, and retained artifacts are alongside it (ignored by Git).
These results cover this local host; Linux and other OTP versions need CI evidence.

The worker demo is the largest absolute gap: 673.839 ms for self-hosted output
versus 67.989 ms for the seed. A separate OTP `call_time` trace of the sample and
its spawned workers recorded 80,190 managed-cell creations, 2,710,669 reads,
629,204 writes, 3,796,612 scope calls, and 13,451,924 runtime-state lookups.
The ordinary harness sees only 82 allocations in the coordinator. Generated
`prime` code still stores its divisor in a managed cell and wraps loop operations
in runtime scopes. This identified native mutable loop/control
flow in the bootstrap emitter as the next optimization target, preserving early
returns and observable pointer identity. The independent seed remains much faster on that path.

Trace evidence: `prime-profile.json` and `profile-workers.escript` in
`_build/selfhost-release/`. Instrumented timings are not benchmark latencies;
only the uninstrumented run above supplies elapsed-time comparisons.

### Self-hosted application baseline and iteration-cell reuse — 2026-10-11

The first archive application baseline used the verified compiler at the
`8ba8ad9` checkout, archive SHA-256
`12bd1d8d8b9ddb7e99291e4078bf07cbf894780705a3c9451a32becc3df7f45b`.
All eight workloads ran with both archive lowerings, both seed lowerings, and
Erlang, for 21 samples and three warmups per variant. All 840 measured executions
passed. Unlike the earlier application results, this measures programs emitted
by an actual self-built compiler. The report is
`_build/selfhost-applications/baseline.json`, including archive, input, and generated
source hashes and a retained build directory.

The largest absolute self-hosted latency was the pointer workload, at 279.280 ms.
Its 200,002 managed allocations included a fresh loop-counter cell every
iteration. Arithmetic also allocated 100,002 cells despite never taking a local
address. A separate instrumented pointer profile found 200,002 `new_cell/2` calls,
900,008 `scope/1` calls, and 4,503,171 runtime state lookups. Instrumented call times
are not application latency measurements.

The bootstrap emitter now reuses for-initializer cells whose addresses are never
taken anywhere in the function. Addressed bindings still get fresh iteration
identity before post; mixed declarations clone only the addressed bindings.
The reference `--no-opt` lowering is unchanged. This reduces allocation and root
bookkeeping without introducing native mutable-loop lowering or removing GC
safepoints. The seed already has broader native control-flow lowering.

A fresh paired run interleaved verified before/after archives, both lowering
modes, both seed modes, and Erlang in one VM per workload. Apple M4 Pro, macOS
27.0.1 ARM64, OTP 29 / ERTS 17.1 JIT; one scheduler, 21 samples and three warmups
per variant, with no concurrent builds/tests. Both compilers used the same
snapshot paths and bytes and the same checkout runtime. Each archive emission
was repeated to check determinism. Median milliseconds and managed allocations:

| Workload | Before | After | Less elapsed time | Managed cells before → after |
| --- | ---: | ---: | ---: | ---: |
| arithmetic | 191.613 | 93.791 | 51.1% | 100,002 → 2 |
| calls | 221.500 | 122.033 | 44.9% | 100,002 → 2 |
| structs | 234.781 | 136.659 | 41.8% | 100,002 → 2 |
| pointers | 299.001 | 193.000 | 35.5% | 200,002 → 100,002 |
| lists | 46.453 | 26.937 | 42.0% | 20,003 → 3 |
| maps | 59.191 | 39.873 | 32.6% | 30,004 → 10,004 |
| strings | 104.434 | 93.559 | 10.4% | 50,002 → 40,002 |
| messages | 81.544 | 60.908 | 25.3% | 20,004 → 10,004 |

All 1,176 measured executions passed result and final managed-root/cell cleanup
checks. Every before/after cell-lowered Erlang program was byte-identical after
normalizing only its module name. Use this paired comparison for attribution;
the earlier baseline was a separate run under different machine conditions.
These are fixed-size, single-scheduler microbenchmarks, not general language
speed or service-throughput claims. Managed allocations exclude ordinary BEAM
heap allocation; message-worker counters remain outside the recorded main-process
counters.

The after archive SHA-256 is
`95cba9ea7cf02847805fc4d6d531295bae0e72664c0bbc7cca7bae0829e48f91`.
Its Go-free proof emitted byte-identical B/C source of 1,860,401 bytes, SHA-256
`3e37d0422b2ab8f50c476884fc77439968fdfd0c3bff4d2354c5bdab5ce46c94`,
and passed all 73 C acceptance executions, including both language/concurrency
lowerings with forced GC. The independent seed/emitter differential tests also
passed, including escaped and interior loop addresses, mixed initializers,
shadowing, pointer-bearing post assignments, continue, returns, failure cleanup,
and signed overflow.

Reproduce the paired run from this checkout with the retained verified archives:

```sh
go run ./cmd/bench \
  -compiler before=bin/linglang-selfhost \
  -compiler after=_build/selfhost-applications/compiler-after \
  -samples 21 -warmup 3 -build-dir _build/selfhost-applications \
  -out _build/selfhost-applications/comparison-next.json
```

Reports, archived inputs, emitted Erlang, measurement scripts, and proof logs
are under `_build/selfhost-applications/` (ignored by Git): `comparison.json`,
`comparison.log`, `pointer-profile.json`, `pointer-profile-after.json`, and
`selfhost-proof.log`. Each report's compiler snapshot paths preserve the exact
archives even after the original paths are upgraded; use those snapshots for
later replay. Remaining pointer-helper parameter cells and mutable control-flow
cells are candidates for the next measured optimization.

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
