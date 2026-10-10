%% Native OTP server with a process-local, mutable language state cell.
-module(linglang_server).
-behaviour(gen_server).
-export([start_link/6, call/6]).
-export([init/1, handle_call/3, handle_cast/2, handle_info/2, terminate/2]).

start_link(Handler, Initial, StateSchema, RequestSchema, ReplySchema, Stress) ->
    gen_server:start_link(?MODULE, {Handler, Initial, StateSchema, RequestSchema, ReplySchema, Stress}, []).

init({Handler, Initial, StateSchema, RequestSchema, ReplySchema, Stress}) ->
    process_flag(trap_exit, true),
    linglang_rt:validate_message(StateSchema, Initial),
    linglang_rt:set_gc_stress(Stress),
    Cell = linglang_rt:server_state(Initial),
    {ok, {Handler, Cell, RequestSchema, ReplySchema}}.

handle_call({linglang_call, Q, R, Request}, _From, {Handler, Cell, Q, R} = State) ->
    try
        linglang_rt:validate_message(Q, Request),
        Reply = Handler(Cell, Request),
        linglang_rt:validate_message(R, Reply),
        %% Handler scopes have unwound. Only the persistent state remains live.
        linglang_rt:collect(),
        {reply, {linglang_reply, R, Reply}, State}
    catch Class:Reason:Stack ->
        erlang:raise(exit, {linglang_failure, Class, Reason, Stack}, Stack)
    end;
handle_call(_, _From, State) ->
    {reply, {linglang_call_error, <<"protocol_mismatch">>}, State}.

handle_cast(_, State) -> {noreply, State}.
handle_info({'EXIT', _, normal}, State) -> {noreply, State};
handle_info({'EXIT', _, Reason}, State) -> {stop, Reason, State};
handle_info(_, State) -> {noreply, State}.

terminate(_, _) -> linglang_rt:server_finish().

%% OTP owns correlation, monitoring and alias cleanup. receive_response abandons
%% timed-out requests, so their eventual replies cannot fill the caller mailbox.
%% Keep timeout distinct from a server that happens to exit with reason timeout.
call(Pid, Q, Request, R, Milliseconds, Zero) when is_pid(Pid), node(Pid) =:= node() ->
    Timeout = linglang_rt:timeout(Milliseconds),
    linglang_rt:validate_message(Q, Request),
    linglang_rt:collect(),
    case Pid =:= self() of
        true -> result(Zero, false, false, <<"calling_self">>);
        false ->
            Id = gen_server:send_request(Pid, {linglang_call, Q, R, Request}),
            case gen_server:receive_response(Id, Timeout) of
                {reply, {linglang_reply, R, Value}} ->
                    linglang_rt:validate_message(R, Value),
                    result(Value, true, false, <<>>);
                {reply, {linglang_call_error, Reason}} when is_binary(Reason) ->
                    result(Zero, false, false, Reason);
                {reply, _} -> result(Zero, false, false, <<"invalid_reply">>);
                timeout -> result(Zero, false, true, <<"timeout">>);
                {error, {Reason, _}} ->
                    result(Zero, false, false, linglang_rt:exit_reason(Reason))
            end
    end;
call(_, _, _, _, _, _) -> erlang:error(linglang_invalid_pid).

result(Value, OK, TimedOut, Reason) ->
    #{field_76616c7565 => Value, field_6f6b => OK,
      field_74696d65644f7574 => TimedOut, field_726561736f6e => Reason}.
