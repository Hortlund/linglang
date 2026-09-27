package compiler

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func command(t *testing.T, name string, args ...string) *exec.Cmd {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return exec.CommandContext(ctx, name, args...)
}

func execute(t *testing.T, source string) (string, error) {
	t.Helper()
	script := "linglang_rt:set_gc_stress(true), try linglang_program:main() of _ -> halt(0) catch _:Reason -> io:format(\"ERROR:~p~n\", [Reason]), halt(1) end."
	got, err := executeScript(t, source, script)
	baseline, baselineErr := executeScriptWithOptions(t, source, script, Options{DisableOptimizations: true})
	if got != baseline || (err != nil) != (baselineErr != nil) {
		t.Fatalf("optimized/baseline mismatch: %q (%v) versus %q (%v)", got, err, baseline, baselineErr)
	}
	return got, err
}

func executeScript(t *testing.T, source, script string) (string, error) {
	t.Helper()
	return executeScriptWithOptions(t, source, script, Options{})
}

func executeScriptWithOptions(t *testing.T, source, script string, options Options) (string, error) {
	t.Helper()
	if _, err := exec.LookPath("erlc"); err != nil {
		t.Fatal("integration tests require Erlang/OTP (erlc)")
	}
	if _, err := exec.LookPath("erl"); err != nil {
		t.Fatal("integration tests require Erlang/OTP (erl)")
	}
	program, err := CompileWithOptions("test.lang", []byte("package main\n"+source), options)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	sources := RuntimeSources()
	sources["linglang_program.erl"] = program
	args := []string{"-o", dir}
	for name, text := range sources {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		args = append(args, filepath.Join(dir, name))
	}
	output, err := command(t, "erlc", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("erlc: %v\n%s\n%s", err, output, program)
	}
	script = "try begin " + strings.TrimSuffix(strings.TrimSpace(script), ".") + " end catch LLTestClass:LLTestReason:LLTestStack -> io:format(\"TEST FAILURE ~p:~p~n~p~n\", [LLTestClass, LLTestReason, LLTestStack]), halt(1) end."
	cmd := command(t, "erl", "-noshell", "-pa", dir, "-eval", script)
	cmd.Dir = dir
	output, err = cmd.CombinedOutput()
	return string(output), err
}

