package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"linglang/internal/sourceformat"
)

type formattedFile struct {
	path string
	data []byte
	mode os.FileMode
}

func formatCommand(args []string) error {
	flags := flag.NewFlagSet("fmt", flag.ContinueOnError)
	check := flags.Bool("check", false, "check formatting without changing files")
	if err := flags.Parse(args); err != nil {
		return err
	}
	paths := flags.Args()
	if len(paths) == 0 {
		paths = []string{"."}
	}
	files := map[string]string{}
	for _, path := range paths {
		if err := findFormatFiles(path, files); err != nil {
			return err
		}
	}
	if len(files) == 0 {
		return fmt.Errorf("no .lang files found")
	}
	var names []string
	for path := range files {
		names = append(names, path)
	}
	sort.Strings(names)
	var changes []formattedFile
	// Parse every file before replacing any of them: a syntax error cannot leave
	// earlier files rewritten. Formatting does not execute or type-check code.
	for _, path := range names {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		formatted, err := formatSource(files[path], data)
		if err != nil {
			return fmt.Errorf("%s: %w", files[path], err)
		}
		if bytes.Equal(data, formatted) {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		changes = append(changes, formattedFile{path, formatted, info.Mode().Perm()})
	}
	for _, change := range changes {
		if !*check {
			if err := replaceFormatted(change); err != nil {
				return err
			}
		}
		fmt.Println(files[change.path])
	}
	if *check && len(changes) > 0 {
		return fmt.Errorf("%d source files need formatting", len(changes))
	}
	return nil
}

func formatSource(filename string, data []byte) ([]byte, error) {
	return sourceformat.Format(filename, data)
}

func findFormatFiles(path string, files map[string]string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				switch entry.Name() {
				case ".git", "_build", "_release", "bin", "dist":
					continue
				}
			} else if filepath.Ext(entry.Name()) != ".lang" || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			if err := findFormatFiles(filepath.Join(path, entry.Name()), files); err != nil {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s: formatting requires a regular file", path)
	}
	// Resolve explicit symlinks so atomic replacement updates the file and keeps
	// the user's symlink intact. Canonical paths also deduplicate overlapping inputs.
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return err
	}
	if _, exists := files[resolved]; !exists {
		files[resolved] = path
	}
	return nil
}

func replaceFormatted(file formattedFile) error {
	output, err := os.CreateTemp(filepath.Dir(file.path), ".linglang-fmt-*")
	if err != nil {
		return err
	}
	defer os.Remove(output.Name())
	defer output.Close()
	if _, err := output.Write(file.data); err != nil {
		return err
	}
	if err := output.Chmod(file.mode); err != nil {
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	return os.Rename(output.Name(), file.path)
}
