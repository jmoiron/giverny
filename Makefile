export GOCACHE ?= $(CURDIR)/.gocache
export CGO_CFLAGS := -g -O2 -Wno-return-local-addr

.PHONY: all build css fmt run test version

all: css build

build:
	go build --tags="fts5" -o giverny .

test:
	go test --tags="fts5" ./...

version:
	@current=$$(sed -nE 's/.*givernyVersion[[:space:]]*=[[:space:]]*Version\{([0-9]+),[[:space:]]*([0-9]+),[[:space:]]*([0-9]+)\}.*/\1.\2.\3/p' main.go); \
	if [ -z "$$current" ]; then \
		echo "could not find a semantic version in main.go" >&2; exit 1; \
	fi; \
	major=$${current%%.*}; remainder=$${current#*.}; \
	minor=$${remainder%%.*}; patch=$${remainder#*.}; \
	next="$$major.$$minor.$$((patch + 1))"; \
	sed -i -E "s/(givernyVersion[[:space:]]*=[[:space:]]*Version\{)[0-9]+,[[:space:]]*[0-9]+,[[:space:]]*[0-9]+(\})/\1$$major, $$minor, $$((patch + 1))\2/" main.go; \
	echo "version $$current -> $$next"

css:
	$(MAKE) -C static

fmt:
	goimports -w $(shell find . -type f -name '*.go' -not -path './.git/*' -print)

run:
	reflex -g '*.go' -s -- sh -c "go build --tags=fts5 -o giverny . && ./giverny --config=dev.cfg.json --debug"
