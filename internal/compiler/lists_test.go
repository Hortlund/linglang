package compiler

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestListSemantics(t *testing.T) {
	tests := []struct{ name, source, want string }{
		{"literals_zero_values_and_persistent_operations", `
func main() {
 var zero List[int]
 empty := List[int]{}
 original := List[int]{65, 66}
 copy := original
 extended := append(original, 67, 68)
 joined := append(extended, original...)
 println(len(zero), len(empty), zero == nil, empty == nil)
 println(original, copy, extended, joined, prepend(64, original))
 println(tail(original), head(original).value, head(original).ok)
 println(head(zero).value, head(zero).ok, head[int](nil).ok, tail(zero) == nil)
 println(append(zero) == nil, len(append(zero, empty...)), len(tail(empty)))
}
`, "0 0 true false\n[65, 66] [65, 66] [65, 66, 67, 68] [65, 66, 67, 68, 65, 66] [64, 65, 66]\n[66] 65 true\n0 false false true\ntrue 0 0\n"},
		{"nested_struct_values_and_copy_semantics", `
type Job struct { id int; labels List[string] }
type Batch struct { jobs List[Job] }
func main() {
 labels := List[string]{"first"}
 jobs := List[Job]{Job{id: 1, labels: labels}}
 labels = append(labels, "second")
 job := head(jobs).value
 job.id = 9
 job.labels = append(job.labels, "changed")
 batch := Batch{jobs: jobs}
 copy := batch
 batch.jobs = append(batch.jobs, job)
 println(len(copy.jobs), len(batch.jobs), head(copy.jobs).value.id, len(head(copy.jobs).value.labels))
 nested := List[List[int]]{List[int]{1, 2}, List[int]{3}}
 for _, row := range nested { println(row) }
}
`, "1 2 1 1\n[1, 2]\n[3]\n"},
		{"range_snapshot_order_and_control_flow", `
func items(calls *int) List[int] { *calls += 1; return List[int]{2, 3, 4} }
func main() {
 calls := 0
 total := 0
 source := items(&calls)
 for i, n := range source {
  source = append(source, 100)
  if i == 1 { continue }
  total += i + n
 }
 for _, n := range items(&calls) { if n == 3 { break }; total += n }
 for i := range (List[int]{7, 8}) { total += i }
 for range (List[int]{1, 2}) { total++ }
 for range (List[int]{}) { panic("empty iteration") }
 var zero List[int]
 for range zero { panic("nil iteration") }
 println(calls, total, len(source))
}
`, "2 13 6\n"},
		{"range_assignment_nested_switches_and_return", `
func firstEven(items List[int]) int {
 for _, n := range items { switch n % 2 { case 0: return n; default: continue } }
 return -1
}
func main() {
 i := -1
 n := -1
 total := 0
 for i, n = range (List[int]{4, 5}) { total += n }
 for _, row := range (List[List[int]]{List[int]{1, 2}, List[int]{3}}) {
  for _, value := range row { switch value { case 2: break; default: total += value } }
 }
 println(i, n, total, firstEven(List[int]{1, 3, 6}), firstEven(List[int]{}))
}
`, "1 5 13 6 -1\n"},
		{"range_variables_have_iteration_identity", `
func main() {
 var firstKey *int
 var lastKey *int
 var firstValue *int
 var lastValue *int
 for i, n := range (List[int]{10, 20, 30}) {
  if i == 0 { firstKey = &i; firstValue = &n }
  lastKey = &i
  lastValue = &n
 }
 println(*firstKey, *lastKey, *firstValue, *lastValue, firstKey == lastKey, firstValue == lastValue)
 n := 0
 for _, n = range (List[int]{4, 5}) { lastValue = &n }
 n++
 println(*lastValue, n)
}
`, "0 2 10 30 false false\n6 6\n"},
		{"pointer_lists_and_recursive_local_types", `
type Node struct { n int; children List[Node]; links List[*Node] }
func makeNode(n int) *Node { return &Node{n: n} }
func churn() int { for i := 0; i < 30; i++ { _ = makeNode(i) }; return 8 }
func slowNode(n int) *Node { _ = churn(); return makeNode(n) }
func nodes() List[*Node] { return List[*Node]{makeNode(1), slowNode(2)} }
func main() {
 items := nodes()
 copy := items
 items = append(items, slowNode(3))
 head(copy).value.n++
 _ = churn()
 for _, node := range items { println(node.n) }
 tree := Node{children: List[Node]{Node{n: 4}}, links: items}
 root := &tree
 root.links = prepend(root, root.links)
 _ = churn()
 println(head(root.children).value.n, head(root.links).value == root)
}
`, "2\n2\n3\n4 true\n"},
		{"temporary_lists_remain_rooted_across_calls", `
type Node struct { n int }
func makeNode(n int) *Node { return &Node{n: n} }
func churn() *Node { for i := 0; i < 30; i++ { _ = makeNode(i) }; return makeNode(9) }
func consume(items List[*Node], other *Node) { println(head(items).value.n, other.n) }
func main() {
 consume(List[*Node]{makeNode(7)}, churn())
 items := append(List[*Node]{makeNode(8)}, churn())
 for _, p := range items { println(p.n) }
 for _, p := range (List[*Node]{makeNode(10), makeNode(11)}) { _ = churn(); println(p.n) }
}
`, "7 9\n8\n9\n10\n11\n"},
		{"pointers_to_lists_preserve_cell_identity", `
func main() {
 items := List[int]{1, 2}
 original := items
 p := &items
 *p = append(*p, 3)
 println(items, original, head(*p).value)
 lists := List[*List[int]]{p}
 *head(lists).value = prepend(0, *p)
 println(items, p == &items)
}
`, "[1, 2, 3] [1, 2] 1\n[0, 1, 2, 3] true\n"},
		{"list_messages_are_typed_value_snapshots", `
type Job struct { id int; inputs List[int] }
func main() {
 jobs := List[Job]{Job{id: 1, inputs: List[int]{2, 3}}}
 send(self(), jobs)
 jobs = append(jobs, Job{id: 2})
 wrong := receive[List[int]](0)
 message := receive[List[Job]](1000)
 println(wrong.ok, message.ok, len(message.value), len(jobs), head(message.value).value.id)
 println(head(message.value).value.inputs)
 var empty List[int]
 send(self(), empty)
 println(receive[List[int]](1000).value == nil)
}
`, "false true 1 2 1\n[2, 3]\ntrue\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := execute(t, tc.source)
			if err != nil || got != tc.want {
				t.Fatalf("got %q (%v), want %q", got, err, tc.want)
			}
		})
	}
}

