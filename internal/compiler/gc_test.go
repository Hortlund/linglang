package compiler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// All semantic cases run with collection at every safe point, including those
// inside nested callees while their caller's expression is suspended.
func TestGCRootLifetimes(t *testing.T) {
	helpers := `
type Node struct { value int; next *Node }
type Box struct { node *Node }
func makeNode(n int) *Node { node := Node{value: n}; return &node }
func churn() int {
 for i := 0; i < 30; i++ { _ = makeNode(i) }
 return 5
}
func consume(node *Node, n int) { println(node.value, n) }
func consumeBox(box Box, n int) { println(box.node.value, n) }
`
	tests := []struct{ name, source, want string }{
		{"temporary_arguments", `
func main() {
 consume(makeNode(7), churn())
 consume(&Node{value: 8}, churn())
 consumeBox(Box{node: makeNode(9)}, churn())
}
`, "7 5\n8 5\n9 5\n"},
		{"snapshot_survives_original_overwrite", `
func clear(box *Box) int { box.node = nil; return churn() }
func main() {
 box := Box{node: makeNode(10)}
 consumeBox(box, clear(&box))
 box.node = makeNode(11)
 consume(box.node, clear(&box))
}
`, "10 5\n11 5\n"},
		{"assignment_target_survives_rhs", `
func clear(node **Node) int { *node = nil; return churn() }
func main() {
 node := makeNode(1)
 node.value = clear(&node)
 makeNode(2).value += churn()
 println(node == nil)
}
`, "true\n"},
		{"interior_pointer_keeps_owning_object", `
type Nested struct { box Box; tag int }
func interior() **Node {
 value := Nested{box: Box{node: makeNode(12)}}
 return &value.box.node
}
func main() {
 p := interior()
 _ = churn()
 println((*p).value)
 *p = makeNode(13)
 _ = churn()
 println((*p).value)
}
`, "12\n13\n"},
		{"nested_call_return_handoffs", `
func relay(n int) *Node {
 if n == 0 { return makeNode(42) }
 node := relay(n - 1)
 _ = churn()
 return node
}
func main() { consume(relay(8), churn()) }
`, "42 5\n"},
		{"temporary_struct_fields", `
type Pair struct { a *Node; b *Node }
func slowNode(n int) *Node { _ = churn(); return makeNode(n) }
func main() {
 pair := Pair{a: makeNode(1), b: slowNode(2)}
 _ = churn()
 println(pair.a.value, pair.b.value)
}
`, "1 2\n"},
		{"live_cycles", `
func ring() *Node {
 a := Node{value: 3}
 b := Node{value: 4, next: &a}
 a.next = &b
 return &a
}
func main() {
 node := ring()
 _ = churn()
 println(node.value, node.next.value, node.next.next == node)
}
`, "3 4 true\n"},
		{"iteration_references_and_post_calls", `
func step(p *int) { _ = churn(); *p += 1 }
func main() {
 var first *int
 var last *int
 for i := 0; i < 4; step(&i) {
  if i == 0 { first = &i }
  last = &i
  if i == 1 { continue }
  if i == 3 { break }
 }
 _ = churn()
 println(*first, *last, first == last)
}
`, "0 3 false\n"},
		{"returns_unwind_nested_loops", `
func find() *Node {
 for i := 0; i < 3; i++ {
  for j := 0; j < 3; j++ {
   if j == 2 { return makeNode(i + j) }
  }
 }
 return nil
}
func main() { consume(find(), churn()) }
`, "2 5\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := execute(t, helpers+tc.source)
			if err != nil || got != tc.want {
				t.Fatalf("got %q (%v), want %q", got, err, tc.want)
			}
		})
	}
}

