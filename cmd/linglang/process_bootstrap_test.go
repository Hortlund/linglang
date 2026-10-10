package main

import (
	"strings"
	"testing"
)

const otpTimerFixture = `
type Tick struct{n int;xs List[int]}
func later(parent Pid){sendAfter(parent,"from exited worker",10)}
func pid()Pid{print(1);return self()}
func tick()Tick{print(2);return Tick{n:8}}
func delay()int{print(3);return 0}
func main(){
    var zero Timer
    println(zero==nil)
    value:=Tick{n:7,xs:List[int]{1,2}}
    timer:=sendAfter(self(),value,0)
    value.n=99;value.xs=append(value.xs,3)
    message:=receive[Tick](5000)
    println(message.ok,message.value.n,len(message.value.xs),cancelTimer(timer))
    timer=sendAfter(self(),9,60000)
    send(self(),42)
    println(cancelTimer(timer),cancelTimer(timer),receive[int](0).value,!receive[int](0).ok)
    timer=(sendAfter[Tick])(pid(),tick(),delay())
    message=receive[Tick](5000)
    println(message.value.n,cancelTimer(timer))
    timer=sendAfter[List[int]](self(),nil,0)
    list:=receive[List[int]](5000)
    println(list.ok,list.value==nil,cancelTimer(timer))
    p:=spawnMonitor(later,self())
    assert(wait(p.monitor,5000).normal)
    println(receive[string](5000).value)
}`

const otpTimerOutput = "true\ntrue 7 2 false\ntrue false 42 true\n1238 false\ntrue true false\nfrom exited worker\n"

const otpMessagesFixture = `
type First struct{n int}
type Second struct{n int}
type Args struct{parent Pid; first First}
func worker(a Args){send(a.parent,a.first);send(a.parent,Second{n:a.first.n+1})}
func main(){
    original:=First{n:7}
    p:=spawnMonitor(worker,Args{parent:self(),first:original})
    original.n=99
    second:=receive[Second](5000)
    first:=receive[First](0)
    println(first.ok,first.value.n,second.ok,second.value.n)
    empty:=receive[First](0)
    done:=wait(p.monitor,5000)
    println(empty.ok,empty.value.n,done.ok,done.normal)
    m:=monitor(self())
    println(!wait(m,0).ok,demonitor(m),!demonitor(m))
}`

// Exercise every supervisor lifecycle operation without relying on sleeps or
// scheduler order. A readiness message is part of the application protocol.
const otpSupervisorFixture = `
func worker(parent Pid){send(parent,self());receive[int](-1)}
func main(){
    s:=startSupervisor(3,5)
    child:=supervise(s,"worker",worker,self(),ChildOptions{restart:"transient"})
    ready:=receive[Pid](5000)
    m:=monitor(ready.value)
    println(child.ok,ready.ok,lookupChild(s,"worker").pid==ready.value)
    stopped:=stopChild(s,"worker")
    gone:=wait(m,5000)
    replacement:=restartChild(s,"worker")
    next:=receive[Pid](5000)
    println(stopped,gone.ok&&gone.reason=="shutdown",replacement.ok&&next.value!=ready.value)
    m=monitor(supervisorPid(s))
    stopped=stopChild(s,"worker")
    removed:=removeChild(s,"worker")
    finished:=stopSupervisor(s)
    println(stopped,removed,finished&&wait(m,5000).ok)
}`

const otpRestartFixture = `
type Args struct{parent Pid;seed int}
type Started struct{pid Pid;seed int}
func worker(a Args){send(a.parent,Started{pid:self(),seed:a.seed});receive[bool](-1);a.seed++}
func main(){
    s:=startSupervisor(3,5)
    a:=Args{parent:self(),seed:7}
    child:=supervise(s,"worker",worker,a,ChildOptions{restart:"permanent"})
    a.seed=99
    first:=receive[Started](5000)
    assert(first.ok)
    m:=monitor(first.value.pid)
    send(first.value.pid,true)
    done:=wait(m,5000)
    second:=receive[Started](5000)
    println(child.ok,done.ok&&done.normal,second.ok&&second.value.pid!=first.value.pid)
    println(second.value.seed==7,stopSupervisor(s))
}`

