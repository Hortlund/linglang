package compiler

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"
)

func (c *compiler) listElement(t types.Type) (types.Type, bool) {
	if !c.processType(t, "List") {
		return nil, false
	}
	args := t.(*types.Named).TypeArgs()
	if args.Len() != 1 {
		return nil, false // The bare generic identifier is not an instantiated list.
	}
	return args.At(0), true
}

func (c *compiler) listLiteral(literal *ast.CompositeLit) (string, error) {
	var values []string
	for _, expr := range literal.Elts {
		if _, keyed := expr.(*ast.KeyValueExpr); keyed {
			return "", c.errorf(expr, "list literals require positional elements")
		}
		value, err := c.expression(expr)
		if err != nil {
			return "", err
		}
		values = append(values, value)
	}
	return c.ordered(values, func(v []string) string {
		return "[" + strings.Join(v, ", ") + "]"
	}), nil
}

func (c *compiler) listIntrinsic(call *ast.CallExpr, name string) (string, error) {
	sig := c.info.TypeOf(call.Fun).(*types.Signature)
	for i := 0; i < sig.Params().Len(); i++ {
		t := sig.Params().At(i).Type()
		if _, list := c.listElement(t); list && !c.supportedType(t, map[types.Type]bool{}) {
			return "", c.errorf(call, "unsupported type %s", t)
		}
	}
	var values []string
	for _, arg := range call.Args {
		value, err := c.expression(arg)
		if err != nil {
			return "", err
		}
		values = append(values, value)
	}
	return c.ordered(values, func(v []string) string {
		if name == "head" {
			element, _ := c.listElement(sig.Params().At(0).Type())
			return "linglang_rt:list_head(" + v[0] + ", " + c.zero(element) + ")"
		}
		return "linglang_rt:list_" + name + "(" + strings.Join(v, ", ") + ")"
	}), nil
}

func (c *compiler) listBuiltin(call *ast.CallExpr) (string, bool, error) {
	id, ok := call.Fun.(*ast.Ident)
	if !ok {
		return "", false, nil
	}
	if _, builtin := c.info.Uses[id].(*types.Builtin); !builtin || (id.Name != "len" && id.Name != "append") {
		return "", false, nil
	}
	if basic, ok := c.info.TypeOf(call.Args[0]).Underlying().(*types.Basic); ok && basic.Kind() == types.String && id.Name == "len" {
		value, err := c.expression(call.Args[0])
		return c.ordered([]string{value}, func(v []string) string { return "byte_size(" + v[0] + ")" }), true, err
	}
	if _, _, dictionary := c.mapTypes(c.info.TypeOf(call.Args[0])); dictionary && id.Name == "len" {
		value, err := c.expression(call.Args[0])
		return c.ordered([]string{value}, func(v []string) string {
			return "linglang_rt:map_length(" + v[0] + ")"
		}), true, err
	}
	if _, list := c.listElement(c.info.TypeOf(call.Args[0])); !list {
		return "", true, c.errorf(call, "%s currently requires a List[T] (len also accepts Map[K,V] and string)", id.Name)
	}
	var values []string
	for _, arg := range call.Args {
		value, err := c.expression(arg)
		if err != nil {
			return "", true, err
		}
		values = append(values, value)
	}
	code := c.ordered(values, func(v []string) string {
		if id.Name == "len" {
			return "linglang_rt:list_length(" + v[0] + ")"
		}
		if call.Ellipsis.IsValid() {
			return "linglang_rt:list_append(" + v[0] + ", " + v[1] + ")"
		}
		return "linglang_rt:list_append(" + v[0] + ", [" + strings.Join(v[1:], ", ") + "])"
	})
	return code, true, nil
}

