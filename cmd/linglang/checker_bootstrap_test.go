package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"linglang/internal/compiler"
)

func TestBootstrapCheckerAgainstSeed(t *testing.T) {
	type fixture struct {
		name    string
		sources []compiler.SourceFile
		valid   bool
	}
	var fixtures []fixture
	for _, name := range []string{"examples/beans.lang", "examples/prime_lab", "bootstrap/lexer", "bootstrap/parser", "bootstrap/resolver", "bootstrap/checker"} {
		path, err := filepath.Abs(filepath.Join("../..", name))
		if err != nil {
			t.Fatal(err)
		}
		sources, err := readPackageSources(path, true)
		if err != nil {
			t.Fatal(err)
		}
		fixtures = append(fixtures, fixture{name: name, sources: sources, valid: true})
	}
	for i, code := range []string{
		"const A=B;const B=1;func f()int{return A}",
		"func f(){println(prepend(1,nil),prepend(true,nil),put(nil,1,2),put(nil,\"bean\",false))}",
		"func f(){println(get[int](Map[int,string]{1:\"bean\"},1).value,remove[int](Map[int,string]{},1),put[int](nil,1,\"bean\"))}",
		"func f(){println((get[int,string])(Map[int,string]{},1).ok,(put[string,int])(nil,\"bean\",1))}",
		"func f(){println(((get[int,string]))(Map[int,string]{},1).ok)}",
		"func f(){println((put[int,string])(nil,1,\"bean\"),(remove[int,string])(Map[int,string]{},1))}",
		"func f(){println(get[int,bool](nil,1).ok,prepend[*int](nil,nil),put[int,List[int]](nil,1,nil))}",
		"func f(){head[int](List[int]{});(get[int,string])(Map[int,string]{},1)}",
		"type N struct{x int};func f(){p:=&((N{x:1}));q:=&(List[int]{1});println(p.x,len(*q))}",
		"func f(){println(make(List[int],2),make(List[int],2,3),make(Map[int,int]),make(Map[int,int],2),new(int))}",
		"func f(){var xs List[int];switch xs{case nil:println(1)};var m Map[int,int];switch m{case nil:println(1)}}",
		"func len(x int)int{return x};func f(){(len)(1)}",
		"func main(){};func init(){}",
		"func f(){println(head[int](nil).ok, get[string,int](nil,\"bean\").ok)}",
		"const (A=iota;B;C=A+1);func f(){println(A,B,C)}",
		"const (A,B=B,1);func f(){println(A,B)}",
		"type N struct{next *N;values List[N]};func f(){println(N{},List[*N]{{}})}",
		"func println(int){};func f(){println(1)}",
		"func f(n int){println(" + strings.Repeat("n+", 600) + "n)}",
		"func worker(n int){};func f(){var Pid int;println(Pid,monitor(spawn(worker,1)));var p *int;p=nil;println(p==nil)}",
		"func f(){type A struct{x int};a:=A{};{type A struct{x bool};b:=A{};println(b)};println(a)}",
		"func f(){var xs List[int];xs=append(xs,1,2);xs=append(xs,List[int]{3}...);println((head[int])(xs).ok);var m Map[string,int];m=put(m,\"bean\",1);println(get(m,\"bean\").value)}",
	} {
		name := fmt.Sprintf("valid-%d", i)
		path := filepath.Join(t.TempDir(), name+".lang")
		source := []byte("package main\n" + code)
		if err := os.WriteFile(path, source, 0600); err != nil {
			t.Fatal(err)
		}
		fixtures = append(fixtures, fixture{name: name, sources: []compiler.SourceFile{{Filename: path, Source: source}}, valid: true})
	}
	for i, code := range []string{
		"func f(){var x int=true;println(x)}",
		"func f(){_ += 1}",
		"func f(){m:=Map[int,int]{};switch m{case Map[int,int]{}:println(1)}}",
		"func f(){xs:=List[int]{};switch xs{case List[int]{}:println(1)}}",
		"func f(){println((get[int])(Map[int,string]{},1))}",
		"func f(){println((put[string])(nil,\"bean\",1))}",
		"func f(){println((get)[int](Map[int,string]{},1))}",
		"func f(){println((put)[int](nil,1,\"bean\"))}",
		"func f(){println((remove)[int](Map[int,string]{},1))}",
		"func f(){println((get)[int,string](Map[int,string]{},1))}",
		"func f(){println((put)[int,string](nil,1,\"bean\"))}",
		"func f(){println((remove)[int,string](Map[int,string]{},1))}",
		"func f(){println(((get))[int](Map[int,string]{},1))}",
		"func f(){println((head)[int](List[int]{}))}",
		"func f(){println((receive)[int](0))}",
		"func f(){_ /= 2}",
		"func f(){_ <<= 2}",
		"func f(){_ += true}",
		"func f(){(int)(1)}",
		"func f(){((len))(\"bean\")}",
		"func f(){(append)(List[int]{},1)}",
		"func f(){(*int)(nil)}",
		"func f(){(new)(int)}",
		"func f(){(make)(Map[int,int])}",
		"func f(){println(make(List[int]))}",
		"func f(){println(make(List[int],true))}",
		"func f(){println(make(List[int],2,false))}",
		"func f(){println(make(List[int],1,2,3))}",
		"func f(){println(make(Map[int,int],false))}",
		"func f(){println(make(Map[int,int],1,2))}",
		"func f(){println(new(int,1))}",
		"type N struct{xs List[int]};func f(){n:=N{};switch n{default:println(1)}}",
		"type N struct{m Map[int,int]};func f(){n:=N{};switch n{case N{}:println(1)}}",
		"func main(x int){println(x)}",
		"func main()int{return 1}",
		"func init(x int){println(x)}",
		"func init()bool{return true}",
		"func f(){println(prepend(nil,nil))}",
		"func f(){println(get[int](nil,1))}",
		"func f(){println(put[bool](nil,true,1))}",
		"func f(){println(get[string](Map[int,int]{},1))}",
		"func f(){println(put[int,string](Map[int,int]{},1,2))}",
		"func f(){x:=1;x=false}",
		"func f()int{return true}",
		"func f(){return 1}",
		"func f(x int){};func g(){f(true)}",
		"func f(x int){};func g(){f()}",
		"func f(){if 1{println(1)}}",
		"func f(){x:=true;x++}",
		"func f(){const x=1;x=2}",
		"func f(){_ = 1+true}",
		"func f(){x:=assert(true);println(x)}",
		"type N struct{x int};func f(){_ = N{x:true}}",
		"type N struct{x int};func f(){_ = N{missing:1}}",
		"type N struct{x int};func f(){_ = N{x:1,x:2}}",
		"type N struct{x int};func f(){println(N{}.missing)}",
		"type N struct{x int};func f(){N{}.x=2}",
		"type A struct{x int};type B struct{x int};func f(){a:=A{};a=B{}}",
		"type A struct{x A}",
		"type A struct{x B};type B struct{x A}",
		"type A struct{x Delivery[A]}",
		"type A struct{x List[int]};func f(){println(A{}==A{})}",
		"func f(){_ = Map[bool,int]{}}",
		"func f(){_ = Map[string,int]{1:2}}",
		"func f(){_ = Map[string,int]{missing:2}}",
		"func f(){_ = List[int]{true}}",
		"func f(){_ = get(Map[int,int]{},\"x\")}",
		"func f(){_ = prepend(true,List[int]{})}",
		"func f(){_ = head[string](List[int]{})}",
		"func f(){_ = put(Map[int,int]{},1,true)}",
		"func f(){var p *int;p=true}",
		"func f(){x:=1;_ = *x}",
		"func f(){for _,v:=range 1{println(v)}}",
		"func worker(n int){};func f(){spawn(worker,true)}",
		"func worker(n int)int{return n};func f(){spawn(worker,1)}",
		"func f(){send(1,2)}",
		"func f(){receive[int](true)}",
		"func f(){wait(self(),0)}",
		"func f(){_ = iota}",
		"const A=B;const B=A",
		"func f(){x:=1;const A=x;println(A)}",
		"const A=readFile(\"bean\")",
		"const _ = 1+true",
		"const int=1;func f(x int){}",
		"func int(){};func f(x int){}",
		"func f(){println(nil)}",
	} {
		name := fmt.Sprintf("invalid-%d", i)
		path := filepath.Join(t.TempDir(), name+".lang")
		source := []byte("package main\n" + code)
		if err := os.WriteFile(path, source, 0600); err != nil {
			t.Fatal(err)
		}
		fixtures = append(fixtures, fixture{name: name, sources: []compiler.SourceFile{{Filename: path, Source: source}}})
	}
	// Exercise forward declarations across physical files in deliberately reversed order.
	dir := t.TempDir()
	var files []compiler.SourceFile
	for i, code := range []string{"package main\nconst Answer=Later;func main(){println(answer(),N{})}", "package main\nconst Later=42;type N struct{next *N};func answer()int{return Answer}"} {
		path := filepath.Join(dir, fmt.Sprintf("part-%d.lang", i))
		if err := os.WriteFile(path, []byte(code), 0600); err != nil {
			t.Fatal(err)
		}
		files = append(files, compiler.SourceFile{Filename: path, Source: []byte(code)})
	}
	fixtures = append(fixtures, fixture{name: "forward-files", sources: files, valid: true})
	for _, f := range fixtures {
		analysis := compiler.Analyze(f.sources)
		valid := len(analysis.Diagnostics) == 0
		// Map key restrictions are language checks after Go's type pass.
		// Consult lowering for negative fixtures which go/types accepts.
		if valid && !f.valid {
			seedSources := append([]compiler.SourceFile(nil), f.sources...)
			seedSources = append(seedSources, compiler.SourceFile{Filename: "oracle_main.lang", Source: []byte("package main\nfunc main(){}")})
			_, err := compiler.CompileFiles(seedSources)
			valid = err == nil
		}
		if valid != f.valid {
			t.Fatalf("seed disagrees with fixture %s (valid=%v): %+v", f.name, f.valid, analysis.Diagnostics)
		}
	}
	sources, err := readSources("../../bootstrap/checker")
	if err != nil {
		t.Fatal(err)
	}
	for _, baseline := range []bool{false, true} {
		t.Run(fmt.Sprintf("baseline=%v", baseline), func(t *testing.T) {
			program, err := compiler.CompileFilesWithOptions(sources, compiler.Options{DisableOptimizations: baseline})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			if err := build(dir, program); err != nil {
				t.Fatal(err)
			}
			for _, f := range fixtures {
				t.Run(f.name, func(t *testing.T) {
					argv := []string{"-noshell", "-pa", dir, "-eval", evaluationScript(false, true), "-extra"}
					for _, source := range f.sources {
						argv = append(argv, source.Filename)
					}
					ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
					defer cancel()
					out, err := exec.CommandContext(ctx, "erl", argv...).CombinedOutput()
					if (err == nil) != f.valid {
						t.Fatalf("checker valid=%v: %v\n%s", f.valid, err, out)
					}
					if f.valid {
						if !strings.Contains(string(out), "types ok\n") || !strings.Contains(string(out), "live_cells => 0") || !strings.Contains(string(out), "root_frames => 0") {
							t.Fatalf("missing success or leaked roots: %s", out)
						}
					} else if !strings.Contains(string(out), f.sources[0].Filename+":") {
						t.Fatalf("missing source diagnostic: %s", out)
					}
				})
			}
		})
	}
}

