package compiler

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const tcpFixture = `
type Client struct{ port int; parent Pid }
func client(c Client){
 s:=tcpConnect("127.0.0.1",c.port,1000);assert(s.ok)
 assert(tcpWrite(s.value,"hello",1000).ok)
 r:=tcpRead(s.value,5,1000);assert(r.ok&&r.value=="world")
 assert(tcpClose(s.value));assert(!tcpClose(s.value));send(c.parent,true)
}
func main(){
 l:=tcpListen("127.0.0.1",0);assert(l.ok)
 port:=tcpPort(l.value);assert(port.ok&&port.value>0)
 assert(tcpAccept(l.value,0).reason=="timeout")
 spawn(client,Client{port:port.value,parent:self()})
 s:=tcpAccept(l.value,1000);assert(s.ok)
 r:=tcpRead(s.value,5,1000);assert(r.ok&&r.value=="hello")
 assert(tcpWrite(s.value,"world",1000).ok)
 assert(receive[bool](1000).value)
 assert(tcpRead(s.value,1,1000).reason=="closed")
 assert(tcpClose(s.value));assert(tcpClose(l.value))
 assert(!tcpListen("127.0.0.1",65536).ok)
 println("tcp ok")
}`

func TestTCP(t *testing.T) {
	for _, noOpt := range []bool{false, true} {
		out, err := executeScriptWithOptions(t, tcpFixture, `linglang_rt:set_gc_stress(true),linglang_program:main(),halt(0).`, Options{DisableOptimizations: noOpt})
		if err != nil || out != "tcp ok\n" {
			t.Fatalf("%s %v", out, err)
		}
	}
}
func TestSocketSendability(t *testing.T) {
	for _, code := range []string{`func main(){s:=tcpListen("127.0.0.1",0);send(self(),s.value)}`, `func worker(s Socket){};func main(){spawn(worker,nil)}`} {
		if _, err := Compile("bad.lang", []byte("package main\n"+code)); err == nil {
			t.Fatal("accepted socket across process boundary")
		}
	}
}

