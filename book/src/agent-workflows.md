# Working with humans and agents

Linglang aims to make small, verifiable changes easy for both people and coding
agents. The useful properties are concrete: explicit imports, static types,
predictable evaluation, visible ownership boundaries, deterministic compiler
output, focused tests, and tools that report what actually failed. Familiar
imperative syntax helps a reader follow the same code an agent edits.

This does not introduce an AI runtime dependency or a second dialect. Tooling
should expose the same semantics and validation to editors, terminal users,
CI, and agents. No prompt execution, model calls, or autonomous permissions are
part of the language. Programs can use the normal process and I/O APIs.

## Discover before generating code

Both drivers expose a human-readable catalogue and the same versioned JSON shape:

```sh
linglang describe
linglang describe --json
linglang-bootstrap describe --json
```

The JSON object has `schemaVersion: 1`, `compiler: "go-seed"` or `"bootstrap"`, `stability`,
`commands`, `capabilities`, `nativeAPI`, and `notes`. Each command has `name`,
`usage`, and `summary`. Each capability has `name`, `status` (`supported`,
`limited`, or `unavailable`), and `notes`. Each native declaration has `name`,
`kind` (`function`, `record`, `collection`, or `opaque`), and `declaration`.

Native signatures and result-record fields are derived from the invoked
checker, sorted by name. The self-hosted catalogue uses checker metadata,
with explicit generic intrinsic descriptions checked against the seed in tests. Opaque handles and collections hide the Go types used
internally for bootstrapping. Generic parameter notation in this catalogue
describes built-ins; it does not enable user-defined generics. Operators and
language built-ins such as `len`, `print`, and `append` are covered by the
[API reference](builtins.md). This is a capability report, not a check that
OTP or the optional SQLite helper is installed.

## The edit/check/test loop

For application work, start with `linglang init app` or `linglang-bootstrap init app`. The generated project
includes a short `AGENTS.md` that any contributor can read. Within the project:

```sh
linglang check --json .
linglang check --json --tests .
linglang test --json --gc-stress .
linglang fmt --check .
```

The first check validates the program root; `--tests` also loads root test files,
permits a library package without `main`, and validates test signatures. Both
check modes run the real compilation and lowering path, discard the generated
Erlang. The Go CLI needs no OTP for these checks; the self-hosted driver itself
runs on OTP, without Go or an external `erlc` command. They do not execute code or assertions,
write build artifacts, or require tests to exist. Run `test` for behavior.
Dependencies' test files remain excluded. Explicit files load only that file.

The self-hosted driver also supports `fmt [--check] [paths...]` and `init [directory]`
without Go. Choose a formatter per project: the Linglang formatter preserves tokens
and uses its own layout, while the seed retains Go printer conventions. Do not
alternate their `--check` policies. See [developer tools](developer-tools.md).

For pointer, control-flow, or process changes, compare the normal backend with
`--no-opt`, and execute tests with `--gc-stress`. Review an updated dependency
before regenerating its lock; use `deps --check` in CI when a lock is present.

## Structured check protocol, version 1

`check --json` writes exactly one JSON object to stdout for a valid invocation.
Exit status is 0 for success and 1 for failed compilation or source loading.
A reported failure does not also print the same error to stderr. Invalid CLI
arguments and output-write failures remain ordinary errors on stderr; clients
must check both exit status and whether a report could be decoded.

```json
{
  "schemaVersion": 1,
  "command": "check",
  "compiler": "go-seed",
  "mode": "program",
  "ok": true,
  "sourceFiles": 1,
  "diagnostics": []
}
```

`compiler` identifies `go-seed` or `bootstrap`; both implement this schema.
`mode` is `program` or `tests`. `sourceFiles` counts selected root source files,
not imported files. Failed root discovery or reading returns 0. The current
compiler stops at its first error; an empty diagnostic array indicates success,
not a promise that warnings or additional errors were collected. Syntax,
typing, and backend restrictions use the same path as building the program.

Each diagnostic has `code`, `severity` (currently `error`), `message`, `location`,
and a `related` array. Codes identify categories; human wording may change.

