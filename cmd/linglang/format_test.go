package main

import (
	"strings"
	"testing"

	"linglang/internal/compiler"
)

func TestFormatGenericHeaderLiterals(t *testing.T) {
	for _, source := range []string{
		"package main\nfunc main(){for _, x := range (List[int]{1, 2}) { println(x) }}",
		"package main\nfunc main(){for range (List[List[int]]{List[int]{1}}) { println(1) }}",
		"package main\nfunc main(){for range (List[/* keep */ int]{1}) { println(1) }}",
		"package main\nfunc main(){for range (List[int]{\n// bean\n1,\n2,\n}) {println(1)}}",
		"package main\nfunc main(){for range (List[\n// bean\nint,\n]{1}) {println(1)}}",
		"package main\nfunc main(){for range (List[\n// bean\nint,\n] /* keep */ {1}) {println(1)}}",
		"package main\nfunc main(){for range ((List[\n// bean\nint,\n] /* keep */ {1})) {println(1)}}",
	} {
		formatted, err := formatSource("test.lang", []byte(source))
		if err != nil {
			t.Fatalf("format: %v", err)
		}
		if !strings.Contains(string(formatted), "range (List[") {
			t.Fatalf("removed required parentheses: %s", formatted)
		}
		if strings.Contains(source, "/* keep */") && strings.Count(string(formatted), "/* keep */") != 1 {
			t.Fatalf("lost comment: %s", formatted)
		}
		if strings.Contains(source, "// bean") && strings.Count(string(formatted), "// bean") != 1 {
			t.Fatalf("lost comment: %s", formatted)
		}
		again, err := formatSource("test.lang", formatted)
		if err != nil || string(again) != string(formatted) {
			t.Fatalf("not idempotent: %v\n%s\n%s", err, formatted, again)
		}
		checkFormattedProgram(t, source, formatted)
	}
}

func TestFormatGenericLiteralComments(t *testing.T) {
	for _, source := range []string{
		"package main\nfunc main(){x := List[\n// element\nint,\n] /* between */ {1}; println(len(x))}",
		"package main\nfunc main(){x := Map[\n// key\nstring,\n// value\nList[\n// element\nint,\n],\n] /* between */ {\"bean\": List[int]{1}}; println(len(x))}",
		"package main\nfunc main(){if (List[\n// element\nint,\n] /* between */ {1} != nil) {println(1)}}",
		"package main\nfunc main(){for (List[\n// element\nint,\n] /* between */ {1} != nil) {break}}",
		"package main\nfunc main(){switch (List[\n// element\nint,\n] /* between */ {1} == nil) {case false: println(1)}}",
		"package main\n// __linglang_fmt_parens__ must survive\nfunc main(){for range (List[int]{1}) {println(1)}}",
	} {
		formatted, err := formatSource("test.lang", []byte(source))
		if err != nil {
			t.Fatalf("format: %v\n%s", err, source)
		}
		for _, comment := range []string{"// element", "// key", "// value", "/* between */", "// __linglang_fmt_parens__ must survive"} {
			if strings.Count(string(formatted), comment) != strings.Count(source, comment) {
				t.Fatalf("changed comment %q: %s", comment, formatted)
			}
		}
		again, err := formatSource("test.lang", formatted)
		if err != nil || string(again) != string(formatted) {
			t.Fatalf("not idempotent: %v\n%s\n%s", err, formatted, again)
		}
		checkFormattedProgram(t, source, formatted)
	}
}

func checkFormattedProgram(t *testing.T, source string, formatted []byte) {
	t.Helper()
	for _, options := range []compiler.Options{{}, {DisableOptimizations: true}} {
		before, err := compiler.CompileWithOptions("test.lang", []byte(source), options)
		if err != nil {
			t.Fatalf("invalid fixture: %v\n%s", err, source)
		}
		after, err := compiler.CompileWithOptions("test.lang", formatted, options)
		if err != nil {
			t.Fatalf("formatted source does not compile: %v\n%s", err, formatted)
		}
		if before != after {
			t.Fatalf("formatting changed compiled program (options=%+v)\n%s", options, formatted)
		}
	}
}
