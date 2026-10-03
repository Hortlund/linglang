package compiler

import (
	"go/ast"
	"strings"
)

// These declarations extend the intrinsic prelude; their bodies are never emitted.
const standardPrelude = `
type TextResult struct { value string; ok bool; reason string }
type IntResult struct { value int; ok bool; reason string }
type IOResult struct { ok bool; reason string }
func readFile(path string) TextResult { return TextResult{} }
func writeFile(path string, text string) IOResult { return IOResult{} }
func split(text string, separator string) List[string] { return nil }
func trim(text string) string { return "" }
func parseInt(text string) IntResult { return IntResult{} }
func formatInt(value int) string { return "" }
func args() List[string] { return nil }
`

func (c *compiler) standardCall(call *ast.CallExpr) (string, bool, error) {
	id := callIdentifier(call.Fun)
	if id == nil {
		return "", false, nil
	}
	operation := map[string]string{
		"readFile": "read_file", "writeFile": "write_file",
		"split": "text_split", "trim": "text_trim",
		"parseInt": "parse_int", "formatInt": "format_int", "args": "arguments",
	}[c.intrinsics[c.info.Uses[id]]]
	if operation == "" {
		return "", false, nil
	}
	var values []string
	for _, arg := range call.Args {
		value, err := c.expression(arg)
		if err != nil {
			return "", true, err
		}
		values = append(values, value)
	}
	return c.ordered(values, func(v []string) string {
		return "linglang_rt:" + operation + "(" + strings.Join(v, ", ") + ")"
	}), true, nil
}
