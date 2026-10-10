# Working on this Linglang project

Use `linglang describe --json` or `linglang-bootstrap describe --json` to discover the installed driver's commands
and native signatures. The language is experimental and is not Go: do not assume
Go packages, goroutines, interfaces, user generics, or multiple return values work.

Keep changes small and explicit. Preserve import visibility and process ownership.
Pointers and socket handles stay within their owning process. Check typed result
records for I/O failures; a server call timeout does not cancel server work.

From this directory:

```sh
linglang check --json .
linglang check --json --tests .
linglang test --json --gc-stress .
linglang fmt --check .
```

Checks do not execute assertions; run tests to verify behavior. JSON checks return
one versioned object on stdout, with exit 0 on success and 1 on failure. Inspect
`ok`, `diagnostics`, and `schemaVersion`; use physical byte locations to edit the
source. Messages explain errors but are not stable identifiers. CLI usage errors
still go to stderr. Do not redirect reports over source files.

Both drivers support JSON checks, `check --tests`, and `test --json`. The self-hosted
name is `linglang-bootstrap`. Test results are JSON Lines on stdout, with program
output on stderr; drain both streams. Require exit 0 and a successful `suite_end`.
`init` and `fmt` also work in the self-hosted driver. Choose one formatter for
the project; its layout differs from the Go printer. Both drivers provide `describe --json` and `deps --check --json .`. See README.md for execution/packaging.
