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

func TestInitProject(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "snow 雪 project")
	if err := run([]string{"init", dir}); err != nil {
		t.Fatal(err)
	}
	guide, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil || !strings.Contains(string(guide), "linglang check --json --tests .") {
		t.Fatalf("missing project workflow guide: %v", err)
	}
	sources, err := readSources(dir)
	if err != nil || len(sources) != 2 {
		t.Fatalf("starter sources: %v, %d", err, len(sources))
	}
	for _, source := range sources {
		if _, err := os.Stat(source.Filename); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := compiler.CompileFiles(sources); err != nil {
		t.Fatal(err)
	}
	// Exercise the generated app, suite and packaged executable, including the
	// runtime rather than merely comparing template text.
	if err := run([]string{"run", "--gc-stress", dir}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"test", "--gc-stress", dir}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"fmt", "--check", dir}); err != nil {
		t.Fatal(err)
	}
	packed := filepath.Join(dir, "counter.escript")
	if err := run([]string{"pack", "-o", packed, dir}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "escript", packed).CombinedOutput()
	if err != nil || string(output) != "Counter: 5\n" {
		t.Fatalf("starter executable: %s (%v)", output, err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "main.lang"))
	if err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"init", dir}); err == nil || !strings.Contains(err.Error(), "empty directory") {
		t.Fatalf("existing project accepted: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "main.lang"))
	if string(before) != string(after) {
		t.Fatal("init changed existing source")
	}
}

func TestInitRejectsExistingContent(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "existing")
	if err := os.WriteFile(file, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{file}, {root}, {""}, {"--unknown"}, {"one", "two"}} {
		if err := initCommand(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	empty := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(empty, link); err == nil {
		if err := initCommand([]string{link}); err == nil {
			t.Fatal("accepted a symlink")
		}
		entries, _ := os.ReadDir(empty)
		if len(entries) != 0 {
			t.Fatal("wrote through a symlink")
		}
	}
}
