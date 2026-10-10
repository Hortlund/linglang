package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func publishedDiagnostics(t *testing.T, output *bytes.Buffer) map[string][]diagnostic {
	t.Helper()
	result := map[string][]diagnostic{}
	reader := bufio.NewReader(output)
	for reader.Buffered() != 0 || output.Len() != 0 {
		reply, err := readReply(reader)
		if err != nil {
			t.Fatal(err)
		}
		var params struct {
			URI         string       `json:"uri"`
			Diagnostics []diagnostic `json:"diagnostics"`
		}
		if err := json.Unmarshal(reply["params"], &params); err != nil {
			t.Fatal(err)
		}
		result[params.URI] = params.Diagnostics
	}
	return result
}

func TestStandaloneDirectoriesAndMarkerChanges(t *testing.T) {
	dir := t.TempDir()
	first, second := filepath.Join(dir, "first.lang"), filepath.Join(dir, "second.lang")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("package main\nfunc main(){}"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.lang"), []byte("not source"), 0600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, standaloneMarker)
	if err := os.WriteFile(marker, nil, 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	server := NewServer()
	server.writer = &output
	open := func(path, text string) {
		t.Helper()
		if err := server.notify(notification("textDocument/didOpen", map[string]any{"textDocument": document{uriFromPath(path), 1, text}})); err != nil {
			t.Fatal(err)
		}
	}
	open(first, "package main\nfunc bad()float64{return 1.5};func main(){}")
	open(second, "package main\nfunc main(){println(missing)}")
	publish := func() map[string][]diagnostic {
		t.Helper()
		output.Reset()
		if err := server.publishDirty(); err != nil {
			t.Fatal(err)
		}
		return publishedDiagnostics(t, &output)
	}
	issues := publish()
	if len(issues) != 2 || len(issues[uriFromPath(first)]) != 1 || !strings.Contains(issues[uriFromPath(first)][0].Message, "unsupported return type") || len(issues[uriFromPath(second)]) != 1 || !strings.Contains(issues[uriFromPath(second)][0].Message, "undefined: missing") {
		t.Fatalf("standalone files were merged or ignored: %+v", issues)
	}
	if err := server.notify(notification("textDocument/didClose", map[string]any{"textDocument": document{URI: uriFromPath(first)}})); err != nil {
		t.Fatal(err)
	}
	issues = publish()
	if len(issues[uriFromPath(first)]) != 0 {
		t.Fatalf("closed standalone still checked: %+v", issues)
	}
	// Deleting the marker resumes package checks; recreating it clears diagnostics
	// previously published for closed siblings. A change in this mode must not
	// silently hide real duplicate declarations in ordinary packages.
	if err := os.Remove(filepath.Join(dir, "broken.lang")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	watched := notification("workspace/didChangeWatchedFiles", map[string]any{"changes": []map[string]string{{"uri": uriFromPath(marker)}}})
	if err := server.notify(watched); err != nil {
		t.Fatal(err)
	}
	issues = publish()
	duplicate := false
	for _, issue := range issues[uriFromPath(second)] {
		duplicate = duplicate || strings.Contains(issue.Message, "main redeclared")
	}
	if !duplicate {
		t.Fatalf("package duplicates hidden: %+v", issues)
	}
	if err := os.WriteFile(marker, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := server.notify(watched); err != nil {
		t.Fatal(err)
	}
	issues = publish()
	if len(issues[uriFromPath(first)]) != 0 || len(issues[uriFromPath(second)]) != 1 || !strings.Contains(issues[uriFromPath(second)][0].Message, "undefined: missing") {
		t.Fatalf("marker did not restore independent analysis: %+v", issues)
	}
}

func TestRepositoryStandaloneProgramsAndNestedPackages(t *testing.T) {
	for _, path := range []string{"../../examples/worker_registration.lang", "../../examples/restart_limit.lang", "../../benchmarks/arithmetic.lang", "../../examples/prime_lab/main.lang", "../../bootstrap/resolver/main.lang"} {
		t.Run(path, func(t *testing.T) {
			absolute, err := filepath.Abs(path)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(absolute)
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			server := NewServer()
			server.writer = &output
			if err := server.notify(notification("textDocument/didOpen", map[string]any{"textDocument": document{uriFromPath(absolute), 1, string(data)}})); err != nil {
				t.Fatal(err)
			}
			if err := server.publishDirty(); err != nil {
				t.Fatal(err)
			}
			for uri, issues := range publishedDiagnostics(t, &output) {
				if len(issues) != 0 {
					t.Fatalf("%s: %+v", uri, issues)
				}
			}
		})
	}
}
