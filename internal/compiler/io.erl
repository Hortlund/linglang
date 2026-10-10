%% Typed, bounded adapters. Foreign runtime terms never escape these records.
-module(linglang_io).
-include_lib("kernel/include/file.hrl").
-export([sha256/1, path_info/1, read_directory/1, canonical_path/1, make_directories/1, write_new_file/2, replace_file/3]).
-export([tcp_listen/2, tcp_connect/3, tcp_accept/2, tcp_port/1,
         tcp_read/3, tcp_write/3, tcp_close/1, finish/0, sqlite_query/4]).

value(Value, OK, Reason) -> #{field_76616c7565 => Value, field_6f6b => OK, field_726561736f6e => Reason}.
io(ok) -> #{field_6f6b => true, field_726561736f6e => <<>>};
io({error, Reason}) -> #{field_6f6b => false, field_726561736f6e => reason(Reason)}.
reason(R) when is_atom(R) -> atom_to_binary(R);
reason(R) when is_binary(R) -> R;
reason(R) -> iolist_to_binary(io_lib:format("~tp", [R])).

socket({ok, Socket}) ->
    Ref = make_ref(), put({linglang_socket, Ref}, Socket),
    value({linglang_socket, self(), Ref}, true, <<>>);
socket({error, Reason}) -> value(nil, false, reason(Reason)).
owned({linglang_socket, Owner, Ref}) when Owner =:= self() ->
    case get({linglang_socket, Ref}) of undefined -> erlang:error(linglang_closed_socket); S -> S end;
owned(_) -> erlang:error(linglang_invalid_socket_owner).

tcp_listen(Host, Port) when Port >= 0, Port =< 65535 ->
    case inet:parse_address(binary_to_list(Host)) of
        {ok, IP} ->
            Family = case tuple_size(IP) of 8 -> inet6; _ -> inet end,
            socket(gen_tcp:listen(Port, [Family, binary, {ip, IP}, {active, false}, {reuseaddr, true}, {packet, 0}]));
        {error, R} -> socket({error, R})
    end;
tcp_listen(_, _) -> socket({error, invalid_port}).

tcp_connect(Host, Port, Milliseconds) when Port > 0, Port =< 65535 ->
    Timeout = linglang_rt:timeout(Milliseconds),
    socket(gen_tcp:connect(binary_to_list(Host), Port,
        [binary, {active, false}, {packet, 0}, {send_timeout_close, true}], Timeout));
tcp_connect(_, _, _) -> socket({error, invalid_port}).
tcp_accept(Listener, Milliseconds) -> socket(gen_tcp:accept(owned(Listener), linglang_rt:timeout(Milliseconds))).
tcp_port(Socket) ->
    case inet:port(owned(Socket)) of {ok, Port} -> value(Port, true, <<>>); {error, R} -> value(0, false, reason(R)) end.
tcp_read(Socket, Size, Milliseconds) when Size >= 0, Size =< 1048576 ->
    case gen_tcp:recv(owned(Socket), Size, linglang_rt:timeout(Milliseconds)) of
        {ok, Data} -> value(Data, true, <<>>); {error, R} -> value(<<>>, false, reason(R))
    end;
tcp_read(_, _, _) -> value(<<>>, false, <<"invalid_read_size">>).
tcp_write(Socket, Bytes, Milliseconds) ->
    S = owned(Socket),
    case inet:setopts(S, [{send_timeout, linglang_rt:timeout(Milliseconds)}, {send_timeout_close, true}]) of
        ok -> io(gen_tcp:send(S, Bytes)); Error -> io(Error)
    end.
tcp_close({linglang_socket, Owner, Ref}) when Owner =:= self() ->
    case erase({linglang_socket, Ref}) of undefined -> false; S -> gen_tcp:close(S), true end;
tcp_close(_) -> erlang:error(linglang_invalid_socket_owner).
finish() ->
    lists:foreach(fun
        ({{linglang_socket, Ref}, S}) -> erase({linglang_socket, Ref}), gen_tcp:close(S);
        (_) -> ok
    end, get()), ok.

