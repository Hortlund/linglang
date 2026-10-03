package compiler

import (
	"strings"
	"testing"
)

func TestAnalyzePackageBuffers(t *testing.T) {
	files := []SourceFile{
		{"main.lang", []byte("package main\nfunc main(){println(answer())}")},
		{"helper.lang", []byte("package main\nfunc answer()int{return 42}")},
		{"helper_test.lang", []byte("package main\nfunc TestAnswer(){assert(answer()==42)}")},
	}
	if result := Analyze(files); len(result.Diagnostics) != 0 || len(result.Files) != 3 {
		t.Fatalf("valid package: %+v", result.Diagnostics)
	}
	files[1].Source = []byte("package main\nfunc answer()string{return \"bean\"}")
	result := Analyze(files)
	if len(result.Diagnostics) == 0 || !strings.Contains(result.Diagnostics[0].Message, "mismatched types") {
		t.Fatalf("changed helper did not invalidate tests: %+v", result.Diagnostics)
	}
}

func TestAnalyzePhysicalPositionsAndPartialSyntax(t *testing.T) {
	source := "package main\n//line fake:100\nfunc main(){println(\"雪😀\", missing)}"
	result := Analyze([]SourceFile{{"real.lang", []byte(source)}})
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Filename != "real.lang" || result.Diagnostics[0].Offset != strings.Index(source, "missing") {
		t.Fatalf("physical diagnostic: %+v", result.Diagnostics)
	}
	for end := 0; end <= len(source); end++ {
		Analyze([]SourceFile{{"prefix.lang", []byte(source[:end])}})
	}
	result = Analyze([]SourceFile{{"helper.lang", []byte("package main\nfunc helper(){}")}})
	if len(result.Diagnostics) != 0 {
		t.Fatalf("editor analysis must allow missing main: %+v", result.Diagnostics)
	}
}

func TestAnalyzeUnnamedReturnTypes(t *testing.T) {
	for _, result := range []string{"float64", "[]int", "map[string]int", "List[float64]", "*float64", "struct{x float64}"} {
		source := "package main\nfunc bad() " + result + " { panic(\"unused\") }\nfunc main(){}"
		analysis := Analyze([]SourceFile{{"result.lang", []byte(source)}})
		found := false
		for _, issue := range analysis.Diagnostics {
			if issue.Filename == "result.lang" && issue.Offset == strings.Index(source, result) && strings.Contains(issue.Message, "unsupported return type") {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing result diagnostic for %s: %+v", result, analysis.Diagnostics)
		}
		if _, err := Compile("result.lang", []byte(source)); err == nil {
			t.Fatalf("compiler accepted unsupported result %s", result)
		}
	}
	for _, result := range []string{"int", "bool", "string", "*int", "List[int]", "Map[string,List[int]]", "RuneResult", "Pid", "struct{x int}"} {
		source := "package main\nfunc good() " + result + " { panic(\"unused\") }\nfunc main(){}"
		if analysis := Analyze([]SourceFile{{"result.lang", []byte(source)}}); len(analysis.Diagnostics) != 0 {
			t.Fatalf("supported result %s rejected: %+v", result, analysis.Diagnostics)
		}
		if _, err := Compile("result.lang", []byte(source)); err != nil {
			t.Fatalf("invalid supported fixture %s: %v", result, err)
		}
	}
}
