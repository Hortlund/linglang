package lsp

import (
	"fmt"
	"go/ast"
	"go/token"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"
)

func pathFromURI(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" || (u.Host != "" && u.Host != "localhost") || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("expected a local file URI")
	}
	path := u.Path
	if runtime.GOOS == "windows" && len(path) >= 3 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	if !filepath.IsAbs(filepath.FromSlash(path)) {
		return "", fmt.Errorf("expected an absolute file URI")
	}
	return filepath.Clean(filepath.FromSlash(path)), nil
}

func uriFromPath(path string) string {
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}

// LSP uses UTF-16 by default; source files and Go's AST use byte offsets.
func positionAt(text string, offset int) Position {
	if offset < 0 {
		offset = 0
	}
	if offset > len(text) {
		offset = len(text)
	}
	var pos Position
	for i := 0; i < offset; {
		r, width := utf8.DecodeRuneInString(text[i:])
		if i+width > offset {
			break
		}
		i += width
		if r == '\n' {
			pos.Line++
			pos.Character = 0
		} else if r == '\r' && i < len(text) && text[i] == '\n' {
			// CRLF's CR belongs to the line terminator, not its character range.
		} else {
			pos.Character++
			if r > 0xffff {
				pos.Character++
			}
		}
	}
	return pos
}

func nodeRange(fset *token.FileSet, text string, start, end token.Pos) Range {
	file := fset.File(start)
	if file == nil {
		return Range{}
	}
	// Parser recovery can synthesize identifiers whose End lies beyond EOF.
	// File.Offset clamps those positions; PositionFor would return a zero offset.
	first, last := file.Offset(start), file.Offset(end)
	if last < first {
		last = first
	}
	return Range{positionAt(text, first), positionAt(text, last)}
}

func outline(fset *token.FileSet, file *ast.File, text string) []documentSymbol {
	result := []documentSymbol{}
	if file == nil {
		return result
	}
	symbol := func(name *ast.Ident, node ast.Node, kind int) documentSymbol {
		return documentSymbol{Name: name.Name, Kind: kind, Range: nodeRange(fset, text, node.Pos(), node.End()), SelectionRange: nodeRange(fset, text, name.Pos(), name.End())}
	}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Name != nil {
				result = append(result, symbol(d.Name, d, 12))
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					item := symbol(s.Name, s, 23)
					if st, ok := s.Type.(*ast.StructType); ok && st.Fields != nil {
						for _, field := range st.Fields.List {
							for _, name := range field.Names {
								item.Children = append(item.Children, symbol(name, field, 8))
							}
						}
					}
					result = append(result, item)
				case *ast.ValueSpec:
					kind := 13
					if d.Tok == token.CONST {
						kind = 14
					}
					for _, name := range s.Names {
						result = append(result, symbol(name, s, kind))
					}
				}
			}
		}
	}
	return result
}
