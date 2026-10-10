# Self-hosted developer tools

The compiler archive now carries a formatter, project generator, capability catalogue, and dependency tool written in
Linglang. An installed archive needs Erlang/OTP, but neither Go, `erlc`, nor the
source checkout, to run these commands.

```sh
linglang-bootstrap init counter
cd counter
linglang-bootstrap fmt .
linglang-bootstrap fmt --check .
linglang-bootstrap check --json --tests .
linglang-bootstrap test --json --gc-stress .
linglang-bootstrap build -o counter.escript .
./counter.escript
```

## Create a project

`init [directory]` defaults to the current directory. It accepts a new directory
(including missing parents) or an existing empty directory. A nonempty directory,
file, or final symlink is rejected. Files are created exclusively, so a file that
appears during creation is never overwritten. An I/O failure may leave a partial
project; inspect it before retrying.

The embedded starter contains a supervised counter, a test, README, AGENTS guide,
and ignore rules. It is identical to the Go driver's starter. Creation does not
run the program, initialize Git, or contact a package registry.

## Format source

`fmt [--check] [paths...]` defaults to `.`. Directories are traversed recursively,
including test files, while `.git`, `_build`, `_release`, `bin`, and `dist`
subdirectories are skipped. Encountered symlinks are skipped to avoid cycles and
unexpected edits outside the tree. Explicit file or directory symlinks are
resolved; formatting a linked file updates its target and preserves the link.
Overlapping paths are deduplicated by canonical absolute path. Changed files
are reported in sorted canonical-path order.

`--check` reports files that would change and exits unsuccessfully when any need
formatting. It never writes. Normal formatting reads, parses, and formats every
selected file before replacing any, so invalid syntax cannot leave earlier files
partially formatted. Each replacement is an atomic rename preserving permission
bits, with source-content checks to catch ordinary intervening edits. This is not
a multi-file transaction: an I/O failure during publication may leave earlier
files formatted. Concurrent writers still need external coordination. Atomic
replacement also gives a formatted hardlink a new inode, as the seed does.

The formatter validates syntax, not names or types. Unresolved imports and
incomplete application APIs are fine; malformed or unsupported parser syntax is
rejected without rewriting. It does not execute source or load imports.

## Layout and preservation

The Linglang formatter uses tabs for indentation, spaces around binary and
assignment operators, and line breaks between statements and nonempty block
bodies. It preserves existing expression line breaks, at most one blank line,
parentheses, and literal spelling. Comments and raw-string contents retain their
original bytes. It does not reorder imports, align columns, or reflow comments.
Before returning output, it reparses and compares syntax trees with the input.
Repeated formatting produces identical bytes.

This is a separate style from the seed's Go printer, not a byte-for-byte `gofmt`
port. Choose one formatter for a project and use that formatter in CI. Existing
repository compiler files currently use the seed style. Formatting copies with
the Linglang tool is covered by the acceptance tests; wholesale restyling is not
required to use the new commands.

`lsp` and `release` still belong to the Go driver. The new
commands remove Go from the terminal create/edit/format/check/test/build loop;
editor language services and those other tools remain the next migration work.

## Discover capabilities and verify dependencies

```sh
linglang-bootstrap describe
linglang-bootstrap describe --json
linglang-bootstrap deps --json .
linglang-bootstrap deps --check --json .
```

`describe` reports the installed driver's actual commands and compiled native
API without loading project files. Its command list distinguishes bootstrap
`build` (an executable archive) from the seed's `build`/`pack` split. Human output
includes command summaries and signatures; JSON supports deterministic discovery.
Ordinary signatures and record fields come from checker metadata. Generic
intrinsic descriptions have differential tests against the seed.

`deps` writes the same version-1 lock format as the seed, including transitive
local imports. Either driver can verify the other's lock. `--check` never writes;
`--json` returns a versioned result for both successes and failures. See
[modules](modules.md) for lock semantics and [agent workflows](agent-workflows.md)
for the report schema. These commands work from an installed compiler archive
outside the source checkout, with OTP but no Go or `erlc`.
