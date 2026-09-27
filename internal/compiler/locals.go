package compiler

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

// A block's arguments are the current versions of its live locals. Assignments
// produce new Erlang variables; edges pass those versions to their successor.
// Addressed locals instead carry a stable managed cell through the same graph.
type local struct {
	object types.Object
	boxed  bool
}

type flowBlock struct {
	id      int
	stmt    ast.Stmt
	clone   types.Object // fresh identity for an addressed for-loop variable
	cond    ast.Expr
	result  ast.Expr
	next    *flowBlock
	other   *flowBlock
	returns bool
	uses    map[types.Object]bool
	defs    map[types.Object]bool
	live    map[types.Object]bool
}

type localLowering struct {
	c          *compiler
	name       string
	locals     []*local
	lookup     map[types.Object]*local
	blocks     []*flowBlock
	rooted     bool
	safePoints bool
}

func (c *compiler) optimizedFunction(fn *ast.FuncDecl) (string, error) {
	g := &localLowering{c: c, name: functionName(fn.Name.Name), lookup: map[types.Object]*local{}}
	sig := c.info.Defs[fn.Name].Type().(*types.Signature)
	add := func(obj types.Object) {
		if _, exists := g.lookup[obj]; exists {
			return
		}
		v := &local{object: obj}
		g.locals = append(g.locals, v)
		g.lookup[obj] = v
	}
	for i := 0; i < sig.Params().Len(); i++ {
		add(sig.Params().At(i))
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if v, ok := c.info.Defs[id].(*types.Var); ok && !v.IsField() {
				add(v)
			}
		}
		return true
	})
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if unary, ok := n.(*ast.UnaryExpr); ok && unary.Op == token.AND {
			if obj, _ := g.valueLocation(unary.X); obj != nil {
				g.lookup[obj].boxed = true
			}
		}
		if expr, ok := n.(ast.Expr); ok && containsReferences(c.info.TypeOf(expr)) {
			g.rooted = true
		}
		if call, ok := n.(*ast.CallExpr); ok {
			if id := callIdentifier(call.Fun); id != nil {
				if _, userFunction := c.info.Uses[id].(*types.Func); userFunction {
					g.safePoints = true
				}
			}
		}
		return true
	})
	for _, v := range g.locals {
		g.rooted = g.rooted || v.boxed || containsReferences(v.object.Type())
	}
	g.safePoints = g.safePoints || g.rooted
	end := g.block()
	end.returns = true
	entry := g.sequence(fn.Body.List, end, nil, nil)
	g.analyzeLiveness()
	c.optimizing, c.rooted = true, g.rooted
	var args, setup []string
	state := map[types.Object]string{}
	for i := 0; i < sig.Params().Len(); i++ {
		obj := sig.Params().At(i)
		arg := c.fresh()
		args = append(args, arg)
		if entry.live[obj] && g.lookup[obj].boxed {
			cell := c.fresh()
			setup = append(setup, cell+" = linglang_rt:new("+arg+")")
			state[obj] = cell
		} else {
			state[obj] = arg
		}
	}
	setup = append(setup, g.edge(entry, state))
	start := strings.Join(setup, ", ")
	if g.rooted {
		start = scoped(start)
	}
	functions := []string{g.name + "(" + strings.Join(args, ", ") + ") -> " + start + "."}
	for _, block := range g.blocks {
		code, err := g.emitBlock(block)
		if err != nil {
			return "", err
		}
		functions = append(functions, code)
	}
	return strings.Join(functions, "\n\n"), nil
}

func containsReferences(t types.Type) bool {
	if t == nil {
		return false
	}
	switch t := t.Underlying().(type) {
	case *types.Pointer:
		return true
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if containsReferences(t.Field(i).Type()) {
				return true
			}
		}
	}
	return false
}

