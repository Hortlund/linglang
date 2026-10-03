package main

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"linglang/internal/compiler"
)

type syntaxRecord struct {
	kind, text                     string
	offset, line, column, children int
}

func TestBootstrapParserRejectsMalformedTypeArguments(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("direct shebang execution requires Unix")
	}
	sources, err := readSources("../../bootstrap/parser")
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
			artifact := filepath.Join(dir, "parser.escript")
			if err := pack(artifact, program, true, true); err != nil {
				t.Fatal(err)
			}
			for i, argument := range []string{"1", "\"bean\"", "f()", "T{}", "int+1", "&Node", "!true", "List[1]"} {
				t.Run(fmt.Sprintf("argument-%d", i), func(t *testing.T) {
					path := filepath.Join(dir, fmt.Sprintf("bad-%d.lang", i))
					data := []byte("package main\nfunc main(){return Map[string," + argument + "]{}}")
					if err := os.WriteFile(path, data, 0600); err != nil {
						t.Fatal(err)
					}
					_, err := parser.ParseFile(token.NewFileSet(), path, data, 0)
					if err == nil {
						t.Fatal("Go oracle accepted malformed type arguments")
					}
					position := err.(scanner.ErrorList)[0].Pos
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					out, err := exec.CommandContext(ctx, artifact, path).CombinedOutput()
					if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
						t.Fatalf("expected parser exit 1, got %v\n%s", err, out)
					}
					want := fmt.Sprintf("%s:%d:%d:", path, position.Line, position.Column)
					if !strings.Contains(string(out), want) {
						t.Fatalf("missing diagnostic at %s\n%s", want, out)
					}
					if !strings.Contains(string(out), "live_cells => 0") || !strings.Contains(string(out), "root_frames => 0") {
						t.Fatalf("parser left roots/cells after failure: %s", out)
					}
				})
			}
		})
	}
}

func TestBootstrapParserPackedWithoutSeed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("isolated symlink PATH requires Unix")
	}
	sources, err := readSources("../../bootstrap/parser")
	if err != nil {
		t.Fatal(err)
	}
	program, err := compiler.CompileFiles(sources)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	artifact := filepath.Join(dir, "parser.escript")
	if err := pack(artifact, program, false, true); err != nil {
		t.Fatal(err)
	}
	isolated := filepath.Join(dir, "runtime")
	if err := os.Mkdir(isolated, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"escript", "erl", "dirname"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		path, err = filepath.EvalSymlinks(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(path, filepath.Join(isolated, name)); err != nil {
			t.Fatal(err)
		}
	}
	var environment []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "PATH", "ERL_LIBS", "ERL_FLAGS", "ERL_AFLAGS", "ERL_ZFLAGS":
		default:
			environment = append(environment, entry)
		}
	}
	environment = append(environment, "PATH="+isolated)
	// A copy of its own source is just input data; the compiler and checkout are
	// absent from PATH, and the executable runs from a separate temporary folder.
	source := filepath.Join(dir, "雪 own source.lang")
	data, err := os.ReadFile("../../bootstrap/parser/expressions.lang")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	want := seedSyntax(t, data)
	previous := ""
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cmd := exec.CommandContext(ctx, artifact, source)
		cmd.Dir, cmd.Env = dir, environment
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("packed parser: %v\n%s", err, out)
		}
		assertSyntaxDump(t, out, want)
		ast := strings.SplitN(string(out), "linglang GC:", 2)[0]
		if i > 0 && ast != previous {
			t.Fatal("packed parser output is not deterministic")
		}
		previous = ast
	}
}

