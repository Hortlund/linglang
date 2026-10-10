package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModuleBindings(t *testing.T) {
	dir := t.TempDir()
	lib := filepath.Join(dir, "lib")
	if err := os.Mkdir(lib, 0700); err != nil {
		t.Fatal(err)
	}
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(lib, name), []byte("package lib\n"+text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("a.lang", `const (_=iota; First; _; Third)
func Truth()bool{return true}
func Size()int{return len("beans")}
func Local()int {const First=9;return First}`)
	write("b.lang", `func Value()int{return First+Third}`)
	source := SourceFile{filepath.Join(dir, "main.lang"), []byte(`package main
import "./lib"
const true=false
const iota=77
func len(s string)int{return 99}
func main(){assert(lib.Truth());assert(!true);assert(lib.Size()==5);assert(len("")==99);assert(lib.Value()==4);assert(lib.Local()==9)}`)}
	for _, baseline := range []bool{false, true} {
		program, err := CompileFilesWithOptions([]SourceFile{source}, Options{DisableOptimizations: baseline})
		if err != nil {
			t.Fatal(err)
		}
		if out, err := executeCompiledScript(t, program, `linglang_rt:set_gc_stress(true),linglang_program:main(),halt(0).`); err != nil || out != "" {
			t.Fatalf("%v %s", err, out)
		}
	}
	for _, declaration := range []string{
		`func Value()int{return hidden()}`,
		`func Value()int{return secret}`,
		`func Value()Hidden{return Hidden{value:1}}`,
	} {
		write("b.lang", declaration)
		source.Source = []byte(`package main
import "./lib"
const secret=42
type Hidden struct{value int}
func hidden()int{return secret}
func main(){println(lib.Value())}`)
		if _, err := CompileFiles([]SourceFile{source}); err == nil || !strings.Contains(err.Error(), "undefined name") {
			t.Fatalf("accepted leaked binding %q: %v", declaration, err)
		}
	}
}

func TestModuleElidedMapKeyBindings(t *testing.T) {
	dir := t.TempDir()
	lib := filepath.Join(dir, "lib")
	if err := os.Mkdir(lib, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(lib, "lib.lang")
	body := `package lib
type Item struct{key int}
func Value()int{
 m:=Map[string,Map[string,int]]{"outer":{key:42}}
 list:=List[Map[string,int]]{{key:1}}
 structs:=List[Item]{{key:2}}
 return get(get(m,"outer").value,"local").value+get(head(list).value,"local").value+head(structs).value.key
}`
	write := func(text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(body + "\nconst key=\"local\"")
	source := SourceFile{filepath.Join(dir, "main.lang"), []byte("package main\nimport \"./lib\"\nconst key=\"wrong\"\nfunc main(){assert(lib.Value()==45)}")}
	for _, baseline := range []bool{false, true} {
		program, err := CompileFilesWithOptions([]SourceFile{source}, Options{DisableOptimizations: baseline})
		if err != nil {
			t.Fatal(err)
		}
		if out, err := executeCompiledScript(t, program, `linglang_rt:set_gc_stress(true),linglang_program:main(),halt(0).`); err != nil || out != "" {
			t.Fatalf("%v %s", err, out)
		}
	}
	write(body)
	if _, err := CompileFiles([]SourceFile{source}); err == nil || !strings.Contains(err.Error(), "undefined name key") {
		t.Fatalf("elided key captured root: %v", err)
	}
}

func TestModuleBoundaryValidation(t *testing.T) {
	for _, tc := range []struct{ name, library, imports, body, want string }{
		{"native_function", `func readFile()int{return 42};func Value()int{return readFile()}`, `import "./lib"`, `println(lib.Value())`, "duplicate declaration"},
		{"native_type", `type Process struct{x int};func Value()int{return 42}`, `import "./lib"`, `println(lib.Value())`, "duplicate declaration"},
		{"native_constant", `const head=42;func Value()int{return head}`, `import "./lib"`, `println(lib.Value())`, "duplicate declaration"},
		{"file_import", `func Value()int{return 42}`, `import "./lib/lib.lang"`, `println(lib.Value())`, "not a directory"},
		{"bare_function_alias", `func Value()int{return 42}`, `import len "./lib"`, `println(len("abc"))`, "requires a member selector"},
		{"bare_type_alias", `func Value()int{return 42}`, `import int "./lib"`, `var n int;println(n)`, "requires a member selector"},
		{"bare_constant_alias", `func Value()int{return 42}`, `import true "./lib"`, `println(true)`, "requires a member selector"},
		{"elided_key_alias", `func Value()int{return 42}`, `import iota "./lib"`, `_=Map[string,Map[int,int]]{"x":{iota:42}}`, "requires a member selector"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, "lib"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "lib/lib.lang"), []byte("package lib\n"+tc.library), 0600); err != nil {
				t.Fatal(err)
			}
			sources := []SourceFile{{filepath.Join(dir, "main.lang"), []byte("package main\n" + tc.imports + "\nfunc main(){" + tc.body + "}")}}
			if _, err := CompileFiles(sources); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}
