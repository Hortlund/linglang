%% Process-local managed cells with explicit roots and safe-point collection.
-module(linglang_rt).
-export([new/1, read/1, write/2, field/2, field_value/2, deref/1,
         binary/3, print/2, loop/4, loop_cell/5,
         list_length/1, list_append/2, list_prepend/2, list_head/2,
         list_tail/1, list_first/1, list_more/1,
         map_length/1, map_get/3, map_put/3, map_remove/2,
         read_file/1, write_file/2, text_split/2, text_trim/1,
         parse_int/1, format_int/1, arguments/0,
         scope/1, keep/1, roots/1, safepoint/0, collect/0, stats/0, set_gc_stress/1,
         spawn_process/4, send_message/3, receive_message/3,
         monitor_process/1, wait_process/2, demonitor_process/1,
         validate_message/2, gc_stress/0, run_worker/3, finish_process/0]).

%% Messages are immutable value snapshots, selected by their complete static
%% schema. Validate at both ends, including messages from ordinary Erlang code.
send_message(Pid, Schema, Value) when is_pid(Pid) ->
    validate_message(Schema, Value),
    Pid ! {linglang_message, Schema, Value},
    ok;
send_message(_, _, _) -> erlang:error(linglang_invalid_pid).

receive_message(Schema, Milliseconds, Zero) ->
    Timeout = timeout(Milliseconds),
    %% Callers have rooted live locals and suspended expression operands.
    collect(),
    receive
        {linglang_message, Schema, Value} ->
            validate_message(Schema, Value),
            #{field_76616c7565 => Value, field_6f6b => true}
    after Timeout ->
        #{field_76616c7565 => Zero, field_6f6b => false}
    end.

validate_message(Schema, Value) ->
    case valid_message(Schema, Value) of
        true -> ok;
        false -> erlang:error(linglang_invalid_message)
    end.

valid_message(int, Value) ->
    is_integer(Value) andalso Value >= -(1 bsl 63) andalso Value < (1 bsl 63);
valid_message(bool, Value) -> is_boolean(Value);
valid_message(string, Value) -> is_binary(Value);
valid_message(pid, Value) -> is_pid(Value) orelse Value =:= nil;
valid_message({list, _}, nil) -> true;
valid_message({list, Element}, Value) -> valid_list(Element, Value);
valid_message({map, _, _}, nil) -> true;
valid_message({map, KeyShape, ValueShape}, Value) when is_map(Value) ->
    maps:fold(fun(Key, Item, Valid) ->
        Valid andalso valid_message(KeyShape, Key) andalso valid_message(ValueShape, Item)
    end, true, Value);
valid_message({named, Name, Shape}, Value) when is_binary(Name) -> valid_message(Shape, Value);
valid_message({struct, Fields}, Value) when is_map(Value), is_list(Fields) ->
    map_size(Value) =:= length(Fields) andalso
    lists:all(fun({Key, Shape}) ->
        case maps:find(Key, Value) of
            {ok, Field} -> valid_message(Shape, Field);
            error -> false
        end
    end, Fields);
valid_message(_, _) -> false.

valid_list(_, []) -> true;
valid_list(Element, [Value | Rest]) ->
    valid_message(Element, Value) andalso valid_list(Element, Rest);
valid_list(_, _) -> false.

timeout(-1) -> infinity;
timeout(Milliseconds) when is_integer(Milliseconds), Milliseconds >= 0,
                           Milliseconds =< 2147483647 -> Milliseconds;
timeout(_) -> erlang:error(linglang_invalid_timeout).

spawn_process(Worker, Arg, Schema, Monitored) ->
    validate_message(Schema, Arg),
    Stress = state(stress, false),
    Run = fun() -> run_worker(Worker, Arg, Stress) end,
    case Monitored of
        false -> erlang:spawn(Run);
        true ->
            {Pid, Ref} = erlang:spawn_monitor(Run),
            #{field_706964 => Pid, field_6d6f6e69746f72 => register_monitor(Pid, Ref)}
    end.

