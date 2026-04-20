PREFIX  ?= /usr/local
BINDIR  ?= $(PREFIX)/bin
VERSION := $(shell grep 'var Version' cmd/ish/main.go | cut -d'"' -f2)
LDFLAGS := -s -w

.PHONY: all build test test-race install uninstall clean fmt vet check examples register-shell unregister-shell

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

register-shell:
	@if grep -qxF '$(BINDIR)/ish' /etc/shells 2>/dev/null; then \
		echo '$(BINDIR)/ish already in /etc/shells'; \
	else \
		echo '$(BINDIR)/ish' >> /etc/shells && \
		echo 'added $(BINDIR)/ish to /etc/shells'; \
	fi

unregister-shell:
	@if grep -qxF '$(BINDIR)/ish' /etc/shells 2>/dev/null; then \
		sed -i '\|^$(BINDIR)/ish$$|d' /etc/shells && \
		echo 'removed $(BINDIR)/ish from /etc/shells'; \
	else \
		echo '$(BINDIR)/ish not in /etc/shells'; \
	fi

clean:
	rm -f ish

examples:
	@for f in examples/*.ish; do \
		printf '%-30s' "$$f:"; \
		timeout 5 ./ish "$$f" > /dev/null 2>&1 && echo "OK" || echo "FAIL"; \
	done

version:
	@echo $(VERSION)