func TestListDiagnostics(t *testing.T) {
	tests := []struct{ source, want string }{
		{`func main() { items := List[float64]{1.5}; println(items) }`, "unsupported type"},
		{`func main() { println(List[float64]{}) }`, "unsupported type"},
		{`func main() { println(head[float64](nil).ok) }`, "unsupported type"},
		{`func main() { items := List[int]{0: 1}; println(items) }`, "positional elements"},
		{`func main() { items := List[int]{1}; items[0] = 2 }`, "unsupported reference target"},
		{`func main() { items := List[int]{1}; println(items[0]) }`, "unsupported expression"},
		{`func main() { items := List[int]{1}; send(self(), List[*int]{&items}) }`, "cannot use"},
		{`func main() { n := 1; send(self(), List[*int]{&n}) }`, "process-local pointers"},
		{`func main() { send(self(), List[List[Monitor]]{}) }`, "monitor handles"},
		{`func main() { send(self(), List[Supervisor]{}) }`, "supervisor handles"},
		{`type Node struct { children List[Node] }; func main() { send(self(), Node{}) }`, "recursive message types"},
		{`func main() { for range "hello" {} }`, "range currently requires"},
		{`type Box struct { n int }; func main() { b := Box{}; for b.n = range (List[int]{1}) {}; println(b.n) }`, "range targets must be identifiers"},
	}
	for _, tc := range tests {
		for _, options := range []Options{{}, {DisableOptimizations: true}} {
			_, err := CompileWithOptions("bad.lang", []byte("package main\n"+tc.source), options)
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "bad.lang:") {
				t.Errorf("%s (options=%+v): got %v, want source location and %q", tc.source, options, err, tc.want)
			}
		}
	}
}

