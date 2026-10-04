package compiler

import (
	"go/ast"
	"strconv"
	"strings"
)

// These declarations extend the intrinsic prelude; their bodies are never emitted.
const standardPrelude = `
type TextResult struct { value string; ok bool; reason string }
type IntResult struct { value int; ok bool; reason string }
type IOResult struct { ok bool; reason string }
type RuneResult struct { value int; width int; ok bool }
type FilesResult struct { value List[string]; ok bool; reason string }
func sourceFiles(paths List[string]) FilesResult { return FilesResult{} }
func buildProgram(source string, path string, inputs List[string], stress bool, stats bool) IOResult { return IOResult{} }
func runProgram(source string, arguments List[string], stress bool, stats bool) IOResult { return IOResult{} }
func readFile(path string) TextResult { return TextResult{} }
func writeFile(path string, text string) IOResult { return IOResult{} }
func split(text string, separator string) List[string] { return nil }
func trim(text string) string { return "" }
func parseInt(text string) IntResult { return IntResult{} }
func formatInt(value int) string { return "" }
func args() List[string] { return nil }
func assert(condition bool) {}
func byteAt(text string, index int) int { return 0 }
func slice(text string, start int, end int) string { return "" }
func join(parts List[string], separator string) string { return "" }
func runeAt(text string, offset int) RuneResult { return RuneResult{} }
func isLetter(value int) bool { return false }
func isDigit(value int) bool { return false }
`

func (c *compiler) standardCall(call *ast.CallExpr) (string, bool, error) {
	id := callIdentifier(call.Fun)
	if id == nil {
		return "", false, nil
	}
	if c.intrinsics[c.info.Uses[id]] == "assert" {
		condition, err := c.expression(call.Args[0])
		if err != nil {
			return "", true, err
		}
		position := c.fset.Position(call.Pos())
		return c.ordered([]string{condition}, func(v []string) string {
			return "linglang_rt:assert_value(" + v[0] + ", " + binaryString(position.Filename) + ", " + strconv.Itoa(position.Line) + ")"
		}), true, nil
	}
	operation := map[string]string{
		"sourceFiles": "source_files", "buildProgram": "build_program", "runProgram": "run_program",
		"readFile": "read_file", "writeFile": "write_file",
		"split": "text_split", "trim": "text_trim",
		"parseInt": "parse_int", "formatInt": "format_int", "args": "arguments",
		"byteAt": "text_byte", "slice": "text_slice", "join": "text_join",
		"runeAt": "text_rune", "isLetter": "unicode_letter", "isDigit": "unicode_digit",
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
