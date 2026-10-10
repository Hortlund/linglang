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
go test ./cmd/linglang -run '^TestBootstrapSelfHostProof$' -count=1 -v -timeout=15m
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