func TestListsUseNativeBEAMValues(t *testing.T) {
	source := `func main() {
 items := List[int]{}
 for i := 0; i < 1000; i++ { items = prepend(i, items) }
 total := 0
 for _, n := range items { total += n }
 println(total, len(items))
}`
	script := `linglang_rt:set_gc_stress(true), linglang_program:main(),
 #{allocated_cells := 0, live_cells := 0, root_frames := 0} = linglang_rt:stats(), halt(0).`
	got, err := executeScript(t, source, script)
	if err != nil || got != "499500 1000\n" {
		t.Fatalf("native list allocation budget: %v\n%s", err, got)
	}
}

func TestPointerListTraversalScalesLinearly(t *testing.T) {
	source := `package main
func sum(items List[*int]) int {
 total := 0
 for _, p := range items { total += *p }
 return total
}
func main() {}`
	for _, baseline := range []bool{false, true} {
		t.Run(fmt.Sprintf("baseline=%t", baseline), func(t *testing.T) {
			program, err := CompileWithOptions("test.lang", []byte(source), Options{DisableOptimizations: baseline})
			if err != nil {
				t.Fatal(err)
			}
			// Expose this language function only in the test module so its input
			// can be prepared outside the measured traversal.
			program = strings.Replace(program, "-export([main/0]).", "-export([main/0, "+functionName("sum")+"/1]).", 1)
			script := fmt.Sprintf(`
Counts = lists:map(fun(N) ->
 Count = linglang_rt:scope(fun() ->
  Pointer = linglang_rt:new(1),
  Items = lists:duplicate(N, Pointer),
  %% Isolate root maintenance from collection; stress tests cover tracing.
  put({linglang_gc, budget}, 1000000000),
  #{collections := Collections} = linglang_rt:stats(),
  {reductions, Before} = process_info(self(), reductions),
  N = linglang_program:%s(Items),
  {reductions, After} = process_info(self(), reductions),
  #{collections := Collections} = linglang_rt:stats(),
  After - Before
 end),
 linglang_rt:collect(),
 #{live_cells := 0, root_frames := 0} = linglang_rt:stats(),
 Count
end, [1000, 2000, 4000]),
io:format("[~p,~p,~p]~n", Counts), halt(0).`, functionName("sum"))
			got, err := executeCompiledScript(t, program, script)
			if err != nil {
				t.Fatalf("pointer-list traversal: %v\n%s", err, got)
			}
			var counts []int
			if err := json.Unmarshal([]byte(got), &counts); err != nil || len(counts) != 3 {
				t.Fatalf("invalid reduction counts %q: %v", got, err)
			}
			t.Logf("reductions for 1000/2000/4000 pointers: %v", counts)
			for i := 1; i < len(counts); i++ {
				// Doubling a linear traversal should cost roughly twice as much.
				// Leave room for BEAM GC; the previous full-tail scans cost 4x.
				if counts[i] >= counts[i-1]*3 {
					t.Fatalf("pointer-list traversal grows superlinearly: %v", counts)
				}
			}
		})
	}
}

func TestGCTracesListCyclesAndReclaimsDroppedElements(t *testing.T) {
	script := `
Stale = linglang_rt:scope(fun() ->
 A = linglang_rt:new(#{links => []}),
 B = linglang_rt:scope(fun() ->
  Child = linglang_rt:new(#{links => [A]}),
  linglang_rt:write(A, #{links => [[Child]]}),
  Child
 end),
 linglang_rt:collect(),
 #{live_cells := 2} = linglang_rt:stats(),
 linglang_rt:write(A, #{links => []}),
 linglang_rt:collect(),
 #{live_cells := 1} = linglang_rt:stats(),
 try linglang_rt:read(B), error(dropped_list_element_survived)
 catch error:linglang_invalid_pointer -> ok end,
 A
end),
linglang_rt:collect(),
#{live_cells := 0, root_frames := 0} = linglang_rt:stats(),
try linglang_rt:read(Stale), error(list_cycle_survived)
catch error:linglang_invalid_pointer -> ok end,
halt(0).`
	got, err := executeScript(t, "func main() {}", script)
	if err != nil {
		t.Fatalf("list cycle collection: %v\n%s", err, got)
	}
}

