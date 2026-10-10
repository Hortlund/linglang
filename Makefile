.PHONY: compiler selfhost sqlite book test
compiler:
	go build -o bin/linglang ./cmd/linglang
selfhost:
	escript tools/bootstrap.escript
sqlite:
	mkdir -p bin
	$(CC) -std=c11 -O2 -Wall -Wextra -Werror -pthread tools/sqlite/main.c -lsqlite3 -o bin/linglang-sqlite
book:
	mdbook build book
test:
	go test ./internal/... ./cmd/bench -count=1 -timeout=10m
