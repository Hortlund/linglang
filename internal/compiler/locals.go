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
	id        int
	stmt      ast.Stmt
	clone     types.Object // fresh identity for an addressed for-loop variable
	rangeRest types.Object // consume a native list cell on the true edge
	rangeHead types.Object // current element, when the range binds a value
	cond      ast.Expr
	result    []ast.Expr
	next      *flowBlock
	other     *flowBlock
	returns   bool
	uses      map[types.Object]bool
	defs      map[types.Object]bool
	live      map[types.Object]bool
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
				if _, userFunction := c.info.Uses[id].(*types.Func); userFunction && !c.nonCollectingLeaves[c.info.Uses[id]] && !c.nonCollectingCall(call) {
					g.safePoints = true
				}
			}
		}
		return true
	})
	for _, v := range g.locals {
		g.rooted = g.rooted || v.boxed || containsReferences(v.object.Type())
	}
	// Pointer-free callers need no managed collection checks for proven leaves.
	// Rooted callers retain checks, including on loops, even for leaf calls.
	g.safePoints = g.safePoints || g.rooted
	// Leaf helpers may borrow their caller's roots. Without managed allocation,
	// loops, blocking intrinsics, or calls that could collect, no managed cell
	// can disappear while the helper executes. BEAM still traces native terms.
	if c.nonCollectingLeaf(fn) {
		g.rooted, g.safePoints = false, false
	}
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
			setup = append(setup, cell+" = "+c.newCell(obj.Type(), arg))
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

func (c *compiler) nonCollectingLeaf(fn *ast.FuncDecl) bool {
	if fn.Body == nil {
		return false // unsupported declarations are diagnosed during lowering
	}
	safe := true
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		if !safe {
			return false
		}
		switch node := node.(type) {
		case *ast.ForStmt, *ast.RangeStmt:
			safe = false // retain safe points on every back edge
		case *ast.UnaryExpr:
			if node.Op == token.AND {
				safe = false // may box a local or allocate a composite literal
			}
		case *ast.CallExpr:
			safe = c.nonCollectingCall(node)
		}
		return safe
	})
	return safe
}

func (c *compiler) nonCollectingCall(call *ast.CallExpr) bool {
	id := callIdentifier(call.Fun)
	if id == nil {
		return false
	}
	obj := c.info.Uses[id]
	if _, builtin := obj.(*types.Builtin); builtin {
		return id.Name == "len" || id.Name == "append"
	}
	// Resolve the actual intrinsic binding: a user function with one of these
	// names may allocate or collect. Keep this list limited to runtime helpers
	// that never call new, collect, safepoint, or another linglang function.
	switch c.intrinsics[obj] {
	case "byteAt", "slice", "runeAt", "formatInt", "trim", "parseInt", "isLetter", "isDigit", "assert":
		return true
	case "prepend", "head", "tail", "get", "put", "remove", "split", "join":
		// These construct only native BEAM terms. Pointer-bearing arguments and
		// results still trigger the caller's normal managed-root protocol.
		return true
	}
	return false
}

func containsReferences(t types.Type) bool {
	return containsReferencesSeen(t, map[types.Type]bool{})
}

