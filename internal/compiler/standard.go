package compiler

import (
	"go/ast"
	"strconv"
	"strings"
)

// These declarations extend the intrinsic prelude; their bodies are never emitted.
const standardPrelude = `
func sha256(text string) string { return "" }
type PathInfo struct { kind string; ok bool; reason string }
func pathInfo(path string) PathInfo { return PathInfo{} }
func readDirectory(path string) FilesResult { return FilesResult{} }
func canonicalPath(path string) TextResult { return TextResult{} }
func makeDirectories(path string) IOResult { return IOResult{} }
func writeNewFile(path string, text string) IOResult { return IOResult{} }
func replaceFile(path string, expected string, text string) IOResult { return IOResult{} }
type SocketResult struct { value Socket; ok bool; reason string }; type SQLValue struct { kind string; value string }; type SQLResult struct { columns List[string]; rows List[List[SQLValue]]; changes int; ok bool; reason string }
func tcpListen(host string, port int) SocketResult { return SocketResult{} }
func tcpConnect(host string, port int, timeout int) SocketResult { return SocketResult{} }
func tcpAccept(socket Socket, timeout int) SocketResult { return SocketResult{} }
func tcpPort(socket Socket) IntResult { return IntResult{} }
func tcpRead(socket Socket, size int, timeout int) TextResult { return TextResult{} }
func tcpWrite(socket Socket, data string, timeout int) IOResult { return IOResult{} }
func tcpClose(socket Socket) bool { return false }
func sqliteQuery(path string, sql string, params List[SQLValue], timeout int) SQLResult { return SQLResult{} }
type TextResult struct { value string; ok bool; reason string }
type IntResult struct { value int; ok bool; reason string }
type IOResult struct { ok bool; reason string }
type RuneResult struct { value int; width int; ok bool }
type FilesResult struct { value List[string]; ok bool; reason string }
func monotonicMillis() int { return 0 }
func modulePath(file string, relative string) string { return "" }
func testFiles(paths List[string]) FilesResult { return FilesResult{} }
type TestRunResult struct { ok bool; timedOut bool; reason string }
func toolReport(report string, success bool) {}
func runTestProgramResult(source string, arguments List[string], stress bool, stats bool, timeout int, stderrOutput bool) TestRunResult { return TestRunResult{} }
func runTestProgram(source string, arguments List[string], stress bool, stats bool, timeout int) IOResult { return IOResult{} }
func moduleFiles(path string) FilesResult { return FilesResult{} }
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
		"sha256": "io:sha256", "pathInfo": "io:path_info", "readDirectory": "io:read_directory", "canonicalPath": "io:canonical_path",
		"makeDirectories": "io:make_directories", "writeNewFile": "io:write_new_file", "replaceFile": "io:replace_file",
		"monotonicMillis": "monotonic_millis", "modulePath": "module_path", "testFiles": "test_files", "runTestProgram": "run_test_program",
		"toolReport": "tool_report", "runTestProgramResult": "run_test_program_result",
		"moduleFiles": "module_files", "sourceFiles": "source_files", "buildProgram": "build_program", "runProgram": "run_program",
		"tcpListen":   "io:tcp_listen",
		"tcpConnect":  "io:tcp_connect",
		"tcpAccept":   "io:tcp_accept",
		"tcpPort":     "io:tcp_port",
		"tcpRead":     "io:tcp_read",
		"tcpWrite":    "io:tcp_write",
		"tcpClose":    "io:tcp_close",
		"sqliteQuery": "io:sqlite_query",
		"readFile":    "read_file", "writeFile": "write_file",
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
		if strings.HasPrefix(operation, "io:") {
			return "linglang_io:" + strings.TrimPrefix(operation, "io:") + "(" + strings.Join(v, ", ") + ")"
		}
		return "linglang_rt:" + operation + "(" + strings.Join(v, ", ") + ")"
	}), true, nil
}
