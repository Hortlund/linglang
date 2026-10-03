package main

import (
	"context"
	"fmt"
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"linglang/internal/compiler"
)

type scannedToken struct {
	offset, line, column int
	kind, text           string
}

// The seed frontend provides an independent compatibility oracle. Both emitted
// backends execute the actual Linglang lexer, including on its own source files.
func TestBootstrapLexerAgainstGoScanner(t *testing.T) {
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, dir := range []string{"examples", "benchmarks", "bootstrap"} {
		if err := filepath.WalkDir(filepath.Join(repo, dir), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() && filepath.Ext(path) == ".lang" {
				paths = append(paths, path)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	for i, data := range []string{
		"", "x", "x\n", "x /* a\nb */ y", "x // a\r\ny",
		"\ufeff雪٢ := 0x_FF\r\n雪٢++", "`a\r\nb` /* a\rb */ '\\U0001F600'",
		"0 0_7 0o_7 0B_1 0X_FF 12_345 99999999999999999999999",
		"<<= >>= &^= ... ++ -- := <- && || &^ <= != ( ) [ ] { } ; ~",
		"// control\x01\x7f\nreturn\n/* end */",
	} {
		path := filepath.Join(t.TempDir(), fmt.Sprintf("fixture-%d.lang", i))
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	sources, err := readSources(filepath.Join(repo, "bootstrap", "lexer"))
	if err != nil {
		t.Fatal(err)
	}
	for _, baseline := range []bool{false, true} {
		name := "optimized"
		if baseline {
			name = "baseline"
		}
		t.Run(name, func(t *testing.T) {
			program, err := compiler.CompileFilesWithOptions(sources, compiler.Options{DisableOptimizations: baseline})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			if err := build(dir, program); err != nil {
				t.Fatal(err)
			}
			for _, path := range paths {
				t.Run(filepath.Base(path), func(t *testing.T) {
					data, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					want := scanSeedTokens(t, data)
					ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
					defer cancel()
					cmd := exec.CommandContext(ctx, "erl", "-noshell", "-pa", dir, "-eval", evaluationScript(false, true), "-extra", path)
					out, err := cmd.CombinedOutput()
					if err != nil {
						t.Fatalf("lexer: %v\n%s", err, out)
					}
					if !strings.Contains(string(out), "live_cells => 0") || !strings.Contains(string(out), "root_frames => 0") {
						t.Fatalf("lexer left roots/cells: %s", out)
					}
					// Stats follow token output on stderr; isolate token lines so we
					// can compare exact raw text, kinds, offsets, and byte columns.
					lines := strings.Split(strings.SplitN(string(out), "linglang GC:", 2)[0], "\n")
					lines = lines[:len(lines)-1]
					if len(lines) != len(want) {
						t.Fatalf("%d tokens, want %d\n%s", len(lines), len(want), out)
					}
					for i, line := range lines {
						parts := strings.SplitN(line, " ", 5)
						if len(parts) != 5 {
							t.Fatalf("bad token output: %q", line)
						}
						offset, e1 := strconv.Atoi(parts[0])
						row, e2 := strconv.Atoi(parts[1])
						column, e3 := strconv.Atoi(parts[2])
						text, e4 := strconv.Unquote(parts[4])
						got := scannedToken{offset, row, column, parts[3], text}
						if e1 != nil || e2 != nil || e3 != nil || e4 != nil || got != want[i] {
							t.Fatalf("token %d: got %+v, want %+v (decode %v)", i, got, want[i], e4)
						}
					}
				})
			}
		})
	}
}

func scanSeedTokens(t *testing.T, data []byte) []scannedToken {
	t.Helper()
	fset := token.NewFileSet()
	file := fset.AddFile("source.lang", -1, len(data))
	var lexer scanner.Scanner
	lexer.Init(file, data, func(pos token.Position, message string) { t.Fatalf("oracle input: %v: %s", pos, message) }, scanner.ScanComments)
	var tokens []scannedToken
	for {
		pos, kind, text := lexer.Scan()
		p := file.PositionFor(pos, false) // Physical positions, independent of //line directives.
		if text == "" {
			text = kind.String()
		}
		if kind == token.EOF {
			text = ""
			// Linglang gives EOF after a terminal newline its natural next-line
			// position. go/token omits that last empty line from its line table.
			if len(data) > 0 && data[len(data)-1] == '\n' {
				p.Line = strings.Count(string(data), "\n") + 1
				p.Column = 1
			}
		}
		if kind == token.COMMENT {
			remaining := string(data[p.Offset:])
			end := strings.IndexByte(remaining, '\n')
			if strings.HasPrefix(remaining, "/*") {
				end = strings.Index(remaining, "*/") + 2
			}
			if end < 0 {
				end = len(remaining)
			}
			text = remaining[:end]
		}
		if kind == token.STRING && len(text) > 0 && text[0] == '`' {
			remaining := string(data[p.Offset+1:])
			text = string(data[p.Offset : p.Offset+strings.IndexByte(remaining, '`')+2])
		}
		tokens = append(tokens, scannedToken{p.Offset, p.Line, p.Column, kind.String(), text})
		if kind == token.EOF {
			return tokens
		}
	}
}
