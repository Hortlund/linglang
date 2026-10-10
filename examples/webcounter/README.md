# A browser application backed by SQLite

From the repository root:

```sh
make compiler sqlite
export PATH="$PWD/bin:$PATH"
linglang run examples/webcounter 8080 counter.sqlite
# Or: linglang-bootstrap run examples/webcounter -- 8080 counter.sqlite
```

Open http://127.0.0.1:8080 and click Add one. Stop and restart the program: the
count persists. The language owns the application, SQL, and HTTP handling; OTP
supervises the listener. The SQLite helper uses the native SQLite library.

This local demo serializes connections, binds only loopback, accepts no request
bodies, and closes each connection after one response. It is not an
internet-facing web framework. The HTTP source package enforces a total header
deadline and an 8 KiB header limit. Keep the SQLite helper on PATH when running
packaged builds. Use a new database path to reset the demo.