gc_stress() -> state(stress, false).

run_worker(Worker, Arg, Stress) ->
    set_gc_stress(Stress),
    try Worker(Arg)
    catch Class:Reason:Stack -> exit({linglang_failure, Class, Reason, Stack})
    after finish_process()
    end.

%% Normal returns and language exceptions synchronously close owned supervisors.
%% Untrappable exits use OTP links instead; the owning BEAM process is reclaimed.
finish_process() ->
    try linglang_sup:finish()
    after collect()
    end.

monitor_process(Pid) when is_pid(Pid) ->
    register_monitor(Pid, erlang:monitor(process, Pid));
monitor_process(_) -> erlang:error(linglang_invalid_pid).

register_monitor(Pid, Ref) ->
    put({linglang_monitor, Ref}, Pid),
    {linglang_monitor, self(), Ref, Pid}.

monitor_identity({linglang_monitor, Owner, Ref, Pid}) when Owner =:= self() -> {Ref, Pid};
monitor_identity({linglang_monitor, _, _, _}) -> erlang:error(linglang_cross_process_monitor);
monitor_identity(_) -> erlang:error(linglang_invalid_monitor).

wait_process(Monitor, Milliseconds) ->
    {Ref, Pid} = monitor_identity(Monitor),
    case get({linglang_monitor, Ref}) of
        Pid -> ok;
        _ -> erlang:error(linglang_inactive_monitor)
    end,
    Timeout = timeout(Milliseconds),
    collect(),
    receive
        {'DOWN', Ref, process, Pid, Reason} ->
            erase({linglang_monitor, Ref}),
            #{field_706964 => Pid, field_6f6b => true,
              field_6e6f726d616c => Reason =:= normal, field_726561736f6e => exit_reason(Reason)}
    after Timeout ->
        #{field_706964 => Pid, field_6f6b => false,
          field_6e6f726d616c => false, field_726561736f6e => <<>>}
    end.

demonitor_process(Monitor) ->
    {Ref, Pid} = monitor_identity(Monitor),
    case erase({linglang_monitor, Ref}) of
        Pid -> erlang:demonitor(Ref, [flush]), true;
        _ -> false
    end.

exit_reason({linglang_failure, _, {linglang_panic, Reason}, _}) when is_binary(Reason) -> Reason;
exit_reason({linglang_failure, _, Reason, _}) -> exit_reason(Reason);
exit_reason(Reason) when is_atom(Reason) -> atom_to_binary(Reason, utf8);
exit_reason(Reason) -> iolist_to_binary(io_lib:format("~tp", [Reason])).

%% Frames describe language roots; BEAM's stack alone cannot root cells stored
%% behind our managed handles. Cleanup must run on return, break, and exceptions.
%% Neither scope exit nor allocation may collect: a returned value can be between
%% frames until the caller immediately stores or roots it.
scope(Fun) ->
    put({linglang_gc, frames}, [[] | state(frames, [])]),
    try Fun()
    after
        [_ | Rest] = state(frames, []),
        put({linglang_gc, frames}, Rest)
    end.

keep(Value) ->
    case state(frames, []) of
        [Top | Rest] ->
            put({linglang_gc, frames}, [[Value | Top] | Rest]);
        [] ->
            case references(Value, []) of
                [] -> ok;
                _ -> erlang:error(linglang_missing_root_scope)
            end
    end,
    Value.

%% Optimized functions replace their root snapshot at block boundaries. Values
%% are rooted before collection; expression scopes protect suspended operands.
%% Retain values without traversing them: scanning every remaining list tail at
%% each block would turn a linear range loop into quadratic work. Managed pointer
%% discovery is deferred until collection (or an explicit statistics request).
roots(Values) ->
    [ _ | Rest ] = state(frames, []),
    put({linglang_gc, frames}, [Values | Rest]),
    ok.

