VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//' || echo 0.0.0)
PREFIX  ?= $(HOME)/.local
GOFLAGS ?= -trimpath
LDFLAGS := -s -w -X main.Version=$(VERSION)
NAME    := brine-$(VERSION)-linux-amd64
DIST    := dist/$(NAME)

.PHONY: all build test vet fmt check install uninstall completions dist deb clean integration restart-test multi-test

all: build

build:
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o build/brine ./cmd/brine

fmt:
	gofmt -w .

# What CI runs.
check:
	@test -z "$$(gofmt -l .)" || { echo "gofmt needed:"; gofmt -l .; exit 1; }
	go vet ./...
	go test -race ./...

test:
	go test -race ./...

vet:
	go vet ./...

completions: build
	mkdir -p build/completions
	build/brine completion bash > build/completions/brine.bash
	build/brine completion zsh  > build/completions/_brine
	build/brine completion fish > build/completions/brine.fish

# User-level install (no root): binary, desktop entry, MIME, icon, man page, completions.
install: build completions
	install -Dm755 build/brine $(PREFIX)/bin/brine
	MAN=packaging/brine.1 COMP=build/completions PREFIX=$(PREFIX) sh packaging/install-extras.sh
	$(PREFIX)/bin/brine desktop install

uninstall:
	-$(PREFIX)/bin/brine desktop remove
	rm -f $(PREFIX)/bin/brine $(PREFIX)/share/man/man1/brine.1

dist: build completions
	rm -rf $(DIST) && mkdir -p $(DIST)/completions
	cp build/brine README.md LICENSE CHANGELOG.md packaging/install.sh packaging/install-extras.sh packaging/brine.1 $(DIST)/
	cp build/completions/* $(DIST)/completions/
	cp assets/brine.desktop assets/brine.svg assets/brine-studio.xml $(DIST)/
	tar --sort=name --owner=0 --group=0 --numeric-owner --mtime='2026-01-01 00:00Z' -C dist -czf $(DIST).tar.gz $(NAME)
	cd dist && sha256sum $(NAME).tar.gz > $(NAME).tar.gz.sha256
	@echo "built $(DIST).tar.gz"

deb: build completions
	VERSION=$(VERSION) packaging/deb/build-deb.sh

integration: build
	BRINE=$(CURDIR)/build/brine scripts/integration-test.sh

restart-test: build
	BRINE=$(CURDIR)/build/brine python3 -I scripts/restart-test.py

multi-test: build
	BRINE=$(CURDIR)/build/brine python3 -I scripts/multi-client-test.py

clean:
	rm -rf build dist
