package compiler

import (
	"strings"
	"testing"
)

func TestMapSemantics(t *testing.T) {
	tests := []struct{ name, source, want string }{
		{"nil_empty_and_persistent_updates", `
func main() {
 var zero Map[string,int]
 empty := Map[string,int]{}
 original := Map[string,int]{"a": 1, "b": 2}
 copy := original
 changed := put(original, "a", 9)
 changed = put(changed, "c", 3)
 removed := remove(changed, "b")
 println(len(zero), len(empty), zero == nil, empty == nil, remove(zero, "a") == nil)
 println(get(zero, "a").value, get(zero, "a").ok, get[int,bool](nil, 1).value)
 println(get(original, "a").value, get(copy, "a").value, get(changed, "a").value)
 println(len(original), len(changed), len(removed), get(removed, "b").ok, len(remove(removed, "missing")))
 println(len(put(zero, "x", 0)), get(put(zero, "x", 0), "x").ok)
 println(remove(Map[int,int]{1: 2}, 1) == nil)
}`, "0 0 true false true\n0 false false\n1 1 9\n2 3 2 false 2\n1 true\nfalse\n"},
		{"nested_values_are_snapshots", `
type Item struct { n int; labels List[string]; scores Map[string,int] }
func main() {
 item := Item{n: 1, labels: List[string]{"first"}, scores: Map[string,int]{"score": 7}}
 values := Map[int,Item]{1: item}
 item.n = 9
 item.labels = append(item.labels, "second")
 item.scores = put(item.scores, "score", 10)
 saved := get(values, 1).value
 println(saved.n, len(saved.labels), get(saved.scores, "score").value, item.n)
 missing := get(values, 2)
 println(missing.ok, missing.value.n, missing.value.labels == nil, missing.value.scores == nil)
 nested := Map[string,Map[int,List[int]]]{"x": Map[int,List[int]]{2: List[int]{3, 4}}}
 println(get(get(nested, "x").value, 2).value)
}`, "1 1 7 9\nfalse 0 true true\n[3, 4]\n"},
		{"literal_and_call_evaluation_order", `
func next(p *int) int { *p += 1; return *p }
func duplicate(p *int) int { *p += 1; return 1 }
func main() {
 n := 0
 values := Map[int,int]{next(&n): next(&n), next(&n): next(&n)}
 println(n, get(values, 1).value, get(values, 3).value)
 values = Map[int,int]{duplicate(&n): next(&n), duplicate(&n): next(&n)}
 println(n, len(values), get(values, 1).value)
 values = put(values, next(&n), next(&n))
 println(n, get(values, 9).value)
 get := 7
 put := 8
 remove := 9
 println(get, put, remove)
}`, "4 2 4\n8 1 8\n10 10\n7 8 9\n"},
		{"pointers_temporaries_and_recursive_local_types", `
type Node struct { n int; children Map[string,Node]; links Map[int,*Node] }
func node(n int) *Node { return &Node{n: n} }
func churn() *Node { for i := 0; i < 30; i++ { _ = node(i) }; return node(9) }
func consume(values Map[int,*Node], other *Node) { println(get(values, 1).value.n, other.n) }
func main() {
 consume(Map[int,*Node]{1: node(7)}, churn())
 values := Map[int,*Node]{1: node(1), 2: churn()}
 copy := values
 values = put(values, 3, churn())
 get(copy, 1).value.n++
 _ = churn()
 println(get(values, 1).value.n, get(values, 2).value.n, get(values, 3).value.n, len(copy))
 root := node(4)
 root.children = Map[string,Node]{"child": Node{n: 5}}
 root.links = put(root.links, 1, root)
 _ = churn()
 println(get(root.children, "child").value.n, get(root.links, 1).value == root)
 missing := get[int,*Node](nil, 1)
 println(missing.ok, missing.value == nil)
}`, "7 9\n2 9 9 2\n5 true\nfalse true\n"},
		{"pointer_to_map_identity", `
func main() {
 items := Map[int,int]{1: 2}
 copy := items
 pointer := &items
 *pointer = put(*pointer, 1, 3)
 println(get(items, 1).value, get(copy, 1).value, pointer == &items)
 holders := Map[string,*Map[int,int]]{"x": pointer}
 *get(holders, "x").value = remove(*pointer, 1)
 println(len(items), len(copy))
}`, "3 2 true\n0 1\n"},
		{"typed_message_snapshots", `
type Item struct { values Map[int,List[string]] }
func main() {
 items := Map[string,Item]{"x": Item{values: Map[int,List[string]]{1: List[string]{"ok"}}}}
 send(self(), items)
 items = put(items, "y", Item{})
 println(receive[Map[int,Item]](0).ok, receive[Map[string,int]](0).ok)
 delivery := receive[Map[string,Item]](1000)
 println(delivery.ok, len(delivery.value), len(items), get(get(delivery.value, "x").value.values, 1).value)
 var zero Map[int,int]
 send(self(), zero)
 send(self(), Map[int,int]{})
 println(receive[Map[int,int]](1000).value == nil, receive[Map[int,int]](1000).value == nil)
}`, "false false\ntrue 1 2 [ok]\ntrue false\n"},
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

func TestMapDiagnostics(t *testing.T) {
	tests := []struct{ source, want string }{
		{`func main() { values := Map[bool,int]{true: 1}; println(values) }`, "unsupported type"},
		{`func main() { println(Map[*int,int]{}) }`, "unsupported type"},
		{`func main() { println(Map[int,float64]{}) }`, "unsupported type"},
		{`func main() { println(get[bool,int](nil, true).ok) }`, "unsupported type"},
		{`func main() { println(get[int,float64](nil, 1).ok) }`, "unsupported type"},
		{`func main() { println(remove[bool,int](nil, true) == nil) }`, "unsupported type"},
		{`func main() { m := Map[int,int]{1: 2}; println(m[1]) }`, "unsupported expression"},
		{`func main() { m := Map[int,int]{}; m[1] = 2 }`, "unsupported reference target"},
		{`func main() { for range (Map[int,int]{}) {} }`, "range currently requires"},
		{`func main() { send(self(), Map[int,*int]{}) }`, "process-local pointers"},
		{`func main() { send(self(), Map[string,List[Monitor]]{}) }`, "monitor handles"},
		{`func main() { send(self(), Map[int,Supervisor]{}) }`, "supervisor handles"},
		{`type Node struct { children Map[int,Node] }; func main() { send(self(), Node{}) }`, "recursive message types"},
		{`func main() { m := map[int]int{}; println(m) }`, "unsupported type"},
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

func TestMapsUseNativeBEAMValues(t *testing.T) {
	source := `func main() {
 var items Map[int,int]
 for i := 0; i < 1000; i++ { items = put(items, i, i) }
 total := 0
 for i := 0; i < 1000; i++ { total += get(items, i).value }
 println(total, len(items))
}`
	script := `linglang_rt:set_gc_stress(true), linglang_program:main(),
 #{allocated_cells := 0, live_cells := 0, root_frames := 0} = linglang_rt:stats(), halt(0).`
	got, err := executeScript(t, source, script)
	if err != nil || got != "499500 1000\n" {
		t.Fatalf("native map allocation budget: %v\n%s", err, got)
	}
}

func TestSupervisedMapArgumentSnapshot(t *testing.T) {
	got, err := executeSupervised(t, `
type Args struct { parent Pid; values Map[string,int] }
type Ready struct { worker Pid; values Map[string,int] }
func worker(args Args) {
 send(args.parent, Ready{worker: self(), values: args.values})
 receive[int](-1)
 panic("restart with original map snapshot")
}
func main() {
 values := Map[string,int]{"score": 4}
 supervisor := startSupervisor(3, 5)
 child := supervise(supervisor, "worker", worker, Args{parent: self(), values: values}, ChildOptions{})
 if !child.ok { panic(child.reason) }
 first := receive[Ready](5000)
 if !first.ok { panic("first start timed out") }
 values = put(values, "score", 9)
 values = put(values, "extra", 1)
 send(first.value.worker, 1)
 replacement := receive[Ready](5000)
 if !replacement.ok { panic("replacement timed out") }
 println(get(first.value.values, "score").value, get(replacement.value.values, "score").value,
  replacement.value.worker != first.value.worker, len(values), len(replacement.value.values))
 if !stopSupervisor(supervisor) { panic("supervisor did not stop") }
}
`)
	if err != nil || got != "4 4 true 2 1\n" {
		t.Fatalf("got %q (%v)", got, err)
	}
}

func TestMapRuntimeBoundary(t *testing.T) {
	script := `
linglang_rt:validate_message({map, int, string}, #{1 => <<"ok">>}),
linglang_rt:validate_message({map, string, {map, int, bool}}, #{<<"x">> => #{2 => true}}),
linglang_rt:validate_message({map, int, string}, nil),
linglang_rt:validate_message({map, int, string}, #{}),
lists:foreach(fun(Value) ->
 try linglang_rt:validate_message({map, int, string}, Value), error(invalid_map_accepted)
 catch error:linglang_invalid_message -> ok end
end, [#{true => <<"ok">>}, #{<<"1">> => <<"ok">>}, #{1 => true},
       #{(1 bsl 63) => <<"ok">>}, #{field_6e => <<"ok">>}, [], 4]),
halt(0).`
	got, err := executeScript(t, "func main() {}", script)
	if err != nil {
		t.Fatalf("map runtime validation: %v\n%s", err, got)
	}
}

func TestGCTracesMapCyclesAndReclaimsRemovedValues(t *testing.T) {
	script := `
Stale = linglang_rt:scope(fun() ->
 A = linglang_rt:new(#{}),
 B = linglang_rt:scope(fun() ->
  Child = linglang_rt:new(#{<<"parent">> => A}),
  linglang_rt:write(A, #{<<"children">> => #{1 => [Child]}}),
  Child
 end),
 linglang_rt:collect(),
 #{live_cells := 2} = linglang_rt:stats(),
 linglang_rt:write(A, linglang_rt:map_remove(linglang_rt:read(A), <<"children">>)),
 linglang_rt:collect(),
 #{live_cells := 1} = linglang_rt:stats(),
 try linglang_rt:read(B), error(removed_map_value_survived)
 catch error:linglang_invalid_pointer -> ok end,
 A
end),
linglang_rt:collect(),
#{live_cells := 0, root_frames := 0} = linglang_rt:stats(),
try linglang_rt:read(Stale), error(map_cycle_survived)
catch error:linglang_invalid_pointer -> ok end,
halt(0).`
	got, err := executeScript(t, "func main() {}", script)
	if err != nil {
		t.Fatalf("map cycle collection: %v\n%s", err, got)
	}
}
