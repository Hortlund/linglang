package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Used against both seed-built lowerings and the actual generation C archive.
func testBootstrapDeveloperTools(t *testing.T, cli string, env []string) {
	t.Helper()
	work := t.TempDir()
	invoke := func(dir string, args ...string) (string, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, cli, args...)
		cmd.Dir, cmd.Env = dir, env
		var out, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &stderr
		err := cmd.Run()
		if ctx.Err() != nil {
			t.Fatalf("developer tool hung: %s", stderr.String())
		}
		if err != nil {
			return out.String() + stderr.String(), err
		}
		return out.String(), err
	}
	project := filepath.Join(work, "nested", "bro's 雪 project")
	if out, err := invoke(work, "init", project); err != nil {
		t.Fatalf("init: %v %s", err, out)
	}
	entries, err := starterFiles.ReadDir("starter")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		expected, _ := starterFiles.ReadFile("starter/" + entry.Name())
		actual, err := os.ReadFile(filepath.Join(project, entry.Name()))
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatalf("embedded template drift: %s (%v)", entry.Name(), err)
		}
	}
	for _, args := range [][]string{{"check", "--tests", project}, {"run", project}, {"test", "--gc-stress", project}, {"fmt", project}, {"fmt", "--check", project}} {
		if out, err := invoke(work, args...); err != nil {
			t.Fatalf("%v: %v %s", args, err, out)
		}
	}
	before, _ := os.ReadFile(filepath.Join(project, "main.lang"))
	for _, args := range [][]string{{"init", project}, {"init", ""}, {"init", "--bad"}, {"init", filepath.Join(project, "main.lang")}, {"init", "one", "two"}} {
		if out, err := invoke(work, args...); err == nil {
			t.Fatalf("accepted %v: %s", args, out)
		}
	}
	after, _ := os.ReadFile(filepath.Join(project, "main.lang"))
	if !bytes.Equal(before, after) {
		t.Fatal("init changed source")
	}
	empty := filepath.Join(work, "empty")
	if err := os.Mkdir(empty, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(work, "directory-link")
	if err := os.Symlink(empty, alias); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{alias, alias + "/", alias + "///"} {
		if out, err := invoke(work, "init", path); err == nil {
			t.Fatalf("init followed symlink %q: %s", path, out)
		}
	}
	if out, err := invoke(empty, "init"); err != nil {
		t.Fatalf("default init: %v %s", err, out)
	}

	t.Run("format_safety", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "a 雪.lang")
		source := []byte("package main;func main(){x:=1;println(x)}")
		if err := os.WriteFile(path, source, 0750); err != nil {
			t.Fatal(err)
		}
		canonical, err := filepath.EvalSymlinks(path)
		if err != nil {
			t.Fatal(err)
		}
		bad := filepath.Join(dir, "z_test.lang")
		os.WriteFile(bad, []byte("package main\nfunc {"), 0600)
		if out, err := invoke(work, "fmt", dir); err == nil {
			t.Fatalf("accepted invalid syntax: %s", out)
		}
		actual, _ := os.ReadFile(path)
		if !bytes.Equal(actual, source) {
			t.Fatal("partial format on syntax failure")
		}
		os.Remove(bad)
		link := filepath.Join(dir, "alias.lang")
		if err := os.Symlink(path, link); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{".git", "_build", "_release", "bin", "dist"} {
			os.Mkdir(filepath.Join(dir, name), 0700)
			os.WriteFile(filepath.Join(dir, name, "bad.lang"), []byte("bad"), 0600)
		}
		// An encountered directory symlink, even a cycle, must not be traversed.
		os.Symlink(dir, filepath.Join(dir, "cycle"))
		if out, err := invoke(work, "fmt", "--check", dir, link); err == nil || strings.Count(out, canonical) != 1 {
			t.Fatalf("check/dedup: %v %s", err, out)
		}
		actual, _ = os.ReadFile(path)
		if !bytes.Equal(actual, source) {
			t.Fatal("--check wrote source")
		}
		if out, err := invoke(work, "fmt", dir, link); err != nil || strings.Count(out, canonical) != 1 {
			t.Fatalf("format/dedup: %v %s", err, out)
		}
		info, _ := os.Lstat(link)
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatal("destroyed explicit symlink")
		}
		info, _ = os.Stat(path)
		if info.Mode().Perm() != 0750 {
			t.Fatal("lost permissions")
		}
		actual, _ = os.ReadFile(path)
		want := "package main\n\nfunc main() {\n\tx := 1\n\tprintln(x)\n}\n"
		if string(actual) != want {
			t.Fatalf("layout: %q", actual)
		}
		if out, err := invoke(work, "fmt", "--check", dir); err != nil || out != "" {
			t.Fatalf("not idempotent: %v %s", err, out)
		}
		if out, err := invoke(work, "run", path); err != nil || strings.TrimSpace(out) != "1" {
			t.Fatalf("changed behavior: %v %s", err, out)
		}
	})
	t.Run("format_corpus", func(t *testing.T) {
		dir := t.TempDir()
		paths, err := filepath.Glob("../../bootstrap/emitter/*.lang")
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range paths {
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, filepath.Base(path)), source, 0600); err != nil {
				t.Fatal(err)
			}
		}
		if out, err := invoke(work, "fmt", dir); err != nil {
			t.Fatalf("compiler source corpus: %v %s", err, out)
		}
		if out, err := invoke(work, "fmt", "--check", dir); err != nil {
			t.Fatalf("compiler source not idempotent: %v %s", err, out)
		}
	})
}
