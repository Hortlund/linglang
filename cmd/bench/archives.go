package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type compilerArtifact struct {
	Label    string `json:"label"`
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	Snapshot string `json:"snapshot"`
}

type fileArtifact struct {
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	Snapshot string `json:"snapshot,omitempty"`
}

func artifact(path string, data []byte) fileArtifact {
	return fileArtifact{Path: path, SHA256: fmt.Sprintf("%x", sha256.Sum256(data))}
}

type compilerFlags []compilerArtifact

func (f *compilerFlags) String() string {
	var entries []string
	for _, c := range *f {
		entries = append(entries, c.Label+"="+c.Path)
	}
	return strings.Join(entries, ", ")
}

func (f *compilerFlags) Set(value string) error {
	label, path, ok := strings.Cut(value, "=")
	if !ok {
		return fmt.Errorf("compiler must be label=path")
	}
	next := append(append(compilerFlags(nil), (*f)...), compilerArtifact{Label: label, Path: path})
	if err := next.validate(); err != nil {
		return err
	}
	*f = next
	return nil
}

var archiveLabel = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

func (f compilerFlags) validate() error {
	seen := map[string]bool{}
	for _, c := range f {
		if !archiveLabel.MatchString(c.Label) || seen[c.Label] || c.Path == "" {
			return fmt.Errorf("compiler requires a unique label (1–32 lowercase letters/digits/underscores, starting with a letter) and a nonempty path: %q", c.Label)
		}
		seen[c.Label] = true
	}
	return nil
}

// Freeze exactly the bytes used for this run, including the actual archive, so
// subsequent rebuilds or benchmark invocations cannot replace its evidence.
func prepareApplicationInputs(cfg config, dir string) ([]compilerArtifact, []fileArtifact, error) {
	var archives []compilerArtifact
	var inputs []fileArtifact
	for _, c := range cfg.compilers {
		path, err := filepath.Abs(c.Path)
		if err != nil {
			return nil, nil, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, err
		}
		c.Path, c.SHA256 = path, artifact(path, data).SHA256
		c.Snapshot = filepath.Join(dir, c.Label+".escript")
		if err := os.WriteFile(c.Snapshot, data, 0755); err != nil {
			return nil, nil, err
		}
		out, err := archiveCommand(c.Snapshot, "describe", "--json")
		if err != nil {
			return nil, nil, err
		}
		var description struct {
			SchemaVersion int    `json:"schemaVersion"`
			Compiler      string `json:"compiler"`
		}
		if err := json.Unmarshal(out, &description); err != nil || description.SchemaVersion != 1 || description.Compiler != "bootstrap" {
			return nil, nil, fmt.Errorf("%s: expected bootstrap describe schema version 1", c.Label)
		}
		archives = append(archives, c)
	}
	names := []string{"bench_reference.erl"}
	for _, workload := range cfg.workloads {
		names = append(names, workload+".lang")
	}
	if cfg.compare {
		names = append(names, "bench_reference.ex", "go/main.go")
	}
	for _, name := range names {
		path, err := filepath.Abs(filepath.Join(cfg.sourceDir, name))
		if err != nil {
			return nil, nil, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, err
		}
		snapshot := filepath.Join(dir, "inputs", name)
		if err := os.MkdirAll(filepath.Dir(snapshot), 0755); err != nil {
			return nil, nil, err
		}
		if err := os.WriteFile(snapshot, data, 0644); err != nil {
			return nil, nil, err
		}
		input := artifact(path, data)
		input.Snapshot = snapshot
		inputs = append(inputs, input)
	}
	return archives, inputs, nil
}

func archiveCommand(archive string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "escript", append([]string{archive}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("archive %s %v: %w\n%s", archive, args, err, stderr.Bytes())
	}
	return out, nil
}

func emitArchive(archive, path string, cells bool) (string, error) {
	args := []string{"emit"}
	if cells {
		args = append(args, "--no-opt")
	}
	args = append(args, path)
	out, err := archiveCommand(archive, args...)
	if err != nil {
		return "", err
	}
	program := string(out)
	if strings.Count(program, "-module(linglang_program).") != 1 {
		return "", fmt.Errorf("archive emit did not return one linglang_program module")
	}
	// Repeat against the same immutable source path before measuring. Archive
	// hashes identify the executable, but do not themselves prove stable output.
	repeated, err := archiveCommand(archive, args...)
	if err != nil {
		return "", err
	}
	if !bytes.Equal(out, repeated) {
		return "", fmt.Errorf("archive emission is nondeterministic for %s", path)
	}
	return program, nil
}
