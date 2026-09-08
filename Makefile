# CoreNet build targets.
#
# Everything here is a thin wrapper around go and test/smoke.sh. Nothing in
# CoreNet depends on Make: the commands in README.md work on their own.

PREFIX ?= /usr/local
BINDIR := $(DESTDIR)$(PREFIX)/bin

BINARIES := corenetd corenet
SOURCES  := $(shell find . -name '*.go' -not -path './.git/*') go.mod go.sum

.PHONY: all build test vet fmt check smoke clean install uninstall help

all: build

## build: build corenetd and corenet in this directory
build: $(BINARIES)

corenetd: $(SOURCES)
	go build -o $@ ./cmd/corenetd

corenet: $(SOURCES)
	go build -o $@ ./cmd/corenet

## test: run the unit tests with the race detector
test:
	go test -race ./...

## vet: run go vet
vet:
	go vet ./...

## fmt: format every Go file in place
fmt:
	gofmt -w $(filter %.go,$(SOURCES))

## smoke: run the end-to-end test (two nodes, DNS, proxy, Docker if present)
smoke:
	./test/smoke.sh

## check: vet, test and smoke, in that order
check: vet test smoke

## clean: remove the built binaries
clean:
	rm -f $(BINARIES)

## install: install both binaries into PREFIX/bin, default /usr/local (needs root)
install: build
	install -d $(BINDIR)
	install -m 0755 $(BINARIES) $(BINDIR)

## uninstall: remove the installed binaries
uninstall:
	rm -f $(addprefix $(BINDIR)/,$(BINARIES))

## help: list these targets
help:
	@sed -n 's/^## //p' $(MAKEFILE_LIST)