// Find the variable whose VALUE contains this location. A pointer indirection
// stops the walk: &s.inner.n needs s's identity, but &s.pointer.n does not.
func (g *localLowering) valueLocation(expr ast.Expr) (types.Object, []string) {
	switch e := expr.(type) {
	case *ast.ParenExpr:
		return g.valueLocation(e.X)
	case *ast.Ident:
		obj := g.c.info.Uses[e]
		if g.lookup[obj] != nil {
			return obj, nil
		}
	case *ast.SelectorExpr:
		if _, pointer := g.c.info.TypeOf(e.X).Underlying().(*types.Pointer); pointer {
			return nil, nil
		}
		obj, path := g.valueLocation(e.X)
		if obj != nil {
			return obj, append(path, fieldName(e.Sel.Name))
		}
	}
	return nil, nil
}

func (g *localLowering) block() *flowBlock {
	b := &flowBlock{id: len(g.blocks), uses: map[types.Object]bool{}, defs: map[types.Object]bool{}, live: map[types.Object]bool{}}
	g.blocks = append(g.blocks, b)
	return b
}

func (g *localLowering) sequence(stmts []ast.Stmt, next, stop, again *flowBlock) *flowBlock {
	for i := len(stmts) - 1; i >= 0; i-- {
		next = g.statement(stmts[i], next, stop, again)
	}
	return next
}

func (g *localLowering) statement(stmt ast.Stmt, next, stop, again *flowBlock) *flowBlock {
	switch s := stmt.(type) {
	case *ast.BlockStmt:
		return g.sequence(s.List, next, stop, again)
	case *ast.IfStmt:
		b := g.block()
		b.cond = s.Cond
		b.next = g.sequence(s.Body.List, next, stop, again)
		b.other = next
		if s.Else != nil {
			b.other = g.statement(s.Else, next, stop, again)
		}
		return b
	case *ast.ForStmt:
		test := g.block()
		test.cond = s.Cond
		test.other = next
		post := test
		if s.Post != nil {
			post = g.statement(s.Post, test, nil, nil)
		}
		if assign, ok := s.Init.(*ast.AssignStmt); ok && assign.Tok == token.DEFINE {
			obj := g.c.info.Defs[assign.Lhs[0].(*ast.Ident)]
			if g.lookup[obj].boxed {
				clone := g.block()
				clone.clone, clone.next = obj, post
				post = clone
			}
		}
		test.next = g.sequence(s.Body.List, post, next, post)
		// A nil condition is an unconditional edge to the body.
		if s.Cond == nil {
			test.other = nil
		}
		entry := test
		if s.Init != nil {
			entry = g.statement(s.Init, test, stop, again)
		}
		return entry
	case *ast.ReturnStmt:
		b := g.block()
		b.returns = true
		if len(s.Results) == 1 {
			b.result = s.Results[0]
		}
		return b
	case *ast.BranchStmt:
		if s.Tok == token.BREAK {
			return stop
		}
		return again
	default:
		b := g.block()
		b.stmt, b.next = stmt, next
		return b
	}
}

func (g *localLowering) use(b *flowBlock, expr ast.Expr) {
	if expr == nil {
		return
	}
	ast.Inspect(expr, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			obj := g.c.info.Uses[id]
			if g.lookup[obj] != nil && !b.defs[obj] {
				b.uses[obj] = true
			}
		}
		return true
	})
}

func (g *localLowering) target(b *flowBlock, expr ast.Expr, read bool) {
	obj, path := g.valueLocation(expr)
	if obj != nil && !g.lookup[obj].boxed {
		if read || len(path) != 0 {
			g.use(b, expr)
		}
		b.defs[obj] = true
	} else {
		g.use(b, expr)
	}
}