new(Value) ->
    case state(frames, []) of
        [] -> erlang:error(linglang_missing_root_scope);
        _ -> ok
    end,
    Id = make_ref(),
    put({linglang_cell, Id}, Value),
    put({linglang_gc, cells}, [Id | state(cells, [])]),
    Live = increment(live_cells, 1),
    increment(allocated_cells, 1),
    increment(since_collection, 1),
    put({linglang_gc, peak_live_cells}, max(Live, state(peak_live_cells, 0))),
    keep({linglang_ptr, self(), Id, []}).

safepoint() ->
    case state(stress, false) orelse state(since_collection, 0) >= state(budget, 256) of
        true -> collect();
        false -> ok
    end.

collect() ->
    Roots = lists:foldl(fun(Frame, Acc) -> maps:merge(root_ids(Frame), Acc) end,
                        #{}, state(frames, [])),
    Marked = mark(maps:keys(Roots), #{}),
    {Alive, Freed} = lists:foldl(fun(Id, {Acc, Count}) ->
        case is_map_key(Id, Marked) of
            true -> {[Id | Acc], Count};
            false -> erase({linglang_cell, Id}), {Acc, Count + 1}
        end
    end, {[], 0}, state(cells, [])),
    Live = length(Alive),
    put({linglang_gc, cells}, Alive),
    put({linglang_gc, live_cells}, Live),
    put({linglang_gc, since_collection}, 0),
    %% Allow proportional growth when a program intentionally retains many cells.
    put({linglang_gc, budget}, max(256, Live)),
    increment(collections, 1),
    increment(reclaimed_cells, Freed),
    ok.

mark([], Seen) -> Seen;
mark([Id | Rest], Seen) ->
    case is_map_key(Id, Seen) of
        true -> mark(Rest, Seen);
        false ->
            case get({linglang_cell, Id}) of
                undefined -> erlang:error(linglang_invalid_root);
                Value -> mark(references(Value, Rest), Seen#{Id => true})
            end
    end.

%% A field reference roots its entire cell. Struct snapshots can contain pointers
%% even when the original struct variable has since been overwritten.
references({linglang_ptr, Owner, Id, _}, Acc) when Owner =:= self() -> [Id | Acc];
references(Value, Acc) when is_map(Value) ->
    maps:fold(fun(_, Field, Refs) -> references(Field, Refs) end, Acc, Value);
references([Head | Tail], Acc) -> references(Tail, references(Head, Acc));
references(_, Acc) -> Acc.

root_ids(Values) ->
    maps:from_keys(lists:foldl(fun references/2, [], Values), true).

%% List[T] values are native immutable BEAM lists. The nil zero value is kept
%% distinct from an empty literal, but both behave as empty in list operations.
list_items(nil) -> [];
list_items(Items) when is_list(Items) -> Items.

list_length(Items) -> length(list_items(Items)).
list_append(Items, More) ->
    case list_items(More) of
        [] -> Items;
        Values -> list_items(Items) ++ Values
    end.
list_prepend(Value, Items) -> [Value | list_items(Items)].
list_head(Items, Zero) ->
    case list_items(Items) of
        [] -> #{field_76616c7565 => Zero, field_6f6b => false};
        [Value | _] -> #{field_76616c7565 => Value, field_6f6b => true}
    end.
list_tail(nil) -> nil;
list_tail([]) -> [];
list_tail([_ | Rest]) -> Rest.
list_first([Value | _]) -> Value.
list_more(nil) -> false;
list_more([]) -> false;
list_more([_ | _]) -> true.

map_items(nil) -> #{};
map_items(Items) when is_map(Items) -> Items.
map_length(Items) -> map_size(map_items(Items)).
map_get(Items, Key, Zero) ->
    case maps:find(Key, map_items(Items)) of
        {ok, Value} -> #{field_76616c7565 => Value, field_6f6b => true};
        error -> #{field_76616c7565 => Zero, field_6f6b => false}
    end.
map_put(Items, Key, Value) -> (map_items(Items))#{Key => Value}.
map_remove(nil, _) -> nil;
map_remove(Items, Key) -> maps:remove(Key, Items).

%% Strings are byte-preserving binaries. File failures return explicit results;
%% decimal parsing uses the language's signed 64-bit range, never wrapping input.
read_file(Path) ->
    case file:read_file(Path) of
        {ok, Text} -> value_result(Text, true, <<>>);
        {error, Reason} -> value_result(<<>>, false, atom_to_binary(Reason, utf8))
    end.

write_file(Path, Text) ->
    case file:write_file(Path, Text) of
        ok -> io_result(true, <<>>);
        {error, Reason} -> io_result(false, atom_to_binary(Reason, utf8))
    end.

text_split(_, <<>>) -> erlang:error(linglang_empty_separator);
text_split(Text, Separator) -> binary:split(Text, Separator, [global]).

%% ASCII whitespace is deliberate: arbitrary file bytes need not be valid UTF-8.
text_trim(Text) ->
    trim_end(trim_start(Text)).

trim_start(<<C, Rest/binary>>) when C =:= 32; C >= 9, C =< 13 -> trim_start(Rest);
trim_start(Text) -> Text.

trim_end(<<>>) -> <<>>;
trim_end(Text) ->
    Last = byte_size(Text) - 1,
    case binary:at(Text, Last) of
        C when C =:= 32; C >= 9, C =< 13 -> trim_end(binary:part(Text, 0, Last));
        _ -> Text
    end.

parse_int(<<$-, Digits/binary>>) -> parse_decimal(Digits, -1, 1 bsl 63);
parse_int(<<$+, Digits/binary>>) -> parse_decimal(Digits, 1, (1 bsl 63) - 1);
parse_int(Digits) -> parse_decimal(Digits, 1, (1 bsl 63) - 1).

parse_decimal(<<>>, _, _) -> value_result(0, false, <<"invalid integer">>);
parse_decimal(Digits, Sign, Limit) ->
    case decimal_digits(Digits, 0, Limit) of
        {ok, Value} -> value_result(Sign * Value, true, <<>>);
        {error, Reason} -> value_result(0, false, Reason)
    end.

%% Bound the accumulator so arbitrarily long input never creates a huge bignum.
decimal_digits(<<>>, Value, _) -> {ok, Value};
decimal_digits(<<Digit, Rest/binary>>, Value, Limit) when Digit >= $0, Digit =< $9 ->
    Next = Value * 10 + Digit - $0,
    case Next =< Limit of
        true -> decimal_digits(Rest, Next, Limit);
        false -> {error, <<"integer out of range">>}
    end;
decimal_digits(_, _, _) -> {error, <<"invalid integer">>}.

format_int(Value) -> integer_to_binary(Value).

arguments() -> [unicode:characters_to_binary(Arg) || Arg <- init:get_plain_arguments()].

value_result(Value, OK, Reason) ->
    #{field_76616c7565 => Value, field_6f6b => OK, field_726561736f6e => Reason}.

io_result(OK, Reason) -> #{field_6f6b => OK, field_726561736f6e => Reason}.

stats() ->
    Frames = state(frames, []),
    #{live_cells => state(live_cells, 0),
      peak_live_cells => state(peak_live_cells, 0),
      allocated_cells => state(allocated_cells, 0),
      reclaimed_cells => state(reclaimed_cells, 0),
      collections => state(collections, 0),
      root_frames => length(Frames),
      root_entries => lists:sum([map_size(root_ids(Frame)) || Frame <- Frames])}.

set_gc_stress(Enabled) when is_boolean(Enabled) ->
    put({linglang_gc, stress}, Enabled),
    ok.

state(Key, Default) ->
    case get({linglang_gc, Key}) of undefined -> Default; Value -> Value end.

increment(Key, Amount) ->
    Value = state(Key, 0) + Amount,
    put({linglang_gc, Key}, Value),
    Value.

deref(nil) -> erlang:error(linglang_nil_pointer);
deref({linglang_ptr, Owner, Id, _Path} = Pointer) when Owner =:= self() ->
    case get({linglang_cell, Id}) of
        undefined -> erlang:error(linglang_invalid_pointer);
        _ -> Pointer
    end;
deref({linglang_ptr, _, _, _}) -> erlang:error(linglang_cross_process_pointer).

read(Pointer) ->
    {linglang_ptr, _, Id, Path} = deref(Pointer),
    read_path(get({linglang_cell, Id}), Path).

write(Pointer, Value) ->
    {linglang_ptr, _, Id, Path} = deref(Pointer),
    put({linglang_cell, Id}, write_path(get({linglang_cell, Id}), Path, Value)),
    ok.

field(Pointer, Name) ->
    {linglang_ptr, Owner, Id, Path} = deref(Pointer),
    {linglang_ptr, Owner, Id, Path ++ [Name]}.

field_value({linglang_ptr, _, _, _} = Pointer, Name) -> maps:get(Name, read(Pointer));
field_value(nil, _) -> erlang:error(linglang_nil_pointer);
field_value(Value, Name) -> maps:get(Name, Value).

read_path(Value, []) -> Value;
read_path(Value, [Name | Rest]) -> read_path(maps:get(Name, Value), Rest).

write_path(_, [], Value) -> Value;
write_path(Map, [Name | Rest], Value) ->
    Map#{Name := write_path(maps:get(Name, Map), Rest, Value)}.

binary(add, A, B) when is_binary(A), is_binary(B) -> <<A/binary, B/binary>>;
binary(add, A, B) -> int64(A + B);
binary(sub, A, B) -> int64(A - B);
binary(mul, A, B) -> int64(A * B);
binary(divide, A, B) -> int64(A div B);
binary(remain, A, B) -> int64(A rem B);
binary(eq, A, B) -> A =:= B;
binary(ne, A, B) -> A =/= B;
binary(lt, A, B) -> A < B;
binary(le, A, B) -> A =< B;
binary(gt, A, B) -> A > B;
binary(ge, A, B) -> A >= B.

int64(Value) ->
    Bits = Value band ((1 bsl 64) - 1),
    case Bits >= (1 bsl 63) of
        true -> Bits - (1 bsl 64);
        false -> Bits
    end.

print(Values, Newline) ->
    Parts = [format_value(V) || V <- Values],
    case Newline of
        true -> io:put_chars([lists:join(" ", Parts), "\n"]);
        false -> io:put_chars(Parts)
    end.

format_value(Value) when is_binary(Value) -> Value;
format_value(Value) when is_list(Value) ->
    ["[", lists:join(", ", [format_value(Item) || Item <- Value]), "]"];
format_value(Value) -> io_lib:format("~tp", [Value]).

loop(Condition, Body, Post, Token) ->
    try loop_next(Condition, Body, Post, Token)
    catch throw:{linglang_break, Token} -> ok
    end.

loop_next(Condition, Body, Post, Token) ->
    safepoint(),
    case scope(Condition) of
        false -> ok;
        true ->
            try Body()
            catch throw:{linglang_continue, Token} -> ok
            end,
            scope(Post),
            loop_next(Condition, Body, Post, Token)
    end.

%% The post statement runs on the new iteration's variable, preserving pointers
%% retained from earlier iterations (the Go 1.22+ loop-variable rule).
loop_cell(Condition, Body, Post, Token, Cell) ->
    try loop_cell_next(Condition, Body, Post, Token, Cell)
    catch throw:{linglang_break, Token} -> ok
    end.

loop_cell_next(Condition, Body, Post, Token, Cell) ->
    Result = scope(fun() ->
        keep(Cell),
        safepoint(),
        case scope(fun() -> Condition(Cell) end) of
            false -> done;
            true ->
                try Body(Cell)
                catch throw:{linglang_continue, Token} -> ok
                end,
                Next = new(read(Cell)),
                scope(fun() -> Post(Next) end),
                {next, Next}
        end
    end),
    %% Recurse after leaving the previous iteration's frame. The new cell is
    %% rooted before the next safe point, with no collection during this handoff.
    case Result of
        done -> ok;
        {next, Next} -> loop_cell_next(Condition, Body, Post, Token, Next)
    end.
