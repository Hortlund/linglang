# Contributing and verification

Use the [change map and tool protocol](agent-workflows.md) to locate the relevant
compiler stages. The root `AGENTS.md` provides the same starting points for coding
agents and human contributors.

Start from executable behavior. For a language change, specify scope, typing,
evaluation order, failure behavior, and ownership before changing lowering.
Test both compiler implementations and both optimization modes where applicable.
Use forced managed GC for code involving pointers, messages, or control flow.

From the repository root:

```sh
make sqlite
go test ./internal/... ./cmd/bench -count=1 -timeout=10m
go test ./cmd/linglang -run 'TestInit|TestBookExamples|TestWebCounter|TestLocalModules|TestBootstrapCLI|TestBootstrapEmitterPrograms' -count=1 -timeout=10m
go vet ./...
go build -o bin/linglang ./cmd/linglang
bin/linglang fmt --check bootstrap/emitter book/examples cmd/linglang/starter
mdbook build book
```

The book's examples are compiled and executed by `TestBookExamples`. Included
starter files are exercised by `TestInitProject`. Independent programs in
`book/examples/` are explicitly loaded one file at a time, not as one package.

## Build a self-hosted compiler

With an existing trusted compiler, the [Go-free workflow](self-hosting.md) refreshes
the runtime, checks B/C equality, and runs the Linglang suites before publishing:

```sh
escript tools/bootstrap.escript --seed bin/linglang-bootstrap --output bin/linglang-selfhost
```

Keep the following independent Go-seeded proof as additional differential coverage.

The fixed-point proof takes several minutes and requires Go and OTP during
setup. It gives the generated compiler an isolated OTP-only PATH:

```sh
LINGLANG_SELFHOST=1 \
LINGLANG_SELFHOST_OUT="$PWD/bin/linglang-bootstrap" \
go test ./cmd/linglang -run '^TestBootstrapSelfHostProof$' -count=1 -v -timeout=45m
```

The artifact is actual generation C, not a renamed Go executable. The command
must finish successfully before calling that artifact verified. It may write
the artifact before a later fixture fails, so retain the test result alongside
the executable. Keep source and embedded runtime together when refreshing tools.

For a faster development build of the Linglang compiler, use the seed:

```sh
bin/linglang pack -o bin/linglang-emitter bootstrap/emitter
bin/linglang-emitter check examples/store.lang
```

This is useful but is not a replacement for the fixed-point proof. A runtime
change requires repackaging tools because executables embed their runtime BEAM
modules. Ignored `bin/` and `_build/` files are not source-of-truth artifacts.

## Release criteria

Run the repository's compatibility workflow for the supported OS/OTP matrix
before declaring a release supported there. Local OTP 29 results alone do not
establish OTP 27/28 or another OS. Keep negative tests for unsupported constructs
and ensure the book says when a feature exists only in the seed.

The [roadmap](status.md) is deliberately separate from the language contract.
Change it as application needs emerge; do not document planned syntax as if it
already compiles.

The self-host proof also runs generation C's test runner, package-linking
fixtures, typed I/O, and the browser/SQLite application (including persistence
across a restart). SQLite checks require a C compiler and SQLite development
headers. Network tests bind ephemeral loopback ports and require no internet.

Structured output is exercised by `TestStructuredRunnerCLI` and the
`TestBootstrapCLI/.*/structured_tooling` subtests, including both lowering modes,
quoted/Unicode source paths, imported errors, test deadlines, and child stdout
redirection. The generation-C proof repeats the same protocol fixtures against
the self-built compiler. JSON test cancellation also covers CPU loops and blocked
stdin under interrupt, terminate, and kill signals on Linux/macOS.

`TestGoFreeBootstrapTool` tests orchestration with a small compiler double under
an OTP-only PATH, including fixed-point mismatch, failing suites, deadlines,
source protection, and preserving an old artifact on failure. It does not replace
running the real Go-free fixed point. `tests/concurrency` supplies Linglang tests
for bounded job dispatch, concurrent server updates, restart recovery, and failure
isolation; the Go-free proof runs them with both lowerings and forced collection.
`TestBootstrapSchedulerDefaults` verifies that application packaging and child
execution honor an operator's scheduler configuration even when their compiler
parent runs with one scheduler.

