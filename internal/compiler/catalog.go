package compiler

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"sort"
)

// NativeDeclaration describes the compiler prelude, not user-declarable generic
// syntax. Opaque/collection types deliberately hide Go type-checker stand-ins.
type NativeDeclaration struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Declaration string `json:"declaration"`
}

// NativeAPI derives documentation from the exact prelude used for checking.
// Language operators and Go-universe builtins are documented separately.
func NativeAPI() ([]NativeDeclaration, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "<linglang-prelude>", PreludeSource(), 0)
	if err != nil {
		return nil, err
	}
	result := []NativeDeclaration{}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			d.Body = nil
			var text bytes.Buffer
			if err := format.Node(&text, fset, d); err != nil {
				return nil, err
			}
			result = append(result, NativeDeclaration{d.Name.Name, "function", text.String()})
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				t := spec.(*ast.TypeSpec)
				kind, declaration := "opaque", t.Name.Name
				if _, ok := t.Type.(*ast.StructType); ok {
					kind = "record"
					var text bytes.Buffer
					if err := format.Node(&text, fset, t); err != nil {
						return nil, err
					}
					declaration = "type " + text.String()
				} else if t.TypeParams != nil {
					kind = "collection"
					declaration += "["
					separator := ""
					for _, field := range t.TypeParams.List {
						for _, name := range field.Names {
							declaration += separator + name.Name
							separator = ", "
						}
					}
					declaration += "]"
				}
				result = append(result, NativeDeclaration{t.Name.Name, kind, declaration})
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}
