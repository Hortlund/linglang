package compiler

import (
	"os"
	"strings"
	"testing"
)

func TestProcessSemantics(t *testing.T) {
	tests := []struct{ name, source, want string }{
		{"native_timers", `
type Tick struct { n int; xs List[int] }
func later(parent Pid) { sendAfter(parent, "alive", 10) }
func main() {
    value := Tick{n:7, xs:List[int]{1,2}}
    timer := sendAfter(self(), value, 0)
    value.n = 99
    value.xs = append(value.xs, 3)
    message := receive[Tick](5000)
    println(message.ok, message.value.n, len(message.value.xs), cancelTimer(timer))
    timer = sendAfter(self(), 9, 60000)
    send(self(), 42)
    println(cancelTimer(timer), cancelTimer(timer), receive[int](0).value, receive[int](0).ok)
    timer = sendAfter[List[int]](self(), nil, 0)
    list := receive[List[int]](5000)
    println(list.ok, list.value == nil, cancelTimer(timer))
    worker := spawnMonitor(later, self())
    println(wait(worker.monitor, 5000).normal, receive[string](5000).value)
}`, "true 7 2 false\ntrue false 42 false\ntrue true false\ntrue alive\n"},
		{"value_snapshots_and_selective_receive", `
type First struct { n int }
type Second struct { n int }
type Args struct { parent Pid; value First }
func echo(args Args) {
    send(args.parent, args.value)
    args.value.n = 100
    send(args.parent, Second{n: args.value.n})
}
func main() {
    original := First{n: 7}
    child := spawnMonitor(echo, Args{parent: self(), value: original})
    original.n = 99
    second := receive[Second](5000)
    first := receive[First](0)
    missing := receive[First](0)
    done := wait(child.monitor, 5000)
    println(second.ok, second.value.n, first.ok, first.value.n, original.n)
    println(missing.ok, missing.value.n, done.ok, done.normal, done.pid == child.pid)
    send(self(), original)
    original.n = 123
    snapshot := receive[First](-1)
    println(snapshot.value.n, original.n)
    send[int](self(), 42)
    number := receive[int](0)
    send(self(), true)
    flag := receive[bool](0)
    send(self(), "hello")
    text := receive[string](0)
    println(number.value, flag.value, text.value)
}`, "true 100 true 7 99\nfalse 0 true true true\n99 123\n42 true hello\n"},
		{"monitor_lifecycle_and_timeout", `
func blocked(parent Pid) {
    send(parent, true)
    request := receive[int](-1)
    if request.value != 9 { panic("wrong request") }
}
func main() {
    child := spawnMonitor(blocked, self())
    ready := receive[bool](5000)
    extra := monitor(child.pid)
    pending := wait(child.monitor, 0)
    println(ready.ok, pending.ok, pending.normal, pending.reason == "")
    println(demonitor(extra), demonitor(extra))
    send(child.pid, 9)
    done := wait(child.monitor, -1)
    println(done.ok, done.normal, done.reason, demonitor(child.monitor))
    late := monitor(child.pid)
    gone := wait(late, 5000)
    println(gone.ok, gone.normal, gone.reason)
    send(child.pid, 123) // asynchronous send to an exited process is allowed
    empty := receive[int](1)
    println(empty.ok, empty.value)
}`, "true false false true\ntrue false\ntrue true normal false\ntrue false noproc\nfalse 0\n"},
		{"crash_isolation_and_atomic_monitor", `
type Empty struct {}
func crash(args Empty) { panic("worker failed") }
func badPointer(args Empty) { var p *int; println(*p) }
func done(args Empty) {}
func main() {
    child := spawnMonitor(crash, Empty{})
    failure := wait(child.monitor, 5000)
    println(failure.ok, failure.normal, failure.reason)
    bad := spawnMonitor(badPointer, Empty{})
    result := wait(bad.monitor, 5000)
    println(result.ok, result.normal, result.reason)
    for i := 0; i < 20; i++ {
        quick := spawnMonitor(done, Empty{})
        exit := wait(quick.monitor, 5000)
        if !exit.ok || !exit.normal { panic("lost immediate exit") }
    }
    println("parent survived")
}`, "true false worker failed\ntrue false linglang_nil_pointer\nparent survived\n"},
		{"roots_survive_blocking_and_children_have_local_cells", `
type Node struct { value int; next *Node }
func makeNode() *Node { n := Node{value: 41}; n.next = &n; return &n }
func worker(parent Pid) {
    local := makeNode()
    for i := 0; i < 1000; i++ { junk := makeNode(); junk.value++ }
    send(parent, local.next.value + 1)
}
func consume(p *Node, message Delivery[int]) { println(p.next.value, message.value) }
func main() {
    p := makeNode()
    process := spawnMonitor(worker, self())
    consume(makeNode(), receive[int](5000))
    done := wait(process.monitor, 5000)
    p.next.value++
    println(done.normal, p.value)
    // Basic spawn and process identities can be passed in value messages.
    child := spawn(worker, self())
    send(self(), child)
    identity := receive[Pid](0)
    message := receive[int](5000)
    println(identity.ok, identity.value == child, message.value)
}`, "41 42\ntrue 42\ntrue true 42\n"},
		{"intrinsic_shadowing_and_nested_messages", `
type Payload struct { sender Pid; result Delivery[int] }
func main() {
    send(self(), Payload{sender: self(), result: Delivery[int]{value: 17, ok: true}})
    data := receive[Payload](0)
    var missing Pid
    empty := receive[Pid](0)
    println(data.ok, data.value.sender == self(), data.value.result.value, empty.value == missing)
    { send := 8; println(send) }
}`, "true true 17 true\n8\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := execute(t, test.source)
			if err != nil || got != test.want {
				t.Fatalf("got %q, %v; want %q", got, err, test.want)
			}
		})
	}
}

