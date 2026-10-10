#!/usr/bin/env escript
%%! +S 1
-mode(compile).
-include_lib("kernel/include/file.hrl").

%% OTP build glue only: every .lang parse/check/link/lower step is performed by
%% the supplied Linglang compiler and its descendants. No Go or erlc subprocess.
main(Args) ->
    %% OTP's graceful SIGTERM handler exits with status 0 even if a build is
    %% incomplete. Use the OS termination status; child lifetime pipes still close.
    ok = os:set_signal(sigterm, default),
    try
        Root = filename:dirname(filename:dirname(filename:absname(escript:script_name()))),
        Options = options(Args, #{root => Root, seed => "bin/linglang-bootstrap",
                                  output => "bin/linglang-selfhost", upgrade => false, timeout => 900000}),
        case Options of help -> usage(); _ -> bootstrap(Options) end
    catch Class:Reason ->
        io:format(standard_error, "bootstrap: ~p: ~tp~n", [Class, Reason]), halt(1)
    end.

usage() ->
    io:format("Usage: escript tools/bootstrap.escript [--seed executable] [--output executable] [--root repository] [--timeout seconds] [--upgrade-seed]~n"
              "Paths are relative to the repository root. Requires an existing archive compiler and OTP, not Go.~n"
              "Rebuilds runtime sources, verifies B/C emission, runs language suites, then atomically publishes C.~n").

