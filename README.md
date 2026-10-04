# linglang

![linglang language](https://img.shields.io/badge/language-linglang-7B3FE4)

a programming language that answers the question nobody fucking asked.

called linglang because i wanted to learn Chinese but somehow installed Erlang instead.

Go writes Erlang. Erlang runs on BEAM. BEAM runs on your computer.
your computer did not agree to any of this.

## installation

bring Go 1.23+ and Erlang/OTP. `erl` and `erlc` must be on your PATH.
if you don't have a PATH, go outside and find one.

```sh
go run ./cmd/linglang run examples/counter.lang
```

you have now installed a situation.

## production ready

no ❤️

## bean ownership

linglang supports one bean.

a bean can have three owners. this is safe because none of them get the bean.
if a process disagrees with `false`, it may send back `true`. the disagreement
is now on a different computer. technically the same computer. different guy.

```sh
go run ./cmd/linglang run examples/beans.lang
```

```text
beans: 1
guy: mine anyway
other guy: mine anyway
another guy: mine anyway
owners: 3
beans: 1
bean status: bean
```

three real BEAM processes, typed messages, maps, and an OTP supervisor.
the supervisor makes sure all three guys stop. the bean does not need to stop.
it was never doing anything.

for more beans, run the program again.

## dying

other languages allow a process to die. linglang allows it to die again.

```sh
go run ./cmd/linglang run examples/fuck.lang
```

among the actual OTP crash reports, you will find:

```text
fuck
fuck
fuck
supervisor: fuck it
```

the worker prints `fuck` and panics. a native OTP supervisor replaces it.
the replacement has the same instructions. unfortunately the instructions are `fuck`.

two restarts are allowed within five seconds. the third death exhausts that
budget, shuts down the supervisor, and exits its owner process. main monitors
the owner and survives. this program exits successfully. it has achieved everything
it set out to do.

## numbers

`int` is a signed 64-bit integer. it goes from -9223372036854775808 to
9223372036854775807. going further wraps around. you have arrived at the other number.

there are no floats. decimals were a mistake.

bitwise `&`, `|`, `^`, and `&^` work, including compound assignments.
`<<` and `>>` shift signed integers. shifting by 64 or more gives zero,
except that a negative number shifted right stays at `-1`. untyped count
expressions use unsigned 64-bit arithmetic; negative `int` counts fail the
process. the bits have left the building.

rune literals work as constants and in `int` contexts, like
`var x int = 'a' << n`. runtime expressions that default to `rune` (32 bits)
are rejected. give the rune an `int` job before it starts moving bits.

`bool` is `true` or `false`. for a third opinion, start another process.

`string` stores bytes, including UTF-8 text. it does not know what the text means.
neither does the compiler.

## variables

variables can vary. we checked.

```go
n := 1
n = 2
n++
println(n) // 3. this is getting out of hand.
```

use `var n int` to start at zero. bool starts at `false`, string at `""`,
and pointers and process handles at `nil`. structs start with zero in each field.
the computer has done the bare minimum.

## constants

`const` is for numbers that have stopped cooperating.

```go
const One = 1
const (
    Ask = iota + 1
    Threaten
    Forget
)
println(One, Ask, Threaten, Forget) // 1 1 2 3
```

`iota` counts the declarations so you don't have to. your contribution is naming them.
constants work at package and function scope, in groups, with constant expressions
and multiple names. supported types are `int`, `bool`, `string`, and untyped
integer, rune, bool, and string constants. a large untyped integer can exist at
compile time, but must fit in an `int` when used as one. big number, small door.

## decisions

`switch` checks cases from left to right. the first match wins. the argument is over
before the other cases get a turn.

```go
const (
    Ask = iota + 1
    Threaten
    Forget
)
switch demand := Threaten; demand {
case Ask, Threaten:
    println("no")
case Forget:
    println("what argument")
default:
    panic("unrecognized nonsense")
}
```

the tag is evaluated once. multiple case values, an initializer, and tagless
`switch { case hungry: ... }` work. `default` runs if no case matches, even if
you put it first. it knows its place.

each case gets its own scope. `break` leaves the nearest loop or switch.
`continue` goes to the next loop iteration, including from inside a switch.
cases don't fall through. `fallthrough`, labelled branches, and type switches
are unsupported. please fall somewhere else.

## lists

lists happen when there is more than one thing. deeply unfortunate.

```go
items := List[string]{"thing"}
oldItems := items
items = append(items, "thing")
items = prepend("suspicious thing", items)
for i, item := range items {
    println(i, item)
}
println(len(oldItems)) // 1. the past refuses to help.
println(head(items).value, head(items).ok)
items = tail(items)
```

`List[T]` is an immutable native Erlang list. assignment and function arguments
keep their snapshots. structs, lists, maps, process IDs, and local pointers can
go in a list. putting a pointer in a list does not make the object immutable.
it is still the same guy.

`append(items, value, ...)`, `append(items, more...)`, and `prepend(value, items)`
return new lists. `head(items)` returns `.value` and `.ok`; an empty head gives
the element's zero value and `false`. `tail` drops the head and leaves empty
lists alone. `len` counts the things. apparently we need a function for that.

prepend, head, and tail take constant time. append copies the left list.
len walks it. range traverses once. where you put the thing affects how fast
you find it. tragic.

`var items List[int]` is nil. `List[int]{}` is empty but non-nil. both behave
as empty lists. one has made slightly more effort.

range accepts index/value bindings, existing identifiers with `=`, blank
targets, or no bindings. its source is evaluated once and kept as a snapshot.
variables declared with `:=` get fresh identity each iteration. when ranging
over a literal directly, put parentheses around it:
`for _, item := range (List[int]{1, 2}) { ... }`. the literal needs a fence.

indexing, indexed writes, slicing, keyed list literals, raw slices, and arrays
are unsupported. you may look at the first item and proceed from there.

## maps

maps let you put shit in them and then ask where the shit is.

```go
pants := Map[string,string]{"contents": "shit"}
cleanPants := put(pants, "contents", "nothing")
println(get(cleanPants, "contents").value) // nothing
println(get(pants, "contents").value)      // shit
cleanPants = remove(cleanPants, "contents")
println(len(cleanPants)) // 0. now there aren't even pants contents.
```

`Map[K,V]` is an immutable native BEAM map. keys are `int` or `string`.
values can be any supported type, including structs, lists, maps, and local
pointers. `put` and `remove` return new snapshots. remember to assign the result,
otherwise you have merely imagined different pants.

`get(items, key)` returns `Delivery[V]`: `.value` and `.ok`. a missing key gives
the value type's zero value and `false`. zero is not evidence that you found it.
`len` counts entries.

`var items Map[int,int]` is nil; `Map[int,int]{}` is empty and non-nil. all helpers
accept both, including inserting into nil with `put`. literal keys and values
are evaluated from left to right; repeated dynamic keys keep the last value.
the earlier value has lost the argument.

use the helpers. map indexing, indexed writes, map range, raw Go maps, and other
key types are unsupported. this map has no roads.

## pointers

two variables can point at the same thing. this saves you from having two things.

```go
n := 1
mine := &n
yours := mine
*yours = 2
println(n, *mine, mine == yours) // 2 2 true. sharing has consequences.
```

structs copy by value; pointers keep the identity of a mutable managed cell.
you can return a pointer to a local. the garbage collector follows pointers
through structs, lists, and maps, including cycles. unused cells get collected.
the collector cannot fix this README because somebody is still reading it.

pointers stay in their creating process. trying to send one to another process
is a compile error. get your own number.

```sh
go run ./cmd/linglang run examples/references.lang
go run ./cmd/linglang run --gc-stats examples/memory.lang
```

## guys

a process is a guy. `Pid` tells you which guy.

| operation | guy activity |
| --- | --- |
| `self()` | find out which guy you are. |
| `spawn(worker, args)` | start another guy. returns a `Pid`. |
| `spawnMonitor(worker, args)` | start a guy and watch him. returns `.pid` and `.monitor`. |
| `send(pid, value)` | put a typed value in his mailbox. |
| `receive[T](timeout)` | wait for a matching type; returns `.value` and `.ok`. |
| `monitor(pid)` | start watching an existing guy. |
| `wait(watcher, timeout)` | returns `.pid`, `.ok`, `.normal`, and `.reason` when he dies. |
| `demonitor(watcher)` | stop watching him. |

timeouts are milliseconds. `0` checks now. `-1` waits forever, which is plenty
of time to think about what you've done.

messages are value snapshots. their complete type schema is checked, including
list elements and map keys and values. sendable lists and maps also work as
worker arguments. pointers, monitors, and supervisor handles can't cross a
process boundary. recursive local structs, lists, and maps work; recursive
message schemas don't. the envelope would never end.

workers must be named functions taking one argument. closures are unsupported.
you have to name the guy.

```sh
go run ./cmd/linglang run examples/counter.lang
```

four guys count to 4000. one guy keeps the number. none of this required four guys.

## adult supervision

OTP supervisors are real native OTP supervisors. we did not implement our own
because that would require adult supervision.

`startSupervisor(maxRestarts, withinSeconds)` creates one.
`supervise(supervisor, name, worker, args, ChildOptions{...})` adds a named child
and returns `.pid`, `.ok`, and `.reason`. restarts reuse the original argument
snapshot. the replacement remembers its instructions but not what it was doing.

restart policies are `"permanent"` (restart even after a normal return),
`"transient"` (restart after failure), and `"temporary"` (don't restart).
the default is permanent. death is a suggestion.

`lookupChild`, `restartChild`, `stopChild`, and `removeChild` take the supervisor
and child name. `stopSupervisor(supervisor)` shuts down the whole family.
`ChildOptions.shutdownMillis` controls the shutdown timeout. supervisor handles
belong to their owner; share `supervisorPid(supervisor)` if someone needs its PID.

```sh
go run ./cmd/linglang run examples/supervision.lang
go run ./cmd/linglang run --gc-stress examples/jobqueue.lang
```

the job queue squares three numbers and kills a worker once. OTP replaces him
and the queue retries the unfinished job. delivery is at least once, so jobs
with side effects must tolerate retries. do not use this to pay rent twice.

queue state is in memory. worker crashes don't lose it. queue or VM exits do.
your state has left the building.

## accidentally useful

the prime lab reads a file, skips bad jobs, counts primes on three supervised
workers, and writes a CSV. sorry. we tried to keep it stupid.

```sh
go run ./cmd/linglang run examples/prime_lab/ examples/prime_jobs.txt prime-results.csv
```

the first worker deliberately crashes. OTP replaces it. the queue retries its
unfinished job and checks both worker PID and assigned job before accepting a
result. every worker sends duplicate acknowledgements. saying it twice does
not make it twice as done. old workers' replies are ignored too.

the results come out in input order even when jobs finish out of order:

```csv
job,limit,prime_count,largest_prime
1,100,25,97
2,1000,168,997
3,10000,1229,9973
```

use one integer from 2 to 100000 per line. blank lines and `#` comments are fine.
invalid jobs are reported and skipped. no valid jobs means a CSV with just the
header. even doing nothing produces paperwork.

paths are optional: defaults are `examples/prime_jobs.txt` and `prime-results.csv`
relative to your current directory. existing reports are overwritten. the sample
finishes with one worker restart and three active workers at its busiest.
the crash reports are expected. the workers and supervisor stop before exit.

state is still in memory and retries still mean at least once execution.
writing a CSV has not made this a bank.

## more files

your file got too big. put it in several files. now you have several problems.

```text
examples/prime_lab/
    main.lang
    messages.lang
    queue.lang
    workers.lang
```

pass a directory to load its `.lang` files in filename order. every file says
`package main`. functions, structs, and constants can use each other across files.
there must be one `func main()`. two mains must settle this outside.

```sh
go run ./cmd/linglang check examples/prime_lab/
go run ./cmd/linglang run examples/prime_lab/
go run ./cmd/linglang build -o _build examples/prime_lab/
go run ./cmd/linglang emit examples/prime_lab/
```

`check` checks syntax, types, and supported language features without running
anything or writing build files. it only needs Go. the guys stay asleep.
errors point to the file and line that did it.

only immediate `.lang` files count. other files and subdirectories are ignored.
passing a file still loads just that file. `examples/prime_lab.lang` is the
standalone version if you want all your problems in one place. imports remain
unsupported. the folder is one program, compiled into one BEAM module with the
same native OTP supervision.

## take linglang outside

`pack` puts your program and the linglang runtime in one executable escript.
your program is portable now. somebody else's problem.

```sh
go run ./cmd/linglang pack -o bin/prime-lab examples/prime_lab/
./bin/prime-lab examples/prime_jobs.txt prime-results.csv
```

copy the executable and your input data somewhere else and run it there.
it needs a compatible Erlang/OTP installation with `escript` and `erl` on PATH.
it does not need Go, linglang, `erlc`, or the source files. OTP still handles
processes, mailboxes, and supervisor restarts. the guys travelled with the program.

without `-o`, a file `counter.lang` produces `counter.escript` in your current
directory; a directory `prime_lab/` produces `prime_lab.escript`. output parents
are created automatically. an existing executable is replaced after a successful
pack. packing over a source file is rejected. the program cannot eat its own recipe.

`--no-opt`, `--gc-stress`, and `--gc-stats` go before the source path and are
baked into the executable. arguments after the executable's name are all program
arguments, including things that look like flags. `args()` excludes the
executable name in main and workers. relative data paths use the current working
directory; input data is not bundled. bring your own numbers.

on Windows, invoke the package with `escript program.escript [args...]`.
the format is OTP's [archive escript](https://www.erlang.org/doc/apps/erts/escript_cmd.html).
this packages your program, not an Erlang installation.

## bring the whole computer

their computer does not have Erlang. apparently this is allowed.
`release` bundles your compiled program, the BEAM runtime, and the OTP libraries
it uses into a `.tar.gz`. unpack it and run. the guys brought their own house.

```sh
go run ./cmd/linglang release -o dist/prime-lab.tar.gz examples/prime_lab/
mkdir -p _release/prime-lab
tar -xzf dist/prime-lab.tar.gz -C _release/prime-lab
./_release/prime-lab/bin/run examples/prime_jobs.txt prime-results.csv
```

send the archive and your input data. the receiving machine needs neither Go nor
an Erlang installation. native OTP supervision, mailboxes, and worker restarts
still work. the VM is included. we did not evict it.
you can symlink `bin/run` into your PATH too. the launcher remembers where it lives.

this currently supports Linux and macOS. build on the OS and CPU architecture
you will run on, with compatible system libraries on the receiving machine.
it bundles your installed Erlang runtime; it does not cross-compile that runtime
or bundle the operating system. the archive's `README.txt` records the target and
OTP version. Erlang's license is included too. the runtime has paperwork.

without `-o`, `counter.lang` produces `counter-<os>-<arch>.tar.gz`; a directory uses
its directory name. `--no-opt`, `--gc-stress`, and `--gc-stats` work just like
`pack`. all arguments after `bin/run` go to the program, and relative data paths
use your working directory. input data is still your problem.

building requires Go and Erlang/OTP, including `erl`, `erlc`, and OTP's release
tools. output parents are created automatically. a failed release keeps the
previous archive, and source files cannot be used as output. this is an OTP
release for running the program to completion; daemon management and hot upgrades
are not wired up yet.

## outside the computer

files are where bytes live when the program is not looking at them.
these functions are available without imports. imports are currently unavailable,
so this is convenient.

| function | result | what the computer does |
| --- | --- | --- |
| `args()` | `List[string]` | program arguments in order; excludes the source/executable name and compiler flags. |
| `readFile(path)` | `TextResult` | reads the whole file. it has committed to reading. |
| `writeFile(path, text)` | `IOResult` | creates or overwrites a file. parent directories must already exist. |
| `split(text, separator)` | `List[string]` | splits on exact separator bytes and keeps empty pieces. an empty separator raises `linglang_empty_separator`. |
| `trim(text)` | `string` | removes ASCII spaces, tabs, line endings, vertical tabs, and form feeds at both ends. |
| `parseInt(text)` | `IntResult` | parses a complete signed decimal integer, with optional `+` or `-`. no surrounding whitespace. |
| `formatInt(value)` | `string` | turns the number into text. the number is now wearing a string. |

results have `.ok` and `.reason`; `TextResult` and `IntResult` also have `.value`.
failure returns the zero value and a nonempty reason. success has an empty reason.
parse errors are `invalid integer` or `integer out of range` at the signed 64-bit
limits. file failures use OTP names like `enoent` or `eacces`. the computer has
decided to explain itself in small noises.

```go
number := parseInt(trim(" 1 "))
if !number.ok { panic(number.reason) }
println(formatInt(number.value) + " things")
```

strings and file contents preserve bytes, including UTF-8. split matches bytes;
trim handles ASCII whitespace. Unicode spaces get to stay. they came all this way.

linglang can now look at individual bytes. this was a difficult promotion.

| function | what it does |
| --- | --- |
| `len(text)` | counts bytes, including for strings returned by functions. |
| `byteAt(text, index)` | returns the byte at a zero-based index, as an `int` from 0 to 255. |
| `slice(text, start, end)` | returns bytes from start up to, but excluding, end. |
| `join(parts, separator)` | joins a `List[string]` in one pass; nil and empty lists give `""`. |
| `runeAt(text, offset)` | decodes one UTF-8 scalar into a `RuneResult` with `.value`, `.width`, and `.ok`. |
| `isLetter(value)` / `isDigit(value)` | checks Unicode letters / decimal digits by code point. |

byte indexes and slice bounds are checked. invalid bounds fail the current process;
they do not quietly select a different byte. slices preserve bytes and may split
a UTF-8 character. `runeAt` accepts the end offset: `.width == 0` and `.ok == false`
means EOF. malformed UTF-8 gives U+FFFD, width 1, and false; a correctly encoded
U+FFFD gives true. the replacement character has identification.

file operations use the [OTP file module](https://www.erlang.org/doc/apps/kernel/file.html).
CLI arguments are passed separately using [`-extra`](https://www.erlang.org/doc/apps/erts/init.html).
quoted paths with spaces work. shell-looking arguments remain arguments.
the semicolon in your filename does not get a little job.

## other buttons

run commands from the repository root. flags go before the source path.
program arguments go after it. the computer cares about this.

```sh
go run ./cmd/linglang check examples/counter.lang
go run ./cmd/linglang run --gc-stress --gc-stats examples/counter.lang
go run ./cmd/linglang run --no-opt examples/counter.lang
go run ./cmd/linglang build -o _build examples/counter.lang
go run ./cmd/linglang pack -o bin/counter examples/counter.lang
go run ./cmd/linglang release -o dist/counter.tar.gz examples/counter.lang
go run ./cmd/linglang emit examples/counter.lang
```

`--gc-stress` collects at every safe point. `--gc-stats` prints the managed-heap
statistics. `--no-opt` uses the original compiler backend. same program, more cells.
small helpers that cannot allocate managed cells or trigger collection borrow
their caller's roots. fewer scopes around `counter.value++`, same pointer.

`build` writes BEAM modules; `pack` bundles an executable; `release` includes the
VM too; `emit` prints generated Erlang. this is where the Erlang was hiding.

## how fast is the guy

```sh
go run ./cmd/bench
go run ./cmd/bench -compare -samples 21 -warmup 3 -out _build/benchmarks/four-languages.json
go run ./cmd/bench -samples 21 -warmup 3 -workload pointers -out _build/benchmarks/pointers.json
```

benchmarks compare the optimized compiler, the old cell backend, and handwritten
Erlang. arithmetic, calls, structs, pointers, lists, maps, strings, and messages
all have to produce the right answer before they get a number. no points for
being fast and wrong. that is just a calculator with confidence.

`-compare` runs optimized linglang against Erlang, Elixir, and native Go instead.
it needs `elixir` on your path. four languages enter. all four must do the maths.

the timer measures program execution, excluding compilation and runtime startup.
results include median and p95 time, managed cell allocations, and reductions.
raw samples go in `_build/benchmarks/results.json`.
[workloads, measurement details, and a local baseline](benchmarks/README.md).

## things you cannot do

imports, closures, floats, arrays, raw Go slices and maps,
list/map indexing, and map range. this is a Go-like subset with BEAM underneath.
it has `package main`, functions, structs, mutable locals, pointers, and enough
control flow to cause an incident. it is not all of Go. some of Go escaped.

## tests

your code has been accused of being wrong. put the allegations in `math_test.lang`:

```go
package main

func TestMath() {
    assert(1 + 1 == 2)
}
```

```sh
go run ./cmd/linglang test bootstrap/checker
go run ./cmd/linglang test --no-opt --gc-stress --gc-stats --timeout 5m bootstrap/checker
go run ./cmd/linglang test --timeout 5s examples/bean_tests
```

these commands run the linglang checker and bean tests. the checker checks the
checker. somebody has to.

`test` loads the immediate `.lang` files in one directory, including `_test.lang`.
functions in those test files whose names start with `Test` run in name order.
they take no arguments, type parameters, or receivers, and return nothing.
helpers can live in either kind of file. `main` is optional and never runs during
tests. an explicit test file loads just that file. no path means the current directory.
normal directory commands (`run`, `check`, `build`, `pack`, `release`, `emit`)
skip `_test.lang` files. the allegations do not ship with the program.

`assert(condition)` evaluates its bool once. false fails the test with the original
source filename and line. it also works in ordinary programs. the computer has
received a complaint.

each test runs in a fresh BEAM VM. mailboxes, supervisors, and leftover workers
cannot wander into the next test. files still use your working directory.
the default timeout is 30 seconds per test, including VM startup and shutdown;
`--timeout` accepts positive durations such as `5s` or `500ms`. a timeout kills
the test VM, reports failure, and runs the next test. failures produce a nonzero
exit status. a suite with no tests fails too. doing nothing is not passing.

the larger bootstrap checker tests use `--timeout 5m` under GC stress.

## arrange the code

```sh
go run ./cmd/linglang fmt bootstrap/checker
go run ./cmd/linglang fmt --check bootstrap/checker
```

`fmt` uses Go-style formatting and keeps comments and file permissions.
it recursively formats `.lang` files, including tests. no path means `.`;
multiple files or directories work too. `.git`, `_build`, `_release`, `bin`,
and `dist` are skipped during directory discovery, as are symlinks. an explicitly
named file symlink formats its target and keeps the link. all selected files
are parsed before any writes. a syntax error cannot leave half the code tidied.

`--check` lists files that need formatting and exits nonzero without changing
them. neither formatting nor checking formatting needs Erlang. your code can
stand straight without a virtual machine.

## linglang in vscode

linglang has colours. red squiggles too. we paid extra for those.

the extension lives in `editors/vscode` in this repo so the grammar and compiler
can change together. it adds highlighting, snippets, syntax/name/type errors,
formatting, and an outline. unsaved `.lang` files in a directory are checked as
one package, including tests and shared bootstrap sources. put a
`.linglang-standalone` file in a directory of independent programs to check each
open file separately. `examples` and `benchmarks` already have one; their
subdirectories still work as packages. editor analysis does not need Erlang;
running your program still does.

```sh
cd editors/vscode
npm ci
npm test
npm run package
```

use Node.js 22+ and Go. install the file in `dist/` through VS Code's
**Extensions: Install from VSIX…**. the default package contains the server for
your machine; `npm run package -- linux-x64` chooses another target.
`linglang.serverPath` can point at your own compiler build. the server also works
with other LSP editors through `linglang lsp` over stdio.

advanced BEAM/message checks still happen when compiling. completion, hover,
rename, and debugging come later. [editor instructions](editors/vscode/README.md)
include the real VS Code smoke test. the squiggle is employed now.

## test the compiler

```sh
go test ./... -timeout=30m
go vet ./...
go test ./internal/compiler -run='^$' -fuzz='^FuzzCompile$' -fuzztime=10s -parallel=2
```

the tests require Erlang/OTP too. both compiler backends are exercised, including
GC stress and supervisor failure paths. the compiler fuzz target accepts inputs
up to 16 KiB and checks that both backends agree on what compiles. it does not
launch a VM for each input. we have verified that the guy dies.

push CI runs compiler and CLI smoke checks on Linux with OTP 29 and Go 1.23,
plus the small example tests on both backends. a separate job checks the editor
grammar and builds a Linux VSIX. the full suite, bootstrap tests, fuzzing, and
Linux/macOS OTP 27–29 matrix are available from Actions → Run workflow by
enabling `full_compatibility`. one computer checks the paperwork. the other five
can go outside.

## linglang reads linglang

the first bootstrap tool is a lexer written in linglang. it turns source into
tokens with raw text, byte offsets, and line/column locations. it can read its
own source. linglang has discovered literacy. this will end badly.

```sh
go build -o bin/linglang ./cmd/linglang
./bin/linglang run bootstrap/lexer bootstrap/lexer/tokens.lang
./bin/linglang test --gc-stress bootstrap/lexer
./bin/linglang fmt --check bootstrap/lexer
```

to run the lexer without Go or the seed compiler around:

```sh
./bin/linglang pack -o bin/linglang-lexer bootstrap/lexer
./bin/linglang-lexer bootstrap/lexer/tokens.lang
```

the packed lexer needs Erlang/OTP. its tokenization is all linglang code;
Go's scanner is used only as a compatibility oracle in tests. comments, Unicode
identifiers, integer literals, quoted text, operators, and automatic semicolons
work. floats are still somebody else's problem.

linglang now has a parser too. words go in. a syntax tree comes out. unfortunately
this counts as thinking.

```sh
./bin/linglang run bootstrap/parser bootstrap/parser/expressions.lang
./bin/linglang test --gc-stress bootstrap/parser
./bin/linglang test --no-opt --gc-stress bootstrap/parser
./bin/linglang fmt --check bootstrap/parser
./bin/linglang pack -o bin/linglang-parser bootstrap/parser
./bin/linglang-parser bootstrap/parser/expressions.lang
```

the parser is written in linglang and shares the lexer through two relative file
symlinks in `bootstrap/parser`. keep those links when copying the source directories.
the packed executable needs only Erlang/OTP. the recipes can stay at home.

functions, structs, constants, expressions, calls, collection literals, assignments,
loops, and switches become an immutable AST with source locations. operator
precedence is checked against Go's parser. broken syntax gets a filename, line,
and column; excessive nesting gets a diagnostic too. the parser has boundaries.
comments remain available through the lexer; the syntax tree omits them.

the resolver is written in linglang too. it knows which variable you meant.
finally somebody does.

```sh
./bin/linglang run bootstrap/resolver examples/counter.lang
./bin/linglang test --gc-stress --gc-stats bootstrap/resolver
./bin/linglang test --no-opt --gc-stress --gc-stats bootstrap/resolver
./bin/linglang fmt --check bootstrap/resolver
./bin/linglang pack -o bin/linglang-resolver bootstrap/resolver
./bin/linglang-resolver bootstrap/resolver/*.lang
```

pass one or more source files from the same package. the resolver tracks
package declarations, forward references, parameters, local scopes, shadowing,
and short declarations. unknown names and duplicate declarations get source
locations. its binding output is checked against the Go seed on both backends,
including its own source. the packed resolver runs with Erlang/OTP and no Go.
keep the relative lexer/parser symlinks when copying the bootstrap directories.

the type checker is written in linglang now. it can tell you that
`1 + true` is stupid. personal growth.

```sh
./bin/linglang run bootstrap/checker examples/counter.lang
./bin/linglang test --gc-stress --gc-stats --timeout 5m bootstrap/checker
./bin/linglang test --no-opt --gc-stress --gc-stats --timeout 5m bootstrap/checker
./bin/linglang fmt --check bootstrap/checker
./bin/linglang pack -o bin/linglang-checker bootstrap/checker
./bin/linglang-checker bootstrap/checker/*.lang
```

it checks primitive types, calls, assignments, returns, distinct struct types,
pointers, fields, literal keys, lists, maps, and native OTP call signatures.
forward declarations work across files. the packed checker checks its own
source with just Erlang/OTP installed. no Go hiding under the table.

constant evaluation is linglang code too. integer arithmetic keeps up to 512
bits at compile time, then checks that values fit when used as runtime integers.
`iota`, constant dependencies, string/rune escapes, comparisons, conversions,
and `len` of constant strings work. duplicate constant map keys, list indices,
and switch cases get diagnostics. bool switch cases can repeat. they have
very little to say.

constant strings keep shared chunks when they grow. `len` does not need a
gigabyte allocation to count a gigabyte. values above 2,000,000,000 bytes get
a diagnostic before concatenation.

this is still an early type pass. complete control-flow checks and OTP message
safety still need the seed compiler. `types ok` means
this pass succeeded; it does not promise the seed can compile the program.
the runtime and OTP stay underneath.

linglang can now emit Erlang for a small executable subset. the compiler has
entered the chat. it brought 55 beans and a recursion problem.

```sh
./bin/linglang pack -o bin/linglang-emitter bootstrap/emitter
mkdir -p _build/bootstrap-demo
cp internal/compiler/runtime.erl _build/bootstrap-demo/linglang_rt.erl
cp internal/compiler/supervision.erl _build/bootstrap-demo/linglang_sup.erl
./bin/linglang-emitter examples/bootstrap_demo/*.lang > _build/bootstrap-demo/linglang_program.erl
erlc -o _build/bootstrap-demo _build/bootstrap-demo/*.erl
erl -noshell -pa _build/bootstrap-demo -eval 'linglang_program:main(), halt().'
```

once the emitter is packed, those compilation and execution steps need only
Erlang/OTP. source goes through the linglang lexer, parser, resolver, type checker,
and emitter. the existing runtime still handles managed cells and GC scopes.
no Go sneaking into the compilation step wearing a fake moustache.

pass explicit source files from one package. functions, recursion, primitive
`int`/`bool`/`string` variables, structs, pointers, constants, arithmetic, bitwise
operations, direct calls, returns, blocks, `if`/`else`, and `for` loops work.
nested loops, `break`, `continue`, and returns from inside loops use the existing
BEAM runtime. printing, `len`, `formatInt`, `trim`, `byteAt`, `slice`, `join`,
`split`, `isLetter`, `isDigit`, `assert`, and string `panic` work too.

struct literals use named fields. struct assignment copies values; pointer
assignment keeps aliases. `&`, `*`, field updates, returned locals, interior
pointers, and cycles work. lists, maps, and `Delivery[T]` values support nested
zero values and literals, `head`, `tail`, `prepend`, `append` (including `...`),
`get`, `put`, and `remove`. list `range` keeps the original snapshot; `:=` gives
each iteration fresh variables, while `=` updates existing variables.
try `./bin/linglang-emitter examples/bootstrap_collections.lang`.

the emitter sorts files, retains checked expression types and bindings,
and rejects unsupported constructs with source locations before printing a module.
switches, OTP calls, file/argument helpers, multiple assignment, and `if`
initializers still need the seed backend. map range, indexed list literals, and
elided pointer literals remain unsupported by both emitters. runtime conversions
support identity casts only; variable declarations need one named variable per
spec. embedded constant strings are capped at 1 MiB and expression/zero-value
nesting at 128 levels. `new` and `make` still need backend support; use an addressed
local or `&Struct{}`. compiling the compiler itself is the next boss fight;
this emitter cannot compile its own source yet.

the loop example counts three ASCII identifiers and sums 1,000 integers:

```sh
./bin/linglang-emitter examples/bootstrap_loops.lang > _build/bootstrap-demo/linglang_program.erl
erlc -o _build/bootstrap-demo _build/bootstrap-demo/linglang_program.erl
erl -noshell -pa _build/bootstrap-demo -eval 'linglang_program:main(), halt().'
```

it prints `identifiers: 3` and `loop sum: 499500`. use the setup above first;
the program and runtime need to live in the same build directory.

```sh
./bin/linglang test --gc-stress --gc-stats --timeout 2m bootstrap/emitter
./bin/linglang test --no-opt --gc-stress --gc-stats --timeout 2m bootstrap/emitter
./bin/linglang fmt --check bootstrap/emitter examples/bootstrap_demo examples/bootstrap_loops.lang examples/bootstrap_collections.lang
```

## why

because why the fuck not?

i wanted `i++` and OTP supervision in the same language.
one thing led to another and now the compiler needs a compiler.

the seed compiler is written in Go. the bootstrap tools are written in linglang.
