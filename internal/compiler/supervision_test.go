package compiler

import (
	"os"
	"strings"
	"testing"
)

// OTP reports are useful in production. Silence them only in these deterministic
// output comparisons; the runtime itself retains the normal OTP logger setup.
func executeSupervised(t *testing.T, source string) (string, error) {
	t.Helper()
	script := `logger:set_primary_config(level, emergency), linglang_rt:set_gc_stress(true),
        try linglang_program:main() of _ ->
            undefined = get(linglang_supervisors),
            #{live_cells := 0, root_frames := 0} = linglang_rt:stats(), halt(0)
        catch _:Reason -> io:format("ERROR:~p~n", [Reason]), halt(1) end.`
	output, err := executeScript(t, source, script)
	baseline, baselineErr := executeScriptWithOptions(t, source, script, Options{DisableOptimizations: true})
	if output != baseline || (err != nil) != (baselineErr != nil) {
		t.Fatalf("backend mismatch: %q (%v) versus %q (%v)", output, err, baseline, baselineErr)
	}
	return output, err
}

func TestSupervisionExample(t *testing.T) {
	source, err := os.ReadFile("../../examples/supervision.lang")
	if err != nil {
		t.Fatal(err)
	}
	got, err := executeSupervised(t, strings.TrimPrefix(string(source), "package main"))
	want := "Before crash: 15\nRestarted with new PID: true\nAfter restart: 10\nSupervisor stopped: true\nChild shutdown: shutdown\n"
	if err != nil || got != want {
		t.Fatalf("got %q, %v; want %q", got, err, want)
	}
}

func TestSupervisionPoliciesAndChildLifecycle(t *testing.T) {
	source := `
type Args struct { parent Pid; seed int }
type Ready struct { pid Pid; seed int }
func worker(args Args) {
    send(args.parent, Ready{pid: self(), seed: args.seed})
    cmd := receive[int](-1)
    if cmd.value == 1 { panic("crash") }
}
func ready() Ready {
    result := receive[Ready](5000)
    if !result.ok { panic("missing ready") }
    return result.value
}
func awaitStatus(s Supervisor, name string, reason string) {
    for i := 0; i < 5000; i++ {
        result := lookupChild(s, name)
        if !result.ok && result.reason == reason { return }
        receive[bool](1)
    }
    panic("unexpected child status")
}
func main() {
    s := startSupervisor(5, 5)
    sibling := supervise(s, "sibling", worker, Args{parent: self(), seed: 1}, ChildOptions{})
    ready()
    args := Args{parent: self(), seed: 13}
    permanent := supervise(s, "permanent", worker, args, ChildOptions{})
    args.seed = 99
    ready()
    send(permanent.pid, 0) // permanent also restarts normal returns
    replacement := ready()
    println(replacement.pid != permanent.pid, replacement.seed)
    unchanged := lookupChild(s, "sibling")
    println(unchanged.ok, unchanged.pid == sibling.pid)
    duplicate := supervise(s, "permanent", worker, args, ChildOptions{})
    println(duplicate.ok, duplicate.reason, removeChild(s, "permanent"))
    println(stopChild(s, "permanent"))
    stopped := lookupChild(s, "permanent")
    existing := supervise(s, "permanent", worker, args, ChildOptions{})
    println(stopped.ok, stopped.reason, existing.reason)
    restarted := restartChild(s, "permanent")
    manual := ready()
    println(restarted.ok, manual.seed, manual.pid != replacement.pid)
    running := restartChild(s, "permanent")
    println(running.ok, running.reason)
    stopChild(s, "permanent")
    println(removeChild(s, "permanent"), removeChild(s, "permanent"))

    transient := supervise(s, "transient", worker, args, ChildOptions{restart: "transient"})
    ready()
    send(transient.pid, 0)
    awaitStatus(s, "transient", "stopped")
    manualTransient := restartChild(s, "transient")
    ready()
    send(manualTransient.pid, 1)
    recovered := ready()
    println(recovered.pid != manualTransient.pid)

    temporary := supervise(s, "temporary", worker, args, ChildOptions{restart: "temporary"})
    ready()
    send(temporary.pid, 1)
    awaitStatus(s, "temporary", "not_found")
    missing := restartChild(s, "temporary")
    println(missing.ok, missing.reason, stopChild(s, "temporary"))
    watcher := monitor(supervisorPid(s))
    println(stopSupervisor(s), stopSupervisor(s))
    exited := wait(watcher, 5000)
    println(exited.ok, exited.normal)
}`
	want := "true 13\ntrue true\nfalse already_started false\ntrue\nfalse stopped already_present\ntrue 13 true\nfalse running\ntrue false\ntrue\nfalse not_found false\ntrue false\ntrue true\n"
	got, err := executeSupervised(t, source)
	if err != nil || got != want {
		t.Fatalf("got %q, %v; want %q", got, err, want)
	}
}