func TestBootstrapParserAgainstGoAST(t *testing.T) {
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, dir := range []string{"examples", "benchmarks", "bootstrap"} {
		if err := filepath.WalkDir(filepath.Join(repo, dir), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() && filepath.Ext(path) == ".lang" {
				paths = append(paths, path)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	for i, source := range []string{
		"package main\nfunc main(){println(1 + 2*3 << 1 &^ 4, !(true || false && true), -1, ^2)}",
		"package main\nfunc f(x,y int,z *Node)(int){return x+y}; type Node struct {x,y int; next *Node}; func main(){var x,y int; x,y=1,2; println(f(x,y,&Node{}))}",
		"package main\nconst(A=iota; B; C int=3); func main(){const(D=1; E=2); var(x int; y=1); for ;x<y; {x++}; for {} }",
		"package main\nfunc main(){if n:=1;n>0 {println(n)} else if false {} else {}; switch n:=1;n {case 1,2: println(n); default: }; switch {case true: }; for i:=0;i<3;i++ {continue}; for range (List[int]{1}){}; x:=0; for x = range (List[int]{1}){} }",
		"package main\nfunc main(){values:=List[Node]{{n:1},{n:2}}; m:=Map[string,List[int]]{\"a\":{1,2}}; println(values,m); append(List[int]{1},List[int]{2}...,); ; }",
		"\ufeffpackage main\r\n//line fake:100\nfunc main(){println(`a\r\nb`, '\\u96ea', \"\\xff\"); /* tail */}\n",
		"package main\nfunc f(int, *Node, List[int], string) int{return 1}; func g(x,y int,z *Node)(){}; func h(int,string,){}; type Node struct{x int}; func main(){if ;true{}; switch ;true{case true:}; var s struct{x int}; println(s)}",
		"package main\nfunc f(x (int), y *(Node)) ((int)){return x}; type Node struct{n (int)}; func main(){var p (*Node); x:=struct{n int}{n:1}; if struct{n int}{n:1}.n>0 {println(x,p)} }",
		"package main\nfunc main(){println(" + strings.Repeat("1+", 600) + "1)}",
		"package main\nfunc main(){println(values[1+2], values[1+2,], f[1+2,int](), f[int,*Node,List[int],Map[string,int],struct{n int},(int),]())}",
	} {
		path := filepath.Join(t.TempDir(), fmt.Sprintf("syntax-%d.lang", i))
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	sources, err := readSources(filepath.Join(repo, "bootstrap", "parser"))
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
			for _, path := range paths {
				t.Run(strings.TrimPrefix(path, repo+string(filepath.Separator)), func(t *testing.T) {
					data, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					want := seedSyntax(t, data)
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					cmd := exec.CommandContext(ctx, "erl", "-noshell", "-pa", dir, "-eval", evaluationScript(false, true), "-extra", path)
					out, err := cmd.CombinedOutput()
					if err != nil {
						t.Fatalf("parser: %v\n%s", err, out)
					}
					assertSyntaxDump(t, out, want)
				})
			}
		})
	}
}

func assertSyntaxDump(t *testing.T, out []byte, want []syntaxRecord) {
	t.Helper()
	if !strings.Contains(string(out), "live_cells => 0") || !strings.Contains(string(out), "root_frames => 0") {
		t.Fatalf("parser leaked roots/cells: %s", out)
	}
	lines := strings.Split(strings.TrimSuffix(strings.SplitN(string(out), "linglang GC:", 2)[0], "\n"), "\n")
	if len(lines) != len(want) {
		t.Fatalf("%d nodes, want %d\n%s", len(lines), len(want), out)
	}
	for i, line := range lines {
		parts := strings.SplitN(line, " ", 5)
		if len(parts) != 5 {
			t.Fatalf("bad record: %q", line)
		}
		last := strings.LastIndexByte(parts[4], ' ')
		if last < 0 {
			t.Fatalf("bad record: %q", line)
		}
		offset, e1 := strconv.Atoi(parts[1])
		row, e2 := strconv.Atoi(parts[2])
		column, e3 := strconv.Atoi(parts[3])
		text, e4 := strconv.Unquote(parts[4][:last])
		children, e5 := strconv.Atoi(parts[4][last+1:])
		got := syntaxRecord{parts[0], text, offset, row, column, children}
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || got != want[i] {
			t.Fatalf("node %d: got %+v, want %+v", i, got, want[i])
		}
	}
}

type seedNode struct {
	kind, text string
	pos        token.Pos
	children   []seedNode
}

func seedSyntax(t *testing.T, source []byte) []syntaxRecord {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "source.lang", source, 0)
	if err != nil {
		t.Fatalf("oracle fixture: %v", err)
	}
	var normalize func(ast.Node) seedNode
	list := func(kind string, pos token.Pos, nodes []ast.Node) seedNode {
		n := seedNode{kind: kind, pos: pos}
		for _, child := range nodes {
			n.children = append(n.children, normalize(child))
		}
		return n
	}
	exprs := func(nodes []ast.Expr) []ast.Node {
		var out []ast.Node
		for _, node := range nodes {
			out = append(out, node)
		}
		return out
	}
	stmts := func(nodes []ast.Stmt) []ast.Node {
		var out []ast.Node
		for _, node := range nodes {
			out = append(out, node)
		}
		return out
	}
	idents := func(nodes []*ast.Ident) []ast.Node {
		var out []ast.Node
		for _, node := range nodes {
			out = append(out, node)
		}
		return out
	}
	normalize = func(node ast.Node) seedNode {
		if node == nil || reflect.ValueOf(node).IsNil() {
			return seedNode{kind: "nil"}
		}
		pos := node.Pos()
		makeNode := func(kind, text string, children ...ast.Node) seedNode {
			n := list(kind, pos, children)
			n.text = text
			return n
		}
		switch n := node.(type) {
		case *ast.File:
			children := []ast.Node{n.Name}
			for _, d := range n.Decls {
				children = append(children, d)
			}
			return list("File", pos, children)
		case *ast.FuncDecl:
			return makeNode("FuncDecl", "", n.Name, n.Type.Params, n.Type.Results, n.Body)
		case *ast.FieldList:
			var children []ast.Node
			for _, f := range n.List {
				children = append(children, f)
			}
			return list("FieldList", pos, children)
		case *ast.Field:
			return seedNode{kind: "Field", pos: pos, children: []seedNode{list("Names", pos, idents(n.Names)), normalize(n.Type)}}
		case *ast.GenDecl:
			var children []ast.Node
			for _, s := range n.Specs {
				children = append(children, s)
			}
			result := list("GenDecl", pos, children)
			result.text = n.Tok.String()
			return result
		case *ast.TypeSpec:
			return makeNode("TypeSpec", "", n.Name, n.Type)
		case *ast.ValueSpec:
			return seedNode{kind: "ValueSpec", pos: pos, children: []seedNode{list("Names", pos, idents(n.Names)), normalize(n.Type), list("Values", pos, exprs(n.Values))}}
		case *ast.StructType:
			return makeNode("StructType", "", n.Fields)
		case *ast.BlockStmt:
			return list("BlockStmt", pos, stmts(n.List))
		case *ast.DeclStmt:
			return makeNode("DeclStmt", "", n.Decl)
		case *ast.ExprStmt:
			return makeNode("ExprStmt", "", n.X)
		case *ast.AssignStmt:
			return seedNode{kind: "AssignStmt", text: n.Tok.String(), pos: pos, children: []seedNode{list("Lhs", pos, exprs(n.Lhs)), list("Rhs", pos, exprs(n.Rhs))}}
		case *ast.IncDecStmt:
			return makeNode("IncDecStmt", n.Tok.String(), n.X)
		case *ast.ReturnStmt:
			return list("ReturnStmt", pos, exprs(n.Results))
		case *ast.IfStmt:
			return makeNode("IfStmt", "", n.Init, n.Cond, n.Body, n.Else)
		case *ast.ForStmt:
			return makeNode("ForStmt", "", n.Init, n.Cond, n.Post, n.Body)
		case *ast.RangeStmt:
			text := ""
			if n.Key != nil {
				text = n.Tok.String()
			}
			return makeNode("RangeStmt", text, n.Key, n.Value, n.X, n.Body)
		case *ast.SwitchStmt:
			return makeNode("SwitchStmt", "", n.Init, n.Tag, n.Body)
		case *ast.CaseClause:
			return seedNode{kind: "CaseClause", pos: pos, children: []seedNode{list("Cases", pos, exprs(n.List)), list("Body", pos, stmts(n.Body))}}
		case *ast.BranchStmt:
			return makeNode("BranchStmt", n.Tok.String())
		case *ast.EmptyStmt:
			return makeNode("EmptyStmt", "")
		case *ast.BinaryExpr:
			return makeNode("BinaryExpr", n.Op.String(), n.X, n.Y)
		case *ast.UnaryExpr:
			return makeNode("UnaryExpr", n.Op.String(), n.X)
		case *ast.StarExpr:
			return makeNode("StarExpr", "*", n.X)
		case *ast.Ident:
			return makeNode("Ident", n.Name)
		case *ast.BasicLit:
			text := n.Value
			if n.Kind == token.STRING && strings.HasPrefix(text, "`") {
				offset := fset.PositionFor(pos, false).Offset
				text = string(source[offset : offset+strings.IndexByte(string(source[offset+1:]), '`')+2])
			}
			return makeNode(n.Kind.String(), text)
		case *ast.IndexExpr:
			return makeNode("IndexExpr", "", n.X, n.Index)
		case *ast.IndexListExpr:
			return list("IndexListExpr", pos, append([]ast.Node{n.X}, exprs(n.Indices)...))
		case *ast.CallExpr:
			text := ""
			if n.Ellipsis.IsValid() {
				text = "..."
			}
			result := list("CallExpr", pos, append([]ast.Node{n.Fun}, exprs(n.Args)...))
			result.text = text
			return result
		case *ast.SelectorExpr:
			return makeNode("SelectorExpr", "", n.X, n.Sel)
		case *ast.CompositeLit:
			return list("CompositeLit", pos, append([]ast.Node{n.Type}, exprs(n.Elts)...))
		case *ast.KeyValueExpr:
			return makeNode("KeyValueExpr", "", n.Key, n.Value)
		case *ast.ParenExpr:
			return makeNode("ParenExpr", "", n.X)
		default:
			t.Fatalf("unsupported oracle node %T", node)
			return seedNode{}
		}
	}
	var records []syntaxRecord
	var walk func(seedNode)
	walk = func(n seedNode) {
		p := fset.PositionFor(n.pos, false)
		records = append(records, syntaxRecord{n.kind, n.text, p.Offset, p.Line, p.Column, len(n.children)})
		for _, child := range n.children {
			walk(child)
		}
	}
	walk(normalize(file))
	return records
}
