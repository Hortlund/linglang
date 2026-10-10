package compiler

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Imports link a closed local graph into the existing single-module backend.
// Names are qualified before type checking; fields remain public value members.
type sourceModule struct {
	files  []*ast.File
	names  map[string]string
	prefix string
	state  int
	name   string
}
type moduleLoader struct {
	library bool
	depth   int
	fset    *token.FileSet
	modules map[string]*sourceModule
	next    int
	files   []*ast.File
	native  *ast.Scope
	observe func(string)
}

func parseModuleFiles(fset *token.FileSet, sources []SourceFile, library bool) ([]*ast.File, error) {
	return parseModuleFilesObserved(fset, sources, library, nil)
}

func parseModuleFilesObserved(fset *token.FileSet, sources []SourceFile, library bool, observe func(string)) ([]*ast.File, error) {
	prelude, err := parser.ParseFile(token.NewFileSet(), "<linglang-prelude>", PreludeSource(), 0)
	if err != nil {
		return nil, err
	}
	l := &moduleLoader{library: library, fset: fset, modules: map[string]*sourceModule{}, native: prelude.Scope, observe: observe}
	root, _ := filepath.Abs(filepath.Dir(sources[0].Filename))
	_, err = l.load(root, sources, true)
	if err != nil {
		issue := IssueForError(err, CodeModule)
		err = &diagnosticError{issue: issue, cause: err}
	}
	return l.files, err
}
func (l *moduleLoader) fail(n ast.Node, format string, args ...any) error {
	return errorAt(CodeModule, l.fset.PositionFor(n.Pos(), false), fmt.Sprintf(format, args...))
}
func (l *moduleLoader) load(dir string, sources []SourceFile, root bool) (*sourceModule, error) {
	if !root && l.observe != nil {
		// Record even missing/broken packages so editor checks recover on changes.
		l.observe(dir)
	}
	if m := l.modules[dir]; m != nil {
		if m.state == 1 {
			return nil, fmt.Errorf("import cycle at %s", dir)
		}
		return m, nil
	}
	l.depth++
	defer func() { l.depth-- }()
	if l.depth > 65 {
		return nil, fmt.Errorf("import nesting exceeds 64 packages")
	}
	m := &sourceModule{names: map[string]string{}, state: 1}
	if !root {
		l.next++
		m.prefix = fmt.Sprintf("__llpkg%d_", l.next)
	}
	l.modules[dir] = m
	if !root {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".lang") && !strings.HasSuffix(entry.Name(), "_test.lang") {
				path := filepath.Join(dir, entry.Name())
				if l.observe != nil {
					l.observe(path)
				}
				info, err := os.Stat(path)
				if err != nil {
					return nil, err
				}
				if !info.Mode().IsRegular() {
					return nil, fmt.Errorf("%s: source must be a regular file", path)
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return nil, err
				}
				sources = append(sources, SourceFile{path, data})
			}
		}
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("%s: no .lang source files", dir)
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Filename < sources[j].Filename })
	top := map[*ast.Object]bool{}
	for i, source := range sources {
		if i > 0 && sources[i-1].Filename == source.Filename {
			return nil, fmt.Errorf("duplicate source file %q", source.Filename)
		}
		file, err := parser.ParseFile(l.fset, source.Filename, source.Source, 0)
		if err != nil {
			return nil, syntaxError(l.fset, source.Filename, err)
		}
		if root && !l.library && file.Name.Name != "main" {
			return nil, l.fail(file.Name, "only package main is supported at the program root")
		}
		if !root && file.Name.Name == "main" {
			return nil, l.fail(file.Name, "imported packages cannot be main")
		}
		if m.name != "" && m.name != file.Name.Name {
			return nil, l.fail(file.Name, "mixed package names")
		}
		m.name = file.Name.Name
		var reserved ast.Node
		ast.Inspect(file, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && strings.HasPrefix(id.Name, "__llpkg") {
				reserved = id
			}
			return true
		})
		if reserved != nil {
			return nil, l.fail(reserved, "names beginning __llpkg are reserved")
		}
		// Scope.Objects is a map. Visit declarations by source position so the
		// first diagnostic is reproducible when several declarations are invalid.
		var names []string
		for name := range file.Scope.Objects {
			names = append(names, name)
		}
		sort.Slice(names, func(i, j int) bool {
			return file.Scope.Objects[names[i]].Pos() < file.Scope.Objects[names[j]].Pos()
		})
		for _, name := range names {
			obj := file.Scope.Objects[name]
			if l.native.Lookup(name) != nil {
				return nil, l.fail(obj.Decl.(ast.Node), "duplicate declaration of prelude name %s", name)
			}
			if name == "init" {
				return nil, l.fail(obj.Decl.(ast.Node), "init and blank function declarations are unsupported")
			}
			m.names[name] = m.prefix + name
			if root && types.Universe.Lookup(name) != nil {
				// A root declaration must not capture another package's builtins.
				m.names[name] = "__llpkg0_" + name
			}
			top[obj] = true
		}
		m.files = append(m.files, file)
	}
	for _, file := range m.files {
		aliases := map[string]*sourceModule{}
		for _, imp := range file.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return nil, err
			}
			if !strings.HasPrefix(path, "./") && !strings.HasPrefix(path, "../") {
				return nil, l.fail(imp, "imports are not supported for remote paths; use ./ or ../ local packages")
			}
			target, err := filepath.Abs(filepath.Join(filepath.Dir(l.fset.PositionFor(file.Pos(), false).Filename), filepath.FromSlash(path)))
			if err != nil {
				return nil, err
			}
			dep, err := l.load(target, nil, false)
			if err != nil {
				return nil, importError(l.fset.PositionFor(imp.Pos(), false), err)
			}
			alias := dep.name
			if imp.Name != nil {
				alias = imp.Name.Name
			}
			if alias == "_" || alias == "." {
				return nil, l.fail(imp, "blank and dot imports are unsupported")
			}
			if aliases[alias] != nil {
				return nil, l.fail(imp, "duplicate import alias %s", alias)
			}
			if _, ok := m.names[alias]; ok {
				return nil, l.fail(imp, "import alias conflicts with package declaration %s", alias)
			}
			aliases[alias] = dep
		}
		// Reserve aliases throughout their file: local shadowing is deliberately an
		// error, making qualification unambiguous in both independent frontends.
		var shadow ast.Node
		fields := map[*ast.Ident]bool{}
		ast.Inspect(file, func(n ast.Node) bool {
			if st, ok := n.(*ast.StructType); ok {
				for _, field := range st.Fields.List {
					for _, name := range field.Names {
						fields[name] = true
					}
				}
			}
			if id, ok := n.(*ast.Ident); ok && !fields[id] && id.Obj != nil && id.Obj.Pos() == id.Pos() && aliases[id.Name] != nil {
				shadow = id
			}
			return true
		})
		if shadow != nil {
			return nil, l.fail(shadow, "cannot shadow an import alias")
		}
		replacements := map[*ast.SelectorExpr]*ast.Ident{}
		skip := map[*ast.Ident]bool{file.Name: true}
		literalTypes := map[*ast.CompositeLit]ast.Expr{}
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.ImportSpec:
				return false
			case *ast.SelectorExpr:
				skip[n.Sel] = true
			case *ast.Field:
				for _, name := range n.Names {
					skip[name] = true
				}
			case *ast.CompositeLit:
				typ := n.Type
				if typ == nil {
					typ = literalTypes[n]
				}
			unwrapLiteral:
				for {
					switch t := typ.(type) {
					case *ast.ParenExpr:
						typ = t.X
					case *ast.StarExpr:
						typ = t.X // An elided literal may also omit its address operator.
					default:
						break unwrapLiteral
					}
				}
				isMap := false
				var elementType ast.Expr
				if ix, ok := typ.(*ast.IndexListExpr); ok {
					if id, ok := ix.X.(*ast.Ident); ok && id.Name == "Map" && len(ix.Indices) == 2 {
						isMap = true
						elementType = ix.Indices[1]
					}
				}
				if ix, ok := typ.(*ast.IndexExpr); ok {
					if id, ok := ix.X.(*ast.Ident); ok && id.Name == "List" {
						elementType = ix.Index
					}
				}
				for _, elt := range n.Elts {
					if kv, ok := elt.(*ast.KeyValueExpr); ok {
						if !isMap {
							if id, ok := kv.Key.(*ast.Ident); ok {
								skip[id] = true
							}
						}
						elt = kv.Value
					}
					// Go permits Map[string,Map[string,int]]{"x": {key: 1}}.
					// Its inner keys are references, unlike elided struct fields.
					if literal, ok := elt.(*ast.CompositeLit); ok && elementType != nil {
						literalTypes[literal] = elementType
					}
				}
			}
			return true
		})
		var bad error
		ast.Inspect(file, func(n ast.Node) bool {
			if bad != nil {
				return false
			}
			if _, ok := n.(*ast.ImportSpec); ok {
				return false
			}
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && aliases[id.Name] != nil {
					dep := aliases[id.Name]
					name, found := dep.names[sel.Sel.Name]
					r := sel.Sel.Name[0]
					if !found || r < 'A' || r > 'Z' {
						bad = l.fail(sel, "package %s does not export %s", id.Name, sel.Sel.Name)
						return false
					}
					replacements[sel] = &ast.Ident{Name: name, NamePos: sel.Pos()}
					return false
				}
			}
			if id, ok := n.(*ast.Ident); ok && !skip[id] {
				if aliases[id.Name] != nil {
					bad = l.fail(id, "import alias %s requires a member selector", id.Name)
					return false
				}
				if name, found := m.names[id.Name]; found && (id.Obj == nil || top[id.Obj]) {
					id.Name = name
				} else if !root && id.Obj == nil && id.Name != "_" && l.native.Lookup(id.Name) == nil && types.Universe.Lookup(id.Name) == nil {
					// Reject free names before the root namespace can capture them.
					bad = l.fail(id, "undefined name %s", id.Name)
				}
			}
			return true
		})
		if bad != nil {
			return nil, bad
		}
		replaceSelectors(reflect.ValueOf(file), replacements)
		var decls []ast.Decl
		for _, d := range file.Decls {
			if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.IMPORT {
				continue
			}
			decls = append(decls, d)
		}
		file.Decls = decls
		file.Imports = nil
		file.Name.Name = "main"
	}
	m.state = 2
	l.files = append(l.files, m.files...)
	return m, nil
}

