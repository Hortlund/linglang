# SQLite port

Build with `make sqlite` (a C compiler, SQLite headers/library, and pthreads are
required). Put `bin/` on PATH when running programs using `sqliteQuery`.
The compiler and ordinary programs do not need this helper.

The helper uses `sqlite3_prepare_v2` and typed `sqlite3_bind_*` calls; values are
never interpolated into SQL. Exactly one statement executes per invocation,
using a new connection. File databases persist; `:memory:` cannot persist across
calls. Transactions spanning calls are not available. Each statement uses normal
SQLite atomicity; timeout or output-limit failure does not promise rollback of
an already committed write.

A length-prefixed binary protocol preserves NUL bytes in text/blob parameters
and results. NULL is distinct from empty text. Input/output are capped at 8 MiB.
The parent deadline, helper alarm, and stdin lifetime guard bound execution and
terminate work when the owning BEAM port closes. SQLite lock waiting is capped
at five seconds (or the smaller requested timeout). No shell is launched.
The lifetime guard waits on the input descriptor without holding a stdio stream
lock, so normal helper exit can flush its response while the parent pipe is open.

This is a native optional dependency. Escript archives and runtime releases do
not bundle this helper or libsqlite3; install it on the receiving machine.

Contracts: [SQLite preparation](https://www.sqlite.org/c3ref/prepare.html) and
[parameter binding](https://www.sqlite.org/c3ref/bind_blob.html).
