package compiler

import (
	"os"
	"testing"
)

func TestMultipleResults(t *testing.T) {
	source, err := os.ReadFile("../../tests/language/multiple_test.lang")
	if err != nil {
		t.Fatal(err)
	}
	program := string(source[len("package main"):]) + "\nfunc main(){TestMultipleResults();println(\"ok\")}"
	if out, err := execute(t, program); err != nil || out != "ok\n" {
		t.Fatalf("multiple results: %q %v", out, err)
	}
}

func TestMultipleResultsRejected(t *testing.T) {
	for _, code := range []string{
		`func f()(int,string){return 1}`,
		`func f()(int,string){return 1,2}`,
		`func f()int{return 1,2}`,
		`func pair()(int,string){return 1,"ok"};func f(){a:=pair();_=a}`,
		`func pair()(int,string){return 1,"ok"};func f(){_,_=pair(),1}`,
		`func pair()(int,string){return 1,"ok"};func f(){println(pair())}`,
		`func f(){a,a:=1,2;_=a}`,
		`func f(){a,b:=1,2;a,b:=2,3;_,_=a,b}`,
		`func f(){_,_:=1,2}`,
		`func f(){a,b:=1,2;a,b=3;_,_=a,b}`,
		`func f()(a int,b string){return}`,
		`func worker(n int)(int,int){return n,n};func f(){spawn(worker,1)}`,
	} {
		if _, err := Compile("bad.lang", []byte("package main\n"+code+";func main(){}")); err == nil {
			t.Errorf("accepted %s", code)
		}
	}
}
