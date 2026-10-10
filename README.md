# Linglang

**LING Is Not Go.**

An experimental imperative language for the BEAM/OTP platform, with C/Go-style
syntax, lightweight processes, typed message payloads, and OTP supervision.

Linglang brings familiar control flow and process-local mutable state to the
BEAM. Programs compile to Erlang modules and run on the Erlang virtual machine.
The compiler is written in Linglang and can rebuild itself using an existing
compiler archive and Erlang/OTP.

**Status:** usable for experimentation, tools, and small services. The language,
runtime APIs, and artifact formats are still evolving. Self-hosting is verified
through compiler fixed-point checks and language tests; it is not a promise of
production readiness or application scalability.

[Read the book](book/src/introduction.md) ·
[Language specification](book/src/specification.md) ·
[Architecture](book/src/architecture.md) ·
[Capabilities and roadmap](book/src/status.md)

## Language and runtime

- Imperative functions, structs, constants, loops, conditionals, and explicit
  process-local mutation, multiple return values, and simultaneous assignment.
- Integers, booleans, strings, pointers, immutable lists, and immutable maps.
- Lightweight BEAM processes, typed message payloads, selective receive,
  monitors, and timers.
- OTP supervisors with restart policies and synchronous stateful servers.
- Multi-file programs, local imports, and content locks for local dependencies.
- File I/O, passive TCP, and SQLite through an optional native helper.

Processes exchange messages; pointers remain local to their owning process.
Supervision supports recovery from failures. Bounded concurrency, resource
ownership, and overload handling remain application design responsibilities.
See [processes and OTP](book/src/processes.md) and
[concurrency and scaling](book/src/scaling.md).

```go
package main

type Result struct{ total int }

func worker(parent Pid) {
    total := 0
    for i := 1; i <= 100; i++ {
        total += i
    }
    send(parent, Result{total: total})
}

func main() {
    process := spawnMonitor(worker, self())
    result := receive[Result](5000)
    if !result.ok {
        panic("worker response timed out")
    }
    println("Total:", result.value.total)
    exit := wait(process.monitor, 5000)
    if !exit.ok || !exit.normal {
        panic("worker did not finish normally")
    }
}
```

Save this as `sum.lang` and run `linglang run sum.lang`. It prints `Total: 5050`.

## Get started

[Compiler downloads and installation](book/src/installation.md) describes the
self-hosted OTP 29 packages and release workflow. Published packages require no Go.
Until the first release is published, build from source below.

For a first build from source, install Go 1.23 or later and Erlang/OTP with
`erl`, `erlc`, and `escript` on PATH. The compatibility targets are OTP 27–29.

```sh
git clone https://github.com/Hortlund/linglang.git
cd linglang
go build -o bin/linglang ./cmd/linglang
export PATH="$PWD/bin:$PATH"

linglang init counter
cd counter
linglang check .
linglang test --gc-stress .
linglang run .
```

The generated project includes a supervised counter, tests, and usage
instructions. It prints `Counter: 5`. Go is required to build this seed compiler,
not to run the resulting executable. Erlang/OTP is required to run programs.

See [getting started](book/src/getting-started.md) for the full walkthrough.

## Use the self-hosted compiler

With a trusted `bin/linglang-bootstrap` archive, rebuild and verify the compiler
from the repository root using only Erlang/OTP:

```sh
escript tools/bootstrap.escript
bin/linglang-selfhost check examples/counter.lang
bin/linglang-selfhost test --gc-stress examples/worker_tests
bin/linglang-selfhost build -o counter.escript examples/counter.lang
./counter.escript
```

The rebuild compares successive compiler generations and runs the language
suites before writing `bin/linglang-selfhost`. A fresh checkout does not include
compiler binaries. Follow the [contributor guide](book/src/contributing.md) to
produce the first archive, then [the self-hosting guide](book/src/self-hosting.md)
for subsequent builds without Go or the external `erlc` command.

Both drivers provide `check`, `emit`, `run`, `test`, `fmt`, `init`, `describe`, and
`deps`. The self-hosted driver's `build` produces an executable escript; the Go
seed uses `pack` for that output and `build` for a BEAM module directory. The Go
driver still provides the language server and release packaging. The two
formatters use different layout conventions; choose one per source tree.

## Tools for developers and agents

The CLI supports interactive use and versioned machine-readable output:

```sh
linglang describe --json
linglang check --json --tests .
linglang test --json --gc-stress .
linglang deps --check --json .
linglang fmt --check .
```

Run these commands inside a project. `describe` reports the invoked compiler's
capabilities and native APIs. Structured checks include source locations; test
output is a stream of result events. `deps --check` verifies an existing local
dependency lock without changing files or fetching packages. Create the lock
with `linglang deps .` first.

See [developer tools](book/src/developer-tools.md),
[agent workflows](book/src/agent-workflows.md), and [JSON schemas](schemas/).
The [VS Code extension](editors/vscode/README.md) supplies highlighting,
diagnostics, formatting, and document outlines through the Go language server.

## Examples and documentation

Start with the [example index](examples/README.md): concurrent counters, worker
registration, restart limits, reusable modules, and a SQLite-backed browser
application. The [networking and SQLite chapter](book/src/io.md) explains the
optional database adapter and the limits of the HTTP example.

The [Linglang Book](book/README.md) covers learning material, the implemented
language specification, compiler architecture, and development workflows. With
mdBook installed, run `mdbook serve book` from the repository root to read it
locally. [Benchmark instructions and results](benchmarks/README.md) document
measurement conditions and comparisons.

## Current limits and contributing

Local imports and checked-in dependencies work today; a remote package registry
and dependency solver are not implemented. TLS, a general HTTP framework, and
broader database APIs remain future work. The self-hosted compiler also has
narrower support for some literals and allocation forms than the Go seed.
Consult the [capability matrix](book/src/status.md) before choosing a workflow.

Contributions should keep the seed compiler, self-hosted compiler, specification,
and book aligned. Start with [the contributor guide](book/src/contributing.md)
and [repository guidance](AGENTS.md) for focused tests, compatibility checks, and
the compiler fixed-point verification process.
