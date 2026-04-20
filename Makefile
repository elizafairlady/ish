PREFIX  ?= /usr/local
BINDIR  ?= $(PREFIX)/bin
VERSION := $(shell grep 'var Version' cmd/ish/main.go | cut -d'"' -f2)
LDFLAGS := -s -w

.PHONY: all build test test-race install uninstall clean fmt vet check examples

all: build

build:
	go build -ldflags '$(LDFLAGS)' -o ish ./cmd/ish

test:
	go test ./...

test-race:
	go test -race ./...

check: vet test-race

vet:
	go vet ./...

fmt:
	gofmt -s -w .

install: build
	install -d $(DESTDIR)$(BINDIR)
	install -m 755 ish $(DESTDIR)$(BINDIR)/ish

uninstall:
	rm -f $(DESTDIR)$(BINDIR)/ish

clean:
	rm -f ish

examples:
	@for f in examples/*.ish; do \
		printf '%-30s' "$$f:"; \
		timeout 5 ./ish "$$f" > /dev/null 2>&1 && echo "OK" || echo "FAIL"; \
	done

version:
	@echo $(VERSION)