| Code | Meaning |
| --- | --- |
| `LL1000` | Source input/read/discovery failure |
| `LL1001` | Syntax error |
| `LL1002` | Module loading, names, or import boundary error |
| `LL1003` | Type-checking error |
| `LL1004` | Language validation or lowering restriction |
| `LL1005` | Test discovery/setup failure (including an empty suite) |
| `LL1006` | Dependency lock differs from the current source closure |
| `LL1099` | Unclassified compiler failure |

The frontends may reject a construct at different stages, so the same invalid
program can have different diagnostic codes, wording, or first-error locations.
The codes identify the detecting stage/category, not a cross-compiler error ID.

`location` is either `null` or `{ "file": "main.lang", "offset": 27,
"line": 2, "column": 15 }`. Files use the compiler's physical input paths;
root paths may be relative, imported paths are generally absolute. Offset is
zero-based **bytes**; line and column are one-based, and column is **bytes**, not
Unicode characters or LSP UTF-16 units. Locations are points, not replacement
ranges. `//line` comments do not redirect them. Clients must convert encodings
before applying editor selections and re-check after edits change offsets.

Import-loading failures preserve a dependency's location when available;
`related` entries record `message` and `location` for the import sites, from
the immediate importer outward. Later type/lowering errors point directly into
the offending file and may have no import trail. I/O errors with no source
location use `null`; missing imported directories point at the import.

The machine-readable schema lives at `schemas/check-v1.schema.json` in the
repository. Consumers should reject unknown schema versions and tolerate new
fields. The schema version describes the output protocol, not language or
compiler compatibility. Read output from the child process pipe; do not redirect
reports into a `.lang` file.

## Structured test events, version 1

Both drivers support streaming test results:

```sh
linglang test --json --gc-stress .
linglang-bootstrap test --json --gc-stress .
linglang-bootstrap check --json --tests .
```

`test --json` writes **JSON Lines**, one complete JSON object per stdout line.
Every event contains `schemaVersion: 1`, `command: "test"`, `compiler` (`go-seed`
or `bootstrap`), and `event`. Events are emitted in this order:

| Event | Additional fields | Meaning |
| --- | --- | --- |
| `suite_start` | `total` | Compilation/discovery completed; selected tests are about to run |
| `test_start` | `name`, `location` | Begin one test attempt |
| `test_end` | `name`, `location`, `status`, `durationMillis`, `reason` | Complete that attempt; status is `pass`, `fail`, or `timeout` |
| `suite_end` | `total`, `passed`, `failed`, `ok` | Final counts; timeouts count as failed |
| `diagnostic` | `diagnostic` | Compilation, discovery, or pre-suite setup error, using the check diagnostic shape |

The normal sequence is a suite start, ordered test-start/test-end pairs, then
one suite end. Tests are sorted by name and run sequentially. A failure or timeout
does not skip later tests. Failures before the suite starts emit `diagnostic`
then `suite_end` with zero counts and `ok: false`; no tests were attempted.
Per-test launch/packaging failures produce `test_end` with `status: "fail"`.
A pass has an empty reason; reason wording and runtime stack traces are not a
stable API. Test locations use the same physical byte coordinates as checks.

All child stdout **and** stderr go to the parent's stderr in JSON mode, including
assertion messages, GC statistics, and direct writes to `/dev/stdout`. Output is
streamed without capturing unbounded buffers or embedding program bytes in JSON.
Consumers should drain both streams while the process runs. Stdin remains
inherited. Human-readable test mode keeps its existing output behavior.

`durationMillis` is nonnegative wall time measured with a monotonic clock between
test events. It includes per-test packaging in the bootstrap runner; the deadline
still begins after packaging and covers VM startup, execution, and shutdown.
Compilation/packaging have no new time bound. The bootstrap output redirection
uses `/bin/sh` on Linux/macOS, with a fixed script and program arguments passed
separately. The shell replaces itself with the child VM, retaining its lifetime
pipe. Compiler termination and test deadlines still stop that VM.

Exit status is 0 only when the suite succeeds, and 1 for a reported failure.
Signals, VM failure, or broken output pipes can interrupt a stream: clients must
require both a successful process exit and a successful `suite_end`. Never treat
missing events or a partial line as a pass. Invalid CLI arguments remain stderr
errors and may produce no events. The per-line schema is
`schemas/test-events-v1.schema.json`; it references the shared diagnostic schema.

