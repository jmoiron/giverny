export GOCACHE ?= $(CURDIR)/.gocache
export CGO_CFLAGS := -g -O2 -Wno-return-local-addr

.PHONY: all build css fmt run test

all: css build

build:
	go build --tags="fts5" -o giverny .

test:
	go test --tags="fts5" ./...

css:
	$(MAKE) -C static

fmt:
	goimports -w $(shell git ls-files '*.go')

run:
	reflex -g '*.go' -s -- sh -c "go build --tags=fts5 -o giverny . && ./giverny --config=dev.cfg.json --debug"
