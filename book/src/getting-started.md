# Getting started

For a published self-hosted compiler download, follow [installation](installation.md).
It requires OTP 29 and runs without Go. The instructions below build from source.

## Build the development compiler

From a checkout, install Go 1.23 or later and Erlang/OTP with `erl`, `erlc`, and
`escript` available on PATH. The repository targets OTP 27–29; the latest local
verification was on OTP 29. Build the Go seed compiler:

```sh
go build -o bin/linglang ./cmd/linglang
export PATH="$PWD/bin:$PATH"
linglang help
```

This command produces a usable compiler now. It also supplies the formatter,
test runner, project generator, packaging, and language server. Go is needed to
build this executable, not to run the resulting executable.

## Your first program

Save this as `hello.lang`:

```go
{{#include ../examples/hello.lang}}
```

```sh
linglang check hello.lang
linglang run hello.lang
```

The output is `Hello from Linglang`. `check` compiles through language validation
without starting the program. `run` builds BEAM modules and starts a VM.

## Start a project

```sh
linglang init counter
cd counter
linglang run .
linglang test --gc-stress .
```

`init` creates a supervised counter, a test, usage instructions, and `.gitignore`.
It accepts a new or empty directory and refuses to overwrite existing content.
It does not initialize Git or download dependencies. The program prints
`Counter: 5`. Read [Processes and OTP servers](processes.md) to understand it.

All immediate `.lang` files in a directory form one program. They declare
`package main`; one file defines `func main()`. Normal builds skip `_test.lang`
files and do not recurse into subdirectories. Use [imports](modules.md) to load
other packages explicitly.

## Use the compiler written in Linglang

A freshly built `bin/linglang-bootstrap` supports:

```sh
linglang-bootstrap test counter
linglang-bootstrap check counter
linglang-bootstrap run counter
linglang-bootstrap build -o counter.escript counter
./counter.escript
```

Run these from the parent of the `counter` directory. The self-built compiler
needs OTP but not Go or `erlc`. Its `build` command creates an executable escript;
the seed's `build` command instead writes a directory of BEAM modules. The seed's
equivalent executable command is `pack`.

Ignored `bin/` files are local build artifacts and are not included in a fresh
checkout. The [contributor guide](contributing.md) shows how to produce and verify
a self-built compiler. Both CLIs support `init`, `fmt`, `test`, `describe`, and
`deps`; use the seed for `release` and `lsp`. The formatters have different layout conventions; choose
one per project. See [developer tools](developer-tools.md).

Once you have a trusted compiler archive, you can rebuild and verify it using
only OTP: `escript tools/bootstrap.escript`. This refreshes the runtime, compares
compiler generations, runs Linglang tests, and writes `bin/linglang-selfhost`.
See [A toolchain without Go](self-hosting.md) for setup and remaining tool gaps.

For a browser application with persistent state, follow [Networking and SQLite](io.md).
