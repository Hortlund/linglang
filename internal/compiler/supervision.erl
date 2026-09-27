%% Language ownership/API adapter. Restart decisions and child lifecycle belong
%% to OTP's supervisor behaviour, not a second implementation of supervision.
-module(linglang_sup).
-behaviour(supervisor).
-export([start/2, pid/1, add/6, lookup/2, restart/2,
         stop_child/2, remove_child/2, stop/1, finish/0]).
-export([init/1, worker_start_link/4, worker_init/5]).

start(MaxRestarts, WithinSeconds)
  when is_integer(MaxRestarts), MaxRestarts >= 0, MaxRestarts =< 1000000,
       is_integer(WithinSeconds), WithinSeconds > 0, WithinSeconds =< 2147483647 ->
    {ok, Pid} = supervisor:start_link(?MODULE, {MaxRestarts, WithinSeconds}),
    put(linglang_supervisors, [Pid | owned()]),
    {linglang_supervisor, self(), Pid};
start(_, _) -> erlang:error(linglang_invalid_restart_limit).

init({MaxRestarts, WithinSeconds}) ->
    {ok, {#{strategy => one_for_one, intensity => MaxRestarts,
            period => WithinSeconds}, []}}.

pid({linglang_supervisor, Owner, Pid}) when Owner =:= self(), is_pid(Pid) -> Pid;
pid({linglang_supervisor, _, _}) -> erlang:error(linglang_cross_process_supervisor);
pid(_) -> erlang:error(linglang_invalid_supervisor).

active(Handle) ->
    Pid = pid(Handle),
    case lists:member(Pid, owned()) andalso is_process_alive(Pid) of
        true -> Pid;
        false -> erlang:error(linglang_inactive_supervisor)
    end.

name(Name) when is_binary(Name), byte_size(Name) > 0 -> Name;
name(_) -> erlang:error(linglang_invalid_child_name).

add(Handle, Name, Worker, Arg, Schema, Options) ->
    Pid = active(Handle),
    Id = name(Name),
    linglang_rt:validate_message(Schema, Arg),
    Restart = restart_policy(maps:get(field_72657374617274, Options)),
    Shutdown = shutdown_timeout(maps:get(field_73687574646f776e4d696c6c6973, Options)),
    Spec = #{id => Id,
             start => {?MODULE, worker_start_link, [Worker, Arg, Schema, linglang_rt:gc_stress()]},
             restart => Restart, shutdown => Shutdown, type => worker,
             modules => [linglang_program]},
    child_result(supervisor:start_child(Pid, Spec)).

restart_policy(<<>>) -> permanent;
restart_policy(<<"permanent">>) -> permanent;
restart_policy(<<"transient">>) -> transient;
restart_policy(<<"temporary">>) -> temporary;
restart_policy(_) -> erlang:error(linglang_invalid_restart_policy).

shutdown_timeout(0) -> 5000;
shutdown_timeout(Value) when is_integer(Value), Value > 0, Value =< 2147483647 -> Value;
shutdown_timeout(_) -> erlang:error(linglang_invalid_shutdown_timeout).

%% Only runtime initialization is acknowledged here. The application protocol
%% must signal its own readiness after completing user initialization.
worker_start_link(Worker, Arg, Schema, Stress) ->
    proc_lib:start_link(?MODULE, worker_init, [self(), Worker, Arg, Schema, Stress]).

worker_init(Parent, Worker, Arg, Schema, Stress) ->
    linglang_rt:validate_message(Schema, Arg),
    linglang_rt:set_gc_stress(Stress),
    proc_lib:init_ack(Parent, {ok, self()}),
    linglang_rt:run_worker(Worker, Arg, Stress).

lookup(Handle, Name) ->
    Pid = active(Handle),
    case lists:keyfind(name(Name), 1, supervisor:which_children(Pid)) of
        {_, Child, _, _} when is_pid(Child) -> child_result({ok, Child});
        {_, restarting, _, _} -> child_result({error, restarting});
        {_, undefined, _, _} -> child_result({error, stopped});
        false -> child_result({error, not_found})
    end.

restart(Handle, Name) ->
    child_result(supervisor:restart_child(active(Handle), name(Name))).

stop_child(Handle, Name) ->
    case supervisor:terminate_child(active(Handle), name(Name)) of
        ok -> true;
        {error, not_found} -> false
    end.

remove_child(Handle, Name) ->
    case supervisor:delete_child(active(Handle), name(Name)) of
        ok -> true;
        {error, not_found} -> false;
        {error, running} -> false;
        {error, restarting} -> false
    end.

child_result({ok, Pid}) -> #{field_706964 => Pid, field_6f6b => true, field_726561736f6e => <<>>};
child_result({ok, Pid, _}) -> child_result({ok, Pid});
child_result({error, {already_started, _}}) -> child_result({error, already_started});
child_result({error, Reason}) ->
    Text = case is_atom(Reason) of
        true -> atom_to_binary(Reason, utf8);
        false -> iolist_to_binary(io_lib:format("~tp", [Reason]))
    end,
    #{field_706964 => nil, field_6f6b => false, field_726561736f6e => Text}.

stop(Handle) ->
    Pid = pid(Handle),
    case lists:member(Pid, owned()) of
        false -> false;
        true ->
            Stopped = stop_pid(Pid),
            put(linglang_supervisors, lists:delete(Pid, owned())),
            Stopped
    end.

stop_pid(Pid) ->
    %% 'normal' keeps explicit shutdown from killing the linked language owner.
    %% OTP waits for each child, applies its shutdown deadline, then exits.
    try gen_server:stop(Pid, normal, infinity) of
        ok -> true
    catch exit:{noproc, _} -> false
    end.

owned() ->
    case get(linglang_supervisors) of undefined -> []; Pids -> Pids end.

finish() ->
    lists:foreach(fun(Pid) -> stop({linglang_supervisor, self(), Pid}) end, owned()),
    erase(linglang_supervisors),
    ok.