// AST interface fields hold expressions; skip object/scope back references.
func replaceSelectors(v reflect.Value, replacements map[*ast.SelectorExpr]*ast.Ident) {
	if !v.IsValid() {
		return
	}
	if v.Kind() == reflect.Interface {
		if v.IsNil() {
			return
		}
		if sel, ok := v.Interface().(*ast.SelectorExpr); ok {
			if id := replacements[sel]; id != nil {
				v.Set(reflect.ValueOf(id))
				return
			}
		}
		replaceSelectors(v.Elem(), replacements)
		return
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return
		}
		if _, ok := v.Interface().(ast.Node); !ok {
			return
		}
		replaceSelectors(v.Elem(), replacements)
		return
	}
	if v.Kind() == reflect.Struct {
		for i := 0; i < v.NumField(); i++ {
			replaceSelectors(v.Field(i), replacements)
		}
	}
	if v.Kind() == reflect.Slice {
		for i := 0; i < v.Len(); i++ {
			replaceSelectors(v.Index(i), replacements)
		}
	}
}

// InputFiles reports the full local dependency closure for output protection.
func InputFiles(sources []SourceFile) ([]SourceFile, error) {
	if len(sources) == 0 {
		return nil, fmt.Errorf("no source files")
	}
	fset := token.NewFileSet()
	files, err := parseModuleFiles(fset, sources, false)
	if err != nil {
		return nil, err
	}
	var inputs []SourceFile
	for _, file := range files {
		inputs = append(inputs, SourceFile{Filename: fset.PositionFor(file.Pos(), false).Filename})
	}
	return inputs, nil
}
