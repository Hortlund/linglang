# Networking and SQLite

Linglang now exposes a narrow typed I/O boundary. TCP uses OTP's passive sockets;
SQLite uses bound parameters through a small native port. Neither API exposes
arbitrary Erlang terms to the language.

## Passive TCP

| Operation | Result |
| --- | --- |
| `tcpListen(address, port)` | `SocketResult`; numeric IPv4/IPv6 bind address, port 0 chooses an ephemeral port |
| `tcpPort(socket)` | `IntResult` containing the local port |
| `tcpConnect(host, port, timeout)` | `SocketResult`; hostname/IPv4 destination |
| `tcpAccept(listener, timeout)` | `SocketResult` for a connection |
| `tcpRead(socket, bytes, timeout)` | `TextResult`; exact positive byte count, or 0 for an available chunk |
| `tcpWrite(socket, data, timeout)` | `IOResult` |
| `tcpClose(socket)` | bool; false if already closed |
| `monotonicMillis()` | Monotonic milliseconds for elapsed-time deadlines |

`SocketResult` has `value Socket`, `ok bool`, and `reason string`. Handle use is
owner-local. Ordinary connection, timeout, and OS failures are result values;
using a foreign, nil, or closed socket is a programming error. Explicit close is
idempotent, and owner completion also releases sockets.

Timeouts use the normal millisecond convention, including `-1` for infinity.
Read size must be 0–1,048,576. A size of zero does not establish a message boundary:
TCP is a stream. The peer may split a write across reads or combine writes.
On a write timeout the socket closes, and some bytes may already have arrived.
There is no TLS, socket transfer between processes, or general async socket API.

The `stdlib/http` source package demonstrates framing over TCP. It accepts one
HTTP/1.1 request per connection, caps headers at 8 KiB, enforces a total deadline,
and rejects bodies, transfer encoding, expectations, and upgrades. It is a local
control-endpoint subset, not a complete HTTP server framework.

## SQLite setup and values

From the repository root:

```sh
make compiler sqlite
export PATH="$PWD/bin:$PATH"
```

Building the helper requires a C compiler, pthreads, and SQLite development
headers/library. Running SQLite programs requires `linglang-sqlite` on PATH and
the compatible platform SQLite library. The compiler itself does not need this
helper for programs that do not call SQLite. Packaged programs do not bundle it.

`sqliteQuery(path, statement, parameters, timeout)` returns:

```go
type SQLValue struct { kind string; value string }
type SQLResult struct {
    columns List[string]
    rows List[List[SQLValue]]
    changes int
    ok bool
    reason string
}
```

These types are built-ins; do not redeclare them. Parameters are
`List[SQLValue]`, bound by position to placeholders (`?`, `?1`, or named SQLite
parameters in their assigned index order). Values are never concatenated into
SQL. `kind` is `text` (also the empty default), `int`, `float`, `blob`, or `null`.
Integers and floats use their string representations; results preserve the type
tag. Text/blob bytes, including NUL, are length-delimited. NULL differs from
empty text. `stdlib/sqlite` provides `Text`, `Int`, `Blob`, and `Null` constructors.

```go
result := sqliteQuery("app.sqlite", "SELECT ? AS name, ? AS count",
    List[SQLValue]{SQLValue{value: "Ada"}, SQLValue{kind: "int", value: "42"}}, 1000)
assert(result.ok)
```

Exactly one statement is accepted; a second statement is rejected before either
executes. A fresh connection is opened and closed for each call. File databases
persist. `:memory:` does not persist across calls, and multi-call transactions
are unavailable. Each statement follows SQLite's transaction behavior.

Input and output are limited to 8 MiB. SQLite lock waiting is capped at five
seconds or the shorter requested timeout. A deadline also covers execution;
closing the owner port or losing its VM terminates the helper. A timeout or
output error can race a committed write. Inspect application state rather than
blindly retrying mutations. Large result sets should use pagination.

## Build a browser application

```sh
linglang run examples/webcounter 8080 counter.sqlite
# Self-hosted alternative:
linglang-bootstrap run examples/webcounter -- 8080 counter.sqlite
```

Open `http://127.0.0.1:8080`. The page reads a counter from SQLite; its button
posts an increment. Restart the program and the value remains. Linglang handles
application logic and SQL, a source package handles HTTP framing, and OTP
supervises the listener.

This is a complete small local application. It serializes connections and binds
loopback. It is not evidence of a production-ready full-stack framework. TLS,
authentication, general HTTP request bodies, connection pools, migrations, and a
remote package registry remain separate work.
