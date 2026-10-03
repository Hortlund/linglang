package lsp

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSharedSourceOverlaysAndInvalidation(t *testing.T) {
	root := t.TempDir()
	first, second := filepath.Join(root, "first"), filepath.Join(root, "second")
	for _, dir := range []string{first, second} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	helper := filepath.Join(first, "helper.lang")
	if err := os.WriteFile(helper, []byte("package main\nfunc answer()int{return 42}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(helper, filepath.Join(second, "helper.lang")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	var output bytes.Buffer
	server := NewServer()
	server.writer = &output
	main := filepath.Join(second, "main.lang")
	if err := server.notify(notification("textDocument/didOpen", map[string]any{"textDocument": document{uriFromPath(main), 1, "package main\nfunc main(){println(answer()+1)}"}})); err != nil {
		t.Fatal(err)
	}
	if err := server.publishDirty(); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := server.notify(notification("textDocument/didOpen", map[string]any{"textDocument": document{uriFromPath(helper), 1, "package main\nfunc answer()string{return \"bean\"}"}})); err != nil {
		t.Fatal(err)
	}
	if !server.dirty[second] {
		t.Fatal("editing a shared file did not invalidate its other package")
	}
	if err := server.publishDirty(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "mismatched types") {
		t.Fatalf("shared buffer did not override symlinked disk source: %s", &output)
	}
}
