# Introduction

Linglang combines imperative, C/Go-style source code with Erlang's BEAM virtual
machine and OTP supervision. Functions use braces, variables can be reassigned,
and pointers refer to mutable state inside one process. Processes communicate
with typed value snapshots and can be supervised by OTP.

The compiler produces Erlang modules, compiles them to BEAM, and
packages runnable programs. A compiler written in Linglang can compile its own
source. This does **not** mean the language or its toolchain is finished.
An existing compiler can also [rebuild and verify the toolchain without Go](self-hosting.md),
using OTP for runtime compilation and packaging. The [scaling chapter](scaling.md)
explains bounded concurrency and the limits of supervision.

This book describes the experimental implementation in this repository. There
is no stable language version or compatibility promise yet. In particular,
Linglang is not a Go implementation: resemblance in syntax does not imply that
Go packages, interfaces, channels, or libraries work here.

Start with [Getting started](getting-started.md) to run a supervised application.
The learning chapters explain the programming model. The [draft specification](specification.md)
records language rules, and [architecture](architecture.md) explains the compiler
and runtime. The [capability matrix](status.md) distinguishes implemented behavior
from work still required.

You can use Linglang today for command-line tools, file-processing programs,
concurrent workers, reusable local packages, TCP services, and SQLite-backed local
applications. The browser counter combines imports, a small HTTP source library,
OTP supervision, and persistent SQLite state. A registry, dependency solver,
TLS/HTTP framework, and production operations tooling are still future work.
