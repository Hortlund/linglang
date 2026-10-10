package main

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"linglang/internal/compiler"
)

// Compare actual lexical bindings, not merely whether a name is accepted.
// Field selectors remain deferred. Map keys have lexical bindings, including
// elided literals; the resolver also retains their deferred key metadata.
func seedBindings(t *testing.T, sources []compiler.SourceFile) []string {
	t.Helper()
	analysis := compiler.Analyze(sources)
	if len(analysis.Diagnostics) != 0 {
		t.Fatalf("oracle package: %+v", analysis.Diagnostics)
	}
	// Analyze retains original trees for outlines but checks renamed module
	// trees. Use the originals for this independent lexical-binding oracle.
	prelude, err := parser.ParseFile(analysis.FileSet, "<prelude>", compiler.PreludeSource(), 0)
	if err != nil {
		t.Fatal(err)
	}
	files := []*ast.File{prelude}
	for _, source := range sources {
		files = append(files, analysis.Files[source.Filename])
	}
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	config := types.Config{GoVersion: "go1.23", Sizes: types.SizesFor("gc", "amd64")}
	if _, err := config.Check("main", analysis.FileSet, files, info); err != nil {
		t.Fatal(err)
	}
	skipped := map[*ast.Ident]bool{}
	parameters := map[*ast.Ident]bool{}
	var records []string
	for _, file := range analysis.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			var deferred *ast.Ident
			skip := true
			switch n := node.(type) {
			case *ast.SelectorExpr:
				deferred = n.Sel
			case *ast.KeyValueExpr:
				deferred, _ = n.Key.(*ast.Ident)
				if obj := info.Uses[deferred]; obj != nil {
					field, ok := obj.(*types.Var)
					skip = ok && field.IsField()
				}
			case *ast.FuncDecl:
				for _, field := range n.Type.Params.List {
					for _, id := range field.Names {
						parameters[id] = true
					}
				}
			}
			if deferred != nil {
				skipped[deferred] = skip
				pos := analysis.FileSet.PositionFor(deferred.Pos(), false)
				records = append(records, fmt.Sprintf("defer %q %d", pos.Filename, pos.Offset))
			}
			return true
		})
	}
	for id, obj := range info.Defs {
		if obj == nil || id.Name == "_" {
			continue
		}
		pos := analysis.FileSet.PositionFor(id.Pos(), false)
		if _, user := analysis.Files[pos.Filename]; !user {
			continue
		}
		kind := ""
		switch object := obj.(type) {
		case *types.Var:
			if object.IsField() {
				continue
			}
			kind = "var"
			if parameters[id] {
				kind = "param"
			}
		case *types.Func:
			kind = "func"
		case *types.TypeName:
			kind = "type"
		case *types.Const:
			kind = "const"
		}
		if kind != "" {
			records = append(records, fmt.Sprintf("def %q %s %q %d", id.Name, kind, pos.Filename, pos.Offset))
		}
	}
	for id, obj := range info.Uses {
		pos := analysis.FileSet.PositionFor(id.Pos(), false)
		if _, user := analysis.Files[pos.Filename]; !user || skipped[id] {
			continue
		}
		definition := analysis.FileSet.PositionFor(obj.Pos(), false)
		if _, user := analysis.Files[definition.Filename]; !user {
			definition.Filename, definition.Offset = "", 0
		}
		records = append(records, fmt.Sprintf("use %q %d %q %q %d", pos.Filename, pos.Offset, obj.Name(), definition.Filename, definition.Offset))
	}
	sort.Strings(records)
	return records
}

