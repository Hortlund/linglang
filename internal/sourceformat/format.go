// Package sourceformat formats linglang source without running it.
package sourceformat

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
)

func Format(filename string, data []byte) ([]byte, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, data, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	// Go's printer strips required header parentheses around generic literals.
	// A temporary call preserves the original parentheses without flattening the
	// type AST or moving its comments. Choose a name absent from all source text
	// so removing it after printing cannot affect identifiers, strings, or comments.
	marker := "__linglang_fmt_parens__"
	for bytes.Contains(data, []byte(marker)) {
		marker += "_"
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch statement := node.(type) {
		case *ast.RangeStmt:
			statement.X = protectHeaderParens(statement.X, marker)
		case *ast.IfStmt:
			statement.Cond = protectHeaderParens(statement.Cond, marker)
		case *ast.ForStmt:
			statement.Cond = protectHeaderParens(statement.Cond, marker)
		case *ast.SwitchStmt:
			statement.Tag = protectHeaderParens(statement.Tag, marker)
		}
		return true
	})
	var output bytes.Buffer
	if err := format.Node(&output, fset, file); err != nil {
		return nil, err
	}
	formatted := bytes.ReplaceAll(output.Bytes(), []byte(marker), nil)
	// Never publish source that our parser cannot read, including uncommon
	// comment placements in a generic type expression.
	if _, err := parser.ParseFile(token.NewFileSet(), filename, formatted, parser.ParseComments); err != nil {
		return nil, fmt.Errorf("formatter produced invalid syntax: %w", err)
	}
	return formatted, nil
}

func protectHeaderParens(expr ast.Expr, marker string) ast.Expr {
	paren, ok := expr.(*ast.ParenExpr)
	if !ok {
		return expr
	}
	generic := false
	ast.Inspect(paren.X, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.ParenExpr:
			return false // Inner parentheses already protect their literals.
		case *ast.CompositeLit:
			switch node.Type.(type) {
			case *ast.IndexExpr, *ast.IndexListExpr:
				generic = true
			}
			return false
		}
		return true
	})
	if generic {
		return &ast.CallExpr{
			Fun:    &ast.Ident{NamePos: paren.Lparen, Name: marker},
			Lparen: paren.Lparen, Args: []ast.Expr{paren.X}, Rparen: paren.Rparen,
		}
	}
	// The printer can strip several nested parentheses before reaching a literal.
	paren.X = protectHeaderParens(paren.X, marker)
	return paren
}
