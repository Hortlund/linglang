# Compiler architecture

Linglang has two frontends and a shared Erlang runtime. Both compile source to
Erlang text and then use OTP's compiler to obtain BEAM modules.

```text
                         Go seed
.lang files -> go/parser -> go/types -> Linglang validation/lowering
                                                    |
                                                    v
                                             Erlang source
                                                    |
                         self-hosted                v
.lang files -> lexer -> parser -> resolver -> checker -> emitter
                                                    |
                                                    v
                                             Erlang source
                                                    |
                             OTP compiler -> BEAM modules -> ERTS
```

The two arrows to Erlang source represent alternative paths, not a pipeline
through the Go frontend followed by the self-hosted frontend.

## The seed

`internal/compiler/` uses Go's parser and type checker as bootstrapping tools.
A synthetic prelude gives built-in operations their static signatures. Calls
are recognized by resolved binding identity, so shadowing a builtin does not
accidentally invoke the runtime operation. Linglang-specific validation rejects
Go constructs that cannot be lowered.

The seed emits one `linglang_program` module. `cmd/linglang/` supplies source
discovery, OTP compilation, execution, packaging, tests, formatting, and releases.
`internal/lsp/` reuses frontend information for editor diagnostics and outlines.

## Tooling boundary

`internal/compiler/diagnostics.go` retains error categories, physical source
locations, and import context as typed data. The Go CLI's `check.go` chooses text
or versioned JSON rendering and uses the full compile path, including lowering.
`Analyze` remains a partial editor analysis, not an alternative full checker.
`catalog.go` extracts native API declarations from the same checking prelude;
`describe.go` owns driver capabilities and the command catalogue shared with help.
The [contributor change map](agent-workflows.md#where-to-make-compiler-changes)
locates both implementations for each kind of language work.

## The self-hosted frontend

`bootstrap/lexer`, `parser`, `resolver`, `checker`, and `emitter` are written in
Linglang. Later stages reuse earlier source files through repository symlinks;
preserve these when copying the source tree. The resolver tracks declaration
identity. The checker records expression types, definitions, and message schemas.
The emitter consumes that checked metadata and enforces its supported subset.

Source paths are sorted for deterministic package emission. Functions and fields
receive encoded Erlang names rather than relying on source names being valid
Erlang atoms. Ordered operand evaluation and lexical runtime scopes preserve
imperative semantics despite Erlang's single-assignment variables.

Constant string concatenation uses an immutable rope: small values stay inline,
and larger values refer to a package-local table of nodes by integer ID. Nodes
link to children by ID, so even a generic root scan cannot recursively expand
shared subtrees. No managed pointers are needed in constant metadata. The checker
returns the node table with its checked result; consumers must retain that table
and never mix IDs from independent checks. IDs let comparisons skip shared
prefixes without expanding enormous strings; distinct nodes still compare their
contents. Consumers materialize bytes only when needed. This is a compiler
representation choice, not different language semantics or a relaxation of
string size limits.

## Module linking

Both drivers load a deterministic, closed graph of relative imports. The Go
loader uses parser object identities and source positions; the Linglang loader
uses the resolver's definition/reference identities. Imported declarations get
reserved internal names before the ordinary checker and emitter run. Diamond
imports reuse one package identity. This keeps the existing single-program BEAM
backend and shared generic built-in types while adding real source namespaces.
Fields remain public members; imports expose only ASCII-uppercase package
functions, constants, and types. There are no package initialization side effects.

The physical filenames survive linking for diagnostics and source-output
protection. Generated package IDs depend on deterministic traversal, not on
machine-specific absolute paths. Separate compilation and stable binary package
interfaces are future work.

## Self-hosted tests

The Linglang driver discovers test files, validates zero-argument test signatures,
emits a dispatcher, sorts names, and reports results. A narrow runtime bridge
packages the emitted module and runs each selected test in a new VM. The existing
stdin-independent lifetime pipe stops a child when its compiler dies or its test
deadline expires. A fresh VM also bounds unmonitored workers and owned resources.

`tooling.lang` carries driver issues as values through parsing and linking, then
renders checks and JSON Lines test events. Native bridges handle process exit
status and descriptor redirection, not compiler error classification. In JSON
mode `/bin/sh` executes the child with stdout redirected to stderr while retaining
the fd 3/4 lifetime pipe; untrusted paths and arguments are separate argv entries.
The Go runner redirects the child's stdout descriptor directly. Both protocols
preserve test output outside the machine-readable stdout stream.

## Developer tools

`formatting.lang` is a lexer/parser-driven whitespace formatter. It uses syntax
roles for blocks, headers, and unary operators, preserves comment/literal bytes,
and reparses output to compare syntax trees before publication. `developer_tools.lang`
owns recursive traversal policy, validation of the complete input set, `--check`,
and project creation. `project_templates.lang` embeds starter bytes; differential
integration tests compare them with `cmd/linglang/starter/` to prevent drift.

The shared runtime exposes narrow filesystem primitives (lstat, directory listing,
symlink resolution, recursive directory creation, exclusive creation, and atomic
replacement). It contains no formatting rules or project templates. The seed's
formatter still uses Go's printer and has a distinct whitespace contract.

`--upgrade-seed` in the proof tool first compiles current checker/emitter native
signatures with a Linglang stub replacing their new developer-tool callers. That
intermediate compiler can then compile the complete source. Published B/C stages
and acceptance tests always use the actual developer tools, never the stub.

## Mutable variables

The reference lowering places mutable language values in process-local cells.
Optimized lowering uses native Erlang values where analysis can preserve behavior.
Address-taken or pointer-bearing values retain appropriate cells and roots.
`--no-opt` is a semantic oracle for testing, not a separate language dialect.

The seed currently performs more control-flow optimization. The bootstrap uses
native values for eligible primitive locals, while many mutable control-flow
bindings still use cells. Performance work must retain forced-GC correctness and
agreement between both compiler paths.

## Shared runtime

`internal/compiler/runtime.erl` implements managed cells, collections, primitive
operations, typed messages, file/text helpers, and bootstrap compilation services.
`supervision.erl` delegates child lifecycle and restart policy to OTP's supervisor.
`server.erl` adapts synchronous calls to OTP's `gen_server`, with a persistent
mutable state cell for each server.
`io.erl` adapts passive OTP TCP sockets and a bounded SQLite native port protocol.
The SQLite helper is C code calling SQLite directly, not compiler logic. It uses
prepared statements, a wall-clock alarm, and a parent-lifetime pipe.

The seed invokes `erlc`. The self-built compiler calls the runtime bridge, which
parses generated Erlang and calls `compile:forms`. It therefore requires OTP's
compiler modules, but not the external `erlc` command or Go. Building Erlang
abstract forms directly is a future backend option, not the current path.

## Bootstrap proof

`tools/bootstrap.escript` provides a Go-free path from an existing compiler. OTP
build glue refreshes the shared runtime and invokes Linglang compiler generations;
all language frontend/backend work stays in Linglang. It checks emitted-source
equality, runs generation C's language suites, and publishes the compiler only
after they pass. See [self-hosting](self-hosting.md) for the exact boundaries.

The proof names compiler generations A, B, and C:

1. The Go seed builds A from the Linglang compiler source.
2. A emits B from the same source.
3. B emits C from the same source.
4. B and C output must be byte-identical; C must run application fixtures and
   preserve expected diagnostics, resource ownership, and final GC cleanup.

Equality demonstrates a compiler fixed point for those inputs. It does not prove
the language implementation correct. Differential fixtures, negative tests,
runtime tests, and application examples cover independent properties.


## Discovery and dependency tooling

`discovery.lang` builds the catalogue from native registrations recorded by the
checker. `native_catalog.lang` supplies human parameter labels and generic
intrinsic schemes; integration tests compare all public signatures with the
independent seed. Command/capability data belongs to the driver, so unsupported
seed-only commands are never advertised by the bootstrap.

`dependencies.lang` asks the existing module linker for the local source closure,
excludes root inputs, sorts relative paths, and hashes raw source bytes. It owns
lock validation and report rendering. `json.lang` is a strict, depth-bounded JSON
reader written in Linglang, including UTF-8 and Unicode surrogate-pair handling.
The runtime supplies only SHA-256 and filesystem primitives. `writeNewFile`
publishes a complete file exclusively using a same-directory temporary file and
atomic hard link; `replaceFile` checks expected bytes and atomically renames.
These operations require a filesystem supporting the corresponding operations.

## Multiple results and assignment

Function result signatures contain ordered component types. The bootstrap stores
these in an internal `tuple` descriptor; source programs cannot name or store a
tuple type. Both compilers lower multiple results to an Erlang tuple and expand
it only at checked return, declaration, assignment, and ordinary-call boundaries.

Assignment lowering captures destination references before right-hand values,
roots them during evaluation, and then performs writes left to right. Values
being handed from the temporary scope to local storage cross no collection
safepoint. Optimized locals retain their native representation where possible;
addressed locals use managed cells. For-loop initializer bindings each receive
new iteration storage before the post statement. Differential tests exercise
these rules with forced collection and the reference lowering.
