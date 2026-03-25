export CGO_CFLAGS := -g -O2 -Wno-return-local-addr

all: css build

build:
	go build --tags="fts5" -o giverny .

css:
	$(MAKE) -C static

fmt:
	goimports -w $(shell git ls-files '*.go')

run:
	reflex -g '*.go' -s -- sh -c "go build --tags=fts5 -o giverny . && ./giverny --config=dev.cfg.json --debug"