func (g *localLowering) analyzeLiveness() {
	for _, b := range g.blocks {
		if b.clone != nil {
			b.uses[b.clone], b.defs[b.clone] = true, true
		}
		switch s := b.stmt.(type) {
		case *ast.DeclStmt:
			for _, spec := range s.Decl.(*ast.GenDecl).Specs {
				v := spec.(*ast.ValueSpec)
				for _, value := range v.Values {
					g.use(b, value)
				}
				b.defs[g.c.info.Defs[v.Names[0]]] = true
			}
		case *ast.AssignStmt:
			g.use(b, s.Rhs[0])
			if s.Tok == token.DEFINE {
				b.defs[g.c.info.Defs[s.Lhs[0].(*ast.Ident)]] = true
			} else {
				g.target(b, s.Lhs[0], s.Tok != token.ASSIGN)
			}
		case *ast.IncDecStmt:
			g.target(b, s.X, true)
		case *ast.ExprStmt:
			g.use(b, s.X)
		}
		g.use(b, b.cond)
		g.use(b, b.result)
	}
	for changed := true; changed; {
		changed = false
		for _, b := range g.blocks {
			for obj := range b.uses {
				if !b.live[obj] {
					b.live[obj], changed = true, true
				}
			}
			for _, next := range []*flowBlock{b.next, b.other} {
				if next == nil {
					continue
				}
				for obj := range next.live {
					if !b.defs[obj] && !b.live[obj] {
						b.live[obj], changed = true, true
					}
				}
			}
		}
	}
}

func (g *localLowering) blockName(b *flowBlock) string { return fmt.Sprintf("%s_b%d", g.name, b.id) }

// Use ordinary arguments for common cases; pack very large environments to
// avoid BEAM's function arity limit without restricting the source language.
func flowArgs(values []string) string {
	joined := strings.Join(values, ", ")
	if len(values) > 200 {
		return "{" + joined + "}"
	}
	return joined
}

func (g *localLowering) edge(next *flowBlock, state map[types.Object]string) string {
	var args []string
	for _, v := range g.locals {
		if next.live[v.object] {
			args = append(args, state[v.object])
		}
	}
	return g.blockName(next) + "(" + flowArgs(args) + ")"
}

func (g *localLowering) eval(code string) string {
	if g.rooted {
		return scoped(code)
	}
	return "(begin " + code + " end)"
}

func (g *localLowering) emitBlock(b *flowBlock) (string, error) {
	c := g.c
	c.cells, c.values = map[types.Object]string{}, map[types.Object]string{}
	state := map[types.Object]string{}
	var params, roots, parts []string
	for _, v := range g.locals {
		if !b.live[v.object] {
			continue
		}
		value := c.fresh()
		state[v.object] = value
		params = append(params, value)
		if v.boxed {
			c.cells[v.object] = value
		} else {
			c.values[v.object] = value
		}
		if v.boxed || containsReferences(v.object.Type()) {
			roots = append(roots, value)
		}
	}
	if g.rooted {
		parts = append(parts, "linglang_rt:roots(["+strings.Join(roots, ", ")+"])")
	}
	if g.safePoints {
		parts = append(parts, "linglang_rt:safepoint()")
	}
	if b.clone != nil {
		obj := b.clone
		value := c.fresh()
		parts = append(parts, value+" = linglang_rt:new(linglang_rt:read("+state[obj]+"))")
		state[obj], c.cells[obj] = value, value
	}
	if b.stmt != nil {
		code, err := g.emitSimple(b.stmt, state)
		if err != nil {
			return "", err
		}
		parts = append(parts, code)
	}
	switch {
	case b.returns:
		value := "ok"
		if b.result != nil {
			var err error
			value, err = c.expression(b.result)
			if err != nil {
				return "", err
			}
		}
		parts = append(parts, g.eval(value))
	case b.cond != nil:
		cond, err := c.expression(b.cond)
		if err != nil {
			return "", err
		}
		parts = append(parts, "case "+g.eval(cond)+" of true -> "+g.edge(b.next, state)+"; false -> "+g.edge(b.other, state)+" end")
	default:
		parts = append(parts, g.edge(b.next, state))
	}
	return g.blockName(b) + "(" + flowArgs(params) + ") ->\n    " + strings.Join(parts, ",\n    ") + ".", nil
}