func TestSQLite(t *testing.T) {
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skip("SQLite adapter requires C compiler")
	}
	dir := t.TempDir()
	helper := filepath.Join(dir, "linglang-sqlite")
	out, err := exec.Command("cc", "-std=c11", "-O2", "-Wall", "-Wextra", "-Werror", "-pthread", "../../tools/sqlite/main.c", "-lsqlite3", "-o", helper).CombinedOutput()
	if err != nil {
		t.Fatalf("build SQLite helper: %s %v", out, err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, noOpt := range []bool{false, true} {
		source := fmt.Sprintf(`func main(){
 db:=%q
 assert(sqliteQuery(db,"CREATE TABLE items(value TEXT)",nil,1000).ok)
 input:="hello'); DROP TABLE items;--\x00雪"
 r:=sqliteQuery(db,"INSERT INTO items VALUES (?)",List[SQLValue]{SQLValue{value:input}},1000);assert(r.ok&&r.changes==1)
 r=sqliteQuery(db,"SELECT value,NULL,X'00ff',42,1.5 FROM items",nil,1000);assert(r.ok&&len(r.rows)==1)
 cells:=head(r.rows).value;assert(head(cells).value.value==input)
 cells=tail(cells);assert(head(cells).value.kind=="null")
 cells=tail(cells);assert(head(cells).value.kind=="blob"&&head(cells).value.value=="\x00\xff")
 cells=tail(cells);assert(head(cells).value.kind=="int"&&head(cells).value.value=="42")
 assert(!sqliteQuery(db,"INSERT INTO items VALUES ('bad'); DELETE FROM items",nil,1000).ok)
 assert(!sqliteQuery(db,"SELECT ?",nil,1000).ok)
 assert(sqliteQuery(db,"SELECT ?",List[SQLValue]{SQLValue{kind:"int",value:"42"}},1000).ok)
 assert(!sqliteQuery(db,"SELECT ?",List[SQLValue]{SQLValue{kind:"int",value:"no"}},1000).ok)
 r=sqliteQuery(db,"WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n) SELECT sum(x) FROM n",nil,50);assert(!r.ok&&r.reason=="timeout")
 r=sqliteQuery(db,"SELECT count(*) FROM items",nil,1000);assert(r.ok&&head(head(r.rows).value).value.value=="1")
 println("sqlite ok")
}`, filepath.Join(dir, fmt.Sprintf("db-%v.sqlite", noOpt)))
		out, err := executeScriptWithOptions(t, source, `linglang_program:main(),halt(0).`, Options{DisableOptimizations: noOpt})
		if err != nil || out != "sqlite ok\n" {
			t.Fatalf("%s %v", out, err)
		}
	}
}

func TestSQLiteHelperLifetime(t *testing.T) {
	dir := t.TempDir()
	helper := filepath.Join(dir, "linglang-sqlite")
	if out, err := exec.Command("cc", "-std=c11", "-O2", "-pthread", "../../tools/sqlite/main.c", "-lsqlite3", "-o", helper).CombinedOutput(); err != nil {
		t.Fatalf("helper: %v %s", err, out)
	}
	var request bytes.Buffer
	binary.Write(&request, binary.BigEndian, uint32(0)) // No helper alarm: EOF alone must terminate it.
	for _, text := range []string{":memory:", "WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n) SELECT sum(x) FROM n"} {
		binary.Write(&request, binary.BigEndian, uint32(len(text)))
		request.WriteString(text)
	}
	binary.Write(&request, binary.BigEndian, uint32(0))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, helper)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err = stdin.Write(request.Bytes()); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	stdin.Close()
	err = cmd.Wait()
	if ctx.Err() != nil {
		t.Fatal("helper survived parent EOF")
	}
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 130 {
		t.Fatalf("expected lifetime-guard exit, got %v", err)
	}
}

func TestSQLiteHelperExitsWithInputOpen(t *testing.T) {
	// Keep stdin open just like the BEAM port. Returning from main must exit
	// normally even while the lifetime guard is waiting for parent EOF.
	helper := filepath.Join(t.TempDir(), "linglang-sqlite")
	if out, err := exec.Command("cc", "-std=c11", "-O2", "-Wall", "-Wextra", "-Werror", "-pthread", "../../tools/sqlite/main.c", "-lsqlite3", "-o", helper).CombinedOutput(); err != nil {
		t.Fatalf("helper: %v %s", err, out)
	}
	for _, query := range []struct {
		sql    string
		status byte
	}{
		{"WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<100000) SELECT sum(x) FROM n", 1},
		{"SELECT missing FROM no_such_table", 0},
	} {
		var request bytes.Buffer
		binary.Write(&request, binary.BigEndian, uint32(0)) // No alarm may rescue a deadlocked exit.
		for _, text := range []string{":memory:", query.sql} {
			binary.Write(&request, binary.BigEndian, uint32(len(text)))
			request.WriteString(text)
		}
		binary.Write(&request, binary.BigEndian, uint32(0))
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		cmd := exec.CommandContext(ctx, helper)
		stdin, err := cmd.StdinPipe()
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		var output bytes.Buffer
		cmd.Stdout = &output
		if err = cmd.Start(); err != nil {
			stdin.Close()
			cancel()
			t.Fatal(err)
		}
		_, writeErr := stdin.Write(request.Bytes())
		err = cmd.Wait()
		stdin.Close()
		timedOut := ctx.Err() != nil
		cancel()
		if writeErr != nil || err != nil || timedOut || output.Len() == 0 || output.Bytes()[0] != query.status {
			t.Fatalf("helper failed with stdin open: write=%v exit=%v timeout=%v output=%q", writeErr, err, timedOut, output.Bytes())
		}
	}
}
