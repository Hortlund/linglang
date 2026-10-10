package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"linglang/internal/compiler"
)

type bookExample struct{ name, source, want string }

func bookExamples(t *testing.T) []bookExample {
	t.Helper()
	examples := []bookExample{
		{name: "hello", want: "Hello from Linglang\n"},
		{name: "values", want: "7\n16\n"},
		{name: "multiple", want: "21 answer\n9 2\n"},
		{name: "collections", want: "2 3\n7\n42\n"},
	}
	for i := range examples {
		data, err := os.ReadFile(filepath.Join("../../book/examples", examples[i].name+".lang"))
		if err != nil {
			t.Fatal(err)
		}
		examples[i].source = strings.TrimPrefix(string(data), "package main")
	}
	var starter strings.Builder
	for _, name := range []string{"counter.lang", "main.lang"} {
		data, err := starterFiles.ReadFile("starter/" + name)
		if err != nil {
			t.Fatal(err)
		}
		starter.WriteString(strings.TrimPrefix(string(data), "package main"))
	}
	return append(examples, bookExample{"starter", starter.String(), "Counter: 5\n"})
}

func TestBookExamples(t *testing.T) {
	for _, example := range bookExamples(t) {
		t.Run(example.name, func(t *testing.T) {
			for _, baseline := range []bool{false, true} {
				module, err := compiler.CompileWithOptions(example.name+".lang", []byte("package main\n"+example.source), compiler.Options{DisableOptimizations: baseline})
				if err != nil {
					t.Fatal(err)
				}
				dir := t.TempDir()
				if err := build(dir, module); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				cmd := exec.CommandContext(ctx, "erl", "+S", "1", "-noshell", "-pa", dir, "-eval", evaluationScript(true, false))
				output, err := cmd.CombinedOutput()
				cancel()
				if err != nil || string(output) != example.want {
					t.Fatalf("baseline=%v: %s (%v), want %q", baseline, output, err, example.want)
				}
			}
		})
	}
}
