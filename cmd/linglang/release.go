package main

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"linglang/internal/compiler"
)

//go:embed release_builder.erl
var releaseBuilder string

//go:embed OTP_LICENSE.txt
var otpLicense string

// release uses OTP's release tooling to package exactly the applications needed
// by our runtime, including ERTS. The result targets the build host, not another OS.
func release(path, program string, gcStress, gcStats bool) error {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return fmt.Errorf("runtime releases currently support Linux and macOS; use pack on %s", runtime.GOOS)
	}
	if _, err := exec.LookPath("erl"); err != nil {
		return fmt.Errorf("Erlang/OTP is required to build a release: erl is not on PATH")
	}
	dir, err := os.MkdirTemp("", "linglang-release-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	appDir := filepath.Join(dir, "lib", "linglang-1", "ebin")
	files := compiler.RuntimeSources()
	files["linglang_program.erl"] = program
	files["linglang_cli.erl"] = "-module(linglang_cli).\n-export([start/0, main/1]).\nstart() -> main(init:get_plain_arguments()).\nmain(Args) -> linglang_rt:set_arguments(Args), " + evaluationScript(gcStress, gcStats) + "\n"
	if err := buildSources(appDir, files); err != nil {
		return err
	}
	app := "{application, linglang, [{description, \"linglang program\"}, {vsn, \"1\"}, {modules, [linglang_cli, linglang_program, linglang_rt, linglang_sup, linglang_server, linglang_io]}, {registered, []}, {applications, [kernel, stdlib]}]}.\n"
	if err := os.WriteFile(filepath.Join(appDir, "linglang.app"), []byte(app), 0644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "OTP_LICENSE.txt"), []byte(otpLicense), 0644); err != nil {
		return err
	}
	if err := buildSources(filepath.Join(dir, "builder"), map[string]string{"linglang_release_builder.erl": releaseBuilder}); err != nil {
		return err
	}
	cmd := exec.Command("erl", "-noshell", "-pa", filepath.Join(dir, "builder"), "-s", "linglang_release_builder", "build", "-extra", dir, runtime.GOOS, runtime.GOARCH)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("OTP release creation failed: %w\n%s", err, out)
	}
	return publishRelease(path, filepath.Join(dir, "linglang.tar.gz"))
}

// Copy beside the destination before renaming, so publishing is atomic even
// when the temporary build directory is on a different filesystem.
func publishRelease(path, archive string) error {
	input, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	output, err := os.CreateTemp(filepath.Dir(path), ".linglang-release-*")
	if err != nil {
		return err
	}
	defer os.Remove(output.Name())
	defer output.Close()
	if _, err := io.Copy(output, input); err != nil {
		return err
	}
	if err := output.Chmod(0644); err != nil {
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	return os.Rename(output.Name(), path)
}
