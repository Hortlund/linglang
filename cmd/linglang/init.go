package main

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed starter/*.lang starter/README.md starter/AGENTS.md starter/.gitignore
var starterFiles embed.FS

func initCommand(args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("init expects at most one directory")
	}
	path := "."
	if len(args) == 1 {
		path = args[0]
	}
	if path == "" || (len(path) > 0 && path[0] == '-') {
		return fmt.Errorf("init expects a directory (use ./ for names starting with a dash)")
	}
	info, err := os.Lstat(path)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("%s: init requires a directory, not a file or symlink", path)
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		if len(entries) != 0 {
			return fmt.Errorf("%s: init requires an empty directory", path)
		}
	} else if !os.IsNotExist(err) {
		return err
	} else if err := os.MkdirAll(path, 0755); err != nil {
		return err
	}
	entries, err := starterFiles.ReadDir("starter")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		data, err := starterFiles.ReadFile("starter/" + entry.Name())
		if err != nil {
			return err
		}
		// Exclusive creation also protects against a file appearing after the
		// empty-directory check. Never overwrite somebody's source or config.
		file, err := os.OpenFile(filepath.Join(path, entry.Name()), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			return err
		}
		_, writeErr := file.Write(data)
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	fmt.Printf("Created Linglang project in %s\nSee README.md in that directory for run, test, and packaging commands.\n", path)
	return nil
}