## Where to make compiler changes

| Concern | Go seed / shared runtime | Linglang frontend |
| --- | --- | --- |
| Syntax and AST | Go parser plus restrictions in `internal/compiler/` | `bootstrap/lexer/`, `bootstrap/parser/` |
| Imports and scope | `internal/compiler/modules.go` | `bootstrap/emitter/modules.lang`, `bootstrap/resolver/` |
| Type rules and native API | `internal/compiler/compiler.go`, `processes.go`, `standard.go`, `maps.go` | `bootstrap/checker/`, especially `native.lang` |
| Lowering and evaluation | `internal/compiler/locals.go`, `lists.go`, `maps.go`, `switches.go`, `processes.go`, `servers.go` | `bootstrap/emitter/emit_*.lang`, `emitter.lang` |
| Runtime ownership and OTP | `internal/compiler/runtime.erl`, `supervision.erl`, `server.erl`, `io.erl` | Shared embedded runtime |
| Formatting and project creation | `cmd/linglang/format.go`, `init.go`, `starter/` | `bootstrap/emitter/formatting.lang`, `developer_tools.lang`, `project_templates.lang` |
| Discovery and dependency locks | `cmd/linglang/describe.go`, `deps.go`, `deps_decode.go` | `bootstrap/emitter/discovery.lang`, `native_catalog.lang`, `dependencies.lang`, `json.lang` |
| CLI and tests | `cmd/linglang/`; structured check/discovery in `check.go`, `describe.go` | `bootstrap/emitter/main.lang`, `testing.lang` |
| Diagnostic transport | `internal/compiler/diagnostics.go`; LSP adapter in `internal/lsp/` | `bootstrap/emitter/tooling.lang` result records and rendering |
| Contract and examples | `book/src/specification.md`, topic chapters, `book/examples/` | Shared documentation and differential fixtures |

Bootstrap stages currently share earlier source through symlinks. Follow the
link to its owning file and preserve that structure. A future move to compiled
module interfaces must preserve declaration identity, source locations, and
deterministic output. Large directory moves alone do not establish those
boundaries.

The structured diagnostic value is separate from how the CLI renders it. New
compiler checks should create typed errors at their source position and keep
that metadata through imports. Do not classify by message substrings. The LSP
still uses partial editor analysis; a clean editor view does not replace a full
check through lowering.

## Current boundary and next steps

Both drivers implement discovery, dependency reports, structured checks,
static test-package checks, and the version-1 test event stream. The independent
frontends still have different supported subsets and may report different
first errors. Package API inspection and richer multi-error diagnostics are
future work; they should preserve these source and output contracts.

See [contributing](contributing.md) for verification commands and the fixed-point
proof. A good handoff records changed behavior, checks actually run, and remaining
limitations rather than claiming success from compilation alone.

## Dependency report protocol, version 1

Both drivers accept `deps [--check] [--json] [file.lang|directory]`, defaulting to
`.`. With `--json`, a valid invocation writes one object to stdout and uses exit
0 for success or 1 for failure. Program code is never executed. Usage errors
and output failures go to stderr. Do not redirect output over source or lock files.

The report contains `schemaVersion: 1`, `command: "deps"`, `compiler`, `mode`
(`write` or `check`), `ok`, `lockFile`, `files`, and `diagnostics`. `lockFile` is an
absolute lexical path, or empty if discovery could not determine it. `files`
contains the observed, sorted dependency records (`path`, `sha256`), not the old
lock contents; a failed discovery may leave it empty or partial. `diagnostics`
uses the same shape as check diagnostics. `LL1006` means the valid lock differs
from the current closure; `LL1001` covers malformed/unsupported lock JSON,
`LL1000` filesystem failures, and `LL1002` linking failures. Source parse errors
retain their source location. Treat exit status and `ok` as authoritative.

The contracts live in `schemas/describe-v1.schema.json`, `deps-v1.schema.json`,
and `lock-v1.schema.json`. Lock files and reports have different schemas: only
the lock is written to `linglang.lock`. Agents should run `deps --check --json`
and review changes before invoking `deps --json` to update the reviewed snapshot.
