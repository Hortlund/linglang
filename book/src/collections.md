# Collections and errors

Lists and maps are immutable values. Updating a collection returns a new
snapshot; assign the result to keep it. This makes sending values between BEAM
processes straightforward.

```go
{{#include ../examples/collections.lang}}
```

The output is `2 3`, then `7`, then `42`. Prepending a label did not change
`previous`. A missing map key and a failed integer parse are ordinary result
values. Check `.ok` before using `.value`; zero alone does not indicate success.

`List[T]` supports `prepend`, `head`, `tail`, `append`, `len`, and `range`.
`head` returns `Delivery[T]` with `value` and `ok`. Nil and empty lists both
behave as empty, although only nil compares equal to `nil`.

Lists are linked lists: prepend/head/tail are constant time, while length and
append traverse the list. Build large sequences by prepending when practical.
There is no indexing or list slicing. Range over a literal using parentheses:
`for _, n := range (List[int]{1, 2}) { ... }`.

`Map[K,V]` accepts `int` or `string` keys. Use `get`, `put`, and `remove`, not
index syntax. `get` returns `Delivery[V]`. There is no map iteration yet.

Collection snapshots can contain local pointers. In that case, the collection
structure is immutable but the pointed-to storage remains mutable. Such values
cannot be sent to another process. See [ownership](memory.md).

Expected failures use result records. Invalid operations, `assert(false)`, and
`panic("reason")` fail the current process. The language has no general
try/catch or defer syntax. Put recovery boundaries around processes using OTP,
and represent recoverable application errors explicitly in response structs.

**Exercise:** look up a missing key and parse a non-number. Print both `.ok`
values and the parse `.reason`. Add a fallback without panicking.