func TestGCListRootSnapshots(t *testing.T) {
	script := `
linglang_rt:scope(fun() ->
 {A, B} = linglang_rt:scope(fun() ->
  {linglang_rt:new(1), linglang_rt:new(2)}
 end),
 Snapshot = #{items => [[A, B]]},
 Snapshot = linglang_rt:keep(Snapshot),
 Snapshot = linglang_rt:keep(Snapshot),
 linglang_rt:collect(),
 #{live_cells := 2, root_entries := 2} = linglang_rt:stats(),
 %% A block snapshot replaces prior values, even if keep rooted them twice.
 linglang_rt:roots([[[B]]]),
 linglang_rt:collect(),
 #{live_cells := 1, root_entries := 1} = linglang_rt:stats(),
 2 = linglang_rt:read(B),
 try linglang_rt:read(A), error(replaced_root_survived)
 catch error:linglang_invalid_pointer -> ok end,
 linglang_rt:scope(fun() ->
  C = linglang_rt:scope(fun() -> linglang_rt:new(3) end),
  linglang_rt:keep(#{items => [C]}),
  linglang_rt:collect(),
  #{live_cells := 2, root_frames := 2, root_entries := 2} = linglang_rt:stats(),
  3 = linglang_rt:read(C),
  linglang_rt:roots([]),
  linglang_rt:collect(),
  #{live_cells := 1} = linglang_rt:stats(),
  try linglang_rt:read(C), error(temporary_root_survived)
  catch error:linglang_invalid_pointer -> ok end
 end),
 linglang_rt:roots([]),
 linglang_rt:collect(),
 #{live_cells := 0, root_entries := 0} = linglang_rt:stats()
end),
#{root_frames := 0} = linglang_rt:stats(), halt(0).`
	got, err := executeScript(t, "func main() {}", script)
	if err != nil {
		t.Fatalf("list root snapshots: %v\n%s", err, got)
	}
}

func TestSupervisedListArgumentSnapshot(t *testing.T) {
	got, err := executeSupervised(t, `
type Args struct { parent Pid; values List[int] }
type Ready struct { worker Pid; values List[int] }
func worker(args Args) {
 send(args.parent, Ready{worker: self(), values: args.values})
 receive[int](-1)
 panic("restart with original list snapshot")
}
func main() {
 values := List[int]{4, 5}
 supervisor := startSupervisor(3, 5)
 child := supervise(supervisor, "worker", worker, Args{parent: self(), values: values}, ChildOptions{})
 if !child.ok { panic(child.reason) }
 first := receive[Ready](5000)
 if !first.ok { panic("first start timed out") }
 values = append(values, 6)
 send(first.value.worker, 1)
 replacement := receive[Ready](5000)
 if !replacement.ok { panic("replacement timed out") }
 println(first.value.values, replacement.value.values, replacement.value.worker != first.value.worker, len(values))
 stopSupervisor(supervisor)
}
`)
	if err != nil || got != "[4, 5] [4, 5] true 3\n" {
		t.Fatalf("got %q (%v)", got, err)
	}
}

func TestListRuntimeBoundary(t *testing.T) {
	script := `
linglang_rt:validate_message({list, int}, [1, 2]),
linglang_rt:validate_message({list, {list, string}}, [[<<"ok">>], []]),
linglang_rt:validate_message({list, int}, nil),
lists:foreach(fun(Value) ->
 try linglang_rt:validate_message({list, int}, Value), error(invalid_list_accepted)
 catch error:linglang_invalid_message -> ok end
end, [[true], [1 | 2], [1, <<"wrong">>], [1 bsl 63], #{}, 4]),
halt(0).`
	got, err := executeScript(t, "func main() {}", script)
	if err != nil {
		t.Fatalf("list runtime validation: %v\n%s", err, got)
	}
}

func TestJobQueueExample(t *testing.T) {
	source, err := os.ReadFile("../../examples/jobqueue.lang")
	if err != nil {
		t.Fatal(err)
	}
	got, err := executeSupervised(t, strings.TrimPrefix(string(source), "package main"))
	want := "Results: [16, 25, 36]\nCompleted: 3\nWorker restarts: 1\nSupervisor stopped: true\n"
	if err != nil || got != want {
		t.Fatalf("got %q (%v), want %q", got, err, want)
	}
}