`TestArchiveProfiler` exercises the OTP-only archive profiler with a small OTP
fixture: compiler arguments, decoded function names and call counts, allocation
cleanup, failures, and deadlines. It runs with `go test ./cmd/bench`; the profiler
itself requires no Go. Correctness tests have no timing thresholds. Follow the
[profiling workflow](self-hosting.md#profile-the-compiler-without-go) and keep heavy
tests separate from uninstrumented performance measurements.

Application benchmarks also accept repeated `-compiler label=archive` arguments.
Each archive emits every workload with optimized and reference-cell lowering;
the harness checks deterministic emission and rotates all variants in one VM.
Archive protocol/failure tests run with `go test ./cmd/bench`. To exercise all
workloads with a real verified compiler, set `LINGLANG_BENCH_COMPILER` to its
absolute path and run `go test ./cmd/bench -run '^TestSelfHostedApplicationWorkloads$'`.
See `benchmarks/README.md` for retained inputs, hashes, and timing boundaries.
`tests/language/iteration_test.lang` checks loop-cell reuse and observable
iteration identities under both lowerings and forced GC in the fixed-point proof.
`tests/language/control_flow_test.lang` covers native nested loops, early returns,
shadowing, parallel assignment/post statements, fresh zero values, integer
wrapping/shifts, and allocating callees with rooted callers. The `local_native_*`
differential fixtures run both emitter builds and both generated lowerings with
forced GC, including evaluation order and more than 200 live locals. The emitter
suite checks cell/read/scope elimination and conservative fallback boundaries.
`pointer_parameters_test.lang` checks rooted native pointer parameters, cycles,
interior mutation, nil values, recursion, shadowing, escaping parameter addresses,
and rebinding through assignment, short declaration, branches, and range. The
`local_pointer_parameter*` differential fixtures also check nil dereference and
panic cleanup under forced GC. Emitter shape tests require an explicit entry root
and retain storage cells for rebinding/address-taking, including unreachable code.
A direct Erlang entry probe passes an otherwise unrooted pointer into generated
code and forces collection in the callee, so caller argument roots cannot mask a
missing parameter root. It also requires zero live cells/roots after return.
`leaf_scopes_test.lang` and the `local_leaf_*` differential fixtures cover aliases,
interior writes, parallel destinations, branch scopes, early return, overflow,
allocating callees, shadowed intrinsic names, and panic cleanup. A direct leaf
entry probe supplies two pointers without caller roots under GC stress: leaves
must not collect, while reference helpers must root both arguments first. Another
unrooted entry probe calls an allocating user-defined `len`, requiring the caller
to retain its parameter root rather than treating the name as an intrinsic. Shape
tests check scope/keep/safepoint/cell elimination and conservative fallbacks.

`mixed_flow_test.lang` and the `local_mixed_*` differential fixtures check native
loop edges with stable addressed cells, interior pointers, pointer-bearing
collections, pointer rebinding, parallel results, nested collecting calls,
break/continue, returned parameter addresses, nil failures, and panic cleanup.
The unrooted entry probe additionally bounds peak live cells over 1,000 iterations,
so stale temporary roots cannot silently accumulate. Shape tests require one
function scope and only the addressed object's cell in a mixed caller, while
addressed loop initializers retain their original lowering.

Developer tools are tested by `TestBootstrapCLI/.*/developer_tools` against both
lowerings under an OTP-only PATH, and by the generation-C proof. Checks cover
embedded template parity, runnable generated projects, syntax-failure write safety,
check-only operation, recursive exclusions, symlinks, permissions, and formatting
idempotence across copies of the compiler source. `formatting_test.lang` contains
syntax-preservation fixtures. Keep the seed's starter files and the embedded
`project_templates.lang` bytes synchronized; the integration test detects drift.
The optional `--upgrade-seed` bootstrap uses `bootstrap/seed-upgrade/` only for the
intermediate compiler, never for the final fixed point.

`TestBootstrapCLI/.*/discovery` checks human/JSON descriptions, native signature
parity, transitive dependency closures, cross-driver lock verification, Unicode
paths/escapes, symlink/`..` input paths, invalid locks, read-only checks, and publication failures. The
actual-C feature test reuses the same checks. `discovery_test.lang` covers the
JSON reader and pure lock/catalogue behavior; filesystem tests exercise SHA-256
and exclusive publication in both lowerings under forced GC. Generic intrinsic
descriptions and parameter labels live in `native_catalog.lang`; ordinary types
are derived from checker registrations rather than a duplicated prelude.

Multiple-result regression programs live in `tests/language`. Run them with both
compilers and with `--no-opt --gc-stress` as well as optimized GC stress. The
bootstrap checker and emitter suites include invalid arity/type cases, and the
Go-seeded proof executes multiple-result programs through the actual compiler C.

Compiler distributions use the [release workflow](installation.md#maintainer-release-procedure).
`tools/release/test-install.sh` tests installation failures and upgrades quickly;
`tools/release/smoke.sh` checks a real packaged compiler without Go or `erlc` on
PATH. Packaging must follow a completed proof, not merely the appearance of its
output file.

`TestBootstrapCLI/.*/release` exercises self-hosted application releases with
both compiler and application lowerings. It checks relocated bundles with an
empty tool PATH, argument/worker behavior, panic exits, file I/O, license parity,
source/import alias protection, and publication failure. `TestSelfHostedFeatures/release`
repeats these checks against a verified C archive. `TestReleaseCLI` uses the same
successful bundle fixtures against the seed; both drivers share OTP packaging.
The older native catalogue needs `--upgrade-seed` when rebuilding across the
`releaseProgram` bridge transition. Starter documentation must remain byte-equal
to the embedded project templates.
