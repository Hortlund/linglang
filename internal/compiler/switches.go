package compiler

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

func supportedConstantType(t types.Type) bool {
	b, ok := t.(*types.Basic)
	if !ok {
		return false
	}
	switch b.Kind() {
	case types.Int, types.Bool, types.String,
		types.UntypedInt, types.UntypedRune, types.UntypedBool, types.UntypedString:
		return true
	}
	return false
}

func (c *compiler) switchStatement(s *ast.SwitchStmt) (string, error) {
	var setup []string
	if s.Init != nil {
		init, err := c.statement(s.Init)
		if err != nil {
			return "", err
		}
		setup = append(setup, init)
	}
	tag := ""
	if s.Tag != nil {
		value, err := c.expression(s.Tag)
		if err != nil {
			return "", err
		}
		tag = c.fresh()
		// The tag is a snapshot and remains rooted while case expressions run.
		setup = append(setup, tag+" = linglang_rt:keep("+value+")")
	}
	stop := c.fresh()
	c.breaks = append(c.breaks, stop)
	defer func() { c.breaks = c.breaks[:len(c.breaks)-1] }()
	setup = append(setup, stop+" = make_ref()")

	type branch struct{ condition, body string }
	var branches []branch
	fallback := "ok"
	for _, stmt := range s.Body.List {
		clause := stmt.(*ast.CaseClause)
		body, err := c.block(&ast.BlockStmt{List: clause.Body})
		if err != nil {
			return "", err
		}
		if clause.List == nil {
			fallback = body
			continue
		}
		var conditions []string
		for _, expr := range clause.List {
			condition, err := c.expression(expr)
			if err != nil {
				return "", err
			}
			if tag != "" {
				condition = c.ordered([]string{tag, condition}, func(v []string) string {
					return "linglang_rt:binary(eq, " + v[0] + ", " + v[1] + ")"
				})
			}
			conditions = append(conditions, "("+condition+")")
		}
		branches = append(branches, branch{strings.Join(conditions, " orelse "), body})
	}
	for i := len(branches) - 1; i >= 0; i-- {
		b := branches[i]
		fallback = "case " + scoped(b.condition) + " of true -> " + b.body + "; false -> " + fallback + " end"
	}
	setup = append(setup, "try "+fallback+" catch throw:{linglang_break, "+stop+"} -> ok end")
	return scoped(strings.Join(setup, ", ")), nil
}

// A switch becomes an ordered chain of ordinary conditional blocks. Tagged
// switches first store their tag in a synthetic local, so existing liveness and
// GC analysis keeps the snapshot alive across calls in case expressions.
func (g *localLowering) switchFlow(s *ast.SwitchStmt, next, again *flowBlock) *flowBlock {
	c := g.c
	var tag *ast.Ident
	var evaluate *ast.AssignStmt
	if s.Tag != nil {
		obj := types.NewVar(s.Switch, nil, "$switch", c.info.TypeOf(s.Tag))
		v := &local{object: obj}
		g.locals = append(g.locals, v)
		g.lookup[obj] = v
		definition := &ast.Ident{NamePos: s.Switch, Name: "$switch"}
		tag = &ast.Ident{NamePos: s.Switch, Name: "$switch"}
		c.info.Defs[definition], c.info.Uses[tag] = obj, obj
		c.info.Types[tag] = types.TypeAndValue{Type: obj.Type()}
		evaluate = &ast.AssignStmt{Lhs: []ast.Expr{definition}, TokPos: s.Switch, Tok: token.DEFINE, Rhs: []ast.Expr{s.Tag}}
	}
	type branch struct {
		clause *ast.CaseClause
		body   *flowBlock
	}
	var branches []branch
	entry := next
	for _, stmt := range s.Body.List {
		clause := stmt.(*ast.CaseClause)
		body := g.sequence(clause.Body, next, next, again)
		if clause.List == nil {
			entry = body
		} else {
			branches = append(branches, branch{clause, body})
		}
	}
	for i := len(branches) - 1; i >= 0; i-- {
		branch := branches[i]
		var condition ast.Expr
		for _, expr := range branch.clause.List {
			comparison := expr
			if tag != nil {
				comparison = g.switchBinary(tag, token.EQL, expr)
			}
			if condition == nil {
				condition = comparison
			} else {
				condition = g.switchBinary(condition, token.LOR, comparison)
			}
		}
		b := g.block()
		b.cond, b.next, b.other = condition, branch.body, entry
		entry = b
	}
	if evaluate != nil {
		entry = g.statement(evaluate, entry, nil, nil)
	}
	if s.Init != nil {
		entry = g.statement(s.Init, entry, nil, nil)
	}
	return entry
}

func (g *localLowering) switchBinary(left ast.Expr, op token.Token, right ast.Expr) ast.Expr {
	expr := &ast.BinaryExpr{X: left, OpPos: left.Pos(), Op: op, Y: right}
	g.c.info.Types[expr] = types.TypeAndValue{Type: types.Typ[types.Bool]}
	return expr
}
