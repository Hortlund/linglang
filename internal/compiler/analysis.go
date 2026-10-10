package compiler

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"sort"
)

// Diagnostic uses physical byte offsets. Editor protocols convert these to
// their negotiated encoding, independently of //line directives.
type Diagnostic struct {
	Filename string
	Offset   int
	Message  string
}

type Analysis struct {
	FileSet     *token.FileSet
	Files       map[string]*ast.File
	Info        *types.Info
	Diagnostics []Diagnostic
	// Dependencies includes imported files and directories, even on load errors.
	Dependencies []string
}

func newCompiler(fset *token.FileSet) *compiler {
	return &compiler{fset: fset, cells: map[types.Object]string{}, info: &types.Info{
		Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{},
		Uses: map[*ast.Ident]types.Object{}, Scopes: map[ast.Node]*types.Scope{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}}
}

func typeConfig() types.Config {
	return types.Config{GoVersion: "go1.23", Sizes: types.SizesFor("gc", "amd64")}
}

// Constant runes and shifts acquiring an int or uint64 context are supported.
// The runtime only implements 64-bit arithmetic, so defaulted int32 expressions
// must be rejected before lowering, including when nested inside comparisons.
func (c *compiler) runtimeRune(expr ast.Expr) bool {
	value := c.info.Types[expr]
	if !value.IsValue() || value.Value != nil || value.Type == nil {
		return false
	}
	basic, ok := value.Type.Underlying().(*types.Basic)
	return ok && basic.Kind() == types.Int32
}

// Analyze checks in-memory package sources and keeps partial syntax trees for
// editor outlines. It needs no main function, emits no code, and starts no VM.
// Type analysis resumes after syntax errors are repaired to avoid cascades.
func Analyze(sources []SourceFile) Analysis {
	sources = append([]SourceFile(nil), sources...)
	sort.Slice(sources, func(i, j int) bool { return sources[i].Filename < sources[j].Filename })
	c := newCompiler(token.NewFileSet())
	result := Analysis{FileSet: c.fset, Files: map[string]*ast.File{}, Info: c.info}
	var files []*ast.File
	add := func(pos token.Pos, message string) {
		p := c.fset.PositionFor(pos, false)
		if p.Filename != "<linglang-prelude>" {
			if _, root := result.Files[p.Filename]; !root && len(sources) != 0 {
				result.Diagnostics = append(result.Diagnostics, Diagnostic{sources[0].Filename, 0, fmt.Sprintf("%s: %s", p, message)})
				return
			}
			result.Diagnostics = append(result.Diagnostics, Diagnostic{p.Filename, p.Offset, message})
		}
	}
	for _, source := range sources {
		file, err := parser.ParseFile(c.fset, source.Filename, source.Source, parser.AllErrors|parser.SkipObjectResolution)
		if file != nil {
			result.Files[source.Filename] = file
			files = append(files, file)
		}
		if errors, ok := err.(scanner.ErrorList); ok {
			for _, issue := range errors {
				result.Diagnostics = append(result.Diagnostics, Diagnostic{source.Filename, issue.Pos.Offset, issue.Msg})
			}
		} else if err != nil {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{source.Filename, 0, err.Error()})
		}
	}
	if len(result.Diagnostics) != 0 || len(files) == 0 {
		return result
	}
	linked, linkErr := parseModuleFilesObserved(c.fset, sources, true, func(path string) {
		result.Dependencies = append(result.Dependencies, path)
	})
	if linkErr != nil {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{sources[0].Filename, 0, linkErr.Error()})
		return result
	}
	files = linked
	prelude, err := c.prelude()
	if err != nil {
		panic(err) // The embedded, compiler-owned declaration contract is invalid.
	}
	config := typeConfig()
	config.Error = func(err error) {
		if issue, ok := err.(types.Error); ok {
			add(issue.Pos, issue.Msg)
		}
	}
	_, err = config.Check("main", c.fset, append([]*ast.File{prelude}, files...), c.info)
	if err != nil {
		return result
	}
	c.registerIntrinsics(prelude)
	// Check language restrictions that do not need lowering. More specialized
	// BEAM/message restrictions are still enforced by the compiler at build time.
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			if expr, ok := node.(ast.Expr); ok && c.runtimeRune(expr) {
				add(expr.Pos(), "runtime rune expressions are unsupported; use an int context")
				return false
			}
			switch n := node.(type) {
			case *ast.FuncDecl:
				if n.Recv != nil || n.Type.TypeParams != nil {
					add(n.Pos(), "methods and generic function declarations are not supported")
				}
				if n.Type.Results != nil {
					if len(n.Type.Results.List) > 1 {
						add(n.Type.Results.Pos(), "multiple return values are not supported")
					}
					for _, field := range n.Type.Results.List {
						if len(field.Names) != 0 {
							add(field.Pos(), "named return values are not supported")
						} else if typ := c.info.TypeOf(field.Type); !c.supportedType(typ, map[types.Type]bool{}) {
							add(field.Type.Pos(), fmt.Sprintf("unsupported return type %s", typ))
						}
					}
				}
			case *ast.TypeSpec:
				if _, ok := n.Type.(*ast.StructType); !ok || n.Assign.IsValid() || n.TypeParams != nil {
					add(n.Pos(), "only non-generic struct type declarations are supported")
				}
			case *ast.Ident:
				if v, ok := c.info.Defs[n].(*types.Var); ok && !c.supportedType(v.Type(), map[types.Type]bool{}) {
					add(n.Pos(), fmt.Sprintf("unsupported type %s", v.Type()))
				}
				if v, ok := c.info.Defs[n].(*types.Const); ok && !supportedConstantType(v.Type()) {
					add(n.Pos(), fmt.Sprintf("unsupported constant type %s", v.Type()))
				}
			case *ast.FuncLit, *ast.GoStmt, *ast.DeferStmt, *ast.SelectStmt, *ast.TypeSwitchStmt, *ast.LabeledStmt, *ast.SliceExpr, *ast.TypeAssertExpr, *ast.Ellipsis:
				add(node.Pos(), "unsupported language construct")
			}
			return true
		})
	}
	return result
}
