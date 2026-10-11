-module(linglang_release_builder).
-export([build/0]).

%% Both drivers use the same OTP release layout and relocatable launcher.
build() ->
    try
        [Stage, OS, Arch] = init:get_plain_arguments(),
        linglang_rt:release_package(Stage, OS, Arch),
        halt(0)
    catch Class:Reason:Stack ->
        io:format(standard_error, "~tp:~tp~n~tp~n", [Class, Reason, Stack]),
        halt(1)
    end.
