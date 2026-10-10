package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompilerDiagnosticLocations(t *testing.T) {
	for _, test := range []struct{ name, source, marker, code string }{
		{"syntax", "package main\n//line virtual.lang:900\nfunc main(){println(雪雪 @)}", "@", CodeSyntax},
		{"type", "package main\n//line virtual.lang:900\nfunc main(){println(\"雪\"); missing()}", "missing", CodeType},
		{"language", "package main\n//line virtual.lang:900\nfunc main(){defer println(1)}", "defer", CodeLanguage},
		{"module", "package main\n//line virtual.lang:900\nimport \"remote/package\"\nfunc main(){}", "\"remote", CodeModule},
		{"entry", "package main\n//line virtual.lang:900\nfunc main(n int){}", "main(n", CodeType},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, baseline := range []bool{false, true} {
				_, err := CompileWithOptions("physical.lang", []byte(test.source), Options{DisableOptimizations: baseline})
				if err == nil {
					t.Fatal("invalid source accepted")
				}
				issue := IssueForError(fmt.Errorf("wrapped: %w", err), CodeInternal)
				offset := strings.Index(test.source, test.marker)
				column := offset - strings.LastIndex(test.source[:offset], "\n")
				if issue.Code != test.code || issue.Location == nil || issue.Location.File != "physical.lang" || issue.Location.Offset != offset || issue.Location.Line != 3 || issue.Location.Column != column {
					t.Fatalf("baseline=%v: %+v location=%+v, expected offset=%d column=%d (%v)", baseline, issue, issue.Location, offset, column, err)
				}
				if strings.Contains(err.Error(), "virtual.lang") {
					t.Fatalf("human diagnostic redirected: %v", err)
				}
			}
		})
	}
}

func TestImportedDiagnosticLocations(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"lib", "leaf"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	lib := filepath.Join(root, "lib", "lib.lang")
	leaf := filepath.Join(root, "leaf", "leaf.lang")
	if err := os.WriteFile(lib, []byte("package lib\nimport \"../leaf\"\nfunc Value()int{return leaf.Value()}"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		source, code string
		related      int
	}{
		{"package leaf\n//line invented.lang:999\nfunc Value()int{return @}", CodeSyntax, 2},
		{"package leaf\nfunc Value()int{return false}", CodeType, 0},
	} {
		if err := os.WriteFile(leaf, []byte(test.source), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := Compile(filepath.Join(root, "main.lang"), []byte("package main\nimport \"./lib\"\nfunc main(){println(lib.Value())}"))
		if err == nil {
			t.Fatal("invalid dependency accepted")
		}
		issue := IssueForError(err, CodeInternal)
		if issue.Code != test.code || issue.Location == nil || issue.Location.File != leaf || len(issue.Related) != test.related {
			t.Fatalf("issue=%+v location=%+v (%v)", issue, issue.Location, err)
		}
		if test.related > 0 && (issue.Related[0].Location.File != lib || issue.Related[1].Location.File != filepath.Join(root, "main.lang")) {
			t.Fatalf("lost import chain: %+v", issue.Related)
		}
	}
}

func TestNativeAPICatalog(t *testing.T) {
	api, err := NativeAPI()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]NativeDeclaration{}
	for i, declaration := range api {
		if i > 0 && api[i-1].Name >= declaration.Name {
			t.Fatal("API entries must be unique and sorted")
		}
		if strings.Contains(declaration.Declaration, "return") || strings.Contains(declaration.Declaration, "chan ") || strings.Contains(declaration.Declaration, "[]") {
			t.Fatalf("leaked prelude implementation: %+v", declaration)
		}
		byName[declaration.Name] = declaration
	}
	if byName["readFile"].Declaration != "func readFile(path string) TextResult" || byName["Socket"].Kind != "opaque" || byName["Map"].Declaration != "Map[K, V]" || !strings.Contains(byName["SQLResult"].Declaration, "rows") {
		t.Fatalf("incomplete native API: %+v", byName)
	}
}

func TestDiagnosticDeclarationOrder(t *testing.T) {
	source := []byte("package main\nfunc writeFile(){}\nfunc readFile(){}\nfunc main(){}")
	for i := 0; i < 50; i++ {
		_, err := Compile("main.lang", source)
		if err == nil {
			t.Fatal("invalid declarations accepted")
		}
		issue := IssueForError(err, CodeInternal)
		if issue.Code != CodeModule || !strings.Contains(issue.Message, "writeFile") || issue.Location == nil || issue.Location.Line != 2 {
			t.Fatalf("unstable declaration ordering: %+v", issue)
		}
	}
}
