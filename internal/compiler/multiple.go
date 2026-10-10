package compiler

import (
	"fmt"
	"go/ast"
	"go/types"
	"strings"
)

// Multiple results are an internal Erlang tuple, never a first-class source type.
func (c *compiler) resultValue(values []ast.Expr) (string, error) {
	if len(values) == 0 {
		return "ok", nil
	}
	var codes []string
	for _, value := range values {
		code, err := c.expression(value)
		if err != nil {
			return "", err
		}
		codes = append(codes, code)
	}
	if len(codes) == 1 {
		return codes[0], nil
	}
	return c.ordered(codes, func(v []string) string { return "{" + strings.Join(v, ", ") + "}" }), nil
}

func resultElement(value string, index, count int) string {
	if count == 1 {
		return value
	}
	return fmt.Sprintf("element(%d, %s)", index+1, value)
}

func (c *compiler) parallelAssignment(lhs, rhs []ast.Expr, g *localLowering, state map[types.Object]string) (string, error) {
	value, err := c.resultValue(rhs)
	if err != nil {
		return "", err
	}
	return c.assignValues(lhs, value, g, state)
}

// Capture all destination references, then RHS values, before performing any
// write. The temporary scope roots references across calls and collections.
// No safe point occurs between its result handoff and installation into locals.
func (c *compiler) assignValues(lhs []ast.Expr, value string, g *localLowering, state map[types.Object]string) (string, error) {
	if len(lhs) == 1 {
		if id, ok := lhs[0].(*ast.Ident); ok {
			if id.Name == "_" {
				return scoped(value), nil
			}
			if obj := c.info.Defs[id]; obj != nil {
				if g == nil {
					return c.cell(obj) + " = " + c.newCell(obj.Type(), scoped(value)), nil
				}
				code := g.declare(obj, value, state)
				if g.rooted && !g.lookup[obj].boxed && containsReferences(obj.Type()) {
					code += ", linglang_rt:keep(" + state[obj] + ")"
				}
				return code, nil
			}
		}
	}
	var inputs []string
	addresses := map[int]int{}
	for i, target := range lhs {
		if id, ok := target.(*ast.Ident); ok && (id.Name == "_" || c.info.Defs[id] != nil) {
			continue
		}
		if g != nil {
			if obj, _ := g.valueLocation(target); obj != nil && !g.lookup[obj].boxed {
				continue
			}
		}
		address, err := c.address(target)
		if err != nil {
			return "", err
		}
		addresses[i] = len(inputs)
		inputs = append(inputs, address)
	}
	inputs = append(inputs, value)
	captured := c.ordered(inputs, func(v []string) string { return "{" + strings.Join(v, ", ") + "}" })
	if g == nil {
		captured = scoped(captured)
	} else {
		captured = g.eval(captured)
	}
	temp := c.fresh()
	parts := []string{temp + " = " + captured}
	values := fmt.Sprintf("element(%d, %s)", len(inputs), temp)
	for i, target := range lhs {
		item := resultElement(values, i, len(lhs))
		if id, ok := target.(*ast.Ident); ok {
			if id.Name == "_" {
				continue
			}
			if obj := c.info.Defs[id]; obj != nil {
				if g == nil {
					parts = append(parts, c.cell(obj)+" = "+c.newCell(obj.Type(), item))
				} else {
					parts = append(parts, g.declare(obj, item, state))
					if g.rooted && !g.lookup[obj].boxed && containsReferences(obj.Type()) {
						parts = append(parts, "linglang_rt:keep("+state[obj]+")")
					}
				}
				continue
			}
		}
		if g != nil {
			if obj, path := g.valueLocation(target); obj != nil && !g.lookup[obj].boxed {
				parts = append(parts, g.assignValue(obj, path, item, state))
				continue
			}
		}
		parts = append(parts, fmt.Sprintf("linglang_rt:write(element(%d, %s), %s)", addresses[i]+1, temp, item))
	}
	parts = append(parts, "ok")
	return "(begin " + strings.Join(parts, ", ") + " end)", nil
}

func (c *compiler) variableDeclaration(spec *ast.ValueSpec, g *localLowering, state map[types.Object]string) (string, error) {
	var lhs []ast.Expr
	for _, name := range spec.Names {
		lhs = append(lhs, name)
	}
	if len(spec.Values) != 0 {
		return c.parallelAssignment(lhs, spec.Values, g, state)
	}
	var zeros []string
	for _, name := range spec.Names {
		zeros = append(zeros, c.zero(c.info.Defs[name].Type()))
	}
	value := zeros[0]
	if len(zeros) > 1 {
		value = "{" + strings.Join(zeros, ", ") + "}"
	}
	return c.assignValues(lhs, value, g, state)
}
