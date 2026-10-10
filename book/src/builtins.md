# Built-in API reference

These names are supplied by the compiler without imports. `T`, `K`, `V`, `S`,
`Q`, and `R` below denote type parameters of built-ins, not support for arbitrary
user-defined generic declarations. This is an API index; the specification and
learning chapters define ownership, timeouts, and failure behavior.

## Collections

| Operation | Result |
| --- | --- |
| `prepend[T](value, items)` | New `List[T]` |
| `append(items, values...)` | New list; also accepts `append(items, more...)` |
| `head[T](items)` | `Delivery[T]` |
| `tail[T](items)` | List without its first element |
| `get[K,V](items, key)` | `Delivery[V]` |
| `put[K,V](items, key, value)` | New `Map[K,V]` |
| `remove[K,V](items, key)` | Map without that key |
| `len(value)` | Byte length, list length, or map entry count |

## Text and files

| Operation | Result |
| --- | --- |
| `sha256(text)` | Lowercase 64-digit SHA-256 of the exact string bytes |
| `args()` | `List[string]`, excluding executable/compiler arguments |
| `readFile(path)` | `TextResult { value string; ok bool; reason string }` |
| `writeFile(path, text)` | `IOResult { ok bool; reason string }` |
| `parseInt(text)` | `IntResult { value int; ok bool; reason string }` |
| `formatInt(value)` | Decimal string |
| `split(text, separator)` | List of byte substrings; separator must be nonempty |
| `trim(text)` | Text without surrounding ASCII whitespace |
| `byteAt(text, index)` | Byte as int; checks bounds |
| `slice(text, start, end)` | Byte substring with exclusive end; checks bounds |
| `join(parts, separator)` | Joined string |
| `runeAt(text, offset)` | `RuneResult { value int; width int; ok bool }` |
| `isLetter(value)`, `isDigit(value)` | Unicode character classification |
| `print(...)`, `println(...)` | Output; println separates arguments and adds newline |
| `assert(condition)`, `panic(reason)` | Assertion or process failure |

File functions read/write whole contents. `sha256` hashes exact bytes (including
NUL and non-UTF-8 data) using OTP crypto; it is a digest, not a password hash. `writeFile` overwrites existing files
and does not create missing parent directories. `parseInt` accepts a complete
signed decimal string within int64 bounds; trim whitespace explicitly.

## Processes and timers

| Operation | Result |
| --- | --- |
| `self()` | Current `Pid` |
| `spawn[T](worker, value)` | Worker `Pid` |
| `spawnMonitor[T](worker, value)` | `Process { pid Pid; monitor Monitor }` |
| `send[T](pid, value)` | Sends typed snapshot |
| `receive[T](timeout)` | `Delivery[T]` |
| `monitor(pid)` | Owner-local `Monitor` |
| `wait(monitor, timeout)` | `Exit { pid Pid; ok bool; normal bool; reason string }` |
| `demonitor(monitor)` | bool |
| `sendAfter[T](pid, value, delay)` | Owner-local `Timer` |
| `cancelTimer(timer)` | bool; cancellation can race delivery |

Explicit type arguments are required where no argument determines T, such as
`receive[int](1000)`, and where an untyped nil needs context. A timer does not
keep its sender alive. Cancellation does not remove an already-delivered message.

## Supervision and synchronous servers

| Operation | Result |
| --- | --- |
| `startSupervisor(maxRestarts, withinSeconds)` | Owner-local `Supervisor` |
| `supervisorPid(supervisor)` | Shareable supervisor `Pid` |
| `supervise[T](supervisor, name, worker, value, options)` | `Child` |
| `superviseServer[S,Q,R](supervisor, name, handler, initial, options)` | `Child` |
| `lookupChild(supervisor, name)`, `restartChild(supervisor, name)` | `Child` |
| `stopChild(supervisor, name)`, `removeChild(supervisor, name)` | bool |
| `stopSupervisor(supervisor)` | bool |
| `call[R,Q](pid, request, timeout)` | `CallResult[R]` |

