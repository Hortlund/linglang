# Concurrency, failure, and scale

Linglang is an imperative language on BEAM. Within one process, statements run
in order, locals mutate, and pointers refer to process-local storage. Between
processes, state is isolated and communication uses typed immutable snapshots.
The compiler lowers these operations to BEAM code and native OTP services.

Self-hosted `build` and `run` applications use OTP's normal scheduler defaults;
they no longer bake in a single-scheduler limit. Operators can configure scheduler
counts through OTP's environment flags. The compiler-proof driver uses one
scheduler for its compiler stages; that build setting is not inherited as an
application launcher policy. More schedulers enable parallel BEAM processes,
not parallel execution of one serialized server callback.

BEAM schedules lightweight processes; OTP supplies supervision and serialized
server callbacks. These are useful foundations for concurrent services. They do
not automatically bound application queues, distribute work, persist state, or
make a single shared server handle unlimited traffic.

## Bound the work you admit

An unbounded loop that spawns a process for every request can exhaust memory.
Sending faster than a consumer can receive also grows its mailbox. Timeouts stop
waiting, and do not cancel an already queued request or a database write.

One current approach is a fixed worker pool with at most one outstanding job per
worker. Dispatch a replacement job only after receiving that worker's completion.
This bounds both active work and application requests in flight. Send a stop
message when the work is exhausted, and monitor every worker so shutdown and
failure are observable. Production code must choose whether failed jobs are
retryable and how to account for their possible side effects.

The executable acceptance test in `tests/concurrency/concurrency_test.lang`
dispatches 4,096 jobs across 64 workers, checks each result exactly once, and
requires every worker to terminate normally. These counts exercise the protocol;
they are not a claimed maximum capacity or throughput measurement.

## Partition mutable services

An OTP server executes one handler at a time. That makes its mutable state easy
to reason about, but long handlers or a single global server become bottlenecks.
Keep shared handlers short. Partition independent entities across servers and
bound caller concurrency. Avoid assuming that adding BEAM schedulers makes one
serialized handler parallel.

The acceptance suite runs 32 clients, each making 32 updates, against one server
and requires the final count to be exactly 1,024. It then deliberately crashes
the server, waits for a replacement PID, verifies the restored initial state,
and checks that new requests work. A separate test verifies that a monitored
worker's failure does not kill its owner or consume unrelated typed messages.

```sh
bin/linglang-selfhost test --json --gc-stress tests/concurrency
bin/linglang-selfhost test --json --no-opt --gc-stress tests/concurrency
```

## Recovery is not durability

Supervision restores a failed process according to its restart policy. Its new
instance gets the original state snapshot, not the state just before the crash.
Old PIDs remain old; callers must look up the replacement. Retry only when the
operation's semantics permit it. Persist data that must survive failure, and
design application protocols for duplicate requests and uncertain outcomes.

Current process destinations are local PIDs. Distributed-node operations, general
backpressure APIs, socket ownership transfer, concurrent network acceptor tools,
durable queues, and hot upgrade contracts remain future work. The small HTTP
example serializes connections and is not a scalable web-server architecture.

Before claiming capacity, run representative traffic on the intended OS/OTP and
hardware. Measure throughput, tail latency, memory, queue growth, and recovery
under worker failure, dependency slowdown, overload, and shutdown. Keep those
measurements separate from correctness tests and compiler fixed-point results.
