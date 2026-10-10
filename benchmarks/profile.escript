#!/usr/bin/env escript
%%! +S 1
-mode(compile).

%% OTP profiling glue: checking/lowering is performed by the archive's actual
%% Linglang compiler. No Go, Python, erlc, source rewrite, or rebuilt compiler.
main(Args) ->
    ok = os:set_signal(sigterm, default),
    try
        case options(Args, #{timeout => 120000, no_opt => false}) of
            help -> io:format("Usage: escript benchmarks/profile.escript --compiler ARCHIVE --source DIRECTORY [--no-opt] [--timeout SECONDS]~n"
                              "Prints JSON call-time data for compiler checking/lowering. OTP 29 required. Timings include profiling overhead.~n");
            Options -> io:format("~s~n", [json:encode(profile(Options))])
        end
    catch Class:Reason ->
        io:format(standard_error, "profile: ~p: ~tp~n", [Class, Reason]), halt(1)
    end.

options(["--help"], _) -> help;
options([], #{compiler := _, source := _} = O) -> O;
options(["--compiler", P | Rest], O) -> options(Rest, O#{compiler => filename:absname(P)});
options(["--source", P | Rest], O) -> options(Rest, O#{source => filename:absname(P)});
options(["--no-opt" | Rest], O) -> options(Rest, O#{no_opt => true});
options(["--timeout", S | Rest], O) ->
    N = list_to_integer(S),
    true = N > 0 andalso N =< 2147483,
    options(Rest, O#{timeout => N * 1000});
options(Args, _) -> error({invalid_arguments, Args}).

profile(#{compiler := Compiler, source := Source, timeout := Timeout, no_opt := NoOpt}) ->
    {ok, CompilerBytes} = file:read_file(Compiler),
    {ok, Sections} = escript:extract(Compiler, []),
    {archive, Archive} = lists:keyfind(archive, 1, Sections),
    {ok, Entries} = zip:extract(Archive, [memory]),
    Modules = [linglang_program, linglang_rt, linglang_sup, linglang_server, linglang_io],
    lists:foreach(fun(M) ->
        Name = atom_to_list(M) ++ ".beam",
        {Name, Beam} = lists:keyfind(Name, 1, Entries),
        {module, M} = code:load_binary(M, Name, Beam)
    end, Modules),
    Traced = [linglang_program, linglang_rt],
    MFAs = [{M, F, A} || M <- Traced, {F, A} <- M:module_info(functions)],
    lists:foreach(fun(M) -> erlang:trace_pattern({M, '_', '_'}, true, [call_time]) end, Traced),
    try
        Arguments = [<<"check">>, unicode:characters_to_binary(Source)] ++
                    case NoOpt of true -> [<<"--no-opt">>]; false -> [] end,
        Parent = self(),
        {Pid, Ref} = spawn_monitor(fun() ->
            receive start -> ok end,
            linglang_rt:set_arguments(Arguments),
            {Micros, _} = timer:tc(fun linglang_program:main/0),
            erlang:trace(self(), false, [call]),
            Stats = linglang_rt:stats(),
            #{live_cells := 0, root_frames := 0, root_entries := 0} = Stats,
            Parent ! {self(), Micros, Stats}
        end),
        try
            1 = erlang:trace(Pid, true, [call]),
            Pid ! start,
            receive
                {Pid, Micros, Stats} ->
                    receive {'DOWN', Ref, process, Pid, normal} -> ok end,
                    Rows = lists:flatmap(fun(MFA) -> measurement(MFA, Pid) end, MFAs),
                    Sorted = lists:sort(fun(A, B) ->
                        {-maps:get(microseconds, A), maps:get(module, A), maps:get(function, A), maps:get(arity, A)} <
                        {-maps:get(microseconds, B), maps:get(module, B), maps:get(function, B), maps:get(arity, B)}
                    end, Rows),
                    #{schemaVersion => 1, compiler => unicode:characters_to_binary(Compiler),
                      compiler_sha256 => binary:encode_hex(crypto:hash(sha256, CompilerBytes), lowercase),
                      source => unicode:characters_to_binary(Source), no_opt => NoOpt,
                      otp => list_to_binary(erlang:system_info(otp_release)),
                      schedulers => erlang:system_info(schedulers_online),
                      instrumented_microseconds => Micros, managed => Stats, functions => Sorted};
                {'DOWN', Ref, process, Pid, Reason} -> error({compiler_failed, Reason})
            after Timeout -> error({timeout, Timeout})
            end
        after
            exit(Pid, kill), erlang:demonitor(Ref, [flush])
        end
    after
        lists:foreach(fun(M) -> erlang:trace_pattern({M, '_', '_'}, false, [call_time]) end, Traced)
    end.

measurement({M, F, A} = MFA, Pid) ->
    case erlang:trace_info(MFA, call_time) of
        {call_time, Times} when is_list(Times) ->
            case lists:keyfind(Pid, 1, Times) of
                {Pid, Count, Seconds, Micros} when Count > 0 ->
                    [#{module => atom_to_binary(M), function => atom_to_binary(F), arity => A,
                       source_function => source_name(F), calls => Count, microseconds => Seconds * 1000000 + Micros}];
                _ -> []
            end;
        _ -> []
    end.

source_name(F) ->
    case atom_to_binary(F) of
        <<"f_", Hex/binary>> ->
            try binary:decode_hex(Hex) catch error:badarg -> atom_to_binary(F) end;
        Name -> Name
    end.
