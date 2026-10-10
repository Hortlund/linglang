#!/usr/bin/env python3
"""Measure packed, self-built compilers against one unchanged source corpus."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import shlex
import shutil
import statistics
import subprocess
import tempfile
import zipfile


MEASURE = """
Parent=self(), {Pid,Ref}=spawn_monitor(fun()->
  linglang_rt:set_arguments([unicode:characters_to_binary(P)||P<-init:get_plain_arguments()]),
  {reductions,R0}=process_info(self(),reductions),
  {Micros,_}=timer:tc(fun linglang_program:main/0),
  {reductions,R1}=process_info(self(),reductions),
  Stats=linglang_rt:stats(),
  #{live_cells:=0,root_frames:=0,root_entries:=0}=Stats,
  Parent!{self(),Stats#{microseconds=>Micros,reductions=>R1-R0}}
end),
receive
  {Pid,Stats}->receive {'DOWN',Ref,process,Pid,normal}->ok end,
    io:format(standard_error,"MEASURE ~s~n",[json:encode(Stats)]),halt(0);
  {'DOWN',Ref,process,Pid,Reason}->io:format(standard_error,"~tp~n",[Reason]),halt(1)
end.
"""


def digest(data):
    return hashlib.sha256(data).hexdigest()


def corpus_manifest(directory):
    return {
        p.name: digest(p.read_bytes())
        for p in sorted(directory.glob("*.lang"))
        if p.is_file() and not p.name.endswith("_test.lang")
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--compiler", action="append", required=True,
                        metavar="NAME=ESCRIPT", help="repeat to interleave compilers")
    parser.add_argument("--corpus", type=Path, required=True)
    parser.add_argument("--samples", type=int, default=3)
    parser.add_argument("--warmup", type=int, default=1)
    parser.add_argument("--no-opt", action="store_true", help="emit using cell lowering")
    parser.add_argument("--out", type=Path, required=True)
    options = parser.parse_args()
    if options.samples < 1 or options.warmup < 0:
        parser.error("samples must be positive and warmup nonnegative")
    corpus = options.corpus.resolve()
    manifest = corpus_manifest(corpus)
    if not manifest:
        parser.error("corpus must contain non-test .lang files")
    compilers = {}
    for item in options.compiler:
        name, separator, path = item.partition("=")
        if not separator or not name or name in compilers:
            parser.error("compiler names must be nonempty and unique: NAME=ESCRIPT")
        compilers[name] = Path(path).resolve()
    blocked = {"PATH", "ERL_LIBS", "ERL_FLAGS", "ERL_AFLAGS", "ERL_ZFLAGS",
               "ERL_ROOTDIR", "ROOTDIR", "BINDIR", "EMU", "PROGNAME", "ESCRIPT_EMULATOR"}
    env = {k: v for k, v in os.environ.items() if k not in blocked}
    rows = []
    provenance = {}
    with tempfile.TemporaryDirectory(prefix="linglang-bootstrap-bench-") as temporary:
        work = Path(temporary)
        otp = work / "otp"
        otp.mkdir()
        for name in ("erl", "dirname", "basename"):
            executable = shutil.which(name)
            if not executable:
                parser.error(f"missing executable: {name}")
            wrapper = otp / name
            # Resolve symlinks before launching relocatable OTP scripts.
            wrapper.write_text("#!/bin/sh\nexec " + shlex.quote(str(Path(executable).resolve())) + ' "$@"\n')
            wrapper.chmod(0o700)
        env["PATH"] = str(otp)
        directories = {}
        for index, (name, path) in enumerate(compilers.items()):
            directory = work / str(index)
            directory.mkdir()
            with zipfile.ZipFile(path) as archive:
                modules = ["linglang_program.beam", "linglang_rt.beam", "linglang_sup.beam"]
                # Preserve comparisons with older, frozen compiler artifacts.
                for optional in ("linglang_server.beam", "linglang_io.beam"):
                    if optional in archive.namelist():
                        modules.append(optional)
                for filename in modules:
                    (directory / filename).write_bytes(archive.read(filename))
            directories[name] = directory
            provenance[name] = {"path": str(path), "artifact_sha256": digest(path.read_bytes()),
                                "compiler_beam_sha256": digest((directory / "linglang_program.beam").read_bytes())}
        otp_release = subprocess.check_output(
            [str(otp / "erl"), "+S", "1", "-noshell", "-eval",
             'io:format("~s",[erlang:system_info(otp_release)]),halt().'], env=env).decode()
        names = list(compilers)
        outputs = {}
        for round_index in range(options.warmup + options.samples):
            order = names[round_index % len(names):] + names[:round_index % len(names)]
            for name in order:
                args = ["emit", str(corpus)] + (["--no-opt"] if options.no_opt else [])
                command = [str(otp / "erl"), "+S", "1", "-noshell", "-pa",
                           str(directories[name]), "-eval", MEASURE, "-extra", *args]
                result = subprocess.run(command, env=env, capture_output=True, timeout=600, check=True)
                marker = result.stderr.decode().split("MEASURE ", 1)
                row = json.loads(marker[1])
                output_hash = digest(result.stdout)
                if name in outputs and outputs[name] != output_hash:
                    raise RuntimeError(f"nondeterministic output from {name}")
                outputs[name] = output_hash
                row.update(compiler=name, warmup=round_index < options.warmup,
                           output_bytes=len(result.stdout), output_sha256=output_hash)
                rows.append(row)
                print(json.dumps(row), flush=True)
        if corpus_manifest(corpus) != manifest:
            raise RuntimeError("corpus changed during measurement")
    medians = {
        name: {key: statistics.median(row[key] for row in rows
                                    if row["compiler"] == name and not row["warmup"])
               for key in ("microseconds", "reductions", "allocated_cells", "peak_live_cells", "collections")}
        for name in names
    }
    report = dict(host=platform.platform(), otp_release=otp_release, corpus=str(corpus),
                  corpus_sha256=manifest, compilers=provenance, no_opt=options.no_opt,
                  samples=options.samples, warmup=options.warmup, records=rows, medians=medians)
    options.out.parent.mkdir(parents=True, exist_ok=True)
    options.out.write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps(medians, indent=2))


if __name__ == "__main__":
    main()
