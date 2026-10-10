package main

import (
	"context"
	"linglang/internal/compiler"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testBootstrapModules(t *testing.T, executable string, env []string) {
	t.Helper()
	t.Run("binding_regressions", func(t *testing.T) { testBootstrapModuleRegressions(t, executable, env) })
	dir := t.TempDir()
	write := func(name, source string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("math/math.lang", `package numbers
const Key="answer"
const value=40
type Answer struct { value int }
func Get() Answer{return Answer{value:value+2}}
func Pair()(Answer,bool){return Get(),true}
func Use(a Answer,ok bool){assert(ok&&a.value==42)}
func Values()Map[string,int]{return Map[string,int]{Key:Get().value}}`)
	write("other/other.lang", `package other
import "../math"
func Get()numbers.Answer{return numbers.Get()}
func Pair()(numbers.Answer,bool){return numbers.Pair()}`)
	source := `package main
import n "./math"
import "./other"
type Holder struct { n int }
func main(){v,ok:=other.Pair();assert(ok&&v.value==42);n.Use(other.Pair());h:=Holder{n:1};assert(h.n==1);a:=n.Get();b:=other.Get();assert(a==b);assert(get(n.Values(),n.Key).value==42);println(a.value)}`
	write("main.lang", source)
	write("main_test.lang", `package main
import "./math"
func TestAnswer(){assert(numbers.Get().value==42)}`)
	invoke := func(args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, executable, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	for _, mode := range []string{"", "--no-opt"} {
		args := []string{"run"}
		if mode != "" {
			args = append(args, mode)
		}
		out, err := invoke(append(args, dir)...)
		if err != nil || !strings.Contains(out, "42\n") {
			t.Fatalf("module run: %v\n%s", err, out)
		}
	}
	out, err := invoke("test", dir)
	if err != nil || !strings.Contains(out, "PASS TestAnswer") {
		t.Fatalf("module tests: %v\n%s", err, out)
	}
	write("math/math_test.lang", `package numbers
func TestLibrary(){assert(Get().value==42)}`)
	out, err = invoke("test", filepath.Join(dir, "math"))
	if err != nil || !strings.Contains(out, "PASS TestLibrary") {
		t.Fatalf("library suite: %v %s", err, out)
	}
	library, e := readPackageSources(filepath.Join(dir, "math"), true)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = compiler.CompileTestFilesWithOptions(library, compiler.Options{}); e != nil {
		t.Fatal(e)
	}
	protected := filepath.Join(dir, "math", "math.lang")
	before, _ := os.ReadFile(protected)
	out, err = invoke("build", "-o", protected, dir)
	if err == nil || !strings.Contains(out, "refusing to overwrite source") {
		t.Fatalf("import overwrite: %v %s", err, out)
	}
	if err = run([]string{"pack", "-o", protected, dir}); err == nil || !strings.Contains(err.Error(), "refusing to overwrite source") {
		t.Fatalf("seed import overwrite: %v", err)
	}
	after, _ := os.ReadFile(protected)
	if string(before) != string(after) {
		t.Fatal("dependency was overwritten")
	}
	rootSources, e := readSources(dir)
	if e != nil {
		t.Fatal(e)
	}
	if diagnostics := compiler.Analyze(rootSources).Diagnostics; len(diagnostics) != 0 {
		t.Fatalf("module editor diagnostics: %+v", diagnostics)
	}
	for _, body := range []string{`println(n.value)`, `n:=1;println(n)`, `println(value)`, `println(__llpkg1_value)`} {
		write("main.lang", `package main
import n "./math"
func main(){`+body+`}`)
		out, err = invoke("check", dir)
		if err == nil {
			t.Fatalf("accepted module boundary %s: %s", body, out)
		}
		sources, e := readSources(dir)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = compiler.CompileFiles(sources); e == nil {
			t.Fatalf("seed accepted %s", body)
		}
	}
	write("main.lang", source)
	write("math/cycle.lang", `package numbers
import "../other"
func Bad(){other.Get()}`)
	out, err = invoke("check", dir)
	if err == nil || !strings.Contains(out, "cycle") {
		t.Fatalf("cycle: %v %s", err, out)
	}
	// Check lexical binding preservation independently of traversal order and
	// root builtin shadows. Blank iota slots must remain anonymous after linking.
	write("bindings/lib/a.lang", `package lib
const (_=iota; First; _; Third)
func Truth()bool{return true}
func Size()int{return len("beans")}
func Local()int{const First=9;return First}`)
	write("bindings/lib/b.lang", `package lib
func Value()int{return First+Third}`)
	write("bindings/main.lang", `package main
import "./lib"
const true=false
const iota=77
func len(s string)int{return 99}
func main(){assert(lib.Truth());assert(!true);assert(lib.Size()==5);assert(len("")==99);assert(lib.Value()==4);assert(lib.Local()==9);println("bindings ok")}`)
	for _, mode := range []string{"", "--no-opt"} {
		args := []string{"run", "--gc-stress"}
		if mode != "" {
			args = append(args, mode)
		}
		out, err = invoke(append(args, filepath.Join(dir, "bindings"))...)
		if err != nil || !strings.Contains(out, "bindings ok") {
			t.Fatalf("bindings: %v %s", err, out)
		}
	}
	write("bindings/main.lang", `package main
import "./lib"
const secret=42
type Hidden struct{value int}
func hidden()int{return secret}
func main(){println(lib.Value())}`)
	for _, declaration := range []string{`func Value()int{return hidden()}`, `func Value()int{return secret}`, `func Value()Hidden{return Hidden{value:1}}`} {
		write("bindings/lib/b.lang", "package lib\n"+declaration)
		out, err = invoke("check", filepath.Join(dir, "bindings"))
		if err == nil || !strings.Contains(out, "undefined name") {
			t.Fatalf("leaked binding: %v %s", err, out)
		}
	}
}

func TestLocalModules(t *testing.T) {
	sources, err := readSources("../../examples/modules")
	if err != nil {
		t.Fatal(err)
	}
	for _, noOpt := range []bool{false, true} {
		if _, err = compiler.CompileFilesWithOptions(sources, compiler.Options{DisableOptimizations: noOpt}); err != nil {
			t.Fatal(err)
		}
	}
}

func testBootstrapModuleRegressions(t *testing.T, executable string, env []string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "lib"), 0700); err != nil {
		t.Fatal(err)
	}
	write := func(path, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, path), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	invoke := func(args ...string) (string, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, executable, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	library := `package lib
const key="local"
type Item struct{key int;len int}
func Value()int{
 m:=Map[string,Map[string,int]]{"outer":{key:42}}
 list:=List[Map[string,int]]{{key:1}}
 structs:=List[Item]{{key:2,len:3}}
 local:="inner"
 nested:=Map[string,List[Map[string,int]]]{"outer":{{local:4}}}
 return get(get(m,"outer").value,"local").value+get(head(list).value,"local").value+head(structs).value.key+head(structs).value.len+get(head(get(nested,"outer").value).value,"inner").value
}`
	write("lib/lib.lang", library)
	for _, shadow := range []string{"", `const key="wrong"`} {
		write("main.lang", "package main\nimport len \"./lib\"\n"+shadow+"\n"+`func main(){x:=len.Item{key:2,len:3};assert(x.len==3);assert(len.Value()==52);println("bindings ok")}`)
		for _, noOpt := range []bool{false, true} {
			args := []string{"run", "--gc-stress"}
			if noOpt {
				args = append(args, "--no-opt")
			}
			out, err := invoke(append(args, dir)...)
			if err != nil || !strings.Contains(out, "bindings ok") {
				t.Fatalf("elided map binding: %v %s", err, out)
			}
		}
	}
	write("main.lang", "package main\nimport \"./lib\"\nconst key=\"root\"\nfunc main(){println(lib.Value())}")
	write("lib/lib.lang", strings.Replace(library, `const key="local"`, "", 1))
	if out, err := invoke("check", dir); err == nil || !strings.Contains(out, "undefined name key") {
		t.Fatalf("captured private root key: %v %s", err, out)
	}
	for _, tc := range []struct{ name, library, imports, body, want string }{
		{"native_function", `func readFile()int{return 42};func Value()int{return readFile()}`, `import "./lib"`, `println(lib.Value())`, "duplicate declaration"},
		{"native_type", `type Process struct{x int};func Value()int{return 42}`, `import "./lib"`, `println(lib.Value())`, "duplicate declaration"},
		{"native_constant", `const head=42;func Value()int{return head}`, `import "./lib"`, `println(lib.Value())`, "duplicate declaration"},
		{"file_import", `func Value()int{return 42}`, `import "./lib/lib.lang"`, `println(lib.Value())`, "package directory"},
		{"bare_function_alias", `func Value()int{return 42}`, `import len "./lib"`, `println(len("abc"))`, "requires a member selector"},
		{"bare_type_alias", `func Value()int{return 42}`, `import int "./lib"`, `var n int;println(n)`, "requires a member selector"},
		{"bare_constant_alias", `func Value()int{return 42}`, `import true "./lib"`, `println(true)`, "requires a member selector"},
		{"elided_key_alias", `func Value()int{return 42}`, `import iota "./lib"`, `_=Map[string,Map[int,int]]{"x":{iota:42}}`, "requires a member selector"},
	} {
		write("lib/lib.lang", "package lib\n"+tc.library)
		write("main.lang", "package main\n"+tc.imports+"\nfunc main(){"+tc.body+"}")
		if out, err := invoke("check", dir); err == nil || !strings.Contains(out, tc.want) {
			t.Fatalf("%s: want %q, got %v %s", tc.name, tc.want, err, out)
		}
	}
}
