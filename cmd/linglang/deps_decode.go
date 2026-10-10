package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"unicode/utf8"
)

// Validate exact fields and duplicate keys before decoding typed records. The
// same version-1 contract is enforced by the Linglang JSON reader and verifier.
func parseDependencyLock(data []byte) (dependencyLock, error) {
	var lock dependencyLock
	if !utf8.Valid(data) {
		return lock, fmt.Errorf("invalid UTF-8")
	}
	if err := dependencyJSONUnicode(data); err != nil {
		return lock, err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	v, err := dependencyJSONValue(d, 0)
	if err != nil {
		return lock, err
	}
	if _, err := d.Token(); err != io.EOF {
		return lock, fmt.Errorf("trailing lock data")
	}
	obj, ok := v.(map[string]any)
	if !ok || len(obj) != 2 || obj["version"] != json.Number("1") {
		return lock, fmt.Errorf("lock requires integer version 1 and files")
	}
	entries, ok := obj["files"].([]any)
	if !ok {
		return lock, fmt.Errorf("lock requires a files array")
	}
	lock = dependencyLock{Version: 1, Files: []dependencyFile{}}
	seen := map[string]bool{}
	for _, entry := range entries {
		object, ok := entry.(map[string]any)
		if !ok || len(object) != 2 {
			return lock, fmt.Errorf("lock entries require path and sha256")
		}
		path, pathOK := object["path"].(string)
		hash, hashOK := object["sha256"].(string)
		if !pathOK || !hashOK || seen[path] {
			return lock, fmt.Errorf("invalid or duplicate lock entry")
		}
		seen[path] = true
		lock.Files = append(lock.Files, dependencyFile{path, hash})
	}
	return lock, nil
}

// encoding/json replaces unpaired surrogates; reject them like the Linglang
// reader instead of silently changing a lock path while decoding it.
func dependencyJSONUnicode(data []byte) error {
	inString := false
	for i := 0; i < len(data); i++ {
		if data[i] == '"' {
			inString = !inString
			continue
		}
		if !inString || data[i] != '\\' {
			continue
		}
		i++
		if i >= len(data) || data[i] != 'u' {
			continue
		}
		if i+4 >= len(data) {
			return fmt.Errorf("invalid Unicode escape")
		}
		v, err := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
		if err != nil {
			return fmt.Errorf("invalid Unicode escape")
		}
		i += 4
		if v >= 0xdc00 && v <= 0xdfff {
			return fmt.Errorf("unpaired low surrogate")
		}
		if v < 0xd800 || v > 0xdbff {
			continue
		}
		if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
			return fmt.Errorf("missing low surrogate")
		}
		low, err := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
		if err != nil || low < 0xdc00 || low > 0xdfff {
			return fmt.Errorf("invalid low surrogate")
		}
		i += 6
	}
	return nil
}

func dependencyJSONValue(d *json.Decoder, depth int) (any, error) {
	if depth > 64 {
		return nil, fmt.Errorf("JSON nesting exceeds 64")
	}
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch t {
	case json.Delim('{'):
		object := map[string]any{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return nil, err
			}
			name, ok := key.(string)
			if !ok {
				return nil, fmt.Errorf("expected object key")
			}
			if _, exists := object[name]; exists {
				return nil, fmt.Errorf("duplicate object key")
			}
			value, err := dependencyJSONValue(d, depth+1)
			if err != nil {
				return nil, err
			}
			object[name] = value
		}
		_, err := d.Token()
		return object, err
	case json.Delim('['):
		array := []any{}
		for d.More() {
			value, err := dependencyJSONValue(d, depth+1)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		_, err := d.Token()
		return array, err
	default:
		if _, delim := t.(json.Delim); delim {
			return nil, fmt.Errorf("unexpected delimiter")
		}
		return t, nil
	}
}
