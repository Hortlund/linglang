# Memory and process ownership

BEAM manages native terms and process heaps. Linglang adds mutable cells because
the source language permits aliasing and assignment through pointers.

A managed pointer records the owning process, a cell identity, and an optional
field path. The cell is stored in that process's dictionary. Field pointers keep
their owning cell alive. Cross-process access is invalid and is excluded by
static message checks as well as runtime ownership checks.

The managed collector maintains lexical root frames. Generated code retains
pointer-bearing operands before operations that can collect. A collection marks
reachable cells and removes unreachable cells from the dictionary. BEAM can then
reclaim their underlying terms. Rooting and BEAM heap management solve different
parts of the same lifetime problem.

An escaped local pointer remains valid because the caller's roots retain its
cell. Returning from a block does not require freeing every cell allocated in
that block. Conversely, leaving a temporary cell in the dictionary forever would
prevent BEAM from reclaiming it; this is why managed collection is necessary.

Statically pointer-free cells do not need recursive tracing of their contents.
The collector still roots the cell itself. Struct descriptors can trace only
pointer-bearing fields. These decisions must depend on proven type information,
not a guess about current runtime contents.

## Server state

OTP returns to its own loop between callbacks. A server therefore holds one
persistent root frame for its state cell. Generated handler functions add and
unwind ordinary temporary frames. Successful calls collect transient cells;
orderly termination clears the persistent frame and finishes owned resources.

Server state must be sendable, so it contains no managed pointers. Its cell is
rooted with content tracing disabled. Otherwise every small lookup in a large
store would scan the entire store during managed collection. A reduction-count
regression test checks that a thousandfold state-size increase does not cause
request collection to grow proportionally.

## Native resources

Sockets belong to their creating process and cannot be sent, stored in sendable
server state, or used by another process. `tcpClose` is explicit and idempotent;
normal process completion also closes registered sockets. Untrappable process
death lets OTP release its owned sockets. To parallelize a service, let workers
create and own their own resources; socket transfer is not implemented yet.

SQLite connections live in a separate short-lived native helper. They cannot
be stored in language values. Closing the port or losing the parent causes the
helper's lifetime guard to exit; SQLite and the OS release connection resources.
Timeout can race a commit, so resource cleanup is not a rollback guarantee.

## Observing failures

A process dying destroys its process-local terms and dictionary. OTP may restart
a supervised process with a fresh state snapshot. Root correctness cannot make
that snapshot durable. Persistence requires an explicit storage design that is
not implemented by the server adapter.

Use `--gc-stress --gc-stats` while testing. Zero final live cells, root entries,
and root frames are useful cleanup checks, but they are not a proof that an
application has no native-term retention or mailbox growth.
