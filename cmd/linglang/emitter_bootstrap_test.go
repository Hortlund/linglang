package main

import (
	"bytes"
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

func TestBootstrapEmitterPrograms(t *testing.T) {
	type fixture struct{ name, code, want, failure string }
	fixtures := []fixture{
		{"operator_chain", `func main(){a:=1;println(a+2+3+4,a+a+a)}`, "10 3\n", ""},
		{"constants_and_shadowing", `const Big=1<<200;const Answer=(Big+7)-Big;func main(){const Answer=8;println(Answer);{const Answer=9;println(Answer)};println(Answer)}`, "8\n9\n8\n", ""},
		{"early_returns", `func f(n int)int{if n<0{return -1}else if n>0{return 1}else{return 0}};func main(){println(f(-7),f(0),f(7))}`, "-1 0 1\n", ""},
		{"parameters", `func sum(a,b int)int{return a+b};func ignore(_ int,_ bool)int{return 3};func unused(int)int{return 4};func main(){println(sum(2,5),ignore(1,true),unused(9))}`, "7 3 4\n", ""},
		{"short_circuit", `func fail()bool{panic("must not run")};func main(){println(false&&fail(),true||fail())}`, "false true\n", ""},
		{"strings", `func main(){s:="雪\x00\xff";println(len(s),byteAt(s,4),len("x")+'a');s+="!";println(len(s),trim(" ok "),slice("abcd",1,3))}`, "5 255 98\n6 ok bc\n", ""},
		{"mutations", `func main(){var n int;var s string;var b bool;n=4;n+=3;n*=2;n--;s="ok";b=n==13;println(n,s,b);_=n+1}`, "13 ok true\n", ""},
		{"bitwise", `func main(){a:=13;b:=6;println(a&b,a|b,a^b,a&^b,^a);a&=b;a|=8;a^=3;a&^=5;a<<=3;a>>=4;println(a);n:=64;a=-1;println(a>>n,a<<n);n=0;x:=1;println(x<<(((1<<63)<<n)>>62),x<<(((1<<n)+18446744073709551615)/2))}`, "4 15 11 9 -14\n5\n-1 0\n4 1\n", ""},
		{"runtime_rune_int_context", `func take(x int)int{return x};func shifted(n int)int{return 'a'<<n};func main(){n:=30;var x int='a'<<n;println(x,take('a'<<n),shifted(n),('a'<<n)+len(""));n=32;x='a'<<n;println(x,('a'<<n)==x);n=0;x=1;println(x<<('a'<<n),'a','a'<<2)}`, "104152956928 104152956928 104152956928 104152956928\n416611827712 true\n0 97 388\n", ""},
		{"contextual_rune", `func main(){n:=0;var x int=('a'+(1<<40))<<n;println(x)}`, "1099511627873\n", ""},
		{"builtin_shadow", `func println(n int){print("local",n)};func main(){println(7)}`, "local7", ""},
		{"negative_shift", `func main(){n:=-1;println(1<<n)}`, "", "linglang_negative_shift"},
		{"panic_cleanup", `func f(n int){x:=n+1;_=x;panic("bean down")};func main(){f(1)}`, "", "bean down"},
		{"assert_location", "func main(){\nassert(false)\n}", "", "fixture.lang:3: assertion failed"},
		{"for_forms_and_scope", `func main(){total:=0;for i:=0;i<5;i++{total+=i};n:=0;for n<3{n++};for{total++;break};for ;;{break};for i:=5;i>3;i--{total+=i};println(total,n)}`, "20 3\n", ""},
		{"nested_break_continue", `func main(){total:=0;for i:=0;i<3;i++{for j:=0;j<4;j++{if j==1{continue};if j==3{break};total+=i+j}};println(total)}`, "12\n", ""},
		{"continue_runs_post", `func main(){total:=0;for i:=0;i<5;i++{if i<3{continue};total+=i};println(total)}`, "7\n", ""},
		{"loop_return", `func find(n int)int{for{for{break};if n==3{return n};n++}};func main(){println(find(0))}`, "3\n", ""},
		{"loop_local_shadowing", `func main(){n:=10;for n:=0;n<3;n++{{n:=40;println(n)};println(n)};println(n)}`, "40\n0\n40\n1\n40\n2\n10\n", ""},
		{"condition_side_effects", `func condition(n int)bool{print(n);return n<3};func main(){n:=0;for condition(n){n++};println(" done",n)}`, "0123 done 3\n", ""},
		{"loop_panic_cleanup", `func main(){for i:=0;i<3;i++{for{panic("loop down")}}}`, "", "loop down"},
		{"struct_values_and_parameters", `type Inner struct{n int};type Outer struct{inner Inner;label string;flag bool};func update(o Outer)Outer{o.inner.n+=3;o.flag=true;return o};func main(){var zero Outer;a:=Outer{inner:Inner{n:2},label:"hi"};b:=a;a=update(a);a.inner.n++;println(zero.inner.n,zero.label=="",zero.flag,a.inner.n,a.flag,b.inner.n,b.label)}`, "0 true false 6 true 2 hi\n", ""},
		{"pointer_aliases_and_parameters", `func change(p *int)**int{*p+=2;return &p};func main(){x:=1;p:=&x;q:=p;r:=change(p);**r+=3;(*q)--;println(x,*p,*q,p==q,*r==p,&*p==p)}`, "5 5 5 true true true\n", ""},
		{"pointer_zero_and_allocation", `type N struct{x int;next *N};func main(){var p *N;var n N;println(p==nil,n.next==nil,n.x);p=&N{};q:=&N{};p.x=7;println(p!=q,p.next==nil,q.x);var saved *N;r:=&saved;*r=p;println((*r).x);var boolean bool;var text string;var integer int;b:=&boolean;s:=&text;i:=&integer;println(*b,*s=="",*i)}`, "true true 0\ntrue true 0\n7\nfalse true 0\n", ""},
		{"returned_and_temporary_roots", emitterPointerHelpers + `func use(a *N,b int){println(a.x,b)};func slow(n int)*N{_=churn();return node(n)};type Pair struct{a *N;b *N};func main(){use(node(7),churn());use(&N{x:8},churn());pair:=Pair{a:node(1),b:slow(2)};_=churn();println(pair.a.x,pair.b.x)}`, "7 5\n8 5\n1 2\n", ""},
		{"snapshot_roots", emitterPointerHelpers + `type Box struct{n *N};func clear(b *Box)int{b.n=nil;return churn()};func use(b Box,n int){println(b.n.x,n)};func main(){b:=Box{n:node(10)};use(b,clear(&b));println(b.n==nil)}`, "10 5\ntrue\n", ""},
		{"assignment_destination_roots", emitterPointerHelpers + `func clear(p **N)int{*p=nil;return churn()};func main(){n:=node(1);n.x=clear(&n);node(2).x+=churn();println(n==nil)}`, "true\n", ""},
		{"compound_evaluation_order", `type N struct{x int};func target(n *N)*N{print("target ");return n};func rhs(n *N)int{print("rhs ");n.x=20;return 3};func main(){n:=N{x:5};target(&n).x+=rhs(&n);println(n.x);target(&n).x=rhs(&n);println(n.x);target(&n).x++;println(n.x)}`, "target rhs 8\ntarget rhs 3\ntarget 4\n", ""},
		{"interior_pointer_escape", emitterPointerHelpers + `type Box struct{n *N};type Outer struct{box Box};func interior()**N{o:=Outer{box:Box{n:node(12)}};return &o.box.n};func main(){p:=interior();_=churn();println((*p).x);*p=node(13);_=churn();println((*p).x);x:=&(*p).x;(*x)++;println((*p).x)}`, "12\n13\n14\n", ""},
		{"cycles_and_recursive_return", emitterPointerHelpers + `func ring()*N{a:=N{x:3};b:=N{x:4,next:&a};a.next=&b;return &a};func relay(n int)*N{if n==0{return ring()};p:=relay(n-1);_=churn();return p};func main(){p:=relay(3);_=churn();println(p.x,p.next.x,p.next.next==p)}`, "3 4 true\n", ""},
		{"loop_pointer_identity", emitterPointerHelpers + `func step(p *int){_=churn();(*p)++};func main(){var first *int;var last *int;for i:=0;i<4;step(&i){if i==0{first=&i};last=&i;if i==1{continue};if i==3{break}};_=churn();println(*first,*last,first==last);i:=0;for i<3{last=&i;i++};println(*last,last==&i)}`, "0 3 false\n3 true\n", ""},
		{"loop_condition_pointer", `func condition(p *int)bool{(*p)++;return *p<5};func main(){var first *int;for i:=0;condition(&i);i++{if first==nil{first=&i}};println(*first)}`, "1\n", ""},
		{"loop_return_pointer", emitterPointerHelpers + `func find()*N{for i:=0;i<3;i++{for j:=0;j<3;j++{if j==2{return node(i+j)}}};return nil};func main(){p:=find();_=churn();println(p.x)}`, "2\n", ""},
		{"pointer_failure_cleanup", `type N struct{next *N};func main(){n:=N{};n.next=&n;var p *int;println(*p)}`, "", "linglang_nil_pointer"},
		{"nil_field_write", `type N struct{x int};func main(){var p *N;p.x=3}`, "", "linglang_nil_pointer"},
		{"nil_address", `func main(){var p *int;_= &*p}`, "", "linglang_nil_pointer"},

		{"collection_zero_and_nil", `type S struct{xs List[int];m Map[string,bool];d Delivery[*int]};func main(){var s S;var xs List[int];var m Map[int,string];empty:=List[int]{};blank:=Map[int,string]{};println(s.xs==nil,s.m==nil,s.d.ok,s.d.value==nil,len(xs),len(m),empty==nil,blank==nil);println(head(xs).value,head(xs).ok,get(m,1).value=="",get(m,1).ok);println(append(xs)==nil,append(xs,empty...)==nil,tail(xs)==nil,remove(m,1)==nil)}`, "true true false true 0 0 false false\n0 false true false\ntrue true true true\n", ""},
		{"collection_operations", `func main(){xs:=prepend(1,List[int]{2,3});ys:=append(xs,4,5);zs:=append(xs,List[int]{6,7}...);println(xs,ys,zs,head(tail(xs)).value);m:=Map[string,int]{"x":1,"y":2};n:=put(m,"x",9);r:=remove(n,"y");println(len(m),get(m,"x").value,get(n,"x").value,len(r),get(r,"y").ok);println(join(split("a,b,c",","),"-"))}`, "[1, 2, 3] [1, 2, 3, 4, 5] [1, 2, 3, 6, 7] 2\n2 1 9 1 false\na-b-c\n", ""},
		{"map_identifier_keys", `func from(key string,n int)Map[string,int]{return Map[string,int]{key:n}};func main(){key:="bean";m:=Map[string,int]{key:7};println(get(m,key).value,get(from(key,8),key).value);n:=3;ints:=Map[int,int]{n:9};println(get(ints,n).value);{key:="inner";m:=Map[string,int]{key:10};println(get(m,key).value)};println(get(m,key).value);for i:=0;i<2;i++{m:=Map[int,int]{i:i+20};println(get(m,i).value)};for _,n:=range (List[int]{4,5}){m:=Map[int,int]{n:n+30};println(get(m,n).value)}}`, "7 8\n9\n10\n7\n20\n21\n34\n35\n", ""},
		{"map_identifier_key_initializers_and_fields", `type S struct{key int};func main(){key:=2;{key:=Map[int,int]{key:11};println(get(key,2).value)};{var key Map[int,int]=Map[int,int]{key:12};println(get(key,2).value)};s:=S{key:13};m:=Map[int,S]{key:{key:14}};println(s.key,get(m,key).value.key);{const key=3;m:=Map[int,int]{key:15};println(get(m,key).value)};m2:=Map[int,int]{(key):16};println(get(m2,key).value)}`, "11\n12\n13 14\n15\n16\n", ""},
		{"collection_generics", `func ignore()int{head[int](nil);put[string,int](nil,"x",1);return 1};func main(){_=ignore();xs:=prepend(3,nil);m:=put(nil,"x",xs);println(head(xs).value,head(get(m,"x").value).value);println((head[int])(nil).ok,(get[string,bool])(nil,"x").value);println(head[int](nil).value,get[int,string](nil,1).ok,tail[int](nil)==nil,remove[int,bool](nil,1)==nil);println(head(prepend[int](4,nil)).value,get(put[string,int](nil,"x",8),"x").value)}`, "3 3\nfalse false\n0 false true true\n4 8\n", ""},
		{"collection_nested_values", `type S struct{n int;xs List[int]};func modify(xs List[S])List[S]{n:=head(xs).value;n.n++;return prepend(n,tail(xs))};func main(){xs:=List[S]{{n:1},{n:2,xs:List[int]{3}}};ys:=modify(xs);m:=Map[string,S]{"one":{n:4}};var d Delivery[S];println(head(xs).value.n,head(ys).value.n,head(head(tail(xs)).value.xs).value,get(m,"one").value.n,d.value.n,d.value.xs==nil,d.ok);println(head(head(List[List[int]]{{5,6}}).value).value)}`, "1 2 3 4 0 true false\n5\n", ""},
		{"collection_order_and_duplicate_keys", `func mark(n int)int{print(n);return n};func key(n int)string{print(n);return "x"};func main(){xs:=List[int]{mark(1),mark(2)};m:=Map[string,int]{key(3):mark(4),key(5):mark(6)};println(" done",xs,len(m),get(m,"x").value);println(append(xs,mark(7),mark(8)))}`, "123456 done [1, 2] 1 6\n78[1, 2, 7, 8]\n", ""},
		{"collection_pointer_roots", emitterPointerHelpers + `func slow(n int)*N{_=churn();return node(n)};func use(xs List[*N],m Map[string,*N],n int){println(head(xs).value.x,get(m,"x").value.x,n)};func main(){xs:=List[*N]{node(1),slow(2)};m:=Map[string,*N]{"x":node(3),"y":slow(4)};_=churn();println(head(xs).value.x,head(tail(xs)).value.x,get(m,"x").value.x,get(m,"y").value.x);use(List[*N]{node(5)},Map[string,*N]{"x":slow(6)},churn());xs=prepend(node(7),tail(xs));m=put(m,"x",slow(8));_=churn();println(head(xs).value.x,get(m,"x").value.x)}`, "1 2 3 4\n5 6 5\n7 8\n", ""},
		{"collection_snapshot_roots", emitterPointerHelpers + `func clear(xs *List[*N],m *Map[int,*N])int{*xs=nil;*m=nil;return churn()};func use(xs List[*N],m Map[int,*N],n int){println(head(xs).value.x,get(m,1).value.x,n)};func main(){xs:=List[*N]{node(1)};m:=Map[int,*N]{1:node(2)};use(xs,m,clear(&xs,&m));println(xs==nil,m==nil)}`, "1 2 5\ntrue true\n", ""},
		{"collection_recursive_types", `type N struct{xs List[N];m Map[string,*N]};func main(){n:=N{};n.m=put(n.m,"self",&n);n.xs=List[N]{N{}};println(get(n.m,"self").value==&n,head(n.xs).value.xs==nil)}`, "true true\n", ""},
		{"collection_builtin_shadowing", `func use(head int,put int)int{return head+put};func append(n int)int{return n+3};func main(){println(use(1,2),append(3))}`, "3 6\n", ""},
		{"range_forms_and_snapshot", `func source()List[int]{print("source ");return List[int]{2,4,6}};func main(){xs:=source();for i,v:=range xs{println(i,v);xs=nil};n:=0;for range source(){n++};println(n,xs==nil);for i:=range (List[int]{7,8}){println(i)};var empty List[int];for range empty{panic("empty")};for _,v:=range (List[int]{9}){println(v)};for i,_:=range (List[int]{10}){println(i) }}`, "source 0 2\n1 4\n2 6\nsource 3 true\n0\n1\n9\n0\n", ""},
		{"range_control_and_assignment", `func main(){i:=10;v:=20;for i,v=range (List[int]{2,4,6,8}){if i==1{continue};if i==3{break};println(i,v)};println(i,v);for _,v=range (List[int]{9,10}){};println(v);total:=0;for _,a:=range (List[int]{1,2}){for _,b:=range (List[int]{3,4}){if b==3{continue};total+=a+b}};println(total);for i:=range (List[int]{5}){println(i)};println(i)}`, "0 2\n2 6\n3 8\n10\n11\n0\n3\n", ""},
		{"range_iteration_pointer_identity", emitterPointerHelpers + `func main(){var indices List[*int];var values List[*int];for i,v:=range (List[int]{4,5,6}){indices=append(indices,&i);values=append(values,&v)};_=churn();println(*head(indices).value,*head(tail(indices)).value,*head(tail(tail(indices))).value);println(*head(values).value,*head(tail(values)).value,*head(tail(tail(values))).value,head(values).value==head(tail(values)).value);v:=0;values=nil;for _,v=range (List[int]{7,8}){values=append(values,&v)};println(*head(values).value,*head(tail(values)).value,head(values).value==head(tail(values)).value)}`, "0 1 2\n4 5 6 false\n8 8 true\n", ""},
		{"range_pointer_snapshot_and_return", emitterPointerHelpers + `func find(xs List[*N])*N{for _,p:=range xs{if p.x==2{return p}};return nil};func main(){xs:=List[*N]{node(1),node(2),node(3)};p:=find(xs);for _,n:=range xs{xs=nil;_=churn();println(n.x)};_=churn();println(p.x)}`, "1\n2\n3\n2\n", ""},
		{"range_failure_cleanup", emitterPointerHelpers + `func main(){for _,p:=range (List[*N]{node(1)}){_=churn();panic(formatInt(p.x))}}`, "", "linglang_panic"},
		{"shadowed_new", `func new(n int)*int{x:=n;return &x};func main(){p:=new(8);println(*p)}`, "8\n", ""},
		{"switch_order_and_default", `func mark(n int)int{print(n);return n};func main(){switch mark(2){default:println("fallback");case mark(1),mark(2),mark(3):println(" match");case mark(4):panic("late")};switch mark(9){case mark(1):panic("wrong");default:println(" fallback");case mark(2):panic("wrong")};switch{};switch{default:println("empty")}}`, "212 match\n912 fallback\nempty\n", ""},
		{"switch_bool_and_initializers", `func mark(n int)bool{print(n);return n==2};func main(){n:=9;switch n:=2;n{case 1:panic("wrong");case 2:x:=3;println(n,x);default:x:=4;println(x)};println(n);switch{case mark(1),mark(2),mark(3):println(" yes");default:panic("wrong")};b:=true;switch b{case false:panic("wrong");case true:println("bool");case true:panic("late")}}`, "2 3\n9\n12 yes\nbool\n", ""},
		{"switch_break_continue_targets", `func main(){for i:=0;i<3;i++{switch i{case 0:continue;case 1:switch{default:break};for{break};println("one");break;default:println("two")};println("after",i)};for _,n:=range (List[int]{4,5}){switch n{case 4:continue;default:break};println(n)}}`, "one\nafter 1\ntwo\nafter 2\n5\n", ""},
		{"switch_return_flow", `func f(n int)int{switch n{case 1:for{break};return 7;default:switch{default:return 8}}};func g()int{for{switch{default:break};return 9}};func main(){println(f(1),f(2),g())}`, "7 8 9\n", ""},
		{"switch_nil_and_structs", `type S struct{n int};func main(){var xs List[int];var m Map[string,int];var p *int;switch xs{case nil:println("list")};switch m{case nil:println("map")};switch p{case nil:println("pointer")};switch (S{n:3}){case S{n:2}:panic("wrong");case S{n:3}:println("struct")}}`, "list\nmap\npointer\nstruct\n", ""},
		{"switch_tag_snapshot_roots", emitterPointerHelpers + `type Box struct{n *N};func clear(b *Box)Box{old:=*b;b.n=nil;_=churn();return old};func main(){b:=Box{n:node(7)};switch b{case clear(&b):println("snapshot")};println(b.n==nil)}`, "snapshot\ntrue\n", ""},
		{"switch_case_roots_and_cleanup", emitterPointerHelpers + `func main(){n:=node(7);switch n{case node(8):panic("wrong");case n:for{_=churn();break};println(n.x);break};_=churn();println(n.x)}`, "7\n7\n", ""},
		{"switch_failure_cleanup", emitterPointerHelpers + `func main(){switch n:=node(7);n.x{default:_=churn();panic("switch down")}}`, "", "switch down"},
		{"native_runes_and_results", `func main(){r:=runeAt("雪😀\xff",0);println(r.value,r.width,r.ok);r=runeAt("雪😀\xff",3);println(r.value,r.width,r.ok);r=runeAt("雪😀\xff",7);println(r.value,r.width,r.ok);r=runeAt("",0);println(r.value,r.width,r.ok);var z RuneResult;var f TextResult;println(z.value,z.width,z.ok,f.value=="",f.ok,f.reason=="");println(len(args()),readFile("").ok)}`, "38634 3 true\n128512 4 true\n65533 1 false\n0 0 false\n0 0 false true false true\n0 false\n", ""},
		{"native_rune_bounds", `func main(){runeAt("a",2)}`, "", "linglang_byte_index_out_of_range"},
		{"typed_recursive_value_cells", emitterPointerHelpers + `type Tree struct{n int;children List[Tree];data Map[string,List[Tree]]};func tree()*Tree{t:=Tree{n:7,children:List[Tree]{Tree{n:8}},data:Map[string,List[Tree]]{"x":List[Tree]{Tree{n:9}}}};return &t};func main(){p:=tree();q:=&p.n;_=churn();*q++;p.children=append(p.children,Tree{n:10});_=churn();println(p.n,head(p.children).value.n,head(get(p.data,"x").value).value.n,len(p.children))}`, "8 8 9 2\n", ""},
		{"typed_recursive_pointer_graph", emitterPointerHelpers + `type A struct{bs List[B]};type B struct{as List[A];nodes Map[string,Delivery[List[*N]]]};func makeA()A{return A{bs:List[B]{B{nodes:Map[string,Delivery[List[*N]]]{"n":Delivery[List[*N]]{value:List[*N]{node(7)},ok:true}}}}}};func consume(a A,n int){b:=head(a.bs).value;d:=get(b.nodes,"n").value;println(head(d.value).value.x,n)};func clear(a *A)int{a.bs=nil;return churn()};func main(){a:=makeA();consume(a,clear(&a));b:=head(makeA().bs).value;_=churn();println(head(get(b.nodes,"n").value.value).value.x)}`, "7 5\n7\n", ""},
		{"native_helper_shadowing", `func use(args int,readFile int,runeAt int)int{return args+readFile+runeAt};func main(){args:=1;readFile:=2;runeAt:=3;println(use(args,readFile,runeAt))}`, "6\n", ""},
	}
	files, err := readSources("../../bootstrap/emitter")
	if err != nil {
		t.Fatal(err)
	}
	run := func(dir string, stress bool, args ...string) (string, string, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "erl", append([]string{"-noshell", "-pa", dir, "-eval", evaluationScript(stress, true)}, args...)...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		if ctx.Err() != nil {
			t.Fatalf("VM timeout: %s\n%s", stdout.String(), stderr.String())
		}
		return stdout.String(), stderr.String(), err
	}
	for _, baseline := range []bool{false, true} {
		t.Run(fmt.Sprintf("baseline=%v", baseline), func(t *testing.T) {
			program, err := compiler.CompileFilesWithOptions(files, compiler.Options{DisableOptimizations: baseline})
			if err != nil {
				t.Fatal(err)
			}
			emitterDir := t.TempDir()
			if err := build(emitterDir, program); err != nil {
				t.Fatal(err)
			}
			for _, f := range fixtures {
				t.Run(f.name, func(t *testing.T) {
					sourceDir := t.TempDir()
					path := filepath.Join(sourceDir, "fixture.lang")
					code := []byte("package main\n" + f.code)
					if err := os.WriteFile(path, code, 0600); err != nil {
						t.Fatal(err)
					}
					// Compare execution with the seed, including exception cleanup.
					seed, err := compiler.CompileWithOptions(path, code, compiler.Options{DisableOptimizations: baseline})
					if err != nil {
						t.Fatal(err)
					}
					seedDir := t.TempDir()
					if err := build(seedDir, seed); err != nil {
						t.Fatal(err)
					}
					seedOut, seedErr, seedFailure := run(seedDir, true)
					module, stderr, err := run(emitterDir, false, "-extra", path)
					if err != nil {
						t.Fatalf("emission: %v\n%s", err, stderr)
					}
					emitterCleanGC(t, stderr)
					generatedDir := t.TempDir()
					if err := build(generatedDir, module); err != nil {
						t.Fatalf("emitted Erlang: %v\n%s", err, module)
					}
					out, stderr, failure := run(generatedDir, true)
					emitterCleanGC(t, stderr)
					emitterCleanGC(t, seedErr)
					if out != seedOut || out != f.want || (failure == nil) != (seedFailure == nil) {
						t.Fatalf("got %q (%v), seed %q (%v), want %q\n%s", out, failure, seedOut, seedFailure, f.want, stderr)
					}
					if f.failure != "" && (failure == nil || !strings.Contains(stderr, f.failure) || !strings.Contains(seedErr, f.failure)) {
						t.Fatalf("missing failure %q: %v\n%s\nseed: %s", f.failure, failure, stderr, seedErr)
					}
					if f.failure == "" && failure != nil {
						t.Fatalf("runtime: %v\n%s", failure, stderr)
					}
				})
			}
		})
	}
}

const emitterPointerHelpers = `type N struct{x int;next *N};func node(n int)*N{x:=N{x:n};return &x};func churn()int{for i:=0;i<30;i++{_=node(i)};return 5};`

func TestBootstrapEmitterPackedWithoutGo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("isolated symlink PATH requires Unix")
	}
	files, err := readSources("../../bootstrap/emitter")
	if err != nil {
		t.Fatal(err)
	}
	program, err := compiler.CompileFiles(files)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	artifact := filepath.Join(dir, "emitter")
	if err := pack(artifact, program, false, false); err != nil {
		t.Fatal(err)
	}
	otp := filepath.Join(dir, "otp")
	env := isolatedOTPEnvironment(t, otp, true)
	invoke := func(name string, args ...string) (string, string, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Env = env
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		if ctx.Err() != nil {
			t.Fatal("isolated command timed out")
		}
		return stdout.String(), stderr.String(), err
	}
	a, _ := filepath.Abs("../../examples/bootstrap_demo/fibonacci.lang")
	b, _ := filepath.Abs("../../examples/bootstrap_demo/main.lang")
	module, stderr, err := invoke(artifact, b, a)
	if err != nil {
		t.Fatalf("packed emission: %v\n%s", err, stderr)
	}
	forward, stderr, err := invoke(artifact, a, b)
	if err != nil || module != forward {
		t.Fatalf("file order changed output: %v\n%s", err, stderr)
	}
	for name, source := range compiler.RuntimeSources() {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "linglang_program.erl"), []byte(module), 0600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err = invoke(filepath.Join(otp, "erlc"), "-o", dir, filepath.Join(dir, "linglang_rt.erl"), filepath.Join(dir, "linglang_sup.erl"), filepath.Join(dir, "linglang_program.erl"))
	if err != nil {
		t.Fatalf("OTP compilation: %v\n%s", err, stderr)
	}
	out, stderr, err := invoke(filepath.Join(otp, "erl"), "-noshell", "-pa", dir, "-eval", evaluationScript(true, true))
	want := "compiler has entered the chat\n55 beans. all accounted for.\nthis bean has its own scope\nactual answer: 55\nreceipt: 16\n"
	if err != nil || out != want {
		t.Fatalf("OTP-only program: %q (%v)\n%s", out, err, stderr)
	}
	emitterCleanGC(t, stderr)
	loopPath, err := filepath.Abs("../../examples/bootstrap_loops.lang")
	if err != nil {
		t.Fatal(err)
	}
	loopModule, stderr, err := invoke(artifact, loopPath)
	if err != nil {
		t.Fatalf("OTP-only loop emission: %v\n%s", err, stderr)
	}
	if err := os.WriteFile(filepath.Join(dir, "linglang_program.erl"), []byte(loopModule), 0600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err = invoke(filepath.Join(otp, "erlc"), "-o", dir, filepath.Join(dir, "linglang_program.erl"))
	if err != nil {
		t.Fatalf("OTP-only loop compilation: %v\n%s", err, stderr)
	}
	out, stderr, err = invoke(filepath.Join(otp, "erl"), "-noshell", "-pa", dir, "-eval", evaluationScript(true, true))
	if err != nil || out != "identifiers: 3\nloop sum: 499500\n" {
		t.Fatalf("OTP-only loop execution: %q (%v)\n%s", out, err, stderr)
	}
	emitterCleanGC(t, stderr)
	referencePath, err := filepath.Abs("../../examples/references.lang")
	if err != nil {
		t.Fatal(err)
	}
	referenceModule, stderr, err := invoke(artifact, referencePath)
	if err != nil {
		t.Fatalf("OTP-only reference emission: %v\n%s", err, stderr)
	}
	if err := os.WriteFile(filepath.Join(dir, "linglang_program.erl"), []byte(referenceModule), 0600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err = invoke(filepath.Join(otp, "erlc"), "-o", dir, filepath.Join(dir, "linglang_program.erl"))
	if err != nil {
		t.Fatalf("OTP-only reference compilation: %v\n%s", err, stderr)
	}
	out, stderr, err = invoke(filepath.Join(otp, "erl"), "-noshell", "-pa", dir, "-eval", evaluationScript(true, true))
	wantReferences := "After birthday: 31\nValue copy: 31 32\nAliased reference: 32 32 32\nReturned reference: 41\nLoop total: 10\n"
	if err != nil || out != wantReferences {
		t.Fatalf("OTP-only reference execution: %q (%v)\n%s", out, err, stderr)
	}
	emitterCleanGC(t, stderr)
	collectionPath, err := filepath.Abs("../../examples/bootstrap_collections.lang")
	if err != nil {
		t.Fatal(err)
	}
	collectionModule, stderr, err := invoke(artifact, collectionPath)
	if err != nil {
		t.Fatalf("OTP-only collection emission: %v\n%s", err, stderr)
	}
	if err := os.WriteFile(filepath.Join(dir, "linglang_program.erl"), []byte(collectionModule), 0600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err = invoke(filepath.Join(otp, "erlc"), "-o", dir, filepath.Join(dir, "linglang_program.erl"))
	if err != nil {
		t.Fatalf("OTP-only collection compilation: %v\n%s", err, stderr)
	}
	out, stderr, err = invoke(filepath.Join(otp, "erl"), "-noshell", "-pa", dir, "-eval", evaluationScript(true, true))
	if err != nil || out != "snapshot total: 15\nmap values: 5 20\niteration values: 0 1 2\nmissing value: 0 false\n" {
		t.Fatalf("OTP-only collection execution: %q (%v)\n%s", out, err, stderr)
	}
	emitterCleanGC(t, stderr)
	for _, code := range []string{"func main(){for range (Map[int,int]{1:2}) {}}", "func main(){println(List[int]{1:2})}", "func main(){println(make(List[int],2))}", "func main(){println(head[Pid](nil))}", "func f()int{};func main(){}", "const N=1<<100;func main(){println(N)}", "func main(){n:=30;println('a'<<n)}", "func main(){n:=30;println('a'>>n)}", "func main(){n:=30;println(('a'<<n)==0)}", "func main(){n:=30;println(('a'<<n)<0)}", "func main(){n:=30;println(^('a'<<n))}", "func main(){n:=30;println(-('a'<<n))}", "func main(){n:=30;println(('a'<<n)+1)}", "func main(){n:=30;_ = 'a'<<n}", "func main(){n:=30;panic('a'<<n)}"} {
		path := filepath.Join(dir, "bad.lang")
		if err := os.WriteFile(path, []byte("package main\n"+code), 0600); err != nil {
			t.Fatal(err)
		}
		out, stderr, err := invoke(artifact, path)
		if err == nil || out != "" || !strings.Contains(stderr, path+":2:") {
			t.Fatalf("missing clean source diagnostic: %q (%v)\n%s", out, err, stderr)
		}
	}
	large := "package main\nconst S0=\"x\";"
	for i := 1; i <= 21; i++ {
		large += fmt.Sprintf("const S%d=S%d+S%d;", i, i-1, i-1)
	}
	large += "func main(){println(S21)}"
	path := filepath.Join(dir, "large.lang")
	if err := os.WriteFile(path, []byte(large), 0600); err != nil {
		t.Fatal(err)
	}
	out, stderr, err = invoke(artifact, path)
	if err == nil || out != "" || !strings.Contains(stderr, "string literal exceeds 1 MiB") {
		t.Fatalf("missing bounded literal diagnostic: %v\n%s", err, stderr)
	}
}

func emitterCleanGC(t *testing.T, stderr string) {
	t.Helper()
	for _, value := range []string{"live_cells => 0", "root_entries => 0", "root_frames => 0"} {
		if !strings.Contains(stderr, value) {
			t.Fatalf("GC did not finish cleanly (%s):\n%s", value, stderr)
		}
	}
}
