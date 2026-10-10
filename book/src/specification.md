# Language specification (draft)

Status: implementation-oriented draft, October 2026. This specifies the shared
implemented core unless a compiler difference is explicitly stated. It is not
a claim of Go compatibility or a stable, complete language standard. Compiler
tests remain executable evidence; disagreements between this draft and observed
behavior should be reported as defects rather than silently treated as extensions.

## Source and program structure

Source files use the `.lang` extension and begin with a `package` declaration. Executable roots use `package main`;
imported libraries use another package name. Line and
block comments use `//` and `/* ... */`. Identifiers, literals, punctuation, and
semicolon insertion follow the supported Go-style syntax. Newlines normally
terminate declarations and statements; opening braces belong on the preceding
construct's line. The bootstrap lexer tracks physical file, line, and column.

A program contains functions, named struct types, and constants. Package-level
mutable variables, methods, interfaces, and user-defined generic
declarations are outside the implemented core. Files in a directory share one
namespace. Relative imports link explicit package dependencies; the
[module contract](modules.md#module-contract) defines visibility and resolution. Duplicate declarations are errors. Directory loading includes only
immediate `.lang` files, sorted by filename, excluding `_test.lang` during normal
builds. Explicit CLI file arguments load those files rather than their siblings;
import declarations always target package directories. Import aliases are
file-local qualifiers and require a member selector, never a bare reference.

Names resolve within their declaring package and lexical scopes. Linking cannot
make an unqualified private declaration or a built-in shadow from another package
visible. A blank constant name `_` creates no binding; it still occupies its
position in a constant group for `iota`. Prelude declarations cannot be
redeclared at package scope, including in imported libraries. Map literal keys
resolve lexically, including keys in nested literals with an omitted type;
struct field keys name fields rather than lexical bindings.

Executable programs require one `func main()` with no parameters or result.
Functions may have zero or more typed parameters and at most one result. A
non-void function must terminate appropriately on all reachable paths accepted
by the compiler's flow checker. Both compilers support test compilation without `main`, including library
packages. Only `Test...` functions declared in root `_test.lang` files are tests;
they must take no arguments and return no value. Dependencies exclude test files.

## Types and zero values

| Type | Meaning | Zero value |
| --- | --- | --- |
| `int` | Signed 64-bit integer | `0` |
| `bool` | Boolean | `false` |
| `string` | Byte sequence | `""` |
| Named struct | Product of named fields | Each field's zero value |
| `*T` | Process-local mutable cell/interior pointer | `nil` |
| `List[T]` | Immutable linked sequence | `nil` |
| `Map[K,V]` | Immutable map; K is int or string | `nil` |
| `Pid` | BEAM process identifier | `nil` |
| `Monitor`, `Supervisor`, `Timer`, `Socket` | Owner-local runtime handle | `nil` |

Built-in generic result records include `Delivery[T]` and `CallResult[T]`.
`Delivery[T]` contains `value T` and `ok bool`. `CallResult[T]` additionally
contains `timedOut bool` and `reason string`. Their zero values follow the same
field-wise rule. These records may be used locally with pointer-bearing T,
but actual messages and server replies must satisfy the sendability rules.

Named structs have distinct type identities. Fields are accessed with `.`;
access through a pointer dereferences it. The portable core uses named-field
struct literals. Recursive value layouts require indirection; recursive message
schemas are rejected even where a local type is representable.

Floats, unsigned runtime integer types, arrays, raw slices, raw Go maps, channels,
function values, and interfaces are not part of the supported runtime types.

## Constants and expressions

Constants may be declared individually or in groups, including `iota`.
Untyped integer constant arithmetic can exceed 64 bits, but a value used as an
`int` must be representable. Bool, string, and rune constants are also supported;
runtime rune expressions require an `int` context rather than a separate rune
runtime type.

Integer runtime arithmetic wraps to signed 64-bit two's-complement values.
Integer division truncates toward zero; remainder has the dividend's sign.
Division by zero fails the process. Bitwise operators and shifts are supported.
An `int` shift count must be nonnegative. Shifts of 64 or more yield zero except
for arithmetic right shifts of negative values, which yield `-1`. Untyped
nonconstant shift-count expressions follow the compiler's unsigned 64-bit
lowering; this is a compatibility corner covered by tests, not an additional
public unsigned type.

`&&` and `||` short-circuit. Calls and composite values preserve operand order.
Supported equality applies to primitives, pointers, handles, and structs whose
fields are comparable. Lists and maps cannot be compared to each other, but can
be compared with `nil`. Pointers compare storage identity, not pointee values.
Strings compare byte contents. String `+` concatenates bytes.

## Declarations, statements, and scope

Local variables use `var name Type [= expression]` or `name := expression`.
Blocks establish lexical scopes. A local name can shadow an outer declaration.
Assignments and compound assignments change local storage. `++` and `--` are
statements. Multiple assignment is not implemented by either emitter; use separate
statements.

`if [initializer;] condition { ... } [else ...]` requires a boolean condition.
The initializer executes once before the condition. Its scope includes the
condition, then-branch, and complete else-branch, and ends with the statement.
A returned or otherwise escaped pointer to initializer storage remains valid.

`for` supports condition-only, infinite, and initializer/condition/post forms.
List range evaluates its source once. Variables declared by range have fresh
identity per iteration; assignment-form range updates existing identifiers.
`switch` supports an optional initializer, optional tag, multiple case values,
and at most one default clause. The first matching case executes. Cases have
their own scopes and do not fall through. `break` targets the nearest loop or
switch; `continue` targets the nearest loop. Labels and `fallthrough` are absent.

`return` exits the current function. `panic(string)` fails the process, and
`assert(false)` fails with a source location. General exception handlers,
`defer`, closures, and multiple return values are not supported.

## Values and ownership

Structs, lists, and maps have value/snapshot semantics. Copying a struct or
collection does not duplicate storage reachable through a contained pointer:
such pointers retain their identity inside the current process. `&` creates a
pointer to addressable local storage; `*` reads or writes the pointed-to value.
There is no pointer arithmetic or manual free operation. Dereferencing nil or
an invalid pointer fails.

Every managed pointer and owner-local handle belongs to one process. Sendable
types comprise primitives, PIDs, structs with sendable fields, lists with
sendable elements, and maps with supported keys and sendable values. Pointers,
Monitor/Supervisor/Timer/Socket handles, and recursively defined message schemas cannot
cross a process boundary. The same restrictions apply to initial server state.

## Concurrency and failure

Execution within a process is imperative: mutable locals and references are
process-local, and statement order is preserved by lowering. BEAM schedules
processes; OTP implements supervision and servers. These platform properties do
not imply bounded mailboxes, durable state, distributed-node support, or a
capacity guarantee. Applications must bound admitted work and define retry and
persistence semantics; see [concurrency and scale](scaling.md).
Application launchers use OTP's scheduler defaults and operator configuration;
the language does not promise a fixed scheduler count or scheduling order.

`spawn` starts a named one-argument function with a sendable value. `send` and
`receive[T]` use complete static schemas for mailbox selection and runtime
validation. Nonmatching messages remain in the mailbox. Destinations must be
local PIDs. There is no static relation between a `Pid` and its expected protocol.

Timeouts for receive, wait, and call use milliseconds: `-1` means infinite,
`0` means no waiting, and positive values up to 2,147,483,647 are accepted.
Invalid timeout values fail the process. Delayed sends require a nonnegative
delay. Expiration does not cancel remote work.

`superviseServer` creates a native OTP server with a named handler of type
`func(*State, Request) Reply`. Requests are serialized. The state cell survives
between callbacks. Both request and reply schemas must match before invocation;
otherwise the call returns `protocol_mismatch`. Failure results contain the
reply's zero value. `calling_self` rejects synchronous self-calls. Timeout is
distinguished from a server that exits with a reason named `timeout`.

OTP supervises restarts according to `ChildOptions`. The default policy is
permanent, the default shutdown timeout is 5,000 ms, and a restarted server gets
the original initial-state snapshot and a new PID. Exhausting a supervisor's
restart budget shuts it down and may terminate its linked owner. No persistence,
distributed API, automatic retry, cast handler, or info handler is implied.

## I/O contracts

TCP operations expose owner-local `Socket` handles and explicit result records.
Sockets are passive byte streams, not typed Linglang mailboxes. Reads/writes
require framing chosen by the application. Explicit close releases a handle;
normal owner completion closes all sockets and process death also releases them.
A closed/foreign handle is a programming error. Failed OS operations return a
reason. See [the I/O chapter](io.md) for sizes, timeouts, and API signatures.

`sqliteQuery` accepts exactly one SQL statement and a positional list of typed
`SQLValue` parameters. Each call opens a new connection, binds values through
SQLite's C API, and closes it. SQL result cells distinguish NULL, integers,
floats, text, and blobs; floats are decimal text because Linglang has no runtime
float type. NULL is distinct from empty text. Multi-call transactions and
persistent `:memory:` connections are not supported. A failure or timeout does
not establish whether a write committed; callers must not retry blindly.

## Implementation limits

The seed uses Go's parser and type checker followed by Linglang-specific
validation. The self-built checker and emitter implement a narrower subset;
see [the matrix](status.md). A syntactically valid Go construct is not thereby
valid Linglang. Diagnostic wording, generated Erlang, archive internals, and
managed-cell statistics are implementation details rather than stable interfaces.


## Tooling contract

Both drivers provide project creation (`init`) and source formatting (`fmt`).
The self-hosted formatter accepts the bootstrap parser's syntax without requiring
successful name/type checking, preserves comment and literal bytes, and verifies
that formatting leaves the parsed syntax tree unchanged. It uses a deterministic,
idempotent whitespace style distinct from the seed's Go printer; formatting is
not part of program semantics. `fmt --check` never writes. All selected files are
parsed/formatted before any replacement; later I/O failures can leave earlier
files updated. See [developer tools](developer-tools.md) for traversal and layout.

`init` uses embedded templates, accepts a new or empty directory, rejects an
existing final symlink, and creates files exclusively. It does not initialize Git,
fetch dependencies, or execute the generated program. Both implementations emit
the same template bytes.

The language has one source syntax for human and automated contributors. Tool
output formats do not change program semantics. Both drivers support version-1
JSON checks (`check --json`), static test-package checks (`check --tests`), and
JSON Lines test events (`test --json`) as specified in
[Working with humans and agents](agent-workflows.md). Capability discovery
(`describe [--json]`) works in both drivers and describes the invoked driver.
`deps [--check] [--json]` records or verifies the version-1 imported source closure;
see [modules](modules.md) and [agent protocols](agent-workflows.md). Structured locations refer to
physical source with byte offsets, independent of line directives or editor
encodings. Error categories describe the rejecting stage; independent frontends
may report a different first error for the same invalid program.

A structured test run reports ordered start/end events and a final suite summary.
Pass/fail/timeout status is explicit; timeouts count as failures, later tests still
run, and failure yields a nonzero exit. Program output is streamed to stderr in
JSON mode. Compiler/discovery failures report a diagnostic and an unsuccessful
zero-count summary. Interrupted output is incomplete, never implicit success.
