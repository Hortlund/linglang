package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Keep the seed tools off PATH while leaving OTP launchers at their installation
// paths. erl derives its runtime directory from $0 on relocatable installations;
// invoking a symlink from our temporary directory breaks that discovery.
func isolatedOTPEnvironment(t *testing.T, dir string, withCompiler bool) []string {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	names := []string{"erl", "escript", "dirname", "basename"}
	if withCompiler {
		names = append(names, "erlc")
	}
	for _, name := range names {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		path, err = filepath.EvalSymlinks(path)
		if err != nil {
			t.Fatal(err)
		}
		path, err = filepath.Abs(path)
		if err != nil {
			t.Fatal(err)
		}
		quoted := "'" + strings.ReplaceAll(path, "'", "'\"'\"'") + "'"
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nexec "+quoted+" \"$@\"\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	var env []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "PATH", "ERL_LIBS", "ERL_FLAGS", "ERL_AFLAGS", "ERL_ZFLAGS", "ERL_ROOTDIR", "ROOTDIR", "BINDIR", "EMU", "PROGNAME", "ESCRIPT_EMULATOR":
		default:
			env = append(env, entry)
		}
	}
	return append(env, "PATH="+dir)
}

func TestIsolatedOTPLaunchers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("OTP shell launchers require Unix")
	}
	// Simulate an OTP installation moved from its build location. The launcher
	// must see its real $0 and have both shell helpers, even with a tiny PATH.
	installation := filepath.Join(t.TempDir(), "雪 OTP's bin")
	if err := os.Mkdir(installation, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"erl", "escript", "erlc"} {
		script := "#!/bin/sh\nparent=$(dirname \"$0\")\nname=$(basename \"$0\")\ntest -f \"$parent/installed\" || exit 12\nprintf '%s:%s\\n' \"$name\" \"$1\"\n"
		if err := os.WriteFile(filepath.Join(installation, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(installation, "installed"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", installation+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ERL_ROOTDIR", "/nonexistent/build-time/otp")
	for _, withCompiler := range []bool{false, true} {
		dir := t.TempDir()
		env := isolatedOTPEnvironment(t, dir, withCompiler)
		for _, name := range []string{"erl", "escript", "erlc"} {
			if name == "erlc" && !withCompiler {
				if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
					t.Fatal("compiler present on runtime-only PATH")
				}
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			cmd := exec.CommandContext(ctx, filepath.Join(dir, name), "argument with spaces")
			cmd.Env = env
			out, err := cmd.CombinedOutput()
			cancel()
			if err != nil || string(out) != name+":argument with spaces\n" {
				t.Fatalf("relocated %s: %v\n%s", name, err, out)
			}
		}
		for _, entry := range env {
			if strings.HasPrefix(entry, "ERL_ROOTDIR=") {
				t.Fatal("inherited OTP root override")
			}
		}
		for _, name := range []string{"go", "linglang"} {
			if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
				t.Fatalf("seed tool %s present on isolated PATH", name)
			}
		}
	}
}
