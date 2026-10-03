# linglang

a programming language that answers the question nobody fucking asked.

called linglang because i wanted to learn Chinese but somehow installed Erlang instead.

Go writes Erlang. Erlang runs on BEAM. BEAM runs on your computer.
your computer did not agree to any of this.

## installation

bring Go 1.23+ and Erlang/OTP. `erl` and `erlc` must be on your PATH.
if you don't have a PATH, go outside and find one.

```sh
go run ./cmd/linglang run examples/beans.lang
```

you have now installed a bean situation.

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

there are no floats. a bean cannot be 1.7 beans. finish your bean.

`bool` is `true` or `false`. for a third opinion, start another process.

`string` stores bytes, including UTF-8 text. it does not know what the text means.
neither does the compiler.

## variables

variables can vary. we checked.

```go
bean := 1
bean = 2
bean++
println(bean) // 3. this is getting out of hand.
```

use `var bean int` to start at zero. bool starts at `false`, string at `""`,
and pointers and process handles at `nil`. structs start with zero in each field.
the computer has done the bare minimum.

## constants

`const` is for numbers that have stopped cooperating.

```go
const Bean = 1
const (
    AskForBean = iota + 1
    ThreatenBean
    ForgetBean
)
println(Bean, AskForBean, ThreatenBean, ForgetBean) // 1 1 2 3
```

`iota` counts the declarations so you don't have to. your contribution is naming them.
constants work at package and function scope, in groups, with constant expressions
and multiple names. supported types are `int`, `bool`, `string`, and untyped
integer, rune, bool, and string constants. a large untyped integer can exist at
compile time, but must fit in an `int` when used as one. big bean, small door.

## decisions

`switch` checks cases from left to right. the first match wins. this is also how
the bean became legally mine.

```go
const (
    AskForBean = iota + 1
    ThreatenBean
    ForgetBean
)
switch demand := ThreatenBean; demand {
case AskForBean, ThreatenBean:
    println("no")
case ForgetBean:
    println("what bean")
default:
    panic("unrecognized bean activity")
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
beans := List[string]{"bean"}
oldBeans := beans
beans = append(beans, "bean")
beans = prepend("suspicious bean", beans)
for i, bean := range beans {
    println(i, bean)
}
println(len(oldBeans)) // 1. the past refuses to help.
println(head(beans).value, head(beans).ok)
beans = tail(beans)
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
len walks it. range traverses once. bean location affects bean speed.

`var beans List[int]` is nil. `List[int]{}` is empty but non-nil. both behave
as empty lists. one has made slightly more effort.

range accepts index/value bindings, existing identifiers with `=`, blank
targets, or no bindings. its source is evaluated once and kept as a snapshot.
variables declared with `:=` get fresh identity each iteration. when ranging
over a literal directly, put parentheses around it:
`for _, bean := range (List[int]{1, 2}) { ... }`. the bean needs a fence.

indexing, indexed writes, slicing, keyed list literals, raw slices, and arrays
are unsupported. you may look at the first bean and proceed from there.

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
bean := 1
mine := &bean
yours := mine
*yours = 2
println(bean, *mine, mine == yours) // 2 2 true. your bean was my bean.
```

structs copy by value; pointers keep the identity of a mutable managed cell.
you can return a pointer to a local. the garbage collector follows pointers
through structs, lists, and maps, including cycles. unused cells get collected.
the collector cannot fix this README because somebody is still reading it.

pointers stay in their creating process. trying to send one to another process
is a compile error. get your own bean.

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
with side effects must tolerate retries. do not use this to feed the bean twice.

queue state is in memory. worker crashes don't lose it. queue or VM exits do.
the bean has no backup bean.

## accidentally useful

the prime lab reads a file, skips bad jobs, counts primes on three supervised
workers, and writes a CSV. sorry. we tried to keep it stupid.

```sh
go run ./cmd/linglang run examples/prime_lab.lang examples/prime_jobs.txt prime-results.csv
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

## outside the computer

files are where bytes live when the program is not looking at them.
these functions are available without imports. imports are currently unavailable,
so this is convenient.

| function | result | what the computer does |
| --- | --- | --- |
| `args()` | `List[string]` | arguments after the source path, in order; excludes the source path and CLI flags. |
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
println(formatInt(number.value) + " bean")
```

strings and file contents preserve bytes, including UTF-8. split matches bytes;
trim handles ASCII whitespace. Unicode spaces get to stay. they came all this way.

file operations use the [OTP file module](https://www.erlang.org/doc/apps/kernel/file.html).
CLI arguments are passed separately using [`-extra`](https://www.erlang.org/doc/apps/erts/init.html).
quoted paths with spaces work. shell-looking arguments remain arguments.
the semicolon in your filename does not get a little job.

## other buttons

run commands from the repository root. flags go before the source path.
program arguments go after it. the computer cares about this.

```sh
go run ./cmd/linglang run --gc-stress --gc-stats examples/beans.lang
go run ./cmd/linglang run --no-opt examples/beans.lang
go run ./cmd/linglang build -o _build examples/beans.lang
go run ./cmd/linglang emit examples/beans.lang
```

`--gc-stress` collects at every safe point. `--gc-stats` prints the managed-heap
statistics. `--no-opt` uses the original compiler backend. same beans, more cells.
`build` writes BEAM modules; `emit` prints generated Erlang. this is where the
Erlang was hiding.

## things you cannot do

imports, multiple source files, closures, floats, arrays, raw Go slices and maps,
list/map indexing, and map range. this is a Go-like subset with BEAM underneath.
it has `package main`, functions, structs, mutable locals, pointers, and enough
control flow to cause an incident. it is not all of Go. some of Go escaped.

## tests

```sh
go test ./...
go vet ./...
```

the tests require Erlang/OTP too. both compiler backends are exercised, including
GC stress and supervisor failure paths. we have verified that the guy dies.

## why

because why the fuck not?

i wanted `i++` and OTP supervision in the same language.
one thing led to another and now there is a bean with three lawyers.

the compiler is written in Go. the bean is written in bean.
