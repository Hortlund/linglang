%% Equivalent workloads in idiomatic Erlang. The pointer workload threads its
%% counter value explicitly; Erlang has no matching general mutable-pointer type.
-module(bench_reference).
-export([arithmetic/0, calls/0, structs/0, pointers/0]).

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

i64(Value) ->
    Bits = Value band ((1 bsl 64) - 1),
    case Bits >= (1 bsl 63) of true -> Bits - (1 bsl 64); false -> Bits end.
