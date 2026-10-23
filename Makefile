VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.1.0)
PREFIX  ?= $(HOME)/.local
GOFLAGS ?= -trimpath
LDFLAGS := -s -w -X main.Version=$(VERSION)
DIST    := dist/brine-$(VERSION)-linux-amd64

.PHONY: all build test vet install uninstall dist deb clean integration restart-test

all: build

build:
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o build/brine ./cmd/brine

test:
	go test -race ./...

vet:
	go vet ./...

# User-level install (no root): binary, desktop entry, MIME, icon.
install: build
	install -Dm755 build/brine $(PREFIX)/bin/brine
	$(PREFIX)/bin/brine desktop install

uninstall:
	-$(PREFIX)/bin/brine desktop remove
	rm -f $(PREFIX)/bin/brine

dist: build
	rm -rf $(DIST) && mkdir -p $(DIST)
	cp build/brine README.md LICENSE packaging/install.sh $(DIST)/
	cp assets/brine.desktop assets/brine.svg assets/brine-studio.xml $(DIST)/
	tar -C dist -czf $(DIST).tar.gz $(notdir $(DIST))
	cd dist && sha256sum $(notdir $(DIST)).tar.gz > $(notdir $(DIST)).tar.gz.sha256
	@echo "built $(DIST).tar.gz"

deb: build
	VERSION=$(VERSION) packaging/deb/build-deb.sh

integration: build
	scripts/integration-test.sh

restart-test: build
	BRINE=$(CURDIR)/build/brine python3 -I scripts/restart-test.py

clean:
	rm -rf build dist
