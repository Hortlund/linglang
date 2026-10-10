package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"linglang/internal/compiler"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func sqliteTestEnvironment(t *testing.T, env []string) []string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("cc", "-std=c11", "-O2", "-Wall", "-Wextra", "-Werror", "-pthread", "../../tools/sqlite/main.c", "-lsqlite3", "-o", filepath.Join(dir, "linglang-sqlite"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("SQLite helper: %v\n%s", err, out)
	}
	env = append([]string(nil), env...)
	for i, value := range env {
		if strings.HasPrefix(value, "PATH=") {
			env[i] = "PATH=" + dir + string(os.PathListSeparator) + strings.TrimPrefix(value, "PATH=")
			return env
		}
	}
	return append(env, "PATH="+dir)
}

func testWebCounter(t *testing.T, artifact string, env []string) {
	t.Helper()
	database := filepath.Join(t.TempDir(), "counter.sqlite")
	for cycle := 0; cycle < 2; cycle++ {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		cmd := exec.CommandContext(ctx, artifact, "0", database)
		cmd.Env = env
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Start(); err != nil {
			cancel()
			t.Fatal(err)
		}
		stopped := false
		stop := func() {
			if !stopped {
				stopped = true
				cmd.Process.Kill()
				cmd.Wait()
				cancel()
			}
		}
		defer stop()
		ready := make(chan string, 1)
		go func() {
			scan := bufio.NewScanner(stdout)
			for scan.Scan() {
				line := scan.Text()
				if strings.HasPrefix(line, "Listening: ") {
					ready <- strings.TrimPrefix(line, "Listening: ")
					return
				}
			}
			ready <- ""
		}()
		var port string
		select {
		case port = <-ready:
		case <-ctx.Done():
			stop()
			t.Fatalf("server startup timeout: %s", stderr.String())
		}
		if port == "" {
			stop()
			t.Fatalf("server did not listen: %s", stderr.String())
		}
		client := &http.Client{Timeout: 5 * time.Second}
		request := func(method string, want int) {
			t.Helper()
			req, e := http.NewRequest(method, "http://127.0.0.1:"+port+"/", nil)
			if e != nil {
				t.Fatal(e)
			}
			resp, e := client.Do(req)
			if e != nil {
				t.Fatal(e)
			}
			body, e := io.ReadAll(resp.Body)
			resp.Body.Close()
			if e != nil || resp.StatusCode != 200 || !strings.Contains(string(body), "Count: "+strconv.Itoa(want)+"</p>") {
				t.Fatalf("%s: %d %s (%v)", method, resp.StatusCode, body, e)
			}
		}
		request("GET", cycle)
		if cycle == 0 {
			request("POST", 1)
			resp, e := client.Post("http://127.0.0.1:"+port+"/", "text/plain", strings.NewReader("body"))
			if e != nil {
				t.Fatal(e)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode != 400 {
				t.Fatalf("accepted unsupported request body: %d", resp.StatusCode)
			}
			request("GET", 1)
		}
		stop()
	}
}

func testBootstrapIO(t *testing.T, executable string, env []string) {
	t.Helper()
	env = sqliteTestEnvironment(t, env)
	dir := t.TempDir()
	source := fmt.Sprintf(`package main
func main(){
 l:=tcpListen("127.0.0.1",0);assert(l.ok&&tcpPort(l.value).value>0);assert(tcpAccept(l.value,0).reason=="timeout");assert(tcpClose(l.value))
 r:=sqliteQuery(%q,"SELECT ?, NULL, X'00ff'",List[SQLValue]{SQLValue{value:"a'\x00雪"}},1000)
 assert(r.ok);cells:=head(r.rows).value;assert(head(cells).value.value=="a'\x00雪");cells=tail(cells);assert(head(cells).value.kind=="null");cells=tail(cells);assert(head(cells).value.value=="\x00\xff")
 println("typed IO ok")
}`, filepath.Join(dir, "db.sqlite"))
	path := filepath.Join(dir, "io.lang")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	for _, noOpt := range []bool{false, true} {
		args := []string{"run", "--gc-stress"}
		if noOpt {
			args = append(args, "--no-opt")
		}
		args = append(args, path)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cmd := exec.CommandContext(ctx, executable, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil || !strings.Contains(string(out), "typed IO ok") {
			t.Fatalf("bootstrap I/O: %v\n%s", err, out)
		}
	}
	web, err := filepath.Abs("../../examples/webcounter")
	if err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(dir, "web.escript")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "build", "-o", artifact, web)
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build web app: %v\n%s", err, out)
	}
	testWebCounter(t, artifact, env)
}

func TestWebCounter(t *testing.T) {
	sources, err := readSources("../../examples/webcounter")
	if err != nil {
		t.Fatal(err)
	}
	program, err := compiler.CompileFiles(sources)
	if err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(t.TempDir(), "web.escript")
	if err = pack(artifact, program, false, false); err != nil {
		t.Fatal(err)
	}
	testWebCounter(t, artifact, sqliteTestEnvironment(t, os.Environ()))
}
