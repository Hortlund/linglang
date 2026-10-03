%% Equivalent workloads in idiomatic Erlang. The pointer workload threads its
%% counter value explicitly; Erlang has no matching general mutable-pointer type.
-module(bench_reference).
-export([arithmetic/0, calls/0, structs/0, pointers/0,
         lists/0, maps/0, strings/0, messages/0]).

arithmetic() -> 4999950000 = arithmetic(0, 0), ok.
arithmetic(100000, Total) -> Total;
arithmetic(I, Total) -> arithmetic(i64(I + 1), i64(Total + I)).

calls() -> 4999950000 = calls(0, 0), ok.
calls(100000, Total) -> Total;
calls(I, Total) -> calls(i64(I + 1), add(Total, I)).
add(A, B) -> i64(A + B).

structs() -> #{x := 4999950000, y := 100000} = structs(0, #{x => 0, y => 0}), ok.
structs(100000, Point) -> Point;
structs(I, Point) ->
    Next = Point#{x := i64(maps:get(x, Point) + I)},
    structs(i64(I + 1), Next#{y := i64(maps:get(y, Next) + 1)}).

pointers() -> #{value := 100000} = pointers(0, #{value => 0}), ok.
pointers(100000, Counter) -> Counter;
pointers(I, Counter) -> pointers(i64(I + 1), increment(Counter)).
increment(Counter) -> Counter#{value := i64(maps:get(value, Counter) + 1)}.

lists() ->
    Items = list_build(0, []),
    20000 = length(Items),
    199990000 = list_sum(Items, 0), ok.
list_build(20000, Items) -> Items;
list_build(I, Items) -> list_build(i64(I + 1), [I | Items]).
list_sum([], Total) -> Total;
list_sum([Item | Rest], Total) -> list_sum(Rest, i64(Total + Item)).

maps() ->
    Items = map_build(0, #{}),
    {99990000, Remaining} = map_read(0, Items, 0),
    5000 = map_size(Remaining), ok.
map_build(10000, Items) -> Items;
map_build(I, Items) -> map_build(i64(I + 1), Items#{I => i64(I * 2)}).
map_read(10000, Items, Total) -> {Total, Items};
map_read(I, Items, Total) ->
    {ok, Value} = maps:find(I, Items),
    Next = case I rem 2 of 0 -> maps:remove(I, Items); _ -> Items end,
    map_read(i64(I + 1), Next, i64(Total + Value)).

strings() -> 1800000 = string_lines(0, 0), ok.
string_lines(10000, Total) -> Total;
string_lines(I, Total) ->
    Fields = binary:split(<<" 12, 34, 56, 78 ">>, <<",">>, [global]),
    Next = lists:foldl(fun(Field, Sum) ->
        i64(Sum + binary_to_integer(string:trim(Field, both, " \t\r\n")))
    end, Total, Fields),
    string_lines(i64(I + 1), Next).

%% Native Erlang messages omit linglang's schema checks and managed-root work.
messages() ->
    Parent = self(),
    {Pid, Ref} = spawn_monitor(fun() -> echo(Parent, 0) end),
    49995000 = round_trips(Pid, 0, 0),
    receive {'DOWN', Ref, process, Pid, normal} -> ok
    after 5000 -> error(worker_timeout) end.
echo(_, 10000) -> ok;
echo(Parent, I) ->
    receive {Parent, Value} -> Parent ! {self(), Value}
    after 5000 -> error(message_timeout) end,
    echo(Parent, i64(I + 1)).
round_trips(_, 10000, Total) -> Total;
round_trips(Pid, I, Total) ->
    Pid ! {self(), I},
    Value = receive {Pid, Reply} -> Reply
    after 5000 -> error(message_timeout) end,
    round_trips(Pid, i64(I + 1), i64(Total + Value)).

i64(Value) ->
    Bits = Value band ((1 bsl 64) - 1),
    case Bits >= (1 bsl 63) of true -> Bits - (1 bsl 64); false -> Bits end.