options(["--help"], _) -> help;
options([], O) -> O;
options(["--root", P | Rest], O) -> options(Rest, O#{root => filename:absname(P)});
options(["--upgrade-seed" | Rest], O) -> options(Rest, O#{upgrade => true});
options(["--seed", P | Rest], O) -> options(Rest, O#{seed => P});
options(["--output", P | Rest], O) -> options(Rest, O#{output => P});
options(["--timeout", S | Rest], O) ->
    N = list_to_integer(S),
    true = N > 0 andalso N =< 2147483,
    options(Rest, O#{timeout => N * 1000});
options(Args, _) -> error({invalid_arguments, Args}).

bootstrap(#{root := Root, seed := SeedPath, output := OutputPath, timeout := Timeout, upgrade := Upgrade}) ->
    Seed = filename:absname(SeedPath, Root),
    Output = filename:absname(OutputPath, Root),
    Source = filename:join(Root, "bootstrap/emitter"),
    Suites = ["bootstrap/lexer", "bootstrap/parser", "bootstrap/resolver",
              "bootstrap/checker", "bootstrap/emitter", "tests/concurrency"],
    RuntimePaths = [{linglang_rt, "runtime.erl"}, {linglang_sup, "supervision.erl"},
                    {linglang_server, "server.erl"}, {linglang_io, "io.erl"}],
    %% Protect even test sources, symlink aliases and hardlinks. Publication must
    %% never replace the seed that is needed to recover from a failed rebuild.
    Inputs = [Seed, filename:absname(escript:script_name()), filename:join(Root, "bootstrap/seed-upgrade/developer_tools.lang")] ++
             lists:append([filelib:wildcard(filename:join([Root, Suite, "*.lang"])) || Suite <- Suites]) ++
             [filename:join([Root, "internal/compiler", P]) || {_, P} <- RuntimePaths],
    protect(Output, Inputs),
    Work = workspace(),
    try
        Runtime = [begin
            Path = filename:join([Root, "internal/compiler", P]),
            {M, Binary} = compiled(compile:file(Path, [binary, return_errors, return_warnings]), M),
            {atom_to_list(M) ++ ".beam", Binary}
        end || {M, P} <- RuntimePaths],
        {ok, Sections} = escript:extract(Seed, []),
        {archive, Archive} = lists:keyfind(archive, 1, Sections),
        {ok, SeedEntries} = zip:extract(Archive, [memory]),
        {"linglang_program.beam", SeedBeam} = lists:keyfind("linglang_program.beam", 1, SeedEntries),
        Original = stage(Work, "original", SeedBeam, Runtime),
        A = case Upgrade of
            false -> Original;
            true ->
                %% The replacement is itself Linglang. It avoids invoking newly
                %% introduced natives until the old compiler has compiled their
                %% checker/emitter definitions. All final stages use real sources.
                Core = lists:sort(filelib:wildcard(filename:join(Source, "*.lang"))),
                Paths = [P || P <- Core, filename:basename(P) =/= "developer_tools.lang",
                             filename:basename(P) =/= "dependencies.lang",
                             not lists:suffix("_test.lang", P)] ++
                        [filename:join(Root, "bootstrap/seed-upgrade/developer_tools.lang")],
                Upgraded = run(Original, ["emit" | Paths], Work, "upgrade-seed", Timeout),
                stage(Work, "a", compile_source(Upgraded, linglang_program), Runtime)
        end,
        BSource = run(A, ["emit", Source], Work, "a-to-b", Timeout),
        B = stage(Work, "b", compile_source(BSource, linglang_program), Runtime),
        CSource = run(B, ["emit", Source], Work, "b-to-c", Timeout),
        case BSource =:= CSource of true -> ok; false -> error(compiler_fixed_point_mismatch) end,
        io:format("Fixed point: ~p bytes, SHA-256 ~s~n", [byte_size(CSource), hex(crypto:hash(sha256, CSource))]),
        CBeam = compile_source(CSource, linglang_program),
        C = stage(Work, "c", CBeam, Runtime),
        %% Exercise the generated C, not the seed. Every suite is written in
        %% Linglang and runs through the self-hosted test dispatcher.
        lists:foreach(fun(Suite) ->
            Result = run(C, ["test", "--gc-stress", "--timeout", "120s", filename:join(Root, Suite)],
                         Work, "suite-" ++ filename:basename(Suite), Timeout),
            io:put_chars(Result)
        end, Suites),
        CellResult = run(C, ["test", "--no-opt", "--gc-stress", "--timeout", "120s",
                             filename:join(Root, "tests/concurrency")], Work, "suite-cells", Timeout),
        io:put_chars(CellResult),
        Launcher = compile_source(<<"-module(linglang_cli).\n-export([main/1]).\nmain(Args) -> linglang_rt:bootstrap_main(Args, false, false).\n">>, linglang_cli),
        {ok, {_, Zip}} = zip:create("compiler.zip", [{"linglang_cli.beam", Launcher},
                            {"linglang_program.beam", CBeam} | Runtime], [memory]),
        Executable = <<"#!/usr/bin/env escript\n%% Linglang self-hosted compiler.\n%%! +S 1 -escript main linglang_cli\n", Zip/binary>>,
        protect(Output, Inputs),
        publish(Output, Executable),
        io:format("Verified compiler published: ~ts~n", [Output])
    after file:del_dir_r(Work) end.

compiled({ok, M, Beam, _}, M) -> {M, Beam};
compiled(Result, M) -> error({otp_compilation_failed, M, Result}).

compile_source(Source, Module) ->
    {ok, Tokens, _} = erl_scan:string(unicode:characters_to_list(Source)),
    Forms = forms(Tokens, [], []),
    {Module, Beam} = compiled(compile:forms(Forms, [binary, return_errors, return_warnings]), Module),
    Beam.

forms([], [], Acc) -> lists:reverse(Acc);
forms([{dot, _} = Dot | Rest], Current, Acc) ->
    {ok, Form} = erl_parse:parse_form(lists:reverse([Dot | Current])),
    forms(Rest, [], [Form | Acc]);
forms([T | Rest], Current, Acc) -> forms(Rest, [T | Current], Acc).

stage(Work, Name, Program, Runtime) ->
    Dir = filename:join(Work, Name),
    ok = file:make_dir(Dir),
    lists:foreach(fun({File, Bytes}) -> ok = file:write_file(filename:join(Dir, File), Bytes) end,
                  [{"linglang_program.beam", Program} | Runtime]),
    Dir.

run(Stage, Args, Work, Name, Timeout) ->
    io:format("Running ~s~n", [Name]),
    Erl = case os:find_executable("erl") of false -> error(erl_missing); P -> P end,
    Out = filename:join(Work, Name ++ ".out"),
    %% The existing runtime guard watches fd3, independent of user stdin. The
    %% shell only redirects fd1; all paths and source arguments remain argv.
    Eval = "ok = os:set_signal(sigterm, default), linglang_rt:bootstrap_run_main(init:get_plain_arguments(), false, false, undefined).",
    Port = open_port({spawn_executable, "/bin/sh"}, [binary, exit_status, nouse_stdio,
        {args, ["-c", "out=$1; shift; exec \"$@\" > \"$out\"", "linglang-bootstrap", Out,
                Erl, "+S", "1", "-noshell", "-pa", Stage, "-eval", Eval, "-extra" | Args]}]),
    Status = try receive {Port, {exit_status, S}} -> S after Timeout -> timeout end
             after try port_close(Port) catch _:_ -> ok end end,
    case Status of
        0 -> {ok, Result} = file:read_file(Out), Result;
        _ -> error({stage_failed, Name, Status})
    end.

workspace() ->
    Base = case os:getenv("TMPDIR") of false -> "/tmp"; D -> D end,
    Dir = filename:join(Base, "linglang-bootstrap-" ++ os:getpid() ++ "-" ++ integer_to_list(erlang:unique_integer([positive]))),
    case file:make_dir(Dir) of
        ok -> ok = file:change_mode(Dir, 8#700), Dir;
        {error, eexist} -> workspace();
        Error -> error({workspace_failed, Error})
    end.

protect(Output, Inputs) ->
    lists:foreach(fun(Input) ->
        Same = case {file:read_file_info(Output), file:read_file_info(Input)} of
            {{ok, A}, {ok, B}} -> A#file_info.inode =:= B#file_info.inode andalso
                A#file_info.major_device =:= B#file_info.major_device andalso
                A#file_info.minor_device =:= B#file_info.minor_device;
            _ -> false
        end,
        case Output =:= filename:absname(Input) orelse Same of
            true -> error({refusing_to_overwrite_input, Input}); false -> ok
        end
    end, Inputs).

publish(Output, Bytes) ->
    ok = filelib:ensure_dir(Output),
    Temp = Output ++ ".tmp-" ++ os:getpid() ++ "-" ++ integer_to_list(erlang:unique_integer([positive])),
    {ok, File} = file:open(Temp, [write, binary, exclusive]),
    try
        ok = file:write(File, Bytes), ok = file:close(File),
        ok = file:change_mode(Temp, 8#755), ok = file:rename(Temp, Output)
    after file:close(File), file:delete(Temp) end.

hex(Bytes) -> lists:flatten([io_lib:format("~2.16.0b", [B]) || <<B>> <= Bytes]).
