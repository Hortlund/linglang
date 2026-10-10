# Linglang examples

Run these commands from the repository root with a built compiler on PATH.
Use `linglang run <path>` with either compiler driver. Files directly in this
folder are independent programs; subdirectories contain multi-file packages.

| Example | Demonstrates |
| --- | --- |
| [counter.lang](counter.lang) | Concurrent workers and a process that owns counter state |
| [worker_registration.lang](worker_registration.lang) | Typed messages, worker registration, maps, monitors, and transient supervision |
| [restart_limit.lang](restart_limit.lang) | Supervisor restart budgets and failure escalation |
| [worker_tests](worker_tests/) | Assertions and a supervised worker restart test |
| [supervision.lang](supervision.lang) | Supervisor lifecycle and child policies |
| [resilient_worker.lang](resilient_worker.lang) | Worker recovery under supervision |
| [jobqueue.lang](jobqueue.lang) | Message-based work distribution |
| [modules](modules/) | Local imports and reusable source packages |
| [store.lang](store.lang) | An in-memory OTP server with synchronous calls and restart recovery |
| [webcounter](webcounter/README.md) | A local browser application with SQLite persistence |
| [prime_lab](prime_lab/) | A multi-file computation example |
| [bootstrap_demo](bootstrap_demo/) | Arithmetic, bit operations, and scope in a multi-file program |
| [bootstrap_loops.lang](bootstrap_loops.lang) | Identifier scanning and loop control |
| [bootstrap_collections.lang](bootstrap_collections.lang) | Immutable collections, pointers, and iteration snapshots |

```sh
linglang run examples/worker_registration.lang
linglang run examples/restart_limit.lang
linglang test --gc-stress examples/worker_tests
```

The restart examples deliberately crash workers. OTP error and supervisor reports
are expected; `restart_limit.lang` succeeds when it prints `restart limit reached`
and exits successfully. The worker test suite asserts that a failed worker is
replaced with a new process.

SQLite examples require the optional helper. Follow the
[networking and SQLite guide](../book/src/io.md) for setup, or the
[webcounter instructions](webcounter/README.md) for the complete application.
