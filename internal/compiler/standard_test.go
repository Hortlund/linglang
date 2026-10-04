package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestTextOperations(t *testing.T) {
	tests := []struct{ name, source, want string }{
		{"split_and_trim", `func main() {
 for _, part := range split("a::b::::", "::") { println("[" + part + "]") }
 println(len(split("", ",")), head(split("", ",")).value == "")
 println(trim(" \t\r\n\v\fhello \t"), trim(" \t") == "")
 println(trim(" 雪 "), trim("\u00a0雪\u00a0"))
 for _, part := range split("雪/å/雪", "/") { println(part) }
}`, "[a]\n[b]\n[]\n[]\n1 true\nhello true\n雪 \u00a0雪\u00a0\n雪\nå\n雪\n"},
		{"signed_decimal_limits", `func main() {
 for _, text := range (List[string]{"0", "-0", "+42", "0007", "9223372036854775807", "-9223372036854775808"}) {
  parsed := parseInt(text)
  println(parsed.ok, formatInt(parsed.value), parsed.reason == "")
 }
 for _, text := range (List[string]{"9223372036854775808", "-9223372036854775809"}) {
  parsed := parseInt(text)
  println(parsed.ok, parsed.value, parsed.reason)
 }
}`, "true 0 true\ntrue 0 true\ntrue 42 true\ntrue 7 true\ntrue 9223372036854775807 true\ntrue -9223372036854775808 true\nfalse 0 integer out of range\nfalse 0 integer out of range\n"},
		{"invalid_integers", `func main() {
 for _, text := range (List[string]{"", "+", "-", " 1", "1 ", "1.0", "1_000", "0x10", "12tail", "１２", "1\x00"}) {
  parsed := parseInt(text)
  if parsed.ok || parsed.value != 0 || parsed.reason != "invalid integer" { panic("bad parse result") }
 }
 println(parseInt(trim(" \t-12\r\n")).value)
}`, "-12\n"},
		{"intrinsics_resolve_by_identity", `func main() {
 trim := 1; split := 2; parseInt := 3; formatInt := 4; args := 5; readFile := 6; writeFile := 7
 println(trim + split + parseInt + formatInt + args + readFile + writeFile)
}`, "28\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := execute(t, tc.source)
			if err != nil || got != tc.want {
				t.Fatalf("got %q (%v), want %q", got, err, tc.want)
			}
		})
	}
}

func TestEmptySplitSeparator(t *testing.T) {
	got, err := execute(t, `func main() { println(split("hello", "")) }`)
	if err == nil || !strings.Contains(got, "linglang_empty_separator") {
		t.Fatalf("got %q (%v), want explicit separator error", got, err)
	}
}

func TestFileOperations(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "雪 report.txt")
	source := fmt.Sprintf(`
func path(order *int) string { *order = *order * 10 + 1; return %s }
func contents(order *int) string { *order = *order * 10 + 2; return "snow: 雪\n\x00\xff" }
func main() {
 order := 0
 written := writeFile(path(&order), contents(&order))
 if !written.ok { panic(written.reason) }
 read := readFile(%s)
 println(order, written.reason == "", read.ok, read.value == "snow: 雪\n\x00\xff", read.reason == "")
 written = writeFile(%s, "short")
 println(written.ok, readFile(%s).value)
 missing := readFile(%s)
 println(missing.ok, missing.value == "", missing.reason)
 failure := writeFile(%s, "cannot create missing directories")
 println(failure.ok, failure.reason)
}`, strconv.Quote(path), strconv.Quote(path), strconv.Quote(path), strconv.Quote(path),
		strconv.Quote(filepath.Join(dir, "missing.txt")), strconv.Quote(filepath.Join(dir, "missing", "report.txt")))
	got, err := execute(t, source)
	want := "12 true true true true\ntrue short\nfalse true enoent\nfalse enoent\n"
	if err != nil || got != want {
		t.Fatalf("got %q (%v), want %q", got, err, want)
	}
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "short" {
		t.Fatalf("file contents %q (%v)", contents, err)
	}
}

func TestStandardResultsCrossProcessBoundary(t *testing.T) {
	got, err := execute(t, `func main() {
 send(self(), parseInt("42"))
 send(self(), readFile(""))
 send(self(), writeFile("", "text"))
 number := receive[IntResult](1000)
 text := receive[TextResult](1000)
 written := receive[IOResult](1000)
 println(number.ok, number.value.value, text.ok, !text.value.ok, written.ok, !written.value.ok)
}`)
	if err != nil || got != "true 42 true true true true\n" {
		t.Fatalf("got %q (%v)", got, err)
	}
}

func TestBootstrapBuildFailurePreservesOutput(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(dir, "existing executable")
	if err := os.WriteFile(output, []byte("previous executable"), 0755); err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf(`func main(){
 failed:=buildProgram("not Erlang",%s,nil,false,false)
 println(failed.ok,readFile(%s).value)
 wrong:=buildProgram("-module(wrong).\\n-export([main/0]).\\nmain()->ok.",%s,nil,false,false)
 println(wrong.ok,wrong.reason)
}`, strconv.Quote(output), strconv.Quote(output), strconv.Quote(output))
	// Feed actual newlines to the Erlang scanner rather than source escapes.
	source = strings.ReplaceAll(source, `\\n`, `\n`)
	got, err := execute(t, source)
	want := "false previous executable\nfalse expected module linglang_program, got wrong\n"
	if err != nil || got != want {
		t.Fatalf("build failure: %q %v, want %q", got, err, want)
	}
	contents, err := os.ReadFile(output)
	if err != nil || string(contents) != "previous executable" {
		t.Fatalf("existing executable changed: %q %v", contents, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary build files leaked: %v %v", entries, err)
	}
}
