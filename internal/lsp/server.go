package lsp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
	"unicode/utf8"

	"linglang/internal/compiler"
	"linglang/internal/sourceformat"
)

type document struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
	Text    string `json:"text"`
}

type Server struct {
	documents    map[string]document
	dirty        map[string]bool
	published    map[string]map[string]bool
	dependencies map[string]map[string]bool
	writer       io.Writer
	started      bool
	shutdown     bool
}

func NewServer() *Server {
	return &Server{documents: map[string]document{}, dirty: map[string]bool{}, published: map[string]map[string]bool{}, dependencies: map[string]map[string]bool{}}
}

// Serve owns document state on one event loop. Full document updates are
// debounced and versioned; all package buffers are analyzed as one snapshot.
func (s *Server) Serve(input io.Reader, output io.Writer) error {
	s.writer = output
	type incoming struct {
		msg message
		err error
	}
	inbox := make(chan incoming)
	done := make(chan struct{})
	defer close(done)
	go func() {
		reader := bufio.NewReader(input)
		for {
			msg, err := readMessage(reader)
			select {
			case inbox <- incoming{msg, err}:
			case <-done:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	for {
		select {
		case next := <-inbox:
			if next.err != nil {
				if errors.Is(next.err, io.EOF) {
					return nil
				}
				return next.err
			}
			msg := next.msg
			if msg.Method == "exit" {
				if !s.shutdown {
					return fmt.Errorf("LSP exit before shutdown")
				}
				return nil
			}
			if len(msg.ID) != 0 {
				result, failure := s.request(msg)
				response := map[string]any{"jsonrpc": "2.0", "id": msg.ID}
				if failure != nil {
					response["error"] = failure
				} else {
					response["result"] = result
				}
				if err := writeMessage(s.writer, response); err != nil {
					return err
				}
			} else if s.started && !s.shutdown {
				if err := s.notify(msg); err != nil {
					return err
				}
			}
			if len(s.dirty) != 0 && !s.shutdown {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(150 * time.Millisecond)
			}
		case <-timer.C:
			if !s.shutdown {
				if err := s.publishDirty(); err != nil {
					return err
				}
			}
		}
	}
}

func (s *Server) request(msg message) (any, *rpcError) {
	if msg.Method == "initialize" && !s.started {
		s.started = true
		return map[string]any{"capabilities": map[string]any{
			"positionEncoding": "utf-16", "textDocumentSync": map[string]any{"openClose": true, "change": 1, "save": map[string]any{"includeText": false}},
			"documentFormattingProvider": true, "documentSymbolProvider": true,
		}, "serverInfo": map[string]string{"name": "linglang"}}, nil
	}
	if !s.started {
		return nil, &rpcError{-32002, "server not initialized"}
	}
	if s.shutdown {
		return nil, &rpcError{-32600, "server has shut down"}
	}
	if msg.Method == "shutdown" {
		s.shutdown = true
		return nil, nil
	}
	if msg.Method != "textDocument/formatting" && msg.Method != "textDocument/documentSymbol" {
		return nil, &rpcError{-32601, "method not supported"}
	}
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
	}
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return nil, &rpcError{-32602, "invalid document params"}
	}
	path, err := pathFromURI(params.TextDocument.URI)
	if err != nil {
		return nil, &rpcError{-32602, err.Error()}
	}
	doc, ok := s.documents[path]
	if !ok {
		return nil, &rpcError{-32602, "document is not open"}
	}
	if msg.Method == "textDocument/formatting" {
		formatted, err := sourceformat.Format(path, []byte(doc.Text))
		if err != nil {
			return nil, &rpcError{-32803, err.Error()}
		}
		edits := []textEdit{}
		if string(formatted) != doc.Text {
			edits = append(edits, textEdit{Range{Position{}, positionAt(doc.Text, len(doc.Text))}, string(formatted)})
		}
		return edits, nil
	}
	analysis := compiler.Analyze([]compiler.SourceFile{{Filename: path, Source: []byte(doc.Text)}})
	return outline(analysis.FileSet, analysis.Files[path], doc.Text), nil
}

func (s *Server) notify(msg message) error {
	var params struct {
		TextDocument   document `json:"textDocument"`
		ContentChanges []struct {
			Text  string `json:"text"`
			Range *Range `json:"range"`
		} `json:"contentChanges"`
		Changes []struct {
			URI string `json:"uri"`
		} `json:"changes"`
	}
	if msg.Method == "workspace/didChangeWatchedFiles" {
		if json.Unmarshal(msg.Params, &params) == nil {
			for _, change := range params.Changes {
				if path, err := pathFromURI(change.URI); err == nil {
					s.markDirty(path)
				}
			}
		}
		return nil
	}
	if msg.Method != "textDocument/didOpen" && msg.Method != "textDocument/didChange" && msg.Method != "textDocument/didClose" && msg.Method != "textDocument/didSave" {
		return nil
	}
	if json.Unmarshal(msg.Params, &params) != nil {
		return nil
	}
	path, err := pathFromURI(params.TextDocument.URI)
	if err != nil || filepath.Ext(path) != ".lang" {
		return nil
	}
	doc, open := s.documents[path]
	switch msg.Method {
	case "textDocument/didOpen":
		if open && params.TextDocument.Version <= doc.Version {
			return nil
		}
		s.documents[path] = params.TextDocument
	case "textDocument/didChange":
		if !open || params.TextDocument.Version <= doc.Version || len(params.ContentChanges) == 0 {
			return nil
		}
		for _, change := range params.ContentChanges {
			if change.Range != nil {
				return nil // The server advertises full-document synchronization.
			}
		}
		doc.Text = params.ContentChanges[len(params.ContentChanges)-1].Text
		doc.Version = params.TextDocument.Version
		s.documents[path] = doc
	case "textDocument/didClose":
		delete(s.documents, path)
		if err := s.publish(path, []diagnostic{}); err != nil {
			return err
		}
	case "textDocument/didSave":
		if !open {
			return nil
		}
	}
	s.markDirty(path)
	return nil
}

func physicalPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

func (s *Server) markDirty(path string) {
	s.dirty[filepath.Dir(path)] = true
	physical := physicalPath(path)
	for dir, paths := range s.dependencies {
		if paths[physical] {
			s.dirty[dir] = true
		}
	}
}

func (s *Server) publish(path string, diagnostics []diagnostic) error {
	params := map[string]any{"uri": uriFromPath(path), "diagnostics": diagnostics}
	if doc, ok := s.documents[path]; ok {
		params["uri"], params["version"] = doc.URI, doc.Version
	}
	return writeMessage(s.writer, map[string]any{"jsonrpc": "2.0", "method": "textDocument/publishDiagnostics", "params": params})
}

func (s *Server) packageSources(dir string) ([]compiler.SourceFile, error) {
	texts := map[string][]byte{}
	s.dependencies[dir] = map[string]bool{}
	// The bootstrap packages share sources through symlinks. An open buffer for
	// the real file or another alias must override its disk bytes in each package.
	var openPaths []string
	for path := range s.documents {
		openPaths = append(openPaths, path)
	}
	sort.Strings(openPaths)
	overlays := map[string]string{}
	for _, path := range openPaths {
		overlays[physicalPath(path)] = s.documents[path].Text
	}
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".lang" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		physical := physicalPath(path)
		s.dependencies[dir][physical] = true
		if doc, open := s.documents[path]; open {
			texts[path] = []byte(doc.Text)
			continue
		}
		if text, open := overlays[physical]; open {
			texts[path] = []byte(text)
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%s: source must be a regular file", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		texts[path] = data
	}
	for path, doc := range s.documents {
		if filepath.Dir(path) == dir {
			texts[path] = []byte(doc.Text)
		}
	}
	var sources []compiler.SourceFile
	for path, text := range texts {
		sources = append(sources, compiler.SourceFile{Filename: path, Source: text})
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Filename < sources[j].Filename })
	return sources, nil
}