%% A small native port binds values with SQLite's C API. No shell, SQL quoting,
%% dynamic atoms, or arbitrary Erlang terms are involved in the protocol.
sqlite_query(Path, SQL, Params, Milliseconds) ->
    Timeout = linglang_rt:timeout(Milliseconds),
    case os:find_executable("linglang-sqlite") of
        false -> sql_error(<<"linglang-sqlite is required on PATH (run make sqlite)">>);
        Executable ->
            Values = case Params of nil -> []; _ -> Params end,
            Limit = case Timeout of infinity -> 0; _ -> Timeout end,
            Input = iolist_to_binary([<<Limit:32/unsigned-big>>, field(Path), field(SQL), <<(length(Values)):32/unsigned-big>>,
                                     [[field(maps:get(field_6b696e64, V)), field(maps:get(field_76616c7565, V))] || V <- Values]]),
            if byte_size(Input) > 8388608 -> sql_error(<<"SQLite input exceeds 8 MiB">>);
               true -> sqlite_port(Executable, Input, Timeout)
            end
    end.
field(B) -> [<<(byte_size(B)):32/unsigned-big>>, B].
sqlite_port(Executable, Input, Timeout) ->
    Port = open_port({spawn_executable, Executable}, [binary, use_stdio, exit_status, {args, []}]),
    try
        true = port_command(Port, Input),
        Deadline = case Timeout of infinity -> infinity; _ -> erlang:monotonic_time(millisecond) + Timeout end,
        sqlite_output(Port, Deadline, [], 0)
    after try port_close(Port) catch _:_ -> ok end end.
sqlite_output(Port, Deadline, Parts, Size) ->
    Wait = case Deadline of infinity -> infinity; _ -> max(0, Deadline - erlang:monotonic_time(millisecond)) end,
    receive
        {Port, {data, Data}} when Size + byte_size(Data) =< 8388608 -> sqlite_output(Port, Deadline, [Data | Parts], Size + byte_size(Data));
        {Port, {data, _}} -> sql_error(<<"SQLite output exceeds 8 MiB">>);
        {Port, {exit_status, 0}} ->
            try sql_decode(iolist_to_binary(lists:reverse(Parts)))
            catch _:_ -> sql_error(<<"invalid SQLite helper response">>) end;
        {Port, {exit_status, 124}} -> sql_error(<<"timeout">>);
        {Port, {exit_status, _}} -> sql_error(<<"SQLite helper failed">>)
    after Wait -> sql_error(<<"timeout">>)
    end.
sql_error(Reason) -> #{field_6f6b => false, field_726561736f6e => Reason, field_636f6c756d6e73 => [], field_726f7773 => [], field_6368616e676573 => 0}.
sql_decode(<<0, Rest/binary>>) -> {Reason, <<>>} = sql_field(Rest), sql_error(Reason);
sql_decode(<<1, Count:32, Rest/binary>>) ->
    {Columns, RowsData} = sql_fields(Count, Rest, []),
    {Rows, Changes} = sql_rows(RowsData, Count, []),
    #{field_6f6b => true, field_726561736f6e => <<>>, field_636f6c756d6e73 => Columns,
      field_726f7773 => Rows, field_6368616e676573 => Changes}.
sql_field(<<Size:32, Value:Size/binary, Rest/binary>>) -> {Value, Rest}.
sql_fields(0, Rest, Acc) -> {lists:reverse(Acc), Rest};
sql_fields(N, Bytes, Acc) -> {V, Rest} = sql_field(Bytes), sql_fields(N-1, Rest, [V | Acc]).
sql_rows(<<0, Changes:64/signed>>, _, Acc) -> {lists:reverse(Acc), Changes};
sql_rows(<<1, Rest/binary>>, Count, Acc) -> {Row, More} = sql_cells(Count, Rest, []), sql_rows(More, Count, [Row | Acc]).
sql_cells(0, Rest, Acc) -> {lists:reverse(Acc), Rest};
sql_cells(N, <<Kind, Bytes/binary>>, Acc) ->
    {Value, Rest} = sql_field(Bytes),
    Name = case Kind of 0 -> <<"null">>; 1 -> <<"int">>; 2 -> <<"float">>; 3 -> <<"text">>; 4 -> <<"blob">> end,
    Cell = #{field_6b696e64 => Name, field_76616c7565 => Value},
    sql_cells(N-1, Rest, [Cell | Acc]).