func TestSupervisionRestartLimitEscalatesAndStopsSiblings(t *testing.T) {
	source := `
type Attempt struct {}
func crash(parent Pid) { send(parent, Attempt{}); panic("again") }
func sibling(parent Pid) { receive[int](-1) }
func owner(parent Pid) {
    s := startSupervisor(2, 5)
    stable := supervise(s, "stable", sibling, parent, ChildOptions{})
    send(parent, stable.pid)
    receive[int](-1)
    supervise(s, "crash", crash, parent, ChildOptions{})
    receive[int](-1)
}
func main() {
    parent := spawnMonitor(owner, self())
    stable := receive[Pid](5000)
    if !stable.ok { panic("no sibling") }
    watcher := monitor(stable.value)
    send(parent.pid, 1)
    failed := wait(parent.monitor, 5000)
    stopped := wait(watcher, 5000)
    println(failed.ok, failed.normal, failed.reason)
    println(stopped.ok, stopped.reason)
    for i := 0; i < 3; i++ {
        attempt := receive[Attempt](5000)
        if !attempt.ok { panic("missing attempt") }
    }
    extra := receive[Attempt](0)
    println("three attempts:", !extra.ok)
}`
	got, err := executeSupervised(t, source)
	want := "true false shutdown\ntrue shutdown\nthree attempts: true\n"
	if err != nil || got != want {
		t.Fatalf("got %q, %v; want %q", got, err, want)
	}
}

func TestSupervisionOwnerReturnAndExceptionCleanUp(t *testing.T) {
	source := `
type Args struct { parent Pid; fail bool }
func idle(n int) { receive[int](-1) }
func owner(args Args) {
    s := startSupervisor(3, 5)
    child := supervise(s, "idle", idle, 0, ChildOptions{})
    send(args.parent, child.pid)
    receive[int](-1)
    if args.fail { panic("owner failure") }
}
func check(fail bool) {
    p := spawnMonitor(owner, Args{parent: self(), fail: fail})
    child := receive[Pid](5000)
    if !child.ok { panic("no child") }
    watcher := monitor(child.value)
    send(p.pid, 1)
    result := wait(p.monitor, 5000)
    stopped := wait(watcher, 0)
    println(result.ok, result.normal, stopped.ok, stopped.reason)
}
func main() { check(false); check(true) }
`
	got, err := executeSupervised(t, source)
	want := "true true true shutdown\ntrue false true shutdown\n"
	if err != nil || got != want {
		t.Fatalf("got %q, %v; want %q", got, err, want)
	}
}

func TestSupervisionRuntimeErrors(t *testing.T) {
	tests := []struct{ name, body, want string }{
		{"negative_restarts", `startSupervisor(-1, 5)`, "linglang_invalid_restart_limit"},
		{"invalid_window", `startSupervisor(3, 0)`, "linglang_invalid_restart_limit"},
		{"invalid_policy", `s := startSupervisor(3, 5); supervise(s, "x", worker, 0, ChildOptions{restart: "forever"})`, "linglang_invalid_restart_policy"},
		{"invalid_deadline", `s := startSupervisor(3, 5); supervise(s, "x", worker, 0, ChildOptions{shutdownMillis: -1})`, "linglang_invalid_shutdown_timeout"},
		{"empty_name", `s := startSupervisor(3, 5); supervise(s, "", worker, 0, ChildOptions{})`, "linglang_invalid_child_name"},
		{"nil_handle", `var s Supervisor; lookupChild(s, "x")`, "linglang_invalid_supervisor"},
		{"stopped_handle", `s := startSupervisor(3, 5); stopSupervisor(s); lookupChild(s, "x")`, "linglang_inactive_supervisor"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := "func worker(n int) {}\nfunc main() {" + test.body + "}"
			got, err := executeSupervised(t, source)
			if err == nil || got != "ERROR:"+test.want+"\n" {
				t.Fatalf("got %q, %v", got, err)
			}
		})
	}
}