// Normalize range into the existing loop IR before either backend runs. The
// hidden remainder holds the original immutable snapshot; each iteration
// declares fresh user variables, including fresh cells when their addresses
// escape. Continue uses the normal loop post step, and break skips it.
func (c *compiler) lowerRanges(file *ast.File) error {
	c.rangeOps = map[ast.Expr]string{}
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
			if err := c.lowerRangeBody(fn.Body.List); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *compiler) lowerRangeBody(body []ast.Stmt) error {
	for i, stmt := range body {
		switch s := stmt.(type) {
		case *ast.BlockStmt:
			if err := c.lowerRangeBody(s.List); err != nil {
				return err
			}
		case *ast.IfStmt:
			if err := c.lowerRangeBody(s.Body.List); err != nil {
				return err
			}
			if s.Else != nil {
				other := []ast.Stmt{s.Else}
				if err := c.lowerRangeBody(other); err != nil {
					return err
				}
				s.Else = other[0]
			}
		case *ast.ForStmt:
			if err := c.lowerRangeBody(s.Body.List); err != nil {
				return err
			}
		case *ast.SwitchStmt:
			for _, clause := range s.Body.List {
				if err := c.lowerRangeBody(clause.(*ast.CaseClause).Body); err != nil {
					return err
				}
			}
		case *ast.RangeStmt:
			element, ok := c.listElement(c.info.TypeOf(s.X))
			if !ok {
				return c.errorf(s, "range currently requires a List[T]")
			}
			for _, target := range []ast.Expr{s.Key, s.Value} {
				if target != nil {
					if _, ok := target.(*ast.Ident); !ok {
						return c.errorf(target, "range targets must be identifiers")
					}
				}
			}
			if err := c.lowerRangeBody(s.Body.List); err != nil {
				return err
			}
			body[i] = c.rangeLoop(s, element)
		}
	}
	return nil
}

func (c *compiler) rangeLoop(s *ast.RangeStmt, element types.Type) ast.Stmt {
	definition, rest := c.rangeLocal(s.For, c.info.TypeOf(s.X))
	setup := []ast.Stmt{&ast.AssignStmt{Lhs: []ast.Expr{definition}, Tok: token.DEFINE, Rhs: []ast.Expr{s.X}}}
	var iteration, post []ast.Stmt
	if key, ok := s.Key.(*ast.Ident); ok && key.Name != "_" {
		indexDefinition, index := c.rangeLocal(s.For, types.Typ[types.Int])
		zero := &ast.BasicLit{ValuePos: s.For, Kind: token.INT, Value: "0"}
		c.info.Types[zero] = types.TypeAndValue{Type: types.Typ[types.Int], Value: constant.MakeInt64(0)}
		setup = append(setup, &ast.AssignStmt{Lhs: []ast.Expr{indexDefinition}, Tok: token.DEFINE, Rhs: []ast.Expr{zero}})
		iteration = append(iteration, &ast.AssignStmt{Lhs: []ast.Expr{key}, Tok: s.Tok, Rhs: []ast.Expr{index}})
		post = append(post, &ast.IncDecStmt{X: index, Tok: token.INC})
	}
	if value, ok := s.Value.(*ast.Ident); ok && value.Name != "_" {
		iteration = append(iteration, &ast.AssignStmt{Lhs: []ast.Expr{value}, Tok: s.Tok, Rhs: []ast.Expr{c.rangeOperation(s.For, "first", rest, element)}})
	}
	post = append(post, &ast.AssignStmt{Lhs: []ast.Expr{rest}, Tok: token.ASSIGN, Rhs: []ast.Expr{c.rangeOperation(s.For, "tail", rest, c.info.TypeOf(s.X))}})
	loop := &ast.ForStmt{For: s.For, Cond: c.rangeOperation(s.For, "more", rest, types.Typ[types.Bool]), Post: &ast.BlockStmt{List: post}, Body: &ast.BlockStmt{List: append(iteration, s.Body.List...)}}
	return &ast.BlockStmt{Lbrace: s.For, List: append(setup, loop)}
}

func (c *compiler) rangeLocal(pos token.Pos, t types.Type) (*ast.Ident, *ast.Ident) {
	name := "$range" + c.fresh()
	obj := types.NewVar(pos, nil, name, t)
	definition, use := &ast.Ident{NamePos: pos, Name: name}, &ast.Ident{NamePos: pos, Name: name}
	c.info.Defs[definition], c.info.Uses[use] = obj, obj
	c.info.Types[definition], c.info.Types[use] = types.TypeAndValue{Type: t}, types.TypeAndValue{Type: t}
	return definition, use
}

func (c *compiler) rangeOperation(pos token.Pos, op string, items ast.Expr, t types.Type) ast.Expr {
	call := &ast.CallExpr{Fun: &ast.Ident{NamePos: pos, Name: "$list_" + op}, Args: []ast.Expr{items}}
	c.info.Types[call] = types.TypeAndValue{Type: t}
	c.rangeOps[call] = op
	return call
}
