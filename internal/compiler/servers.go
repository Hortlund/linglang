package compiler

import (
	"go/ast"
	"go/types"
)

func (c *compiler) serverCall(call *ast.CallExpr, name string) (string, error) {
	sig := c.info.TypeOf(call.Fun).(*types.Signature)
	if name == "call" {
		request, err := c.messageSchema(sig.Params().At(1).Type())
		if err != nil {
			return "", c.errorf(call, "call request: %v", err)
		}
		replyType := sig.Results().At(0).Type().(*types.Named).TypeArgs().At(0)
		reply, err := c.messageSchema(replyType)
		if err != nil {
			return "", c.errorf(call, "call reply: %v", err)
		}
		var args []string
		for _, arg := range call.Args {
			value, err := c.expression(arg)
			if err != nil {
				return "", err
			}
			args = append(args, value)
		}
		return c.ordered(args, func(v []string) string {
			return "linglang_server:call(" + v[0] + ", " + request + ", " + v[1] + ", " + reply + ", " + v[2] + ", " + c.zero(replyType) + ")"
		}), nil
	}
	handler, ok := call.Args[2].(*ast.Ident)
	if !ok {
		return "", c.errorf(call.Args[2], "superviseServer requires a named language function")
	}
	obj, ok := c.info.Uses[handler].(*types.Func)
	if !ok || c.intrinsics[obj] != "" {
		return "", c.errorf(handler, "superviseServer requires a named language function")
	}
	handlerSig := sig.Params().At(2).Type().(*types.Signature)
	var schemas []string
	for _, typ := range []types.Type{sig.Params().At(3).Type(), handlerSig.Params().At(1).Type(), handlerSig.Results().At(0).Type()} {
		schema, err := c.messageSchema(typ)
		if err != nil {
			return "", c.errorf(call, "superviseServer: %v", err)
		}
		schemas = append(schemas, schema)
	}
	var args []string
	for i, arg := range call.Args {
		if i == 2 {
			continue
		}
		value, err := c.expression(arg)
		if err != nil {
			return "", err
		}
		args = append(args, value)
	}
	return c.ordered(args, func(v []string) string {
		return "linglang_sup:add_server(" + v[0] + ", " + v[1] + ", fun " + functionName(handler.Name) + "/2, " + v[2] + ", " + schemas[0] + ", " + schemas[1] + ", " + schemas[2] + ", " + v[3] + ")"
	}), nil
}
