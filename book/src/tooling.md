# Testing and shipping

## Tests

Write parameterless, resultless `Test...` functions in `_test.lang` files. The
seed and self-hosted CLIs discover them and run each in a fresh BEAM VM. A failing assertion or
panic fails that test; later tests still run. A timeout also terminates the VM,
including its workers. Tests do not run the application's `main`.

```go
{{#include ../../cmd/linglang/starter/counter_test.lang}}
```

```sh
linglang test --gc-stress .
linglang test --no-opt --gc-stress --timeout 30s .
linglang fmt --check .
```

Use `--gc-stress` to collect managed cells at every safe point. `--gc-stats`
prints runtime heap statistics. These are debugging tools, not production
performance settings. The self-hosted runner performs discovery, signature validation, deterministic
ordering, and reporting in Linglang:

```sh
linglang-bootstrap test --gc-stress .
linglang-bootstrap test --no-opt --timeout 30s .
```

It accepts one directory or explicit file, defaulting to `.`. Its timeout syntax
is positive integer milliseconds, `Nms`, or `Ns`; the default is 30 seconds.
The maximum is 2,147,483,647 ms. Each deadline covers child VM startup, execution,
and shutdown; compilation/packaging precede it. Each test gets a fresh VM, an
empty mailbox, and a clean resource lifetime. The dispatch name is visible as
its sole `args()` element. Failure/timeout does not prevent later tests, and any
failure gives a nonzero suite status. An empty suite is an error.

The seed retains Go duration syntax (such as `500ms` or `1m`) and compiles the
module once. The first self-hosted runner emits once but packages per test;
large suites currently pay that overhead. A built test artifact is not a new
public distribution format.

## Formatting and project creation without Go

The self-hosted driver now supports `init [directory]` and
`fmt [--check] [file|directory ...]`. The [developer-tools chapter](developer-tools.md)
defines traversal, source safety, layout, and migration from the seed formatter.
These commands do not type-check or execute the project being edited.

## Editor

The VS Code extension in `editors/vscode/` provides highlighting, diagnostics,
formatting, and an outline. `linglang lsp` serves the language protocol over
standard input/output. Completion, rename, and debugging are not implemented.
The language server uses the Go frontend and loads local imports from disk.
Saved dependency changes and file watcher notifications recheck importing
documents, including transitive imports and files added to imported directories.
Errors inside a dependency are reported on the importing document with the
dependency's source location.
Unsaved dependency buffers are not yet overlaid across package boundaries.
Advanced lowering errors may only
appear when compiling.

## Distribution formats

| Command | Output | Receiver needs |
| --- | --- | --- |
| `linglang build -o _build .` | BEAM module directory | Compatible OTP and a launcher |
| `linglang pack -o app.escript .` | Executable archive | Compatible OTP |
| `linglang-bootstrap build -o app.escript .` | Executable archive | Compatible OTP |
| `linglang release -o dist/app.tar.gz .` | Program plus ERTS/OTP | Matching OS/CPU and compatible system libraries |

`pack` and bootstrap `build` embed the Linglang runtime. They do not include
input data or OTP itself. Arguments passed to the executable are visible through
`args()`. Relative file paths are resolved from the caller's working directory.

`release` supports Linux and macOS builds for the build machine's architecture.
It is not a cross-compilation facility. Unpack the archive and run `bin/run`.
The release currently runs the program to completion; daemon management and
hot-code upgrades are not integrated.


## Native SQLite dependency

`make sqlite` builds `bin/linglang-sqlite`; put it on the program's PATH.
SQLite programs also need the platform SQLite library. Neither escript archives
nor `release` currently bundle this optional native helper or libsqlite3.
Ordinary TCP and non-SQLite programs only require their usual OTP runtime.

## Automation and static test checks

Use `linglang describe --json` to discover the invoked driver's commands, capabilities,
and native API. `linglang check --json .` returns structured errors from the full
compiler; `linglang check --json --tests .` checks test and library roots without
executing them. Go CLI checks need no OTP; bootstrap checks run on OTP. The
[automation chapter](agent-workflows.md) specifies the versioned protocol and
byte-based source positions. Both drivers support these check options and
`test --json` event streams;
`linglang-bootstrap describe --json` exposes the self-hosted catalogue. Both also
provide `deps [--check] [--json]` for local dependency locks.

`test --json` sends versioned JSON Lines progress and results to stdout; test
program output and runtime diagnostics go to stderr. Each test has explicit
pass/fail/timeout status and a physical source location. Require a successful
`suite_end` and process exit before treating a suite as passing. The bootstrap
runner preserves its parent-lifetime pipe while redirecting output through
`/bin/sh` on Linux/macOS. See the [event protocol](agent-workflows.md#structured-test-events-version-1).
