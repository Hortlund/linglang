package compiler

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestServerExample(t *testing.T) {
	source, err := os.ReadFile("../../examples/store.lang")
	if err != nil {
		t.Fatal(err)
	}
	out, err := executeSupervised(t, strings.TrimPrefix(string(source), "package main"))
	want := "Greeting: hello from BEAM\nConcurrent clients completed: 4\nCall failed: store crashed\nOTP restarted store: true\nPrevious client data survived: false\nRestored greeting: hello from BEAM\n"
	if err != nil || out != want {
		t.Fatalf("got %q (%v), want %q", out, err, want)
	}
}

func TestServerCallRuntime(t *testing.T) {
	// A blocked callback makes timeout deterministic. A subsequent successful
	// call proves the late reply has been sent before inspecting the mailbox.
	source := `
type Request struct{n int;block bool}
func handler(state *int,q Request)int{if q.block{receive[bool](-1)};*state+=q.n;return *state}
func main(){
 s:=startSupervisor(3,5)
 p:=superviseServer(s,"server",handler,10,ChildOptions{}).pid
 send(self(),123)
 r:=call[int](p,Request{n:1,block:true},0)
 assert(!r.ok&&r.timedOut&&r.reason=="timeout"&&r.value==0)
 send(p,true)
 r=call[int](p,Request{n:2},5000)
 assert(r.ok&&!r.timedOut&&r.reason==""&&r.value==13)
 assert(receive[int](0).value==123)
 assert(call[string](p,Request{},5000).reason=="protocol_mismatch")
 assert(call[int](p,"wrong request",5000).reason=="protocol_mismatch")
 assert(call[int](self(),0,0).reason=="calling_self")
 assert(stopSupervisor(s))
 r=call[int](p,Request{},5000)
 assert(!r.ok&&!r.timedOut&&r.reason=="noproc")
}`
	script := `process_flag(trap_exit, false), logger:set_primary_config(level, emergency), linglang_rt:set_gc_stress(true),
 linglang_program:main(), {messages, []} = process_info(self(), messages),
 #{live_cells := 0, root_entries := 0, root_frames := 0} = linglang_rt:stats(),
 {monitors, []} = process_info(self(), monitors), halt(0).`
	for _, baseline := range []bool{false, true} {
		out, err := executeScriptWithOptions(t, source, script, Options{DisableOptimizations: baseline})
		if err != nil || out != "" {
			t.Fatalf("baseline=%v: %s (%v)", baseline, out, err)
		}
	}
}

func TestServerRuntimeStateCleanup(t *testing.T) {
	// Run OTP callbacks in this process to inspect roots after termination.
	// Exercise persistent aliases and transient cells across callback boundaries.
	script := `linglang_rt:set_gc_stress(true),
 H = fun(Cell, N) -> linglang_rt:scope(fun() ->
     _ = linglang_rt:new(99), linglang_rt:collect(),
     linglang_rt:write(Cell, linglang_rt:read(Cell) + N), linglang_rt:read(Cell)
 end) end,
 {ok, State} = linglang_server:init({H, 10, int, int, int, true}),
 {reply, {linglang_reply, int, 12}, State} = linglang_server:handle_call({linglang_call, int, int, 2}, unused, State),
 {reply, {linglang_reply, int, 15}, State} = linglang_server:handle_call({linglang_call, int, int, 3}, unused, State),
 #{live_cells := 1, root_entries := 1, root_frames := 1} = linglang_rt:stats(),
 ok = linglang_server:terminate(normal, State),
 #{live_cells := 0, root_entries := 0, root_frames := 0} = linglang_rt:stats(),
 halt(0).`
	out, err := executeScript(t, `func main(){}`, script)
	if err != nil || out != "" {
		t.Fatalf("%s (%v)", out, err)
	}
}