`Child` has `pid Pid`, `ok bool`, and `reason string`. `ChildOptions` has
`restart string` and `shutdownMillis int`; zero fields select defaults. Calls
usually use `call[Reply](pid, request, timeout)` with Q inferred. For nil:
`call[List[int], List[int]](pid, nil, 1000)`.

## Network and database I/O

The [I/O chapter](io.md) specifies socket ownership, framing, typed SQL values,
and cancellation semantics. `tcpListen`, `tcpConnect`, and `tcpAccept` return
`SocketResult`. `tcpPort` returns `IntResult`; `tcpRead` returns `TextResult`;
`tcpWrite` returns `IOResult`; `tcpClose` returns bool. `monotonicMillis` returns
an int for elapsed-time measurements. `sqliteQuery` returns `SQLResult` and
accepts `List[SQLValue]` parameters.

## Filesystem tools

| Operation | Result |
| --- | --- |
| `pathInfo(path)` | `PathInfo { kind string; ok bool; reason string }`; does not follow the final symlink |
| `readDirectory(path)` | `FilesResult`; sorted immediate entry names, including hidden entries |
| `canonicalPath(path)` | `TextResult`; absolute existing path with symlinks resolved (up to 40 links) |
| `makeDirectories(path)` | `IOResult`; recursively create directories, or succeed if present |
| `writeNewFile(path, text)` | `IOResult`; atomic exclusive publication, refusing existing files and links |
| `replaceFile(path, expected, text)` | `IOResult`; replace a regular file atomically, preserving permission bits |

`pathInfo.kind` is the OTP file kind, normally `regular`, `directory`, or `symlink`.
Missing inputs return `ok: false` with an OS reason such as `enoent`.
`replaceFile` requires a path whose final component is a regular file, compares
current bytes with `expected` before writing and again before rename, and refuses
stale content. This detects ordinary intervening edits; it is not a filesystem
transaction or a lock against concurrent writers. Multi-file operations may
partially complete on an I/O failure. Tools own traversal and editing policy.

## Compiler bridge

`sourceFiles(paths)` returns `FilesResult { value List[string]; ok bool; reason string }`.
`buildProgram(source, path, inputs, stress, stats)` compiles generated Erlang and
packages an executable. `runProgram(source, arguments, stress, stats)` builds
and runs generated Erlang in a child VM. Both return `IOResult`.

`moduleFiles(path)` returns the same `FilesResult`, requires a package directory,
and excludes `_test.lang` files. It rejects individual file targets.
`testFiles(paths)` includes `_test.lang` in directory discovery.
`runTestProgram(source, arguments, stress, stats, timeoutMillis)` runs generated
Erlang with a positive bounded VM lifetime and returns `IOResult`.
`modulePath(filename, relative)` returns a normalized absolute path for import
identity. These are compiler bridges, not a general filesystem API.

These are bootstrap infrastructure, not general Erlang interoperability APIs.
They accept compiler-generated Erlang source, not `.lang` source. The `inputs`
list protects source paths from being overwritten by a build.


`runTestProgramResult(source, arguments, stress, stats, timeoutMillis, stderrOutput)`
returns `TestRunResult { ok bool; timedOut bool; reason string }`. With
`stderrOutput: true`, child stdout joins stderr using `/bin/sh` on Linux/macOS;
stdin and the independent parent-lifetime guard remain intact. `timedOut` is a
native status flag, not a classification inferred from reason text. Packaging
precedes the deadline. The older `runTestProgram` retains its `IOResult` contract.

`toolReport(report, success)` writes the already-serialized report plus a newline.
If success is false, it raises a reported-failure marker that the CLI entry
wrapper maps to exit 1 without printing a duplicate diagnostic. Normal root
cleanup still runs. It neither validates JSON nor classifies errors: report
construction, diagnostics, and test discovery belong to the Linglang driver.