%% Filesystem primitives for language-owned developer tools. lstat deliberately
%% exposes symlinks: recursive traversal policy belongs to the Linglang caller.
path_info(Path) ->
    case file:read_link_info(Path) of
        {ok, #file_info{type = Type}} ->
            #{field_6b696e64 => atom_to_binary(Type), field_6f6b => true, field_726561736f6e => <<>>};
        {error, R} -> #{field_6b696e64 => <<>>, field_6f6b => false, field_726561736f6e => reason(R)}
    end.
read_directory(Path) ->
    case file:list_dir(Path) of
        {ok, Names} -> value(lists:sort([unicode:characters_to_binary(N) || N <- Names]), true, <<>>);
        {error, R} -> value([], false, reason(R))
    end.
canonical_path(Path) ->
    try value(resolve_path(filename:split(filename:absname(Path)), <<"/">>, 0), true, <<>>)
    catch throw:{path_error, R} -> value(<<>>, false, reason(R)) end.
resolve_path(_, _, N) when N > 40 -> throw({path_error, eloop});
resolve_path([], Base, _) -> Base;
resolve_path([<<"/">> | Rest], _, N) -> resolve_path(Rest, <<"/">>, N);
resolve_path([<<".">> | Rest], Base, N) -> resolve_path(Rest, Base, N);
resolve_path([<<"..">> | Rest], Base, N) -> resolve_path(Rest, filename:dirname(Base), N);
resolve_path([Part | Rest], Base, N) ->
    Next = filename:join(Base, Part),
    case file:read_link_info(Next) of
        {ok, #file_info{type = symlink}} ->
            case file:read_link(Next) of
                {ok, Target} -> resolve_path(filename:split(Target) ++ Rest, Base, N + 1);
                {error, R} -> throw({path_error, R})
            end;
        {ok, #file_info{type = directory}} -> resolve_path(Rest, Next, N);
        {ok, _} when Rest =:= [] -> Next;
        {ok, _} -> throw({path_error, enotdir});
        {error, R} -> throw({path_error, R})
    end.
make_directories(Path) -> io(filelib:ensure_dir(filename:join(Path, <<".linglang-directory">>))).
%% Publish complete bytes exclusively. Hard-link publication is atomic and
%% fails if any file/link already occupies Path; cleanup never removes Path.
write_new_file(Path, Text) ->
    try
        {Temp, F} = replacement_temp(filename:dirname(Path)),
        try
            {ok, #file_info{mode = Mode}} = file:read_file_info(Temp),
            ok = file:change_mode(Temp, 8#600),
            ok = file:write(F, Text), ok = file:close(F),
            ok = file:change_mode(Temp, Mode band 8#777),
            io(file:make_link(Temp, Path))
        after file:close(F), file:delete(Temp) end
    catch error:{badmatch, {error, R}} -> io({error, R}) end.

sha256(Text) ->
    list_to_binary(lists:flatten([io_lib:format("~2.16.0b", [B]) || <<B>> <= crypto:hash(sha256, Text)])).

replace_file(Path, Expected, Text) ->
    %% Require a resolved regular path. Rename replaces one complete file and
    %% preserves permissions; compare again before publishing to detect edits.
    try
        {ok, #file_info{type = regular, mode = Mode}} = file:read_link_info(Path),
        {ok, Expected} = file:read_file(Path),
        {Temp, F} = replacement_temp(filename:dirname(Path)),
        try
            %% Keep new contents private until the original permissions are set.
            ok = file:change_mode(Temp, 8#600),
            ok = file:write(F, Text), ok = file:close(F),
            ok = file:change_mode(Temp, Mode band 8#777),
            {ok, Expected} = file:read_file(Path),
            ok = file:rename(Temp, Path), io(ok)
        after file:close(F), file:delete(Temp) end
    catch error:{badmatch, {error, R}} -> io({error, R});
          error:{badmatch, _} -> io({error, <<"source changed or is not a regular file">>}) end.
replacement_temp(Dir) ->
    Name = list_to_binary(".linglang-fmt-" ++ os:getpid() ++ "-" ++ integer_to_list(erlang:unique_integer([positive]))),
    Path = filename:join(Dir, Name),
    case file:open(Path, [write, binary, exclusive]) of
        {ok, F} -> {Path, F};
        {error, eexist} -> replacement_temp(Dir);
        Error -> error({badmatch, Error})
    end.
