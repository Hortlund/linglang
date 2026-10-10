# Modules and dependencies

A directory is a package. Packages make helpers reusable without mixing every
application declaration into one namespace. Both compilers link local imports.

The repository's `examples/modules/main.lang` imports a sibling library:

```go
{{#include ../../examples/modules/main.lang}}
```

The library declares its own package:

```go
{{#include ../../examples/modules/math/math.lang}}
```

Run `linglang run examples/modules` or
`linglang-bootstrap run examples/modules`. Both print `42 42`.
`linglang-bootstrap test examples/modules` discovers the root package tests.
A library directory can also be tested directly without a `main` function.

## Module contract

- Imports precede declarations. Single and grouped imports are supported.
- Paths begin with `./` or `../` and resolve relative to the importing file.
  Import targets must be directories; importing a single source file is an error.
  Directory loading selects sorted immediate `.lang` files, excluding tests.
- The default alias is the dependency's declared package name. An explicit
  alias, such as `import numbers "./math"`, is local to that source file.
  Use an alias only as the qualifier in `numbers.Member`; an alias cannot be
  used as a standalone value, function, or type, even when it shares a built-in name.
- Package functions, types, and constants beginning with ASCII `A`–`Z` are
  exported. Other declarations are private. All fields of a public struct are
  accessible, including lowercase fields; Go's field visibility rule does not
  apply. All files in a package must use the same package name.
- Imported libraries cannot declare `package main`. Executable roots require
  `main`; test roots may instead use a library package name.
- Dot/blank imports and shadowing an import alias are rejected. Unused imports
  are permitted. `init` functions and mutable package variables are unsupported.
- Import cycles are errors. A shared dependency has one identity within a build.
  Nesting is limited to 64 imports. Identity uses lexically normalized paths;
  symlink aliases are not guaranteed to deduplicate. Avoid alternate symlink
  paths to the same dependency.
- Internal names beginning `__llpkg` are reserved. Imported code cannot reach
  another package's private declarations through unqualified names.
  Each package resolves names in its own scope: shadowing a built-in such as
  `len` or `true` in the application does not change its meaning in a library.
  Blank constant declarations (`_`), including skipped `iota` values, remain
  anonymous when packages are linked. Identifier keys in nested map literals
  retain their lexical bindings even when the inner literal omits its type.
- Prelude declarations such as `readFile`, `Process`, and `head` cannot be
  redeclared at package scope, in either roots or dependencies. Local variables
  may shadow them; language built-ins such as `len` may also be shadowed at
  package scope.
- Both compilers lower the graph to one BEAM program. There is no dynamic module
  loading, separate-compilation cache, or stable binary interface yet.

All dependencies participate in checking, and executable output is protected
against overwriting imported source files as well as root sources.

## A reproducible package workflow

Start with sibling packages inside your repository. For third-party code,
check sources into `vendor/`, preserve their relative directory layout, and
record the license and exact upstream commit. Import that checked-in directory:

```go
import codec "./vendor/codec"
```

This is the initial package ecosystem: ordinary versioned source packages and
explicit dependencies. The compiler does not download code or run install
scripts. There is no registry, semantic-version solver, remote import syntax,
or remote lockfile resolution yet. Those need a versioning and integrity contract
before we add fetching. `stdlib/` provides small HTTP and SQLite helper packages as
examples of libraries using this model.

Both drivers can record and verify the complete imported source closure:

```sh
linglang deps .            # writes linglang.lock
linglang deps --check .    # fails if a dependency changed or the graph differs
```

The version-1 JSON lock lists project-relative filenames and SHA-256 content
hashes, sorted deterministically. Root source files are excluded. Commit the lock
and run `deps --check` in CI before building. This verification is explicit;
ordinary builds do not silently update or enforce a lock. Use
`linglang-bootstrap deps --json .` to write with a structured report, or
`linglang-bootstrap deps --check --json .` to verify without writing. Hashes detect drift against the reviewed lock;
they do not establish who authored the code or replace provenance/license review.

**Exercise:** move a calculation into an exported library function, add a private
helper, and test the library directory independently. Try importing the private
helper and verify that the compiler rejects it.

The selected dependency-lock root currently requires `package main`; library
roots can be tested with `test` but do not have an independent lock command yet.
Lock files use exactly `version` and `files`; each entry has exactly `path` and
`sha256`. Paths use `/`, are relative to the selected root directory (or the
parent of an explicit source file), and may contain `../` for sibling packages.
Source discovery preserves the selected filesystem path, including symlink/`..`
components, as the compiler does. Lexical normalization is used separately for
package identities and lock paths; it must not change which source is read.
Imported source files are included transitively, even when their declarations
are unused. Root sources and directory-discovered `_test.lang` files are excluded.
No dependencies produces `files: []`. JSON whitespace and object key order do
not matter; the file array must match the sorted current closure. Unknown or
duplicate fields, duplicate paths, malformed JSON, and unsupported versions fail.
Checking never rewrites the lock. Updating computes the closure before atomic
publication and rejects a final lock symlink or nonregular file. It never executes
source or fetches dependencies. The bootstrap uses its supported parser/linker
subset; neither tool type-checks the whole application for dependency locking.
Concurrent source/lock writers require external coordination. SHA-256 detects
byte changes, including comments, but a lock is not authentication or a sandbox.
