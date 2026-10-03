// Package lsp implements linglang's editor server over the LSP stdio transport.
package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func readMessage(reader *bufio.Reader) (message, error) {
	length := -1
	headerBytes := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return message{}, err
		}
		headerBytes += len(line)
		if headerBytes > 8192 {
			return message{}, fmt.Errorf("LSP header exceeds 8 KiB")
		}
		if line == "\r\n" {
			break
		}
		name, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			return message{}, fmt.Errorf("invalid LSP header")
		}
		if strings.EqualFold(name, "Content-Length") {
			if length != -1 {
				return message{}, fmt.Errorf("duplicate Content-Length")
			}
			length, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil || length < 0 || length > 16<<20 {
				return message{}, fmt.Errorf("invalid Content-Length")
			}
		}
	}
	if length < 0 {
		return message{}, fmt.Errorf("missing Content-Length")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(reader, body); err != nil {
		return message{}, err
	}
	var msg message
	if err := json.Unmarshal(body, &msg); err != nil {
		return message{}, err
	}
	if msg.JSONRPC != "2.0" || msg.Method == "" {
		return message{}, fmt.Errorf("invalid JSON-RPC message")
	}
	return msg, nil
}

func writeMessage(writer io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "Content-Length: %d\r\n\r\n", len(data)); err != nil {
		return err
	}
	_, err = writer.Write(data)
	return err
}

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type textEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

type diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"`
	Source   string `json:"source"`
	Message  string `json:"message"`
}

type documentSymbol struct {
	Name           string           `json:"name"`
	Kind           int              `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []documentSymbol `json:"children,omitempty"`
}
