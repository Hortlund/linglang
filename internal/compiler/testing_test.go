package compiler

import (
	"strings"
	"testing"
)

func TestCompileLanguageTests(t *testing.T) {
	sources := []SourceFile{
		{Filename: "beans.lang", Source: []byte("package main\nfunc increment(n int) int { return n + 1 }\nfunc TestHelper() {}")},
		{Filename: "beans_test.lang", Source: []byte("package main\nfunc Test雪() { assert(increment(2) == 3) }\nfunc TestAlias() { n := 1; p := &n; *p = 2; assert(n == 2) }\n")},
	}
	for _, options := range []Options{{}, {DisableOptimizations: true}} {
		program, tests, err := CompileTestFilesWithOptions(sources, options)
		if err != nil || len(tests) != 2 || tests[0].Name != "TestAlias" || tests[1].Name != "Test雪" || tests[1].Line != 2 {
			t.Fatalf("discovery: %+v (%v)", tests, err)
		}
		for _, test := range tests {
			script := "linglang_rt:set_gc_stress(true), linglang_program:run_test(" + binaryString(test.Name) + "), #{live_cells := 0, root_frames := 0} = linglang_rt:stats(), halt(0)."
			if out, err := executeCompiledScript(t, program, script); err != nil || out != "" {
				t.Fatalf("run %s: %v\n%s", test.Name, err, out)
			}
		}
	}
	for _, source := range []string{
		"func TestBad(n int) {}", "func TestBad() int { return 1 }", "func TestBad[T any]() {}",
	} {
		_, _, err := CompileTestFilesWithOptions([]SourceFile{{Filename: "bad_test.lang", Source: []byte("package main\n" + source)}}, Options{})
		if err == nil || !strings.Contains(err.Error(), "bad_test.lang:2:") || !strings.Contains(err.Error(), "test functions must have no") {
			t.Fatalf("invalid test signature accepted: %v", err)
		}
	}
}

func TestAssertEvaluationAndLocation(t *testing.T) {
	source := `func condition(n *int) bool { *n++; return true }
func main() { n := 0; assert(condition(&n)); assert(n == 1); assert(false) }`
	for _, options := range []Options{{}, {DisableOptimizations: true}} {
		program, err := CompileWithOptions("雪 beans.lang", []byte("package main\n"+source), options)
		if err != nil {
			t.Fatal(err)
		}
		script := `try linglang_program:main(), error(assertion_did_not_fail) catch error:{linglang_assertion, File, 3} -> ` + binaryString("雪 beans.lang") + ` = File end,
 #{live_cells := 0, root_frames := 0} = linglang_rt:stats(), halt(0).`
		if out, err := executeCompiledScript(t, program, script); err != nil || out != "" {
			t.Fatalf("assertion semantics: %v\n%s", err, out)
		}
	}
	if _, err := Compile("bad.lang", []byte("package main\nfunc main() { assert(42) }")); err == nil {
		t.Fatal("non-boolean assertion accepted")
	}
}

// Fuzzing runs through the real type checker and both lowerings, without needing
// to launch OTP for malformed input. Accepted syntax must agree across backends.
func FuzzCompile(f *testing.F) {
	for _, source := range []string{
		"", "package main\nfunc main() {}", "package main\nfunc main() { assert(true) }",
		"package main\nfunc main() { n := 1; p := &n; *p++; println(n) }",
		"package main\ntype Node struct { next *Node }; func main() { n := &Node{}; n.next = n }",
		"package main\nfunc main() { values := List[int]{1, 2}; for _, n := range values { assert(n > 0) } }",
		"package main\nfunc main() { spawn(assert, true) }",
		"package main\nfunc main() { s := join(List[string]{\"snow\", \"雪\"}, \" \" ); assert(byteAt(s, 0) == 115); println(runeAt(s, len(s)).width) }",
	} {
		f.Add([]byte(source))
	}
	f.Fuzz(func(t *testing.T, source []byte) {
		if len(source) > 16384 {
			t.Skip("limit compiler fuzz inputs to 16 KiB")
		}
		_, optimized := CompileWithOptions("fuzz.lang", source, Options{})
		_, baseline := CompileWithOptions("fuzz.lang", source, Options{DisableOptimizations: true})
		if (optimized == nil) != (baseline == nil) {
			t.Fatalf("backend acceptance differs: optimized=%v, baseline=%v", optimized, baseline)
		}
	})
}
