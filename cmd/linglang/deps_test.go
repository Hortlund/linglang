package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestDependencyLockDecoder(t *testing.T) {
	for _, input := range []string{`{"version":1,"files":[]}`, `{"files":[{"sha256":"hash","path":"\ud83d\udca1"}],"version":1}`} {
		if _, err := parseDependencyLock([]byte(input)); err != nil {
			t.Fatal(input, err)
		}
	}
	for _, input := range []string{`null`, `{}`, `{"version":1,"files":null}`, `{"version":1.0,"files":[]}`, `{"Version":1,"files":[]}`, `{"version":1,"files":[],"files":[]}`, `{"version":1,"files":[]} null`, `{"version":1,"files":[{"path":"\ud800","sha256":"hash"}]}`, `{"version":1,"files":[{"path":"\udc00","sha256":"hash"}]}`, `{"version":1,"files":[{"path":"a","sha256":"x"},{"path":"a","sha256":"x"}]}`} {
		if _, err := parseDependencyLock([]byte(input)); err == nil {
			t.Fatal("accepted", input)
		}
	}
}

func TestDependencyReportOutputFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.lang"), []byte("package main\nfunc main(){}"), 0600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	err := depsCommandOutput([]string{"--json", dir}, brokenDependencyWriter{}, &stderr)
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
}

type brokenDependencyWriter struct{}

func (brokenDependencyWriter) Write(p []byte) (int, error) { return 0, io.ErrClosedPipe }

func TestDependencyLock(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "lib")
	if err := os.Mkdir(lib, 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(lib, "lib.lang")
	for path, code := range map[string]string{filepath.Join(root, "main.lang"): "package main\nimport \"./lib\"\nfunc main(){println(lib.Answer())}", source: "package lib\nfunc Answer()int{return 42}"} {
		if err := os.WriteFile(path, []byte(code), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := depsCommand([]string{root}); err != nil {
		t.Fatal(err)
	}
	if err := depsCommand([]string{"--check", root}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(root, "linglang.lock"))
	if err := depsCommand([]string{root}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(root, "linglang.lock"))
	if string(before) != string(after) {
		t.Fatal("nondeterministic lock")
	}
	if err := os.WriteFile(source, []byte("package lib\nfunc Answer()int{return 43}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := depsCommand([]string{"--check", root}); err == nil {
		t.Fatal("accepted changed dependency")
	}
	if err := depsCommand([]string{root}); err != nil {
		t.Fatal(err)
	}
	if err := depsCommand([]string{"--check", root}); err != nil {
		t.Fatal(err)
	}
}
