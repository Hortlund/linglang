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