func TestBEAMSemantics(t *testing.T) {
	tests := []struct{ name, source, want string }{
		{"alias_and_value_copy", `
type User struct { age int }
func birthday(p *User) { p.age++ }
func main() {
 u := User{age: 30}
 copy := u
 birthday(&u)
 p := &u
 q := p
 q.age++
 println(u.age, copy.age, p.age, p == q)
}`, "32 30 32 true\n"},
		{"pointer_parameter_is_a_value", `
func redirect(p *int) { v := 99; p = &v; *p = 100 }
func replace(p **int) { v := 42; *p = &v }
func main() { a := 1; p := &a; redirect(p); println(a, *p); replace(&p); println(a, *p) }
`, "1 1\n1 42\n"},
		{"escaping_reference", `
type User struct { age int }
func makeUser() *User { u := User{age: 40}; return &u }
func main() { p := makeUser(); p.age++; q := &User{age: 7}; println(p.age, q.age) }
`, "41 7\n"},
		{"field_pointer_tracks_reassignment", `
type Inner struct { n int }
type Outer struct { inner Inner }
func main() {
 u := Outer{inner: Inner{n: 1}}
 p := &u.inner.n
 copy := u
 u = Outer{inner: Inner{n: 9}}
 println(*p)
 *p += 3
 println(u.inner.n, copy.inner.n, p == &u.inner.n)
}
`, "9\n12 1 true\n"},
		{"struct_copy_preserves_pointer_fields", `
type Node struct { value int; next *Node }
func main() {
 tail := Node{value: 2}
 head := Node{value: 1, next: &tail}
 copy := head
 copy.value = 10
 copy.next.value = 20
 tail.next = &head
 println(head.value, tail.value, copy.value, tail.next == &head)
}
`, "1 20 10 true\n"},
		{"control_flow_and_shadowing", `
func find() int {
 total := 0
 for i := 0; i < 10; i++ {
  if i == 2 { continue }
  if i == 5 { break }
  total += i
 }
 { total := 99; println(total) }
 for { if total == 8 { return total }; return -1 }
}
func main() { println(find()); x := 0; for x < 3 { x++ }; println(x) }
`, "99\n8\n3\n"},
		{"nested_loops", `
func main() {
 count := 0
 for i := 0; i < 3; i++ {
  for j := 0; j < 5; j++ {
   if j == 1 { continue }
   if j == 3 { break }
   count++
  }
 }
 println(count)
}
`, "6\n"},
		{"loop_references_have_iteration_identity", `
func main() {
 var first *int
 var last *int
 for i := 0; i < 3; i++ {
  if i == 0 { first = &i }
  last = &i
 }
 println(*first, *last, first == last)
 j := 0
 for j = 0; j < 3; j++ { last = &j }
 println(*last, j)
}
`, "0 2 false\n3 3\n"},
		{"short_circuit_and_evaluation_order", `
func bump(p *int) int { *p += 1; return *p }
func yes(p *int) bool { *p += 10; return true }
func pair(a int, b int) { println(a, b) }
func main() {
 n := 0
 a := false && yes(&n)
 b := true || yes(&n)
 pair(bump(&n), bump(&n))
 println(a, b, n)
 println(bump(&n) - bump(&n))
}
`, "1 2\nfalse true 2\n-1\n"},
		{"assignment_target_evaluated_once", `
type Value struct { n int }
func target(calls *int, value *Value) *Value { *calls += 1; return value }
func main() {
 calls := 0
 value := Value{n: 10}
 target(&calls, &value).n += 5
 target(&calls, &value).n++
 println(calls, value.n)
}
`, "2 16\n"},
		{"zero_values_and_strings", `
type Item struct { n int; ok bool; title string; next *Item }
func main() {
 var item Item
 println(item.n, item.ok, item.title == "", item.next == nil)
 greeting := "Hej "
 println(greeting + "världen 🌍")
}
`, "0 false true true\nHej världen 🌍\n"},
		{"signed_int64", `
func main() {
 n := 9223372036854775807
 n++
 println(n)
 println(-n, n / -1)
 a := -7
 println(a / 3, a % 3)
}
`, "-9223372036854775808\n-9223372036854775808 -9223372036854775808\n-2 -1\n"},
		{"recursion", `
func factorial(n int) int { if n <= 1 { return 1 }; return n * factorial(n - 1) }
func main() { println(factorial(6)) }
`, "720\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := execute(t, tc.source)
			if err != nil {
				t.Fatalf("execution: %v\n%s", err, got)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNilDereferenceFails(t *testing.T) {
	got, err := execute(t, "func main() { var p *int; println(*p) }")
	if err == nil || !strings.Contains(got, "linglang_nil_pointer") {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestDiagnostics(t *testing.T) {
	tests := []struct{ source, want string }{
		{"package main\nfunc main() { x := 1; x = true; println(x) }", "cannot use true"},
		{"package main\nfunc main() { println(missing) }", "undefined: missing"},
		{"package other\nfunc main() {}", "only package main"},
		{"package main\nimport \"fmt\"\nfunc main() { fmt.Println(1) }", "imports are not supported"},
		{"package main\nfunc main() { go main() }", "unsupported statement"},
		{"package main\nfunc main() { x := []int{1}; println(x) }", "unsupported type"},
		{"package main\nfunc main() { x := 1.5; println(x) }", "unsupported type"},
		{"package main\nfunc main() { a, b := 1, 2; println(a,b) }", "multiple assignment"},
		{"package main\nvar x = 1\nfunc main() { println(x) }", "top-level variables"},
		{"package main\nfunc main() { defer main() }", "unsupported statement"},
		{"package main\nfunc init() {}\nfunc main() {}", "init and blank function"},
		{"package main\nfunc main() { var f = func() {}; f() }", "unsupported type"},
		{"package main\nfunc f() {}", "entry point is required"},
		{"package main\ntype N struct { x int }\nfunc main() { n := N{1}; println(n.x) }", "named fields"},
	}
	for _, tc := range tests {
		_, err := Compile("bad.lang", []byte(tc.source))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s\ngot %v, want %q", tc.source, err, tc.want)
		}
		if err != nil && !strings.Contains(err.Error(), "bad.lang:") {
			t.Errorf("missing source location: %v", err)
		}
	}
}

func TestRuntimeRejectsCrossProcessReference(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "linglang_rt.erl")
	if err := os.WriteFile(path, []byte(Runtime), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := command(t, "erlc", "-Werror", "-o", dir, path).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	script := `linglang_rt:scope(fun() -> P = linglang_rt:new(1), Parent = self(), spawn(fun() -> Result = try linglang_rt:read(P), accepted catch error:linglang_cross_process_pointer -> rejected end, Parent ! Result end), receive rejected -> halt(0); _ -> halt(1) after 1000 -> halt(2) end end).`
	if out, err := command(t, "erl", "-noshell", "-pa", dir, "-eval", script).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
}