func containsReferencesSeen(t types.Type, seen map[types.Type]bool) bool {
	if t == nil {
		return false
	}
	if seen[t] {
		return false
	}
	seen[t] = true
	switch t := t.Underlying().(type) {
	case *types.Pointer:
		return true
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if containsReferencesSeen(t.Field(i).Type(), seen) {
				return true
			}
		}
	case *types.Tuple:
		for i := 0; i < t.Len(); i++ {
			if containsReferencesSeen(t.At(i).Type(), seen) {
				return true
			}
		}
	case *types.Slice:
		return containsReferencesSeen(t.Elem(), seen)
	case *types.Map:
		return containsReferencesSeen(t.Key(), seen) || containsReferencesSeen(t.Elem(), seen)
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
	case *ast.DeclStmt:
		if s.Decl.(*ast.GenDecl).Tok == token.CONST {
			return next
		}
		b := g.block()
		b.stmt, b.next = stmt, next
		return b
	case *ast.SwitchStmt:
		return g.switchFlow(s, next, again)
	case *ast.IfStmt:
		b := g.block()
		b.cond = s.Cond
		b.next = g.sequence(s.Body.List, next, stop, again)
		b.other = next
		if s.Else != nil {
			b.other = g.statement(s.Else, next, stop, again)
		}
		if s.Init != nil {
			return g.statement(s.Init, b, stop, again)
		}
		return b
	case *ast.ForStmt:
		if info := g.c.rangeLoops[s]; info != nil {
			return g.listRangeFlow(s, info, next)
		}
		test := g.block()
		test.cond = s.Cond
		test.other = next
		post := test
		if s.Post != nil {
			post = g.statement(s.Post, test, nil, nil)
		}
		if assign, ok := s.Init.(*ast.AssignStmt); ok && assign.Tok == token.DEFINE {
			for _, lhs := range assign.Lhs {
				obj := g.c.info.Defs[lhs.(*ast.Ident)]
				if g.lookup[obj] != nil && g.lookup[obj].boxed {
					clone := g.block()
					clone.clone, clone.next = obj, post
					post = clone
				}
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
		b.result = s.Results
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
		if b.rangeRest != nil {
			b.uses[b.rangeRest], b.defs[b.rangeRest] = true, true
			if b.rangeHead != nil {
				b.defs[b.rangeHead] = true
			}
		}
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
				for _, name := range v.Names {
					b.defs[g.c.info.Defs[name]] = true
				}
			}
		case *ast.AssignStmt:
			for _, rhs := range s.Rhs {
				g.use(b, rhs)
			}
			// Destination references are read before any destination is defined.
			for _, lhs := range s.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && (id.Name == "_" || g.c.info.Defs[id] != nil) {
					continue
				}
				g.use(b, lhs)
			}
			for _, lhs := range s.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && g.c.info.Defs[id] != nil {
					b.defs[g.c.info.Defs[id]] = true
				} else {
					g.target(b, lhs, s.Tok != token.ASSIGN && s.Tok != token.DEFINE)
				}
			}
		case *ast.IncDecStmt:
			g.target(b, s.X, true)
		case *ast.ExprStmt:
			g.use(b, s.X)
		}
		g.use(b, b.cond)
		for _, value := range b.result {
			g.use(b, value)
		}
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
		parts = append(parts, value+" = "+c.newCell(obj.Type(), "linglang_rt:read("+state[obj]+")"))
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
	case b.rangeRest != nil:
		rest := state[b.rangeRest]
		empty := g.edge(b.other, state)
		head := "_"
		if b.rangeHead != nil {
			head = c.fresh()
			state[b.rangeHead] = head
		}
		tail := c.fresh()
		state[b.rangeRest] = tail
		parts = append(parts, "case "+rest+" of ["+head+" | "+tail+"] -> "+g.edge(b.next, state)+"; [] -> "+empty+"; nil -> "+empty+" end")
	case b.returns:
		value, err := c.resultValue(b.result)
		if err != nil {
			return "", err
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
		return name + " = " + c.newCell(obj.Type(), g.eval(value))
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
			code, err := c.variableDeclaration(spec.(*ast.ValueSpec), g, state)
			if err != nil {
				return "", err
			}
			parts = append(parts, code)
		}
		return strings.Join(parts, ", "), nil
	case *ast.AssignStmt:
		if len(s.Lhs) > 1 && (s.Tok == token.DEFINE || s.Tok == token.ASSIGN) {
			return c.parallelAssignment(s.Lhs, s.Rhs, g, state)
		}
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
				op := map[token.Token]string{token.ADD_ASSIGN: "add", token.SUB_ASSIGN: "sub", token.MUL_ASSIGN: "mul", token.QUO_ASSIGN: "divide", token.REM_ASSIGN: "remain", token.AND_ASSIGN: "bit_and", token.OR_ASSIGN: "bit_or", token.XOR_ASSIGN: "bit_xor", token.AND_NOT_ASSIGN: "bit_clear", token.SHL_ASSIGN: "shift_left", token.SHR_ASSIGN: "shift_right"}[s.Tok]
				if s.Tok == token.SHL_ASSIGN || s.Tok == token.SHR_ASSIGN {
					value = c.shiftCount(s.Rhs[0], value)
				}
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
