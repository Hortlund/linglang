package compiler

import (
	"strings"
	"testing"
)

func TestByteTextPrimitives(t *testing.T) {
	got, err := execute(t, `
func text(order *int) string { *order = *order * 10 + 1; return "雪\x00\xff" }
func index(order *int) int { *order = *order * 10 + 2; return 3 }
func end(order *int) int { *order = *order * 10 + 3; return 5 }
func parts(order *int) List[string] { *order = *order * 10 + 1; return List[string]{"a", "b"} }
func separator(order *int) string { *order = *order * 10 + 2; return ":" }
func main() {
 order := 0
 assert(len(text(&order)) == 5 && order == 1)
 order = 0
 assert(byteAt(text(&order), index(&order)) == 0 && order == 12)
 order = 0
 assert(slice(text(&order), index(&order), end(&order)) == "\x00\xff" && order == 123)
 order = 0
 assert(join(parts(&order), separator(&order)) == "a:b" && order == 12)
 assert(slice("abc", 3, 3) == "" && slice("雪", 0, 1) == "\xe9")
 var parts List[string]
 assert(join(parts, "|") == "" && join(List[string]{}, "|") == "")
 assert(join(List[string]{"雪", "", "\x00\xff"}, "::") == "雪::::\x00\xff")
 r := runeAt("雪😀\xff", 0)
 assert(r.ok && r.value == 38634 && r.width == 3)
 r = runeAt("雪😀\xff", 3)
 assert(r.ok && r.value == 128512 && r.width == 4)
 r = runeAt("雪😀\xff", 7)
 assert(!r.ok && r.value == 65533 && r.width == 1)
 r = runeAt("", 0)
 assert(!r.ok && r.width == 0)
 assert(runeAt("\ufffd", 0).ok)
 for _, bad := range (List[string]{"\xc0\x80", "\xed\xa0\x80", "\xf4\x90\x80\x80", "\xe9\x9b", "\x80"}) { assert(!runeAt(bad, 0).ok) }
 assert(isLetter('雪') && isLetter('å') && isLetter('A') && !isLetter('٢'))
 assert(isDigit('٢') && isDigit('0') && !isDigit('²') && !isDigit('雪'))
 assert(!isLetter(-1) && !isDigit(1114112) && !isLetter(55296))
 send(self(), runeAt("雪", 0))
 assert(receive[RuneResult](1000).value.value == 38634)
 byteAt := 1; slice := 2; join := 3; runeAt := 4; isLetter := 5; isDigit := 6
 assert(byteAt + slice + join + runeAt + isLetter + isDigit == 21)
 println("text primitives ok")
}`)
	if err != nil || got != "text primitives ok\n" {
		t.Fatalf("got %q (%v)", got, err)
	}
}

func TestByteTextTypeErrors(t *testing.T) {
	for _, expression := range []string{`byteAt("a", true)`, `slice("a", 0, "1")`, `join(List[int]{1}, "")`, `runeAt(1, 0)`, `isLetter("雪")`} {
		if _, err := Compile("bad.lang", []byte("package main\nfunc main() { "+expression+" }")); err == nil {
			t.Fatalf("accepted %s", expression)
		}
	}
}

func TestByteTextBounds(t *testing.T) {
	for _, tc := range []struct{ expression, failure string }{
		{`byteAt("", 0)`, "linglang_byte_index_out_of_range"},
		{`byteAt("a", -1)`, "linglang_byte_index_out_of_range"},
		{`byteAt("a", 9223372036854775807)`, "linglang_byte_index_out_of_range"},
		{`slice("a", -1, 0)`, "linglang_string_slice_out_of_range"},
		{`slice("a", 1, 0)`, "linglang_string_slice_out_of_range"},
		{`slice("a", 0, 2)`, "linglang_string_slice_out_of_range"},
		{`runeAt("a", -1)`, "linglang_byte_index_out_of_range"},
		{`runeAt("a", 2)`, "linglang_byte_index_out_of_range"},
	} {
		got, err := execute(t, "func main() { println("+tc.expression+") }")
		if err == nil || !strings.Contains(got, tc.failure) {
			t.Fatalf("%s: got %q (%v)", tc.expression, got, err)
		}
	}
}
