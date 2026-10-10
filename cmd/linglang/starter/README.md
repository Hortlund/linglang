# Supervised counter

These commands use the installed self-hosted `linglang` compiler. Run them inside
this directory with `linglang` and Erlang/OTP on your PATH:

```sh
linglang fmt .
linglang fmt --check .
linglang check .
linglang run .
linglang test --gc-stress .
linglang build -o counter.escript .
./counter.escript
```

Both application runs print `Counter: 5`. The first format command normalizes the
starter sources for your compiler before checking their formatting.

The installed compiler provides `init`, `fmt`, `check`, `emit`, `build`, `run`,
`test`, `describe`, and `deps` using OTP alone. Executables produced by `build`
also need OTP. No `linglang-bootstrap` alias or Go installation is required.
See `AGENTS.md` for capability discovery, dependency locks, and structured checks.

If you built the Go seed compiler from source instead, use its `pack` command in
place of `build` above to create `counter.escript`; the seed's `build` produces a
directory. The Go seed also provides `release` (an application bundle containing
OTP for the current OS and CPU) and `lsp`. Those commands are unavailable in the
installed self-hosted compiler. Use `linglang describe --json` to check the driver
you are running.

`counter.lang` defines the handler. `main.lang` starts a supervisor and makes a
call. `counter_test.lang` verifies that state persists between calls. Normal
directory builds exclude `_test.lang` files. Calls can time out; they do not
cancel server work. Restarting the server resets its state to the initial value.

Checks validate code without executing assertions; run tests to verify behavior.
Test results with `--json` are JSON Lines on stdout, with program output on stderr.
A successful test stream ends with `suite_end` and exit 0.

Choose one formatter per project: the Go seed uses Go printer conventions,
while the self-hosted formatter uses its own stable token-preserving layout.
Both preserve program behavior, but their whitespace is not byte-identical.
