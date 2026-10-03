package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func readReply(reader *bufio.Reader) (map[string]json.RawMessage, error) {
	header, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	length, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(header, "Content-Length:")))
	if err != nil {
		return nil, err
	}
	if line, err := reader.ReadString('\n'); err != nil || line != "\r\n" {
		return nil, fmt.Errorf("bad framing: %q %v", line, err)
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(reader, data); err != nil {
		return nil, err
	}
	var reply map[string]json.RawMessage
	err = json.Unmarshal(data, &reply)
	return reply, err
}

func notification(method string, params any) message {
	data, _ := json.Marshal(params)
	return message{JSONRPC: "2.0", Method: method, Params: data}
}

func TestStdioLifecycleAndFormatting(t *testing.T) {
	input, send := io.Pipe()
	output, receive := io.Pipe()
	t.Cleanup(func() { input.Close(); send.Close(); output.Close(); receive.Close() })
	server := NewServer()
	done := make(chan error, 1)
	go func() { done <- server.Serve(input, receive) }()
	replies := make(chan map[string]json.RawMessage, 8)
	go func() {
		reader := bufio.NewReader(output)
		for {
			reply, err := readReply(reader)
			if err != nil {
				return
			}
			replies <- reply
		}
	}()
	write := func(method string, id int, params any) {
		t.Helper()
		msg := notification(method, params)
		if id != 0 {
			msg.ID = json.RawMessage(strconv.Itoa(id))
		}
		if err := writeMessage(send, msg); err != nil {
			t.Fatal(err)
		}
	}
	read := func(id int) map[string]json.RawMessage {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case reply := <-replies:
				if string(reply["id"]) == strconv.Itoa(id) {
					return reply
				}
			case <-deadline:
				t.Fatal("LSP reply timed out")
				return nil
			}
		}
	}
	write("initialize", 1, map[string]any{})
	if reply := read(1); !bytes.Contains(reply["result"], []byte(`"positionEncoding":"utf-16"`)) {
		t.Fatalf("initialize: %s", reply)
	}
	write("initialized", 0, map[string]any{})
	path := filepath.Join(t.TempDir(), "雪.lang")
	uri := uriFromPath(path)
	text := "package main\r\nfunc main(){println(\"😀\")}\r\n"
	write("textDocument/didOpen", 0, map[string]any{"textDocument": document{uri, 1, text}})
	write("textDocument/formatting", 2, map[string]any{"textDocument": map[string]string{"uri": uri}})
	var edits []textEdit
	if err := json.Unmarshal(read(2)["result"], &edits); err != nil || len(edits) != 1 || edits[0].Range.End != (Position{2, 0}) {
		t.Fatalf("format edits: %+v, %v", edits, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("formatting wrote an unsaved file")
	}
	write("textDocument/documentSymbol", 3, map[string]any{"textDocument": map[string]string{"uri": uri}})
	var symbols []documentSymbol
	if err := json.Unmarshal(read(3)["result"], &symbols); err != nil || len(symbols) != 1 || symbols[0].Name != "main" {
		t.Fatalf("outline: %+v %v", symbols, err)
	}
	write("shutdown", 4, nil)
	if reply := read(4); string(reply["result"]) != "null" {
		t.Fatalf("shutdown: %s", reply)
	}
	write("exit", 0, nil)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown timed out")
	}
}

func TestPackageDiagnosticsOverlaysAndVersions(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.lang")
	helper := filepath.Join(dir, "helper.lang")
	if err := os.WriteFile(helper, []byte("package main\nfunc answer()int{return 42}"), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	server := NewServer()
	server.writer = &output
	open := func(path, text string, version int) {
		t.Helper()
		if err := server.notify(notification("textDocument/didOpen", map[string]any{"textDocument": document{uriFromPath(path), version, text}})); err != nil {
			t.Fatal(err)
		}
	}
	open(main, "package main\r\nfunc main(){println(\"雪😀\", answer(), missing)}", 1)
	open(helper, "package main\nfunc answer()int{return 42}", 1)
	if err := server.publishDirty(); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(&output)
	var params struct {
		URI         string       `json:"uri"`
		Version     int          `json:"version"`
		Diagnostics []diagnostic `json:"diagnostics"`
	}
	for i := 0; i < 2; i++ {
		reply, err := readReply(reader)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(reply["params"], &params); err != nil {
			t.Fatal(err)
		}
		if params.URI == uriFromPath(main) {
			if len(params.Diagnostics) != 1 || params.Diagnostics[0].Range.Start != (Position{1, 37}) || params.Version != 1 {
				t.Fatalf("UTF-16 diagnostics: %+v", params)
			}
		}
	}
	change := func(path, text string, version int) {
		t.Helper()
		err := server.notify(notification("textDocument/didChange", map[string]any{"textDocument": document{URI: uriFromPath(path), Version: version}, "contentChanges": []map[string]string{{"text": text}}}))
		if err != nil {
			t.Fatal(err)
		}
	}
	change(main, "package main\nfunc main(){n:=answer();println(n+1)}", 2)
	change(helper, "package main\nfunc answer()string{return \"bean\"}", 2)
	change(helper, "package main\nfunc answer()int{return 42}", 1) // stale
	if !strings.Contains(server.documents[helper].Text, "string") {
		t.Fatal("stale update replaced the latest buffer")
	}
	output.Reset()
	if err := server.publishDirty(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "mismatched types") || !strings.Contains(output.String(), `"version":2`) {
		t.Fatalf("helper overlay did not recheck its sibling: %s", &output)
	}
	if err := server.notify(notification("textDocument/didClose", map[string]any{"textDocument": document{URI: uriFromPath(helper)}})); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := server.publishDirty(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "mismatched types") {
		t.Fatalf("close did not restore disk source: %s", &output)
	}
}

func TestPositionsAndFraming(t *testing.T) {
	for _, tc := range []struct {
		text   string
		offset int
		want   Position
	}{
		{"雪😀x", len("雪😀"), Position{0, 3}},
		{"雪😀x\r\ny", len("雪😀x\r\n"), Position{1, 0}},
		{"x\r\n", 2, Position{0, 1}},
	} {
		if got := positionAt(tc.text, tc.offset); got != tc.want {
			t.Fatalf("%q at %d: %+v, want %+v", tc.text, tc.offset, got, tc.want)
		}
	}
	for _, input := range []string{"Content-Length: -1\r\n\r\n", "Content-Length: 16777217\r\n\r\n", "Content-Length: 2\r\nContent-Length: 2\r\n\r\n{}", "Content-Type: application/json\r\n\r\n", "Content-Length: 2\r\n\r\n{"} {
		if _, err := readMessage(bufio.NewReader(strings.NewReader(input))); err == nil {
			t.Fatalf("accepted bad frame %q", input)
		}
	}
}
