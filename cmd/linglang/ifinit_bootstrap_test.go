package main

// Run through both compilers, both lowering modes and the actual self-built
// compiler. Assertions cover lexical scope, execution order and escaped roots.
const ifInitializerFixture = `
func escaped()*int{if n:=7;n>0{return &n};return nil}
func main(){
 x:=9
 if x:=1;x>2{panic("wrong branch")}else if y:=x+1;y==2{assert(x==1&&y==2)}else{panic("lost initializer")}
 assert(x==9)
 if x=x+1;x==10{x++}else{panic("initializer not run")}
 assert(x==11)
 var p *int
 if n:=3;n==3{p=&n;(*p)++}
 for i:=0;i<300;i++{n:=i;_= &n}
 assert(*p==4)
 p=escaped()
 for i:=0;i<300;i++{n:=i;_= &n}
 assert(*p==7)
 total:=0
 for i:=0;i<4;i++{if n:=i;n==1{continue}else if n==3{break};total+=i}
 assert(total==2)
 if r:=parseInt("42");r.ok{assert(r.value==42)}else{panic(r.reason)}
 println("if initializers ok")
}`
