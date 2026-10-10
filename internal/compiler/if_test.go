package compiler

import "testing"

func TestIfInitializerScopeAndLifetime(t *testing.T) {
	source := `
func result()*int{if n:=42;n>0{return &n};return nil}
func main(){
 n:=9
 if n:=1;n>2{panic("wrong")}else if m:=n+1;m==2{assert(n==1)}else{panic("scope")}
 assert(n==9)
 if n++;n==10{n++}
 assert(n==11)
 var p *int
 if x:=7;x>0{p=&x;(*p)++}
 for i:=0;i<300;i++{x:=i;_=&x}
 assert(*p==8)
 p=result()
 for i:=0;i<300;i++{x:=i;_=&x}
 assert(*p==42)
}`
	script := `linglang_rt:set_gc_stress(true), linglang_program:main(),
 #{live_cells := 0, root_entries := 0, root_frames := 0} = linglang_rt:stats(), halt(0).`
	for _, baseline := range []bool{false, true} {
		out, err := executeScriptWithOptions(t, source, script, Options{DisableOptimizations: baseline})
		if err != nil || out != "" {
			t.Fatalf("baseline=%v: %s (%v)", baseline, out, err)
		}
	}
}

func TestIfInitializerRejectsInvalidScope(t *testing.T) {
	for _, source := range []string{
		`func main(){if n:=1;n>0{};println(n)}`,
		`func main(){if n:=1;n{}}`,
		`func main(){if n:=1;n>0{}else{println(m)}}`,
	} {
		if _, err := Compile("bad.lang", []byte("package main\n"+source)); err == nil {
			t.Fatalf("accepted %s", source)
		}
	}
}