func TestCounterExample(t *testing.T) {
	source, err := os.ReadFile("../../examples/counter.lang")
	if err != nil {
		t.Fatal(err)
	}
	got, err := execute(t, strings.TrimPrefix(string(source), "package main"))
	if err != nil || got != "Counter: 4000\n" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestProcessRuntimeErrors(t *testing.T) {
	tests := []struct{ name, source, want string }{
		{"negative_timeout", `func main() { receive[int](-2) }`, "linglang_invalid_timeout"},
		{"large_timeout", `func main() { receive[int](2147483648) }`, "linglang_invalid_timeout"},
		{"nil_send", `func main() { var p Pid; send(p, 1) }`, "linglang_invalid_pid"},
		{"nil_monitor", `func main() { var p Pid; monitor(p) }`, "linglang_invalid_pid"},
		{"nil_wait", `func main() { var m Monitor; wait(m, 0) }`, "linglang_invalid_monitor"},
		{"consumed_monitor", `func done(n int) {}; func main() { p := spawnMonitor(done, 0); wait(p.monitor, -1); wait(p.monitor, 0) }`, "linglang_inactive_monitor"},
		{"cancelled_monitor", `func main() { m := monitor(self()); demonitor(m); wait(m, 0) }`, "linglang_inactive_monitor"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := execute(t, test.source)
			if err == nil || got != "ERROR:"+test.want+"\n" {
				t.Fatalf("got %q, %v", got, err)
			}
		})
	}
}

func TestProcessCompileRejections(t *testing.T) {
	tests := []struct{ name, source, want string }{
		{"timer_pointer", `func main(){x:=1;sendAfter(self(),&x,0)}`, "process-local pointers"},
		{"timer_handle", `func main(){t:=sendAfter(self(),1,0);send(self(),t)}`, "timer handles belong"},
		{"timer_receive", `func main(){receive[Timer](0)}`, "timer handles belong"},
		{"timer_literal", `func main(){println(Timer{})}`, "invalid composite literal"},
		{"timer_delay_type", `func main(){sendAfter(self(),1,true)}`, "cannot use"},
		{"send_pointer", `func main() { x := 1; send(self(), &x) }`, "process-local pointers"},
		{"send_nil_pointer", `func main() { var p *int; send(self(), p) }`, "process-local pointers"},
		{"send_nested_pointer", `type Inner struct { p *int }; type Outer struct { inner Inner }; func main() { send(self(), Outer{}) }`, "field inner: field p: process-local pointers"},
		{"spawn_pointer", `func worker(p *int) {}; func main() { x := 1; spawn(worker, &x) }`, "process-local pointers"},
		{"spawn_nested_pointer", `type Arg struct { p *int }; func worker(a Arg) {}; func main() { spawnMonitor(worker, Arg{}) }`, "field p: process-local pointers"},
		{"receive_pointer", `func main() { receive[*int](0) }`, "process-local pointers"},
		{"send_monitor", `func main() { m := monitor(self()); send(self(), m) }`, "monitor handles belong"},
		{"send_process", `func worker(n int) {}; func main() { p := spawnMonitor(worker, 1); send(self(), p) }`, "field monitor: monitor handles belong"},
		{"spawn_monitor_handle", `func worker(m Monitor) {}; func main() { spawn(worker, monitor(self())) }`, "monitor handles belong"},
		{"receive_monitor", `func main() { receive[Monitor](0) }`, "monitor handles belong"},
		{"closure", `func main() { spawn(func(n int) {}, 1) }`, "named language function"},
		{"intrinsic_worker", `func main() { spawn(monitor, self()) }`, "does not match"},
		{"returning_worker", `func worker(n int) int { return n }; func main() { spawn(worker, 1) }`, "does not match"},
		{"wrong_argument", `func worker(n int) {}; func main() { spawn(worker, true) }`, "cannot use"},
		{"opaque_literal", `func main() { p := Pid{}; println(p) }`, "invalid composite literal"},
		{"opaque_conversion", `func main() { p := Pid(nil); println(p) }`, "type conversions"},
		{"channel_operations", `func main() { p := self(); p <- 1 }`, "unsupported statement"},
		{"channel_receive", `func main() { p := self(); println(<-p) }`, "unsupported unary operator"},
		{"panic_non_string", `func main() { panic(42) }`, "panic requires a string"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, opts := range []Options{{}, {DisableOptimizations: true}} {
				_, err := CompileWithOptions("bad.lang", []byte("package main\n"+test.source), opts)
				if err == nil || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("got %v; want %q", err, test.want)
				}
			}
		})
	}
}