func assertBindingDump(t *testing.T, out []byte, want []string) {
	t.Helper()
	if !strings.Contains(string(out), "live_cells => 0") || !strings.Contains(string(out), "root_frames => 0") {
		t.Fatalf("resolver left roots/cells: %s", out)
	}
	// Only complete lines beginning with a record tag belong to this protocol.
	// A source filename containing the text "linglang GC:" remains ordinary data.
	var got []string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "def ") || strings.HasPrefix(line, "use ") || strings.HasPrefix(line, "defer ") {
			got = append(got, line)
		}
	}
	sort.Strings(got)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("binding mismatch\ngot:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestBootstrapResolverAgainstSeedBindings(t *testing.T) {
	var packages [][]compiler.SourceFile
	for _, path := range []string{"../../examples/worker_registration.lang", "../../examples/prime_lab", "../../bootstrap/lexer", "../../bootstrap/parser", "../../bootstrap/resolver"} {
		absolute, err := filepath.Abs(path)
		if err != nil {
			t.Fatal(err)
		}
		sources, err := readPackageSources(absolute, true)
		if err != nil {
			t.Fatal(err)
		}
		packages = append(packages, sources)
	}
	for i, code := range []string{
		"package main\nfunc f(x int){x,y:=x,2;println(x,y);{x:=x;println(x)};if x:=x;x>0{println(x)}else{println(x)};for i:=0;i<2;i++{println(i)};for i,x:=range (List[int]{1}){println(i,x)};switch{case true:y:=1;println(y);default:y:=2;println(y)}}",
		"package main\nfunc f(int int,x int){println(int,x)}; const(A=iota;B;C=A); type N struct{next *N;value int};func main(){n:=N{value:A};println(n.value,Map[int,int]{A:B})}",
		"package main\nfunc println(int){};func main(){println(1);x:=0;for x=range (List[int]{1}){println(x)}}",
		"package main\nfunc f(n int){println(" + strings.Repeat("n+", 600) + "n)}",
	} {
		path := filepath.Join(t.TempDir(), fmt.Sprintf("scope-%d.lang", i))
		if err := os.WriteFile(path, []byte(code), 0600); err != nil {
			t.Fatal(err)
		}
		packages = append(packages, []compiler.SourceFile{{Filename: path, Source: []byte(code)}})
	}
	// Forward references really cross files, not just declaration order.
	dir := t.TempDir()
	var forward []compiler.SourceFile
	for name, code := range map[string]string{"main.lang": "package main\nfunc main(){println(answer(),N{})}", "helper.lang": "package main\ntype N struct{next *N};const Value=42;func answer()int{return Value}"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(code), 0600); err != nil {
			t.Fatal(err)
		}
		forward = append(forward, compiler.SourceFile{Filename: path, Source: []byte(code)})
	}
	packages = append(packages, forward)
	sources, err := readSources("../../bootstrap/resolver")
	if err != nil {
		t.Fatal(err)
	}
	for _, baseline := range []bool{false, true} {
		name := "optimized"
		if baseline {
			name = "baseline"
		}
		t.Run(name, func(t *testing.T) {
			program, err := compiler.CompileFilesWithOptions(sources, compiler.Options{DisableOptimizations: baseline})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			if err := build(dir, program); err != nil {
				t.Fatal(err)
			}
			for i, files := range packages {
				t.Run(fmt.Sprintf("package-%d", i), func(t *testing.T) {
					want := seedBindings(t, files)
					args := []string{"-noshell", "-pa", dir, "-eval", evaluationScript(false, true), "-extra"}
					for _, file := range files {
						args = append(args, file.Filename)
					}
					budget := 30 * time.Second
					if baseline && len(files) > 1 {
						// Whole frontend packages carry much larger ASTs and symbol
						// tables than the single-file scope fixtures. The cell backend
						// needs more time on hosted runners, especially older OTP.
						budget = 2 * time.Minute
					}
					ctx, cancel := context.WithTimeout(context.Background(), budget)
					defer cancel()
					out, err := exec.CommandContext(ctx, "erl", args...).CombinedOutput()
					if ctx.Err() != nil {
						t.Fatalf("resolver timed out after %s\n%s", budget, out)
					}
					if err != nil {
						t.Fatalf("resolver: %v\n%s", err, out)
					}
					assertBindingDump(t, out, want)
				})
			}
		})
	}
}

func TestBootstrapResolverPackedWithoutSeed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("isolated symlink PATH requires Unix")
	}
	sources, err := readSources("../../bootstrap/resolver")
	if err != nil {
		t.Fatal(err)
	}
	program, err := compiler.CompileFiles(sources)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	artifact := filepath.Join(dir, "resolver.escript")
	if err := pack(artifact, program, false, true); err != nil {
		t.Fatal(err)
	}
	isolated := filepath.Join(dir, "runtime")
	environment := isolatedOTPEnvironment(t, isolated, false)
	args := []string{}
	for i, source := range sources {
		path := filepath.Join(dir, "雪 "+filepath.Base(source.Filename))
		if err := os.WriteFile(path, source.Source, 0600); err != nil {
			t.Fatal(err)
		}
		sources[i].Filename = path
		args = append(args, path)
	}
	want := seedBindings(t, sources)
	var previous []byte
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cmd := exec.CommandContext(ctx, artifact, args...)
		cmd.Dir, cmd.Env = dir, environment
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("isolated resolver: %v\n%s", err, out)
		}
		assertBindingDump(t, out, want)
		// Sorted reference equality above is independent of dump order. Repeated
		// stdout additionally checks that traversal order is deterministic.
		data := strings.SplitN(string(out), "\nlinglang GC:", 2)[0]
		if i > 0 && data != string(previous) {
			t.Fatal("packed resolver output is not deterministic")
		}
		previous = []byte(data)
	}
}