func (g *localLowering) declare(obj types.Object, value string, state map[types.Object]string) string {
	c := g.c
	name := c.fresh()
	state[obj] = name
	if g.lookup[obj].boxed {
		c.cells[obj] = name
		return name + " = linglang_rt:new(" + g.eval(value) + ")"
	}
	c.values[obj] = name
	return name + " = " + g.eval(value)
}

func (g *localLowering) emitSimple(stmt ast.Stmt, state map[types.Object]string) (string, error) {
	c := g.c
	switch s := stmt.(type) {
	case *ast.EmptyStmt:
		return "ok", nil
	case *ast.DeclStmt:
		var parts []string
		for _, spec := range s.Decl.(*ast.GenDecl).Specs {
			v := spec.(*ast.ValueSpec)
			obj := c.info.Defs[v.Names[0]]
			value := c.zero(obj.Type())
			if len(v.Values) == 1 {
				var err error
				value, err = c.expression(v.Values[0])
				if err != nil {
					return "", err
				}
			}
			parts = append(parts, g.declare(obj, value, state))
			// A later initializer may call a function and collect. New direct
			// pointer values must join the function roots before that happens.
			if !g.lookup[obj].boxed && containsReferences(obj.Type()) {
				parts = append(parts, "linglang_rt:keep("+state[obj]+")")
			}
		}
		return strings.Join(parts, ", "), nil
	case *ast.AssignStmt:
		if s.Tok == token.DEFINE {
			value, err := c.expression(s.Rhs[0])
			if err != nil {
				return "", err
			}
			return g.declare(c.info.Defs[s.Lhs[0].(*ast.Ident)], value, state), nil
		}
		if obj, path := g.valueLocation(s.Lhs[0]); obj != nil && !g.lookup[obj].boxed {
			value, err := c.expression(s.Rhs[0])
			if err != nil {
				return "", err
			}
			if s.Tok != token.ASSIGN {
				op := map[token.Token]string{token.ADD_ASSIGN: "add", token.SUB_ASSIGN: "sub", token.MUL_ASSIGN: "mul", token.QUO_ASSIGN: "divide", token.REM_ASSIGN: "remain"}[s.Tok]
				value = c.ordered([]string{readFields(state[obj], path), value}, func(v []string) string { return "linglang_rt:binary(" + op + ", " + v[0] + ", " + v[1] + ")" })
			}
			return g.assignValue(obj, path, value, state), nil
		}
	case *ast.IncDecStmt:
		if obj, path := g.valueLocation(s.X); obj != nil && !g.lookup[obj].boxed {
			op := "add"
			if s.Tok == token.DEC {
				op = "sub"
			}
			value := "linglang_rt:binary(" + op + ", " + readFields(state[obj], path) + ", 1)"
			return g.assignValue(obj, path, value, state), nil
		}
	}
	// Pointer writes, calls, and discarded expressions retain the established
	// target/operand evaluation order and temporary-root protocol.
	code, err := c.statementCode(stmt)
	if err != nil {
		return "", err
	}
	return g.eval(code), nil
}

func readFields(value string, path []string) string {
	for _, field := range path {
		value = "maps:get(" + field + ", " + value + ")"
	}
	return value
}

func updateFields(base string, path []string, value string) string {
	if len(path) == 0 {
		return value
	}
	child := "maps:get(" + path[0] + ", " + base + ")"
	return "(" + base + ")#{" + path[0] + " := " + updateFields(child, path[1:], value) + "}"
}

func (g *localLowering) assignValue(obj types.Object, path []string, value string, state map[types.Object]string) string {
	name := g.c.fresh()
	code := updateFields(state[obj], path, value)
	state[obj], g.c.values[obj] = name, name
	return name + " = " + g.eval(code)
}
