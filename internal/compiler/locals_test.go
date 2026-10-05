package compiler

import (
	"fmt"
	"strings"
	"testing"
)

func TestLocalControlFlow(t *testing.T) {
	tests := []struct{ name, source, want string }{
		{"branch_versions_and_early_returns", `
func calculate(flag bool) int {
 x := 1
 if flag { x = 3 } else { x = 5 }
 { x := 100; x++; println(x) }
 if x == 5 { return x }
 x += 7
 return x
}
func main() { println(calculate(true), calculate(false)) }
`, "101\n101\n10 5\n"},
		{"nested_loop_updates", `
func main() {
 total := 0
 outer := 0
 for outer < 4 {
  outer++
  for inner := 0; inner < 5; inner++ {
   if inner == 1 { total += 10; continue }
   if inner == 3 { total++; break }
   total += outer
  }
 }
 println(total, outer)
}
`, "64 4\n"},
		{"nested_value_struct_updates", `
type Inner struct { n int }
type Outer struct { inner Inner; flag bool }
func add(o Outer) Outer { o.inner.n += 3; return o }
func main() {
 a := Outer{inner: Inner{n: 2}}
 b := a
 for i := 0; i < 3; i++ { a.inner.n++; a.flag = !a.flag }
 a = add(a)
 println(a.inner.n, a.flag, b.inner.n)
}
`, "8 true 2\n"},
		{"field_addresses_and_pointer_boundaries", `
type Value struct { n int }
type Holder struct { p *Value }
func bump(p *int) { *p += 1 }
func main() {
 value := Value{n: 2}
 holder := Holder{p: &value}
 field := &holder.p.n
 bump(field)
 holder.p.n += 4
 println(value.n, *field)
}
`, "7 7\n"},
		{"address_taken_in_one_branch", `
func main() {
 n := 1
 var p *int
 flag := true
 if flag { p = &n } else { n = 20 }
 n += 2
 println(*p, n)
}
`, "3 3\n"},
		{"pointer_values_at_branch_merges", `
type Node struct { n int }
func makeNode(n int) *Node { return &Node{n: n} }
func main() {
 p := makeNode(1)
 for i := 0; i < 5; i++ {
  if i % 2 == 0 { p = makeNode(i) } else { p.n++ }
 }
 println(p.n)
}
`, "4\n"},
		{"grouped_initializers_keep_new_roots", `
type Node struct { n int }
func makeNode(n int) *Node { return &Node{n: n} }
func churn() int { for i := 0; i < 30; i++ { _ = makeNode(i) }; return 7 }
func main() {
 var (
  p = makeNode(3)
  q = makeNode(churn())
  n = p.n + q.n
 )
 println(n)
}
`, "10\n"},
		{"addressed_parameter_identity", `
func parameter(p *int) **int { return &p }
func main() { n := 1; p := parameter(&n); **p += 2; println(n) }
`, "3\n"},
		{"condition_updates_addressed_loop_variable", `
func condition(p *int) bool { *p += 1; return *p < 5 }
func main() {
 var first *int
 for i := 0; condition(&i); i++ {
  if first == nil { first = &i }
 }
 println(*first)
}
`, "1\n"},
		{"leaf_borrowed_pointer_return_survives_next_call", `
type Node struct { n int; next *Node }
func forward(p *Node) *Node { var alias *Node = p; return alias }
func link(a, b *Node) *Node { a.next = b; b.n++; return b }
func churn() int { for i := 0; i < 20; i++ { _ = &Node{n:i} }; return 7 }
func makeNode(n int) *Node { return &Node{n:n} }
func main() {
 a := makeNode(3)
 b := makeNode(4)
 println(forward(link(a,b)).n, churn(), a.next.n)
 var p = forward(makeNode(9))
 var q = makeNode(churn())
 println(p.n, q.n)
}
`, "5 7 5\n9 7\n"},
		{"shadowed_builtin_can_collect", `
type Node struct { n int }
func len(s string) *Node { for n := 0; n < 30; n++ { _ = &Node{n:n} }; return &Node{n:42} }
func read() int { p := len(""); _ = len(""); return p.n }
func main() { println(read()) }
`, "42\n"},
		{"collection_leaf_root_handoff", `
type Node struct { n int }
func node(n int) *Node { return &Node{n:n} }
func churn() int { for i:=0;i<30;i++{_=node(i)};return 9 }
func packed(p *Node) Map[int,List[*Node]] { return put(Map[int,List[*Node]]{},1,append(List[*Node]{},p)) }
func unpacked(m Map[int,List[*Node]]) *Node { return head(get(m,1).value).value }
func consume(m Map[int,List[*Node]], n int) { println(unpacked(m).n,n) }
func main() {
 consume(packed(node(7)),churn())
 p:=unpacked(packed(node(8)))
 _=churn()
 println(p.n)
}
`, "7 9\n8\n"},
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

func TestBorrowedRootLeafLowering(t *testing.T) {
	for _, tc := range []struct {
		name, prefix, body string
		borrow             bool
	}{
		{name: "pointer_update", body: "*p++; return *p", borrow: true},
		{name: "pointer_local", body: "var alias *int = p; return *alias", borrow: true},
		{name: "noncollecting_intrinsics", body: "assert(*p >= 0); _ = formatInt(*p); return *p", borrow: true},
		{name: "native_collections", body: "xs:=append(List[*int]{},p);xs=prepend(p,tail(xs));m:=put(Map[int,*int]{},1,head(xs).value);return *get(m,1).value", borrow: true},
		{name: "managed_local", body: "n := 1; q := &n; return *q + *p"},
		{name: "managed_literal", prefix: "type Node struct { n int }", body: "q := &Node{n:1}; return q.n + *p"},
		{name: "user_call", prefix: "func value() int { return 1 }", body: "return value() + *p"},
		{name: "builtin_shadow", prefix: "func len(s string) int { return 1 }", body: "_ = len(\"local\"); return *p"},
		{name: "loop", body: "for i := 0; i < 2; i++ { *p++ }; return *p"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "package main\n" + tc.prefix + "\nfunc helper(p *int) int {" + tc.body + "}\nfunc main() {}"
			program, err := Compile("leaf.lang", []byte(source))
			if err != nil {
				t.Fatal(err)
			}
			start := strings.Index(program, "\n"+functionName("helper")+"(")
			if start < 0 {
				t.Fatal("missing helper definition")
			}
			next := strings.Index(program[start+1:], "\n"+functionName("main")+"(")
			if next < 0 {
				t.Fatal("missing main definition")
			}
			body := program[start : start+1+next]
			protocol := strings.Contains(body, "linglang_rt:scope(") || strings.Contains(body, "linglang_rt:roots(") || strings.Contains(body, "linglang_rt:safepoint()") || strings.Contains(body, "linglang_rt:keep(")
			if protocol == tc.borrow {
				t.Fatalf("borrow=%v, root protocol=%v:\n%s", tc.borrow, protocol, body)
			}
		})
	}
}

func TestLeafCallerSafePoints(t *testing.T) {
	if _, err := Compile("bodyless.lang", []byte("package main\nfunc helper()\nfunc main(){helper()}")); err == nil || !strings.Contains(err.Error(), "bodyless") {
		t.Fatalf("bodyless declaration: %v", err)
	}
	for _, tc := range []struct {
		name, helper, main string
		safePoints         bool
	}{
		{"arithmetic", "func helper(n int) int { return n+1 }", "n:=0;for n<3{n=helper(n)}", false},
		{"branch", "func helper(n int) int { if n>0{return n};return -n }", "_=helper(-1)", false},
		{"allocation", "func helper(n int) int { p:=&n;return *p }", "_=helper(1)", true},
		{"callee_loop", "func helper(n int) int { for n<3{n++};return n }", "_=helper(1)", true},
		{"recursive", "func helper(n int) int { if n==0{return 0};return helper(n-1) }", "_=helper(2)", true},
		{"indirect", "func helper(n int) int { return other(n) };func other(n int) int { return n+1 }", "_=helper(1)", true},
		{"shadowed_intrinsic", "func helper(n int) int { return len(n) };func len(n int) int { p:=&n;return *p }", "_=helper(1)", true},
		{"rooted_caller", "func helper(n int) int { return n+1 }", "n:=1;p:=&n;for *p<3{*p=helper(*p)}", true},
		{"blocking", "func helper(n int) int { return receive[int](n).value }", "_=helper(0)", true},
		{"native_collections", "func helper(n int) int { return n }", "xs:=prepend(1,List[int]{});xs=append(xs,2);xs=tail(xs);m:=put(Map[int,int]{},1,head(xs).value);assert(get(m,1).value==2);_=remove(m,1)", false},
		{"native_text", "func helper(n int) int { return n }", "xs:=split(\"1,2\",\",\");s:=join(xs,\"\");assert(parseInt(trim(s)).value==12)", false},
		{"rooted_collections", "func helper(n int) int { return n }", "n:=1;xs:=prepend(&n,List[*int]{});for len(xs)>0{xs=tail(xs)}", true},
		{"builtin_shadow", "func helper(n int) int { return n };func len(n int) int { p:=&n;return *p }", "_=len(1)", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Put the caller before a callee in another file; discovery must not
			// depend on declaration order or classify by spelling alone.
			program, err := CompileFiles([]SourceFile{
				{Filename: "a.lang", Source: []byte("package main\nfunc main(){" + tc.main + "}")},
				{Filename: "z.lang", Source: []byte("package main\n" + tc.helper)},
			})
			if err != nil {
				t.Fatal(err)
			}
			start := strings.Index(program, "\n"+functionName("main")+"(")
			end := strings.Index(program, "\n"+functionName("helper")+"(")
			if start < 0 || end <= start {
				t.Fatal("missing caller/callee definitions")
			}
			body := program[start:end]
			if got := strings.Contains(body, "linglang_rt:safepoint()"); got != tc.safePoints {
				t.Fatalf("safe points=%v, want %v:\n%s", got, tc.safePoints, body)
			}
		})
	}
}

