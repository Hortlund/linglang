# Values, mutation, and control flow

Linglang has `int`, `bool`, and `string`. An `int` is signed 64-bit; runtime
overflow wraps. Strings store bytes, including UTF-8, and `len(text)` counts
bytes. There are no floating-point types yet.

Declare a variable with `name := expression` or `var name Type`. A declaration
without an initializer uses the type's zero value. Assignments, compound
assignments, `++`, and `--` update variables. The assignment is local to the
current process; another process cannot share that variable's storage.

```go
{{#include ../examples/values.lang}}
```

This prints `7` and then `16`. `pointer` and `total` refer to the same local
storage. Returning a pointer to a local variable is supported: the runtime keeps
the storage alive while the pointer remains reachable.

`if` may have an initializer before its condition. The initializer runs once;
its declarations are visible in the condition and both branches, including an
`else if`, but not after the statement. A declaration can shadow an outer name.
This works in both the seed and self-built compilers.

Use `for condition { ... }`, `for { ... }`, or a three-part loop as above.
`break` exits the nearest loop or switch; `continue` starts the next iteration
of the nearest loop. A `switch` evaluates its tag once, checks cases in source
order, and never falls through. Tagless switches test boolean cases.

Functions have typed parameters and zero or more unnamed results. Use a named
struct when returned fields need names or a reusable value type. Field access through a pointer
automatically dereferences it. User-defined generic functions, methods, closures,
and interfaces are not available.

**Exercise:** add an `else` branch to the example, then shadow `total` in the
initializer. Verify which variable changes. Run again with `--no-opt` to compare
the reference lowering with the optimized compiler.

## Multiple results and simultaneous assignment

Use multiple results for a small group of values returned together:

```go
{{#include ../examples/multiple.lang}}
```

This prints `21 answer` and then `9 2`. Assignment evaluates every destination
reference and right-hand value before writing any destination, so a swap does
not need a temporary variable. Writes happen from left to right. `a, a = 1, 2`
leaves `a` equal to `2`.

`n, text := answer()` declares both names. If `n` already exists in the same
scope, it updates `n` and declares `text`; at least one nonblank name must be
new. An outer `n` is shadowed instead. `_, text = answer()` evaluates the call
and discards its first result. `var x, y int` initializes both variables to zero.

A sole call can forward all its results with `return answer()` or supply an
ordinary function's arguments with `consume(answer())`. Multiple results cannot
be stored as a tuple or combined with additional arguments. Built-ins and native
APIs require you to bind the results first. Named return variables and variadic
functions are not implemented. See the [specification](specification.md) for
the precise scope, evaluation, and failure rules.
