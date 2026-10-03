-module(linglang_release_builder).
-export([build/0]).

build() ->
    try
        [Stage, OS, Arch] = init:get_plain_arguments(),
        Root = code:root_dir(),
        ERTS = erlang:system_info(version),
        System = erlang:system_info(system_architecture),
        check_architecture(Arch, System),
        Kernel = app_version(kernel),
        Stdlib = app_version(stdlib),
        Name = filename:join(Stage, "linglang"),
        Release = {release, {"linglang", "1"}, {erts, ERTS},
                   [{kernel, Kernel}, {stdlib, Stdlib}, {linglang, "1"}]},
        ok = file:write_file(Name ++ ".rel", io_lib:format("~tp.~n", [Release])),
        AppPath = filename:join([Stage, "lib", "linglang-1", "ebin"]),
        Options = [silent, no_warn_sasl, {path, [AppPath]}],
        {ok, _, []} = systools:make_script(Name, [no_dot_erlang | Options]),
        Launcher = filename:join(Stage, "run"),
        ok = file:write_file(Launcher, launcher(ERTS)),
        ok = file:change_mode(Launcher, 8#755),
        Readme = filename:join(Stage, "README.txt"),
        ok = file:write_file(Readme, io_lib:format(
            "linglang runtime release (~s/~s, OTP ~s, ERTS ~s)~n"
            "Bundled runtime target: ~s~n"
            "Run: ./bin/run [program arguments...]~n"
            "Includes Erlang/OTP; no Go or Erlang installation is required.~n"
            "Targets the build host OS/architecture and compatible system libraries.~n"
            "Relative data paths use your working directory. Input data is not bundled.~n"
            "Erlang/OTP: https://www.erlang.org/ (Apache License 2.0; see OTP_LICENSE.txt).~n",
            [OS, Arch, erlang:system_info(otp_release), ERTS, System])),
        Extras = [{Launcher, "bin/run"}, {Readme, "README.txt"},
                  {filename:join(Stage, "OTP_LICENSE.txt"), "OTP_LICENSE.txt"}],
        {ok, _, []} = systools:make_tar(Name, [{erts, Root}, {extra_files, Extras} | Options]),
        halt(0)
    catch Class:Reason:Stack ->
        io:format(standard_error, "~tp:~tp~n~tp~n", [Class, Reason, Stack]),
        halt(1)
    end.

app_version(App) ->
    {ok, Version} = application:get_key(App, vsn),
    Version.

%% A native Go tool can encounter an Intel OTP installation under Rosetta (or
%% vice versa). Refuse to label a bundle for the wrong architecture.
check_architecture(Arch, System) when Arch == "amd64"; Arch == "arm64" ->
    Prefix = case Arch of "amd64" -> "x86_64"; "arm64" -> "aarch64" end,
    case lists:prefix(Prefix ++ "-", System) of
        true -> ok;
        false -> error({runtime_architecture_mismatch, Arch, System})
    end;
check_architecture(_, _) -> ok.

%% Invoke erlexec directly: an installation's erl shell script can contain its
%% original absolute paths. Use the system readlink by absolute path so symlink
%% resolution also works with an empty PATH and no Erlang installation.
launcher(ERTS) ->
    io_lib:format(
        "#!/bin/sh\n"
        "case \"$0\" in */*) launcher=$0 ;; *) launcher=$(command -v \"$0\") ;; esac\n"
        "readlink=/usr/bin/readlink\n"
        "[ -x \"$readlink\" ] || readlink=/bin/readlink\n"
        "links=0\n"
        "while [ -L \"$launcher\" ]; do\n"
        "    links=$((links + 1))\n"
        "    if [ \"$links\" -gt 40 ]; then echo 'linglang: too many launcher symlinks' >&2; exit 1; fi\n"
        "    directory=$(CDPATH= cd -- \"${launcher%/*}\" && pwd -P) || exit 1\n"
        "    target=$(\"$readlink\" \"$launcher\") || exit 1\n"
        "    case \"$target\" in /*) launcher=$target ;; *) launcher=$directory/$target ;; esac\n"
        "done\n"
        "ROOTDIR=$(CDPATH= cd -- \"${launcher%/*}/..\" && pwd -P) || exit 1\n"
        "BINDIR=\"$ROOTDIR/erts-~s/bin\"\n"
        "EMU=beam\n"
        "PROGNAME=linglang\n"
        "export ROOTDIR BINDIR EMU PROGNAME\n"
        "exec \"$BINDIR/erlexec\" -boot \"$ROOTDIR/releases/1/start\" -noshell -s linglang_cli start -extra \"$@\"\n",
        [ERTS]).