func TestLocalAllocationBudgets(t *testing.T) {
	tests := []struct {
		name, source string
		allocations  int
	}{
		{"arithmetic", `func main() { n := 0; for i := 0; i < 10000; i++ { n += i }; println(n) }`, 0},
		{"calls", `func add(a int, b int) int { return a + b }; func main() { n := 0; for i := 0; i < 10000; i++ { n = add(n, i) }; println(n) }`, 0},
		{"value_structs", `type Value struct { n int }; func main() { v := Value{}; for i := 0; i < 10000; i++ { v.n += i }; println(v.n) }`, 0},
		{"pointer_calls", `type Value struct { n int }; func bump(p *Value) { p.n++ }; func main() { v := Value{}; for i := 0; i < 10000; i++ { bump(&v) }; println(v.n) }`, 1},
		{"pointer_field_boundary", `type Value struct { n int }; type Holder struct { p *Value }; func main() { v := Value{}; h := Holder{p: &v}; p := &h.p.n; *p = 9; println(v.n) }`, 1},
		{"addressed_loop_identity", `func main() { var p *int; for i := 0; i < 3; i++ { p = &i }; println(*p) }`, 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			script := fmt.Sprintf(`linglang_rt:set_gc_stress(true), linglang_program:main(), #{allocated_cells := %d, live_cells := 0, root_frames := 0} = linglang_rt:stats(), halt(0).`, tc.allocations)
			got, err := executeScript(t, tc.source, script)
			if err != nil {
				t.Fatalf("allocation budget: %v\n%s", err, got)
			}
		})
	}
}

func TestLargeLiveEnvironment(t *testing.T) {
	var source strings.Builder
	source.WriteString("func main() {\n")
	var names []string
	for i := 0; i < 205; i++ {
		name := fmt.Sprintf("n%d", i)
		fmt.Fprintf(&source, "%s := %d\n", name, i)
		names = append(names, name)
	}
	fmt.Fprintf(&source, "println(%s)\n}", strings.Join(names, "+"))
	got, err := execute(t, source.String())
	if err != nil || got != "20910\n" {
		t.Fatalf("large environment: %v\n%s", err, got)
	}
}

func TestOptimizationOutputIsDeterministic(t *testing.T) {
	source := []byte("package main\nfunc main() { x:=0; for i:=0;i<10;i++ { if i>3 { x+=i } }; println(x) }")
	first, err := Compile("test.lang", source)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		next, err := Compile("test.lang", source)
		if err != nil || first != next {
			t.Fatalf("nondeterministic compiler output: %v", err)
		}
	}
}
