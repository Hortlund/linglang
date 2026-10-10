# Compiler capabilities and roadmap

This is an experimental toolchain, not a stable release. Self-hosting means the
compiler can build itself; compatibility and production readiness need separate
evidence.

## What works now

| Capability | Go seed CLI | Linglang bootstrap CLI |
| --- | --- | --- |
| Checking, Erlang emission, source execution | Yes | Supported subset |
| OTP-dependent executable | `pack` | `build` |
| Multi-file programs and local relative imports | Yes | Yes |
| Test discovery, isolation, bounded execution | `test` | `test` |
| Int/bool/string, structs, pointers, lists, maps | Yes | Yes; narrower literal/allocation support |
| Loops, switch, if initializers | Yes | Yes |
| Processes, monitors, timers, supervisors, synchronous servers | Yes | Yes |
| Passive TCP and SQLite prepared statements | Yes | Yes |
| Multiple results and simultaneous assignment | Yes | Yes |
| Downloadable self-hosted toolchain | Release CI builds and verifies it | Portable OTP 29 archive; Linux/macOS install checks |
| General positional struct literals, `new`/`make` forms | Broader support | Not fully supported |
| Structured checks and test events | `check --json`, `check --tests`, `test --json` | Yes |
| Capability and native API discovery | `describe [--json]` | `describe [--json]` |
| Local dependency content lock | `deps [--check] [--json]` | `deps [--check] [--json]` |
| Project creation and formatting | `init`, `fmt` (Go printer) | `init`, `fmt` (Linglang token formatter) |
| Language server | `lsp` | No |
| Bundle ERTS/OTP for Linux/macOS | `release` | No |
| Rebuild and verify an existing compiler without Go | Independent seed oracle | `tools/bootstrap.escript`, using OTP build glue |
| Remote packages, dependency solver/registry | No | No |
| TLS, general HTTP framework, PostgreSQL | No | No |

SQLite requires the optional native helper and platform library. Local imports
support reusable packages and checked-in vendoring; they do not imply a remote
package ecosystem. The HTTP source library is a deliberately limited subset.

## Can I build a full program?

Yes: CLI tools, file processors, concurrent workers, supervised state services,
TCP applications, and small local SQLite-backed browser applications.
`examples/webcounter` is a complete browser-to-database example with state that
survives process restarts. The compiler itself remains a substantial multi-file
application.

Arbitrary production full-stack applications still need APIs and infrastructure
beyond these foundations: a complete HTTP/TLS stack, authentication libraries,
migrations/pooling, service deployment, and dependency tooling. Ship experiments
with explicit tests and failure handling; language and artifact compatibility can
still change.

## Next acceptance criteria

1. **Toolchain quality.** Run the OS/OTP compatibility matrix, extend differential
   package tests, and reduce measured compiler and self-hosted test packaging costs.
   The [OTP-only profiler](self-hosting.md#profile-the-compiler-without-go) can inspect
   shipped archives; pair profiles with unchanged-corpus timing and forced-GC tests.
   Measure application workloads emitted by the self-built compiler as well as
   the Go seed before extending its native-local optimizations.
   The [Go-free rebuild](self-hosting.md)
   checks a fixed point and runs language suites. Move application release
   orchestration into Linglang next, retaining OTP's packaging machinery, then
   port the language server. Close the positional-literal and allocation-form
   gaps against the independent seed oracle. Extend package API
   inspection and multi-error diagnostics. Keep the two formatter contracts explicit and extend source-preservation tests.
2. **Package distribution.** Specify package identity, versions, integrity,
   lockfiles, and license metadata before adding remote fetching and a registry.
3. **Service APIs.** Add TLS and broader HTTP framing with adversarial tests;
   define socket ownership transfer and concurrent acceptor patterns.
4. **Database lifecycle.** Design owner-local persistent connections, transaction
   scopes, cancellation/commit semantics, migrations, and deployment packaging.
5. **Stable release.** Publish repeatable toolchain artifacts only after compatibility,
   diagnostics, resource lifetime, and real applications meet explicit criteria.

Typed process destinations, asynchronous server callbacks, direct Erlang abstract
forms, and richer package inspection remain useful language/compiler work. A stable
release date would be premature; the implementation and acceptance tests should
set that decision.