const otpSchemaFixture = `
type A struct{n int}
type B struct{n int}
type Pair struct{a A;b A}
type Payload struct{from Pid;xs List[Map[string,Delivery[A]]];ok bool}
func worker(xs List[int]){}
func main(){
    send(self(),A{n:1});send(self(),B{n:2});send(self(),Pair{})
    send(self(),Payload{from:self()})
    send[Pid](self(),nil)
    (send[List[int]])(self(),nil)
    send[Map[string,A]](self(),nil)
    send(self(),Delivery[Map[string,List[A]]]{})
    send(self(),List[Delivery[List[B]]]{})
    send(self(),Child{});send(self(),Exit{})
    p:=(spawnMonitor[List[int]])(worker,nil)
    assert(wait(p.monitor,5000).normal)
    assert((receive[Pid])(0).ok)
    assert(receive[List[int]](0).ok)
}`

// Compare complete wire schemas with the seed, not just round trips through
// one compiler (which can agree with itself while breaking interoperability).
func assertMessageSchemas(t *testing.T, seed, module string) {
	t.Helper()
	count := 0
	for _, call := range strings.Split(seed, "linglang_rt:send_message(")[1:] {
		_, schema, ok := strings.Cut(call, ", ")
		if !ok {
			t.Fatal("missing seed message schema")
		}
		depth := 0
		for i, char := range schema {
			if char == '{' || char == '[' {
				depth++
			} else if char == '}' || char == ']' {
				depth--
			} else if char == ',' && depth == 0 {
				schema = schema[:i]
				break
			}
		}
		if !strings.Contains(module, ", "+schema+", ") {
			t.Fatalf("bootstrap output missing seed wire schema: %s", schema)
		}
		count++
	}
	if count != 11 {
		t.Fatalf("checked %d schemas, want 11", count)
	}
}

const otpServerFixture = `
type Query struct{n int;block bool}
func handler(state *int,q Query)int{if q.block{receive[bool](-1)};*state+=q.n;return *state}
func listHandler(state *List[int],q List[int])List[int]{*state=q;return *state}
func target(p Pid)Pid{print(1);return p}
func query()Query{print(2);return Query{n:3}}
func deadline()int{print(3);return 5000}
func use(p *int,r CallResult[int]){println(*p,r.value)}
func main(){
 s:=startSupervisor(3,5)
 p:=superviseServer(s,"server",handler,10,ChildOptions{}).pid
 send(self(),123)
 r:=call[int](p,Query{n:1,block:true},0)
 assert(!r.ok&&r.timedOut&&r.reason=="timeout"&&r.value==0)
 send(p,true)
 r=call[int](p,Query{n:2},5000)
 assert(r.ok&&!r.timedOut&&r.reason==""&&r.value==13)
 assert(receive[int](0).value==123)
 assert(call[string](p,Query{},5000).reason=="protocol_mismatch")
 r=call[int](p,"wrong request",5000)
 assert(r.reason=="protocol_mismatch")
 assert(call[int](self(),0,0).reason=="calling_self")
 n:=42
 use(&n,(call[int,Query])(target(p),query(),deadline()))
 var zero CallResult[List[int]]
 assert(zero.value==nil&&!zero.ok&&!zero.timedOut&&zero.reason=="")
 zero=CallResult[List[int]]{value:List[int]{1,2},ok:true,reason:"record"}
 send(self(),zero)
 assert(receive[CallResult[List[int]]](0).value.reason=="record")
 lp:=(superviseServer[List[int],List[int],List[int]])(s,"list",listHandler,nil,ChildOptions{}).pid
 zero=call[List[int],List[int]](lp,nil,5000)
 assert(zero.ok&&zero.value==nil)
 zero=call[List[int]](lp,List[int]{4,5},5000)
 assert(zero.ok&&len(zero.value)==2)
 assert(stopSupervisor(s))
 r=call[int](p,Query{},5000)
 assert(!r.ok&&!r.timedOut&&r.reason=="noproc")
 println("server calls ok")
}`

const otpServerOutput = "12342 16\nserver calls ok\n"
const otpStoreOutput = "Greeting: hello from BEAM\nConcurrent clients completed: 4\nCall failed: store crashed\nOTP restarted store: true\nPrevious client data survived: false\nRestored greeting: hello from BEAM\n"
