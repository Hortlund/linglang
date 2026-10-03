package lsp

import (
	"testing"

	"linglang/internal/compiler"
)

func TestOutlineDuringIncompleteEdits(t *testing.T) {
	text := "package main\ntype 雪 struct { value int; next *雪 }\nconst Answer=42\nfunc main(){println(Answer)}"
	before := func(a, b Position) bool { return a.Line < b.Line || a.Line == b.Line && a.Character <= b.Character }
	for end := 0; end <= len(text); end++ {
		prefix := text[:end]
		analysis := compiler.Analyze([]compiler.SourceFile{{Filename: "edit.lang", Source: []byte(prefix)}})
		var check func([]documentSymbol)
		check = func(symbols []documentSymbol) {
			for _, symbol := range symbols {
				if !before(symbol.Range.Start, symbol.SelectionRange.Start) ||
					!before(symbol.SelectionRange.Start, symbol.SelectionRange.End) ||
					!before(symbol.SelectionRange.End, symbol.Range.End) ||
					!before(symbol.Range.End, positionAt(prefix, len(prefix))) {
					t.Fatalf("prefix %d: invalid symbol ranges: %+v", end, symbol)
				}
				check(symbol.Children)
			}
		}
		check(outline(analysis.FileSet, analysis.Files["edit.lang"], prefix))
	}
}