const standaloneMarker = ".linglang-standalone"

// Directory packages are the default. An explicit marker opts a directory of
// independent programs into file analysis without hiding duplicate declarations
// in ordinary packages. The marker applies only to its immediate directory.
func (s *Server) analysisGroups(dir string) ([][]compiler.SourceFile, error) {
	info, err := os.Stat(filepath.Join(dir, standaloneMarker))
	if os.IsNotExist(err) {
		sources, err := s.packageSources(dir)
		return [][]compiler.SourceFile{sources}, err
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: standalone marker must be a regular file", filepath.Join(dir, standaloneMarker))
	}
	s.dependencies[dir] = map[string]bool{}
	var paths []string
	for path := range s.documents {
		if filepath.Dir(path) == dir {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	var groups [][]compiler.SourceFile
	for _, path := range paths {
		s.dependencies[dir][physicalPath(path)] = true
		groups = append(groups, []compiler.SourceFile{{Filename: path, Source: []byte(s.documents[path].Text)}})
	}
	return groups, nil
}

func (s *Server) publishDirty() error {
	var dirs []string
	for dir := range s.dirty {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	s.dirty = map[string]bool{}
	for _, dir := range dirs {
		active := false
		for path := range s.documents {
			active = active || filepath.Dir(path) == dir
		}
		byPath := map[string][]diagnostic{}
		if active {
			groups, err := s.analysisGroups(dir)
			if err != nil {
				for path := range s.documents {
					if filepath.Dir(path) == dir {
						byPath[path] = []diagnostic{{Range{}, 1, "linglang", err.Error()}}
					}
				}
			} else {
				for _, sources := range groups {
					texts := map[string]string{}
					for _, source := range sources {
						texts[source.Filename] = string(source.Source)
						byPath[source.Filename] = []diagnostic{}
					}
					analysis := compiler.Analyze(sources)
					for _, issue := range analysis.Diagnostics {
						text, ok := texts[issue.Filename]
						if !ok {
							continue
						}
						offset := issue.Offset
						if offset < 0 {
							offset = 0
						}
						if offset > len(text) {
							offset = len(text)
						}
						_, width := utf8.DecodeRuneInString(text[offset:])
						byPath[issue.Filename] = append(byPath[issue.Filename], diagnostic{Range{positionAt(text, offset), positionAt(text, offset+width)}, 1, "linglang", issue.Message})
					}
				}
			}
		}
		for path := range s.published[dir] {
			if _, found := byPath[path]; !found {
				byPath[path] = []diagnostic{}
			}
		}
		var paths []string
		for path := range byPath {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		s.published[dir] = map[string]bool{}
		for _, path := range paths {
			if err := s.publish(path, byPath[path]); err != nil {
				return err
			}
			if active {
				s.published[dir][path] = true
			}
		}
	}
	return nil
}
