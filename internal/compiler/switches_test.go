package compiler

import (
	"strings"
	"testing"
)

func TestConstantsAndSwitchSemantics(t *testing.T) {
	tests := []struct{ name, source, want string }{
		{"constant_groups_folding_and_shadowing", `
const (
 Add = iota + 1
 Done
 Read
)
const Prefix string = "job:"
const Enabled bool = true
const Wide = 9223372036854775808
const Answer, Label = 6 * 7, Prefix + "ok"
func main() {
 const (
  Local = iota + 10
  Next
 )
 const Small int = Wide - 1
 println(Add, Done, Read, Answer, Label, Enabled, Local, Next, Small)
 { const Answer = 9; println(Answer) }
 println(Answer)
}
`, "1 2 3 42 job:ok true 10 11 9223372036854775807\n9\n42\n"},
		{"tagged_cases_default_and_no_fallthrough", `
const Add = 1
func classify(n int) string {
 switch n {
 default: return "other"
 case Add, 2: return "small"
 case 3: return "three"
 }
}
func main() {
 println(classify(1), classify(2), classify(3), classify(4))
 n := 0
 switch 1 { case 1: n++; case 2: n += 10 }
 switch 9 { case 1: n += 100 }
 switch "ready" { case "ready": println(n); default: panic("wrong string case") }
 switch false { case true: panic("wrong bool case"); case false: println("false") }
}
`, "small small three other\n1\nfalse\n"},
		{"tag_and_cases_evaluated_once_in_order", `
func mark(trace *int, n int) int { *trace = *trace * 10 + n; return n }
func main() {
 trace := 0
 switch mark(&trace, 2) {
 default: panic("default ran too soon")
 case mark(&trace, 1), mark(&trace, 2), mark(&trace, 3): println(trace)
 case mark(&trace, 4): panic("late case ran")
 }
 println(trace)
}
`, "212\n212\n"},
		{"tagless_conditions_short_circuit", `
func check(trace *int, n int, matches bool) bool { *trace = *trace * 10 + n; return matches }
func main() {
 trace := 0
 switch {
 default: panic("wrong default")
 case check(&trace, 1, false): panic("wrong first case")
 case check(&trace, 2, false), check(&trace, 3, true), check(&trace, 4, true): println(trace)
 case check(&trace, 5, true): panic("late case ran")
 }
 println(trace)
}
`, "123\n123\n"},
		{"initializer_and_case_scopes", `
func main() {
 n := 100
 switch n := 2; n {
 case 1: value := 10; println(value)
 case 2: value := 20; println(n, value)
 default: value := 30; println(value)
 }
 switch n++; { case n == 101: println(n) }
 switch constValue := 3; constValue { case 3: const Result = 7; println(Result) }
 println(n)
}
`, "2 20\n101\n7\n101\n"},
		{"break_continue_and_nested_control_flow", `
func main() {
 total := 0
 for i := 0; i < 4; i++ {
  switch i {
  case 0: total += 1; break; total += 1000
  case 1: continue
  case 2:
   switch { case true: total += 2; break; total += 1000 }
   for j := 0; j < 3; j++ { total += 3; break }
   total += 4
  default: total += 5
  }
  total += 10
 }
 println(total)
 for {
  switch 1 { case 1: break }
  total++
  break
 }
 println(total)
}
`, "45\n46\n"},
		{"nested_switch_continue_targets_loop", `
func main() {
 total := 0
 for i := 0; i < 3; i++ {
  switch i {
  case 1: switch { case true: total += 2; continue }
  default: total += 1
  }
  total += 10
 }
 println(total)
}
`, "24\n"},
		{"tag_snapshot_and_reference_comparisons", `
type Value struct { n int; p *int }
func change(p *int) int { *p = 2; return 1 }
func main() {
 n := 1
 switch n { case change(&n): println("snapshot", n); default: panic("tag was reread") }
 p := &n
 switch p { case nil: panic("nil case"); case &n: println(*p) }
 value := Value{n: 7, p: p}
 switch value {
 case Value{n: 7, p: &n}: println("struct")
 default: panic("struct mismatch")
 }
 switch self() { case self(): println("pid") }
}
`, "snapshot 2\n2\nstruct\npid\n"},
		{"reference_liveness_at_merges_and_returns", `
type Node struct { n int }
func makeNode(n int) *Node { return &Node{n: n} }
func pick(n int) *Node {
 switch p := makeNode(n); p.n {
 case 1: return p
 default: return makeNode(n + 10)
 }
}
func main() {
 var saved *int
 p := makeNode(0)
 for i := 0; i < 4; i++ {
  switch i {
  case 0, 2: p = pick(1)
  default: p = pick(i)
  }
  switch n := p.n; n {
  case 1: saved = &n; n++
  default: p.n++
  }
 }
 println(*saved, p.n)
}
`, "2 14\n"},
		{"empty_switch_still_evaluates_init_and_tag", `
func bump(p *int) int { *p += 1; return *p }
func main() {
 n := 0
 switch bump(&n); bump(&n) {}
 switch {}
 switch bump(&n) { default: }
 println(n)
}
`, "3\n"},
		{"temporary_tag_survives_case_collections", `
type Node struct { n int }
type Box struct { node *Node }
func makeNode(n int) *Node { return &Node{n: n} }
func churn() *Node { for i := 0; i < 30; i++ { _ = makeNode(i) }; return nil }
func main() {
 switch (Box{node: makeNode(7)}) {
 case Box{node: churn()}: panic("unexpected match")
 case Box{node: churn()}: panic("unexpected later match")
 default: println("alive")
 }
 switch makeNode(8) {
 case churn(): panic("unexpected pointer match")
 default: println("pointer")
 }
}
`, "alive\npointer\n"},
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

func TestConstantAndSwitchDiagnostics(t *testing.T) {
	tests := []struct{ source, want string }{
		{`const Bad = 1.5; func main() {}`, "unsupported constant type"},
		{`func main() { const Bad = 1i }`, "unsupported constant type"},
		{`const Bad uint = 1; func main() {}`, "unsupported constant type"},
		{`const Bad int = 9223372036854775808; func main() {}`, "overflows"},
		{`const Fixed = 1; func main() { Fixed = 2 }`, "cannot assign"},
		{`func main() { switch 1 { case 1: fallthrough; case 2: println(2) } }`, "fallthrough is not supported"},
		{`func main() { switch 1 { case "one": } }`, "cannot convert"},
		{`func main() { switch 1 { case 1: case 1: } }`, "duplicate case"},
		{`func main() { switch { default: default: } }`, "multiple defaults"},
		{`func main() { switch n := 1; n { case 1: }; println(n) }`, "undefined: n"},
		{`func main() { switch 1 { case 1: n := 1; println(n); case 2: println(n) } }`, "undefined: n"},
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