func TestServerCollectionDoesNotScaleWithStateSize(t *testing.T) {
	// Count BEAM reductions instead of wall time. Initialization validates the
	// whole state, so measure only requests after initialization. A thousandfold
	// increase in pointer-free state must not turn each lookup into a full scan.
	script := `Counts = lists:map(fun(Size) ->
 Items = maps:from_list([{I, I} || I <- lists:seq(1, Size)]),
 H = fun(Cell, Key) -> maps:get(Key, linglang_rt:read(Cell)) end,
 {ok, State} = linglang_server:init({H, Items, {map, int, int}, int, int, false}),
 {reductions, Before} = process_info(self(), reductions),
 lists:foreach(fun(_) ->
     {reply, {linglang_reply, int, 1}, State} =
         linglang_server:handle_call({linglang_call, int, int, 1}, unused, State)
 end, lists:seq(1, 20)),
 {reductions, After} = process_info(self(), reductions),
 {_, Cell, _, _} = State,
 Items = linglang_rt:read(Cell),
 #{live_cells := 1, root_entries := 1, root_frames := 1} = linglang_rt:stats(),
 ok = linglang_server:terminate(normal, State),
 #{live_cells := 0, root_entries := 0, root_frames := 0} = linglang_rt:stats(),
 After - Before
end, [100, 100000]),
io:format("[~p,~p]~n", Counts), halt(0).`
	out, err := executeScript(t, `func main(){}`, script)
	if err != nil {
		t.Fatalf("%s (%v)", out, err)
	}
	var counts []int
	if err := json.Unmarshal([]byte(out), &counts); err != nil || len(counts) != 2 {
		t.Fatalf("invalid reduction counts %q: %v", out, err)
	}
	t.Logf("reductions for 20 requests with 100/100000 state entries: %v", counts)
	// Allow ample room for BEAM GC and OTP-version differences. Full-state
	// tracing costs hundreds of times more for the larger map.
	if counts[1] >= counts[0]*10 {
		t.Fatalf("request collection scales with server state size: %v", counts)
	}
}

func TestServerTypeBoundaries(t *testing.T) {
	for _, source := range []string{
		`func main(){call[int](self(),&1,0)}`,
		`func main(){n:=1;call[int](self(),&n,0)}`,
		`func main(){call[*int](self(),1,0)}`,
		`func main(){call[Timer](self(),1,0)}`,
		`func handler(s **int,q int)int{return 0};func main(){superviseServer(startSupervisor(3,5),"s",handler,nil,ChildOptions{})}`,
		`func handler(s *int,q *int)int{return 0};func main(){superviseServer(startSupervisor(3,5),"s",handler,0,ChildOptions{})}`,
		`func handler(s *int,q int)*int{return s};func main(){superviseServer(startSupervisor(3,5),"s",handler,0,ChildOptions{})}`,
		`func handler(s int,q int)int{return 0};func main(){superviseServer(startSupervisor(3,5),"s",handler,0,ChildOptions{})}`,
		`func handler(s *int,q int){};func main(){superviseServer(startSupervisor(3,5),"s",handler,0,ChildOptions{})}`,
		`func handler(s *int,q int)int{return 0};func main(){superviseServer[string](startSupervisor(3,5),"s",handler,0,ChildOptions{})}`,
		`func main(){call[int](self(),nil,0)}`,
	} {
		if _, err := Compile("bad.lang", []byte("package main\n"+source)); err == nil {
			t.Fatalf("accepted %s", source)
		}
	}
}

func TestServerProtocolAndLifecycle(t *testing.T) {
	script := `process_flag(trap_exit, false), logger:set_primary_config(level, emergency),
    S = linglang_sup:start(3, 5),
    O = #{field_72657374617274 => <<"temporary">>, field_73687574646f776e4d696c6c6973 => 1000},
    H = fun(Cell, N) ->
        case N of
            -1 -> exit(timeout);
            -2 -> Inner = linglang_sup:start(1, 5), true = linglang_sup:stop(Inner);
            _ -> ok
        end,
        linglang_rt:scope(fun() -> linglang_rt:write(Cell, N) end), N
    end,
    #{field_706964 := P} = linglang_sup:add_server(S, <<"s">>, H, 0, int, int, int, O),
    #{field_6f6b := true} = linglang_server:call(P, int, -2, int, 5000, 0),
    #{field_6f6b := true, field_76616c7565 := 7} = linglang_server:call(P, int, 7, int, 5000, 0),
    _ = sys:get_status(P),
    Parent = self(),
    _ = sys:replace_state(P, fun(State) -> Parent ! linglang_rt:stats(), State end),
    receive #{live_cells := 1, root_frames := 1, root_entries := 1} -> ok after 1000 -> error(missing_stats) end,
    #{field_6f6b := false, field_74696d65644f7574 := false, field_726561736f6e := <<"timeout">>} = linglang_server:call(P, int, -1, int, 5000, 0),
    true = linglang_sup:stop(S),
    {messages, []} = process_info(self(), messages),
    halt(0).`
	out, err := executeScript(t, `func main(){}`, script)
	if err != nil || out != "" {
		t.Fatalf("%s (%v)", out, err)
	}
}
