package compiler

import (
	"go/ast"
	"go/types"
	"strings"
)

const mapPrelude = `
type Map[K comparable, V any] map[K]V
func get[K comparable, V any](items Map[K,V], key K) Delivery[V] { return Delivery[V]{} }
func put[K comparable, V any](items Map[K,V], key K, value V) Map[K,V] { return nil }
func remove[K comparable, V any](items Map[K,V], key K) Map[K,V] { return nil }
`

func (c *compiler) mapTypes(t types.Type) (types.Type, types.Type, bool) {
	if !c.processType(t, "Map") {
		return nil, nil, false
	}
	args := t.(*types.Named).TypeArgs()
	if args.Len() != 2 {
		return nil, nil, false
	}
	return args.At(0), args.At(1), true
}

func supportedMapKey(t types.Type) bool {
	basic, ok := t.(*types.Basic)
	return ok && (basic.Kind() == types.Int || basic.Kind() == types.String)
}

func (c *compiler) mapLiteral(literal *ast.CompositeLit) (string, error) {
	var values []string
	for _, expr := range literal.Elts {
		pair, ok := expr.(*ast.KeyValueExpr)
		if !ok {
			return "", c.errorf(expr, "map literals require key: value elements")
		}
		for _, item := range []ast.Expr{pair.Key, pair.Value} {
			value, err := c.expression(item)
			if err != nil {
				return "", err
			}
			values = append(values, value)
		}
	}
	return c.ordered(values, func(v []string) string {
		var pairs []string
		for i := 0; i < len(v); i += 2 {
			pairs = append(pairs, "{"+v[i]+", "+v[i+1]+"}")
		}
		return "maps:from_list([" + strings.Join(pairs, ", ") + "])"
	}), nil
}

func (c *compiler) mapCall(call *ast.CallExpr) (string, bool, error) {
	id := callIdentifier(call.Fun)
	if id == nil {
		return "", false, nil
	}
	name := c.intrinsics[c.info.Uses[id]]
	if name != "get" && name != "put" && name != "remove" {
		return "", false, nil
	}
	sig := c.info.TypeOf(call.Fun).(*types.Signature)
	items := sig.Params().At(0).Type()
	key, _, _ := c.mapTypes(items)
	if !supportedMapKey(key) {
		return "", true, c.errorf(call, "unsupported type %s (Map keys must be int or string)", items)
	}
	if !c.supportedType(items, map[types.Type]bool{}) {
		return "", true, c.errorf(call, "unsupported type %s", items)
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
		if name == "get" {
			_, element, _ := c.mapTypes(items)
			v = append(v, c.zero(element))
		}
		return "linglang_rt:map_" + name + "(" + strings.Join(v, ", ") + ")"
	})
	return code, true, nil
}