func TestProcessRuntimeBoundary(t *testing.T) {
	script := `
    Expect = fun(Fun, Expected) ->
        try Fun(), erlang:error(expected_failure)
        catch error:Expected -> ok end
    end,
    Expect(fun() -> linglang_rt:send_message(self(), int, {linglang_ptr, self(), make_ref(), []}) end, linglang_invalid_message),
    Expect(fun() -> linglang_rt:send_message(self(), int, 1 bsl 63) end, linglang_invalid_message),
    Schema = {struct, [{field_6e, int}]},
    Expect(fun() -> linglang_rt:send_message(self(), Schema, #{field_6e => 1, extra => 2}) end, linglang_invalid_message),
    self() ! {linglang_message, Schema, #{field_6e => {linglang_ptr, self(), make_ref(), []}}},
    Expect(fun() -> linglang_rt:receive_message(Schema, 0, #{field_6e => 0}) end, linglang_invalid_message),
    Expect(fun() -> linglang_rt:spawn_process(fun(_) -> ok end, nil, int, false) end, linglang_invalid_message),
    Expect(fun() -> linglang_rt:send_after(self(), int, <<"bad">>, 0) end, linglang_invalid_message),
    Expect(fun() -> linglang_rt:send_after(nil, int, 1, 0) end, linglang_invalid_pid),
    Expect(fun() -> linglang_rt:send_after(self(), int, 1, -1) end, linglang_invalid_timeout),
    Expect(fun() -> linglang_rt:send_after(self(), int, 1, 2147483648) end, linglang_invalid_timeout),
    Expect(fun() -> linglang_rt:cancel_timer(nil) end, linglang_invalid_timer),
    Expect(fun() -> linglang_rt:cancel_timer({linglang_timer, self(), bad}) end, linglang_invalid_timer),
    Timer = linglang_rt:send_after(self(), int, 1, 60000),
    {linglang_timer, _, TimerRef} = Timer,
    Parent = self(),
    Child = spawn(fun() -> receive stop -> ok end end),
    Expect(fun() -> linglang_rt:cancel_timer({linglang_timer, Child, TimerRef}) end, linglang_cross_process_timer),
    true = linglang_rt:cancel_timer(Timer),
    false = linglang_rt:cancel_timer(Timer),
    Foreign = {linglang_monitor, Child, make_ref(), Parent},
    Expect(fun() -> linglang_rt:wait_process(Foreign, 0) end, linglang_cross_process_monitor),
    Expect(fun() -> linglang_rt:demonitor_process(Foreign) end, linglang_cross_process_monitor),
    Child ! stop,
    M = linglang_rt:monitor_process(self()),
    {linglang_monitor, _, Ref, _} = M,
    self() ! {'DOWN', Ref, process, self(), normal},
    true = linglang_rt:demonitor_process(M),
    receive {'DOWN', Ref, _, _, _} -> erlang:error(down_not_flushed) after 0 -> ok end,
    undefined = get({linglang_monitor, Ref}),
    linglang_rt:set_gc_stress(true),
    Spawned = linglang_rt:spawn_process(fun(P) ->
        [] = [K || {{linglang_cell, _} = K, _} <- get()],
        true = get({linglang_gc, stress}),
        P ! isolated
    end, self(), pid, true),
    receive isolated -> ok after 5000 -> erlang:error(child_timeout) end,
    #{field_6f6b := true, field_6e6f726d616c := true} = linglang_rt:wait_process(maps:get(field_6d6f6e69746f72, Spawned), 5000),
    [] = [K || {{linglang_monitor, _} = K, _} <- get()],
    io:format("ok~n"), halt(0).`
	got, err := executeScript(t, "func main() {}", script)
	if err != nil || got != "ok\n" {
		t.Fatalf("got %q, %v", got, err)
	}
}
