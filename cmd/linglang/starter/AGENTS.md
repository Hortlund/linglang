# Working on this Linglang project

The installed self-hosted compiler is named `linglang`. Use
`linglang describe --json` to discover its commands and native signatures.
The language is experimental and is not Go: do not assume Go packages,
goroutines, interfaces, or user generics work.

Keep changes small and explicit. Preserve import visibility and process ownership.
Pointers and socket handles stay within their owning process. Check typed result
records for I/O failures; a server call timeout does not cancel server work.

From this directory, format the starter sources before checking their layout:

```sh
linglang describe --json
linglang fmt .
linglang fmt --check .
linglang check --json .
linglang check --json --tests .
linglang test --json --gc-stress .
linglang deps --json .
linglang deps --check --json .
```

`deps` writes or refreshes the local dependency lock; `deps --check` verifies it
without changing it. Neither command fetches packages. During later checks, use
`fmt --check` to verify formatting without rewriting source files.

Checks do not execute assertions; run tests to verify behavior. JSON checks return
one versioned object on stdout, with exit 0 on success and 1 on failure. Inspect
`ok`, `diagnostics`, and `schemaVersion`; use physical byte locations to edit the
source. Messages explain errors but are not stable identifiers. CLI usage errors
still go to stderr. Do not redirect reports over source files.

Test results are JSON Lines on stdout, with program output on stderr; drain both
streams. Require exit 0 and a successful `suite_end`. Both the self-hosted compiler
and Go seed support these structured checks and tests. Choose one formatter for
the project; the self-hosted layout differs from the Go printer.

Use `linglang build -o counter.escript .` with the installed compiler to create an
OTP-dependent executable. `pack`, `release`, and `lsp` are Go seed commands, not
installed compiler commands. See README.md for execution and seed differences.
