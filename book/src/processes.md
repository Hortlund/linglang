# Processes and OTP servers

BEAM processes are independent execution contexts with mailboxes. They are not
OS processes, and their language pointers cannot cross a process boundary.
`spawn(worker, value)` starts a named one-argument function. `send(pid, value)`
sends a typed snapshot. `receive[T](milliseconds)` selects a message with the
matching schema; unrelated messages stay queued.

`spawnMonitor`, `monitor`, and `wait` expose process failure. A monitor observes
failure; a supervisor decides when to restart a child. Linglang's supervisors
are native OTP supervisors, not a second restart system.

## A supervised counter

The project generator creates this handler in `counter.lang`:

```go
{{#include ../../cmd/linglang/starter/counter.lang}}
```

And this entry point in `main.lang`:

```go
{{#include ../../cmd/linglang/starter/main.lang}}
```

The supervisor allows three restarts within five seconds. `superviseServer`
starts a native OTP `gen_server`; OTP serializes requests. The handler receives
a pointer to persistent, process-local state and returns a reply. Both the state
contents and the request/reply types must be sendable.

`call[int]` specifies the reply type and infers the request type. The result has
`value`, `ok`, `timedOut`, and `reason`. A successful call has an empty reason.
Failure returns the reply's zero value. The request and reply schemas are both
checked before a handler is invoked, because a plain `Pid` has no static protocol.

Timeouts use milliseconds: `0` polls and `-1` waits indefinitely. A timeout
abandons the reply, **not the operation**. Do not automatically retry a mutation;
the server may already have applied it. Late replies are discarded through OTP
aliases and cannot be mistaken for the response to a later call.

## Failure and recovery

The default restart policy is `permanent`. `ChildOptions` also permits
`transient` and `temporary`. A restarted server receives the original initial
state snapshot. It gets a new PID; use `lookupChild` before addressing its
replacement. There is no persistent storage or automatic retry in this API.

`stopSupervisor` shuts down its children. Owners also close their supervisors
when the owning process finishes. Exceeding a supervisor's restart budget can
terminate its linked owner. Keep the owner alive for as long as its service
should run.

For a larger worked example, run `examples/store.lang` from the repository.
It starts four clients, crashes a store, observes its replacement, and shows
that the initial snapshot was restored. The OTP crash report is expected.

**Exercise:** make two calls to the counter and observe accumulated state.
Then start a second counter under another child name. Verify their state is
independent. Avoid a crash exercise until you can explain the restart policy.