func TestBootstrapCheckerPackedWithoutSeed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("isolated symlink PATH requires Unix")
	}
	sources, err := readSources("../../bootstrap/checker")
	if err != nil {
		t.Fatal(err)
	}
	program, err := compiler.CompileFiles(sources)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	artifact := filepath.Join(dir, "checker.escript")
	if err := pack(artifact, program, false, true); err != nil {
		t.Fatal(err)
	}
	isolated := filepath.Join(dir, "runtime")
	if err := os.Mkdir(isolated, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"escript", "erl", "dirname"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		path, err = filepath.EvalSymlinks(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(path, filepath.Join(isolated, name)); err != nil {
			t.Fatal(err)
		}
	}
	var env []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "PATH", "ERL_LIBS", "ERL_FLAGS", "ERL_AFLAGS", "ERL_ZFLAGS":
		default:
			env = append(env, entry)
		}
	}
	env = append(env, "PATH="+isolated)
	var args []string
	for _, source := range sources {
		path := filepath.Join(dir, "雪 "+filepath.Base(source.Filename))
		if err := os.WriteFile(path, source.Source, 0600); err != nil {
			t.Fatal(err)
		}
		args = append(args, path)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, artifact, args...)
	command.Dir, command.Env = dir, env
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("packed checker without seed: %v\n%s", err, out)
	}
	if !strings.HasPrefix(string(out), "types ok\n") || !strings.Contains(string(out), "live_cells => 0") || !strings.Contains(string(out), "root_frames => 0") {
		t.Fatalf("packed checker: %s", out)
	}
}
