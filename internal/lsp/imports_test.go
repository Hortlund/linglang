package lsp

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportedDependencyInvalidation(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "main.lang")
	leaf := filepath.Join(dir, "shared", "value.lang")
	write := func(path, text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(leaf, "package shared\nfunc Value()int{return 42}")
	write(filepath.Join(dir, "lib", "lib.lang"), "package lib\nimport \"../shared\"\nfunc Answer()int{return shared.Value()}")
	source := "package main\nimport \"./lib\"\nfunc main(){var n int=lib.Answer();println(n)}"
	write(root, source)
	var output bytes.Buffer
	server := NewServer()
	server.writer = &output
	notify := func(method string, params any) {
		t.Helper()
		if err := server.notify(notification(method, params)); err != nil {
			t.Fatal(err)
		}
	}
	changed := func(path string) {
		notify("workspace/didChangeWatchedFiles", map[string]any{"changes": []map[string]any{{"uri": uriFromPath(path), "type": 2}}})
	}
	publish := func(want string) {
		t.Helper()
		output.Reset()
		if err := server.publishDirty(); err != nil {
			t.Fatal(err)
		}
		issues, published := publishedDiagnostics(t, &output)[uriFromPath(root)]
		if !published {
			t.Fatal("importer was not rechecked")
		}
		if want == "" && len(issues) != 0 {
			t.Fatalf("unexpected diagnostics: %+v", issues)
		}
		if want != "" && (len(issues) == 0 || !strings.Contains(issues[0].Message, want)) {
			t.Fatalf("missing %q: %+v", want, issues)
		}
	}
	notify("textDocument/didOpen", map[string]any{"textDocument": document{uriFromPath(root), 1, source}})
	notify("textDocument/didOpen", map[string]any{"textDocument": document{uriFromPath(leaf), 1, "package shared\nfunc Value()int{return 42}"}})
	publish("")
	// A transitive dependency's type error must reach the importing document.
	write(leaf, "package shared\nfunc Value()string{return \"wrong\"}")
	changed(leaf)
	publish("cannot use")
	write(leaf, "package shared\nfunc Value()int{return 42}")
	notify("textDocument/didSave", map[string]any{"textDocument": document{URI: uriFromPath(leaf)}})
	publish("")
	// Watch package directories too: this file was not in the previous closure.
	added := filepath.Join(dir, "shared", "added.lang")
	write(added, "package shared\nfunc Broken()int{return missing}")
	changed(added)
	publish("undefined name missing")
	if err := os.Remove(added); err != nil {
		t.Fatal(err)
	}
	changed(added)
	publish("")
	// Failed loads must keep their dependency so restoring a file clears errors.
	if err := os.Remove(leaf); err != nil {
		t.Fatal(err)
	}
	changed(leaf)
	publish("no .lang source files")
	write(leaf, "package shared\nfunc Value()int{return 42}")
	changed(leaf)
	publish("")
	// Removed imports must stop invalidating the document.
	notify("textDocument/didChange", map[string]any{"textDocument": document{URI: uriFromPath(root), Version: 2}, "contentChanges": []map[string]string{{"text": "package main\nfunc main(){}"}}})
	publish("")
	changed(leaf)
	if server.dirty[dir] {
		t.Fatal("removed import still invalidates root")
	}
}

func TestMissingImportCreationInvalidatesImporter(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "main.lang")
	var output bytes.Buffer
	server := NewServer()
	server.writer = &output
	server.documents[root] = document{uriFromPath(root), 1, "package main\nimport \"./later\"\nfunc main(){println(later.Value())}"}
	server.markDirty(root)
	if err := server.publishDirty(); err != nil {
		t.Fatal(err)
	}
	if issues := publishedDiagnostics(t, &output)[uriFromPath(root)]; len(issues) == 0 {
		t.Fatal("missing import accepted")
	}
	path := filepath.Join(dir, "later", "value.lang")
	if err := os.Mkdir(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package later\nfunc Value()int{return 42}"), 0600); err != nil {
		t.Fatal(err)
	}
	server.markDirty(path)
	if err := server.publishDirty(); err != nil {
		t.Fatal(err)
	}
	issues, published := publishedDiagnostics(t, &output)[uriFromPath(root)]
	if !published || len(issues) != 0 {
		t.Fatalf("missing package did not recover: %+v", issues)
	}
}
