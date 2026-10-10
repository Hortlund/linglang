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

Functions have typed parameters and at most one return value. Use a struct for
several results. Named structs group fields; field access through a pointer
automatically dereferences it. User-defined generic functions, methods, closures,
and interfaces are not available.

**Exercise:** add an `else` branch to the example, then shadow `total` in the
initializer. Verify which variable changes. Run again with `--no-opt` to compare
the reference lowering with the optimized compiler.