func TestSupervisionCompileRejections(t *testing.T) {
	tests := []struct{ name, source, want string }{
		{"pointer_argument", `func worker(p *int) {}; func main() { s := startSupervisor(3, 5); n := 1; supervise(s, "x", worker, &n, ChildOptions{}) }`, "process-local pointers"},
		{"nested_pointer", `type Arg struct { p *int }; func worker(a Arg) {}; func main() { s := startSupervisor(3, 5); supervise(s, "x", worker, Arg{}, ChildOptions{}) }`, "field p: process-local pointers"},
		{"monitor_argument", `func worker(m Monitor) {}; func main() { s := startSupervisor(3, 5); supervise(s, "x", worker, monitor(self()), ChildOptions{}) }`, "monitor handles belong"},
		{"supervisor_argument", `func worker(s Supervisor) {}; func main() { s := startSupervisor(3, 5); supervise(s, "x", worker, s, ChildOptions{}) }`, "supervisor handles belong"},
		{"supervisor_message", `type Arg struct { s Supervisor }; func main() { send(self(), Arg{}) }`, "field s: supervisor handles belong"},
		{"supervisor_receive", `func main() { receive[Supervisor](0) }`, "supervisor handles belong"},
		{"closure", `func main() { s := startSupervisor(3, 5); supervise(s, "x", func(n int) {}, 1, ChildOptions{}) }`, "named language function"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, opts := range []Options{{}, {DisableOptimizations: true}} {
				_, err := CompileWithOptions("bad.lang", []byte("package main\n"+test.source), opts)
				if err == nil || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("got %v; want %q", err, test.want)
				}
			}
		})
	}
}

func TestOTPSupervisionLinksDeadlinesAndOwnership(t *testing.T) {
	script := `
    logger:set_primary_config(level, emergency),
    Parent = self(),
    Options = #{field_72657374617274 => <<>>, field_73687574646f776e4d696c6c6973 => 20},
    {Owner, OwnerRef} = spawn_monitor(fun() ->
        S = linglang_sup:start(3, 5),
        #{field_706964 := Child} = linglang_sup:add(S, <<"child">>,
            fun(P) -> P ! {ready, self()}, receive stop -> ok end end, Parent, pid, Options),
        Parent ! {owned, S, Child},
        receive stop -> ok end
    end),
    {S, Child} = receive {owned, H, C} -> {H, C} after 5000 -> error(owner_timeout) end,
    {linglang_supervisor, Owner, Sup} = S,
    [{specs, 1}, {active, 1}, {supervisors, 0}, {workers, 1}] = supervisor:count_children(Sup),
    try linglang_sup:stop(S), error(expected_ownership_failure)
    catch error:linglang_cross_process_supervisor -> ok end,
    SupRef = monitor(process, Sup), ChildRef = monitor(process, Child),
    exit(Owner, kill),
    receive {'DOWN', OwnerRef, process, Owner, killed} -> ok after 5000 -> error(owner_alive) end,
    receive {'DOWN', SupRef, process, Sup, killed} -> ok after 5000 -> error(supervisor_alive) end,
    receive {'DOWN', ChildRef, process, Child, shutdown} -> ok after 5000 -> error(child_alive) end,

    Local = linglang_sup:start(3, 5),
    #{field_706964 := Stubborn} = linglang_sup:add(Local, <<"stubborn">>,
        fun(P) -> process_flag(trap_exit, true), P ! stubborn_ready,
                  receive never -> ok end end, Parent, pid, Options),
    receive stubborn_ready -> ok after 5000 -> error(not_ready) end,
    StubbornRef = monitor(process, Stubborn),
    true = linglang_sup:stop(Local),
    receive {'DOWN', StubbornRef, process, Stubborn, killed} -> ok after 5000 -> error(deadline_ignored) end,
    [] = get(linglang_supervisors),
    io:format("ok~n"), halt(0).`
	got, err := executeScript(t, "func main() {}", script)
	if err != nil || got != "ok\n" {
		t.Fatalf("got %q, %v", got, err)
	}
}