// Runtime tests use the same root protocol as the compiler. Erlang locals aren't
// automatically roots: this deliberately lets us retain a stale handle to check
// that its cell really was erased, rather than merely removed from statistics.
func TestCollectorGraphsAndCleanup(t *testing.T) {
	script := `
put(unrelated_application_key, untouched),
Stale = linglang_rt:scope(fun() ->
 A = linglang_rt:new(#{next => nil}),
 B = linglang_rt:new(#{next => A}),
 linglang_rt:write(A, #{next => B}),
 linglang_rt:collect(),
 #{live_cells := 2} = linglang_rt:stats(),
 A
end),
linglang_rt:collect(),
#{live_cells := 0, root_frames := 0, root_entries := 0} = linglang_rt:stats(),
try linglang_rt:read(Stale), error(stale_reference_survived)
catch error:linglang_invalid_pointer -> ok end,

linglang_rt:scope(fun() ->
 Root = linglang_rt:new(#{next => nil}),
 linglang_rt:scope(fun() ->
  Child = linglang_rt:new(#{value => 41}),
  linglang_rt:write(Root, #{next => Child})
 end),
 linglang_rt:collect(),
 #{live_cells := 2} = linglang_rt:stats(),
 ChildValue = linglang_rt:read(maps:get(next, linglang_rt:read(Root))),
 #{value := 41} = ChildValue,
 linglang_rt:write(Root, #{next => nil}),
 linglang_rt:collect(),
 #{live_cells := 1} = linglang_rt:stats()
end),
linglang_rt:collect(),

lists:foreach(fun(Class) ->
 try linglang_rt:scope(fun() ->
  linglang_rt:new(1),
  linglang_rt:scope(fun() -> linglang_rt:new(2), erlang:Class(expected) end)
 end)
 catch Class:expected -> ok end,
 linglang_rt:collect(),
 #{live_cells := 0, root_frames := 0, root_entries := 0} = linglang_rt:stats()
end, [throw, error, exit]),

linglang_rt:scope(fun() ->
 Head = linglang_rt:scope(fun() ->
  lists:foldl(fun(I, Next) -> linglang_rt:new(#{value => I, next => Next}) end, nil, lists:seq(1, 20000))
 end),
 linglang_rt:keep(Head),
 linglang_rt:collect(),
 #{live_cells := 20000, root_entries := 1} = linglang_rt:stats(),
 #{value := 20000} = linglang_rt:read(Head)
end),
linglang_rt:collect(),
#{live_cells := 0, root_frames := 0} = linglang_rt:stats(),
[] = [Id || {{linglang_cell, Id}, _} <- get()],
untouched = get(unrelated_application_key),
io:format("ok~n"), halt(0).`
	got, err := executeScript(t, "func main() {}", script)
	if err != nil || got != "ok\n" {
		t.Fatalf("collector invariants: %v\n%s", err, got)
	}
}

func TestGCUnwindsRuntimeFailure(t *testing.T) {
	source := `
type Node struct { next *Node }
func fail() {
 node := Node{}
 node.next = &node
 var p *int
 println(*p)
}
func main() { for i := 0; i < 2; i++ { fail() } }
`
	script := `
linglang_rt:set_gc_stress(true),
lists:foreach(fun(_) ->
 try linglang_program:main(), error(expected_nil_failure)
 catch error:linglang_nil_pointer -> ok end,
 #{live_cells := 0, root_frames := 0, root_entries := 0} = linglang_rt:stats(),
 [] = [Id || {{linglang_cell, Id}, _} <- get()]
end, lists:seq(1, 100)),
io:format("ok~n"), halt(0).`
	got, err := executeScript(t, source, script)
	if err != nil || got != "ok\n" {
		t.Fatalf("exception cleanup: %v\n%s", err, got)
	}
}

func TestGCBoundedRepeatedAllocation(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "examples", "memory.lang"))
	if err != nil {
		t.Fatal(err)
	}
	script := `
linglang_program:main(),
erlang:garbage_collect(),
{memory, WarmBytes} = process_info(self(), memory),
lists:foreach(fun(_) -> linglang_program:main() end, lists:seq(1, 5)),
erlang:garbage_collect(),
{memory, FinalBytes} = process_info(self(), memory),
#{allocated_cells := Allocated, live_cells := 0, peak_live_cells := Peak,
  reclaimed_cells := Allocated, root_frames := 0, root_entries := 0,
  collections := Collections} = linglang_rt:stats(),
240000 = Allocated,
true = Peak < 300,
true = Collections > 500,
true = FinalBytes < WarmBytes * 4 + 262144,
[] = [Id || {{linglang_cell, Id}, _} <- get()],
io:format("METRICS:{\"allocated\":~p,\"peak_cells\":~p,\"collections\":~p,\"warm_bytes\":~p,\"final_bytes\":~p}~n", [Allocated, Peak, Collections, WarmBytes, FinalBytes]),
halt(0).`
	got, err := executeScript(t, strings.TrimPrefix(string(source), "package main\n"), script)
	if err != nil {
		t.Fatalf("bounded allocation: %v\n%s", err, got)
	}
	_, metrics, ok := strings.Cut(got, "METRICS:")
	if !ok {
		t.Fatalf("missing metrics: %s", got)
	}
	var parsed map[string]int
	if err := json.Unmarshal([]byte(metrics), &parsed); err != nil {
		t.Fatal(err)
	}
	t.Logf("same-process stress metrics: %v", parsed)
}

