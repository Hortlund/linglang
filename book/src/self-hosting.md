# A toolchain without Go

The Linglang compiler's lexer, parser, resolver, checker, linker, emitter, command
driver, and test runner are written in Linglang. An existing compiler can rebuild
them and run programs using Erlang/OTP alone. You no longer need to invoke a Go
test to perform the compiler fixed-point check.

A [release archive](installation.md) supplies a verified compiler without Go.
You can use its installed `linglang` as `--seed /absolute/path/to/linglang`.

From a source checkout with a trusted `bin/linglang-bootstrap` archive:

```sh
escript tools/bootstrap.escript
# Equivalent convenience target:
make selfhost
```

The default output is `bin/linglang-selfhost`. The input compiler is preserved.
Then use the resulting tool directly:

```sh
bin/linglang-selfhost check --json examples/webcounter
bin/linglang-selfhost test --json --gc-stress tests/concurrency
bin/linglang-selfhost build -o counter.escript examples/webcounter
```

## What the rebuild proves

The build command performs these steps:

1. Compile the checkout's shared Erlang runtime sources with OTP's compiler API.
   This refreshes runtime changes instead of silently copying the old compiler's
   embedded runtime. The old compiler must be compatible with that runtime.
2. Run the supplied compiler A to emit compiler B from `bootstrap/emitter`.
3. Compile B with OTP, then run B on the same Linglang sources to emit C.
4. Require byte-for-byte equality of B and C's Erlang source. Print the byte count
   and SHA-256 digest. This compares emitted compiler code, not archive metadata.
5. Compile and run C against the lexer, parser, resolver, checker, emitter, and
   concurrency and language test suites with forced managed collection. Repeat the
   concurrency and language suites with the reference cell lowering.
6. Package C and the freshly compiled runtime, then publish the complete executable
   with an atomic rename. A failed stage leaves an existing output executable intact.

The orchestration is a small OTP escript, `tools/bootstrap.escript`. It discovers
and invokes build stages, compiles generated Erlang, compares outputs, and packages
BEAM files. It contains no Linglang parsing, checking, or lowering. The language
compiler and acceptance tests execute as Linglang programs throughout. Neither Go
nor the external `erlc` executable is required by this workflow.

Each stage has a 900-second default deadline. A separate parent-lifetime pipe
terminates the child VM if the build driver exits or times out, including when
compiler code is looping without reading stdin. Temporary build files are removed
on normal completion and handled errors; an abrupt OS kill can leave a temporary
directory. Individual language tests have 120-second deadlines. Output and OTP
crash reports from deliberately failing workers are expected during fault tests.

```sh
escript tools/bootstrap.escript \
  --seed bin/linglang-bootstrap \
  --output bin/linglang-next \
  --timeout 1200
```

`--root` selects a checkout; relative seed/output paths resolve against it. The
workflow currently targets Linux/macOS and requires `/bin/sh`, `erl`, `escript`,
OTP's compiler/crypto modules, and the shell helpers used by the OTP installation.
It refuses to overwrite its seed, build script, runtime inputs, or language suite
sources, including existing hardlink and symlink aliases.

## What still depends on other tools

A first build from source without an existing compiler still uses the Go seed.
Keep a trusted compiler artifact to continue rebuilding without Go. A reproducible
source bootstrap is not a proof of compiler provenance or freedom from inherited
compiler defects. The Go seed remains useful as an independent semantic oracle.

The Go driver still owns `lsp` and `release`.
The self-hosted driver owns `check`, `emit`, `build`, `run`, `test`, `fmt`, `init`, `describe`, and `deps`.
An application edit/format/check/test/build loop now needs only the compiler
archive and OTP. See [developer tools](developer-tools.md) for formatter differences. Moving the
remaining developer commands is future work; use the capability matrix when
choosing a workflow. SQLite also has an optional C helper. Erlang/OTP remains the
intended platform and runtime dependency.

The fixed point and language suites complement the Go differential, launcher,
I/O, and packaging tests. They do not replace the OS/OTP compatibility matrix or
application-specific load testing.

Expect the full verification to take several minutes: C compiles the checker
and emitter themselves as test packages before running their tests. This is a
compiler acceptance workflow, separate from the much smaller application edit,
check, and test loop.

## Upgrade a seed that predates filesystem and hashing primitives

An older compiler does not recognize newly added native signatures. For the
developer-tool native-API transitions, use the existing Linglang archive directly:

```sh
escript tools/bootstrap.escript --upgrade-seed \
  --seed bin/linglang-bootstrap --output bin/linglang-next --timeout 2400
```

This first builds a temporary compiler with the new checker/emitter definitions
and compatibility stubs for `fmt`, `init`, and `deps`. The temporary compiler then builds
the complete current compiler. B/C equality and the same real language suites
are still required before publication. The published artifact contains the actual
tools. No Go is involved. After upgrading, normal rebuilds need no upgrade flag.
This bridge covers this native-API transition, not arbitrary historical language
versions. Keep the old seed until the new artifact has passed its acceptance tests.
