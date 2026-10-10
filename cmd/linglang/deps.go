package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"linglang/internal/compiler"
	"os"
	"path/filepath"
	"reflect"
	"sort"
)

type dependencyFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type dependencyLock struct {
	Version int              `json:"version"`
	Files   []dependencyFile `json:"files"`
}

// The first package tool records a content-addressed local dependency closure.
// It does not fetch packages, execute scripts, or silently rewrite a check.
func depsCommand(args []string) error {
	return depsCommandOutput(args, os.Stdout, os.Stderr)
}

type dependencyReport struct {
	SchemaVersion int              `json:"schemaVersion"`
	Command       string           `json:"command"`
	Compiler      string           `json:"compiler"`
	Mode          string           `json:"mode"`
	OK            bool             `json:"ok"`
	LockFile      string           `json:"lockFile"`
	Files         []dependencyFile `json:"files"`
	Diagnostics   []compiler.Issue `json:"diagnostics"`
}

func depsCommandOutput(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("deps", flag.ContinueOnError)
	flags.SetOutput(stderr)
	check := flags.Bool("check", false, "verify checked-in dependency source hashes")
	jsonOutput := flags.Bool("json", false, "write a version-1 dependency report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return fmt.Errorf("deps expects one source file or directory")
	}
	path := "."
	if flags.NArg() == 1 {
		path = flags.Arg(0)
	}
	if path == "" {
		return fmt.Errorf("deps expects a nonempty source path")
	}
	report := dependencyReport{SchemaVersion: 1, Command: "deps", Compiler: "go-seed", Mode: "write", Files: []dependencyFile{}, Diagnostics: []compiler.Issue{}}
	if *check {
		report.Mode = "check"
	}
	code := compiler.CodeInput
	err := dependencyOperation(path, *check, &report, &code)
	report.OK = err == nil
	if err != nil {
		report.Diagnostics = append(report.Diagnostics, compiler.IssueForError(err, code))
	}
	if *jsonOutput {
		if writeErr := json.NewEncoder(stdout).Encode(report); writeErr != nil {
			return writeErr
		}
		if err != nil {
			return reportedError{err}
		}
		return nil
	}
	if err != nil {
		return err
	}
	if *check {
		_, err = fmt.Fprintf(stdout, "Verified %d dependency source files\n", len(report.Files))
	} else {
		_, err = fmt.Fprintf(stdout, "Locked %d dependency source files in %s\n", len(report.Files), report.LockFile)
	}
	return err
}

func dependencyOperation(path string, check bool, report *dependencyReport, code *string) error {
	sources, err := readSources(path)
	if err != nil {
		return err
	}
	*code = compiler.CodeModule
	inputs, err := compiler.InputFiles(sources)
	if err != nil {
		return err
	}
	*code = compiler.CodeInput
	root, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if info, err := os.Stat(root); err != nil {
		return err
	} else if !info.IsDir() {
		root = filepath.Dir(root)
	}
	own := map[string]bool{}
	for _, source := range sources {
		absolute, err := filepath.Abs(source.Filename)
		if err != nil {
			return err
		}
		own[absolute] = true
	}
	lock := dependencyLock{Version: 1, Files: []dependencyFile{}}
	for _, source := range inputs {
		absolute, err := filepath.Abs(source.Filename)
		if err != nil {
			return err
		}
		if own[absolute] {
			continue
		}
		relative, err := filepath.Rel(root, absolute)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(absolute)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(data)
		lock.Files = append(lock.Files, dependencyFile{filepath.ToSlash(relative), hex.EncodeToString(hash[:])})
	}
	sort.Slice(lock.Files, func(i, j int) bool { return lock.Files[i].Path < lock.Files[j].Path })
	target := filepath.Join(root, "linglang.lock")
	report.LockFile, report.Files = target, lock.Files
	if info, err := os.Lstat(target); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("%s: lock must be a regular file", target)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if check {
		data, err := os.ReadFile(target)
		if err != nil {
			return err
		}
		*code = compiler.CodeSyntax
		previous, err := parseDependencyLock(data)
		if err != nil {
			return fmt.Errorf("%s: %w", target, err)
		}
		if !reflect.DeepEqual(previous, lock) {
			*code = compiler.CodeDependency
			return fmt.Errorf("%s: dependencies changed; review sources and run linglang deps to update", target)
		}
		return nil
	}
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temp, err := os.CreateTemp(root, ".linglang-lock-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err = temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	if err = os.Rename(temp.Name(), target); err != nil {
		return err
	}
	return nil
}
