# Supervised counter

Run these commands inside this directory with `linglang` on your PATH:

```sh
linglang check .
linglang run .
linglang test --gc-stress .
linglang fmt --check .
linglang pack -o counter.escript .
./counter.escript
```

Expected application output: `Counter: 5`.

The self-built compiler provides `init`, `fmt`, checks, tests, and executable
builds using OTP alone. Capability discovery and dependency locks also work without Go. The Go driver
still provides `release` and `lsp`:

```sh
linglang-bootstrap describe --json
linglang-bootstrap deps --json .
linglang-bootstrap deps --check --json .
linglang-bootstrap fmt .
linglang-bootstrap fmt --check .
linglang-bootstrap test --gc-stress .
linglang-bootstrap check .
linglang-bootstrap run .
linglang-bootstrap build -o counter.escript .
```

Escript executables need Erlang/OTP. To bundle the runtime for your current
OS and CPU, use `linglang release -o dist/counter.tar.gz .`.

`counter.lang` defines the handler. `main.lang` starts a supervisor and makes a
call. `counter_test.lang` verifies that state persists between calls. Normal
directory builds exclude `_test.lang` files. Calls can time out; they do not
cancel server work. Restarting the server resets its state to the initial value.

For automation, `linglang describe --json` lists capabilities and native APIs.
`linglang check --json .` reports machine-readable errors; add `--tests` to check
test signatures and bodies without executing them. Go CLI checks need no OTP; bootstrap checks run on OTP.
See `AGENTS.md` for the shared human/agent development loop.

Both drivers support `test --json` for streaming test events on stdout. Program
output goes to stderr. `linglang-bootstrap check --json --tests .` validates this
suite without running it. A successful test stream ends with `suite_end` and exit 0.

Choose one formatter per project: the Go driver uses Go printer conventions,
while the self-hosted formatter uses its own stable token-preserving layout.
Both preserve program behavior, but their whitespace is not byte-identical.