func TestGCTemporaryScopesStayBounded(t *testing.T) {
	// Conditions and post statements can allocate without declaring a variable in
	// the loop body. Their temporary roots must end on every iteration too.
	source := `
type Node struct { value int }
func makeNode(n int) *Node { return &Node{value: n} }
func condition(n *int, limit *Node) bool { return *n < limit.value }
func step(n *int, amount *Node) { *n += amount.value }
func main() {
 i := 0
 for condition(&i, makeNode(10000)) {
  _ = makeNode(i)
  i++
 }
 for j := 0; condition(&j, makeNode(10000)); step(&j, makeNode(1)) {}
 println(i)
}
`
	script := `linglang_program:main(), #{live_cells := 0, peak_live_cells := Peak, root_frames := 0} = linglang_rt:stats(), true = Peak < 300, io:format("bounded~n"), halt(0).`
	got, err := executeScript(t, source, script)
	if err != nil || got != "10000\nbounded\n" {
		t.Fatalf("temporary scopes: %v\n%s", err, got)
	}
}

// Static pointer-free cell contents may be large immutable values. Collection
// must retain the cell and interior handles without walking those contents.
func TestGCPointerFreeCellTracing(t *testing.T) {
	script := `
{module, linglang_rt} = code:ensure_loaded(linglang_rt),
1 = erlang:trace_pattern({linglang_rt, references, 2}, true, [call_count]),
linglang_rt:scope(fun() ->
 P = linglang_rt:new(#{n => 1, items => lists:seq(1, 10000)}, false),
 Q = linglang_rt:field(P, n),
 linglang_rt:write(Q, 7),
 linglang_rt:collect(),
 7 = linglang_rt:read(Q),
 #{live_cells := 1, root_entries := 1} = linglang_rt:stats(),
 {call_count, Calls} = erlang:trace_info({linglang_rt, references, 2}, call_count),
 true = Calls > 0 andalso Calls < 50,
 %% A normally traced parent must retain a pointer-free child after the child's
 %% allocation frame closes. Overwriting that edge must then reclaim the child.
 Parent = linglang_rt:new(#{child => nil, items => lists:seq(1, 10000)}, {fields, [child]}),
 Child = linglang_rt:scope(fun() ->
  C = linglang_rt:new(#{n => 9}, false),
  linglang_rt:write(linglang_rt:field(Parent, child), C),
  C
 end),
 linglang_rt:collect(),
 #{live_cells := 3} = linglang_rt:stats(),
 #{n := 9} = linglang_rt:read(Child),
 linglang_rt:write(linglang_rt:field(Parent, child), nil),
 linglang_rt:collect(),
 #{live_cells := 2} = linglang_rt:stats(),
 {call_count, FieldCalls} = erlang:trace_info({linglang_rt, references, 2}, call_count),
 true = FieldCalls > Calls andalso FieldCalls < 100,
 try linglang_rt:read(Child), error(stale_child_survived)
 catch error:linglang_invalid_pointer -> ok end
end),
linglang_rt:collect(),
#{live_cells := 0, root_frames := 0, root_entries := 0} = linglang_rt:stats(),
[] = [K || {{linglang_cell, _} = K, _} <- get()],
[] = [K || {{linglang_cell_trace, _} = K, _} <- get()],
erlang:trace_pattern({linglang_rt, references, 2}, false, [call_count]),
io:format("ok~n"), halt(0).`
	got, err := executeScript(t, "func main() {}", script)
	if err != nil || got != "ok\n" {
		t.Fatalf("pointer-free cell tracing: %v\n%s", err, got)
	}
}
