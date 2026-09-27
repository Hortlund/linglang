# linglang

a programming language that answers the question nobody fucking asked.

called linglang because i wanted to learn Chinese but somehow installed Erlang instead.

## why

because why the fuck not?

i wanted `i++` and OTP supervision in the same language.
one thing led to another and now Go writes Erlang. nobody intervened.

## features, allegedly

- **mutable variables.** change your mind. the computer can deal with it.
- **real pointers on BEAM.** they stay in their own process. even this has boundaries.
- **garbage collection.** collects the garbage at runtime. source code remains unfortunately.
- **OTP supervision.** your worker can get fired and immediately rehired with no memory of the incident.
- **no arrays.** off-by-one errors down 100%. you're welcome.
- **written in Go. compiles to Erlang.** a diplomatic incident with a build command.

## production ready

no ❤️

## fuck around and find out

bring Go 1.23+ and Erlang/OTP (`erl` + `erlc`). this part is real.

```sh
go run ./cmd/linglang run examples/supervision.lang
```
