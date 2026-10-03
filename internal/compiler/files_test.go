package compiler

import (
	"strings"
	"testing"
)

func TestCompileFilesBEAMSemantics(t *testing.T) {
	sources := []SourceFile{
		{Filename: "workers.lang", Source: []byte(`package main
const Base = 7
func makeNode(n int) *Node { return &Node{n: n} }
func churn() { for i := 0; i < 30; i++ { _ = makeNode(i) } }
func total(values Map[string,List[int]]) int {
 sum := 0
 for _, n := range get(values, "beans").value { sum += n }
 return sum
}
func worker(args Args) {
 send(args.parent, Ready{pid: self(), sum: total(args.values)})
 receive[int](-1)
 panic("restart across source files")
}`)},
		{Filename: "types.lang", Source: []byte(`package main
type Node struct { n int; links Map[int,*Node] }
type Args struct { parent Pid; values Map[string,List[int]] }
type Ready struct { pid Pid; sum int }`)},
		{Filename: "main.lang", Source: []byte(`package main
func main() {
 nodes := Map[int,*Node]{1: makeNode(Base), 2: makeNode(9)}
 get(nodes, 1).value.links = nodes
 churn()
 println(get(nodes, 1).value.n + get(get(nodes, 1).value.links, 2).value.n)
 values := Map[string,List[int]]{"beans": List[int]{1, 2}}
 supervisor := startSupervisor(3, 5)
 child := supervise(supervisor, "worker", worker, Args{parent: self(), values: values}, ChildOptions{})
 if !child.ok { panic(child.reason) }
 first := receive[Ready](5000)
 if !first.ok { panic("first worker missing") }
 values = put(values, "beans", List[int]{9})
 for _, n := range get(values, "beans").value { println(n) }
 send(first.value.pid, 1)
 replacement := receive[Ready](5000)
 if !replacement.ok { panic("replacement missing") }
 println(first.value.sum, replacement.value.sum, first.value.pid != replacement.value.pid)
 if !stopSupervisor(supervisor) { panic("supervisor did not stop") }
}`)},
	}
	script := `logger:set_primary_config(level, emergency), linglang_rt:set_gc_stress(true),
 linglang_program:main(), undefined = get(linglang_supervisors),
 #{live_cells := 0, root_frames := 0} = linglang_rt:stats(), halt(0).`
	for _, options := range []Options{{}, {DisableOptimizations: true}} {
		program, err := CompileFilesWithOptions(sources, options)
		if err != nil {
			t.Fatal(err)
		}
		got, err := executeCompiledScript(t, program, script)
		if err != nil || got != "16\n9\n3 3 true\n" {
			t.Fatalf("options=%+v: got %q (%v)", options, got, err)
		}
	}
}

func TestCompileFilesDeterministicOrderAndRecursion(t *testing.T) {
	sources := []SourceFile{
		{Filename: "z.lang", Source: []byte("package main\nfunc odd(n int) bool { if n == 0 { return false }; return even(n - 1) }")},
		{Filename: "a.lang", Source: []byte("package main\nfunc even(n int) bool { if n == 0 { return true }; return odd(n - 1) }; func main() { println(even(8), odd(9)) }")},
	}
	for _, options := range []Options{{}, {DisableOptimizations: true}} {
		first, err := CompileFilesWithOptions(sources, options)
		if err != nil {
			t.Fatal(err)
		}
		if sources[0].Filename != "z.lang" {
			t.Fatal("compilation reordered the caller's input")
		}
		second, err := CompileFilesWithOptions([]SourceFile{sources[1], sources[0]}, options)
		if err != nil || first != second {
			t.Fatalf("order-dependent output (options=%+v): %v", options, err)
		}
		got, err := executeCompiledScript(t, first, "linglang_rt:set_gc_stress(true), linglang_program:main(), halt(0).")
		if err != nil || got != "true true\n" {
			t.Fatalf("cross-file recursion: got %q (%v)", got, err)
		}
	}
}

func TestCompileFilesDiagnostics(t *testing.T) {
	tests := []struct{ name, helper, location, message string }{
		{"syntax", "package main\nfunc helper( {", "helper.lang:2:", "expected"},
		{"type", "package main\nfunc helper() int { return \"wrong\" }", "helper.lang:2:", "cannot use"},
		{"lowering", "package main\nfunc helper() int {\n defer println(1)\n return 1\n}", "helper.lang:3:", "unsupported statement"},
		{"unsupported_type", "package main\nfunc helper() int {\n n := 1.5\n println(n)\n return 1\n}", "helper.lang:3:", "unsupported type"},
		{"import", "package main\nimport \"fmt\"\nfunc helper() int { return 1 }", "helper.lang:2:", "imports are not supported"},
		{"package", "package beans\nfunc helper() int { return 1 }", "helper.lang:1:", "only package main"},
		{"duplicate", "package main\nfunc helper() int { return 1 }; func main() {}", "main.lang:2:", "main redeclared"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, options := range []Options{{}, {DisableOptimizations: true}} {
				_, err := CompileFilesWithOptions([]SourceFile{
					{Filename: "main.lang", Source: []byte("package main\nfunc main() { println(helper()) }")},
					{Filename: "helper.lang", Source: []byte(tc.helper)},
				}, options)
				if err == nil || !strings.Contains(err.Error(), tc.location) || !strings.Contains(err.Error(), tc.message) {
					t.Fatalf("options=%+v: got %v, want %s and %q", options, err, tc.location, tc.message)
				}
			}
		})
	}
	_, err := CompileFiles([]SourceFile{
		{Filename: "a.lang", Source: []byte("package main\nfunc helper() {}")},
		{Filename: "z.lang", Source: []byte("package main\nfunc main(n int) {}")},
	})
	if err == nil || !strings.Contains(err.Error(), "z.lang:2:") || !strings.Contains(err.Error(), "main must have no") {
		t.Fatalf("main diagnostic lost its original file: %v", err)
	}
	for _, tc := range []struct {
		sources []SourceFile
		message string
	}{
		{nil, "no source files"},
		{[]SourceFile{{Filename: "main.lang", Source: []byte("package main\nfunc main() {}")}, {Filename: "main.lang", Source: []byte("package main")}}, "duplicate source file"},
		{[]SourceFile{{Filename: "helper.lang", Source: []byte("package main\nfunc helper() {}")}}, "entry point is required"},
	} {
		if _, err := CompileFiles(tc.sources); err == nil || !strings.Contains(err.Error(), tc.message) {
			t.Fatalf("got %v, want %q", err, tc.message)
		}
	}
}
