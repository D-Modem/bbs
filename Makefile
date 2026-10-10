PREFIX ?= /usr/local
GO ?= go
# go3270 is replaced by the Neo-Desktop/go3270-sec fork (go.mod): fetch it
# from GitHub directly, not through the public proxy and checksum database
export GOPRIVATE ?= github.com/Neo-Desktop/*

all: bbs

bbs:
	$(GO) build -o bbs ./cmd/bbs

test:
	$(GO) vet ./...
	$(GO) test -race ./...

install: bbs
	install -D -m 0755 bbs $(DESTDIR)$(PREFIX)/bin/bbs
	@if [ ! -e $(DESTDIR)/etc/bbs/bbs.conf ]; then \
		install -D -m 0640 bbs.conf.sample $(DESTDIR)/etc/bbs/bbs.conf; \
	fi

# the dosemu2 image DOS doors run in (Docker; it builds dosemu2 from source)
doors-image:
	docker build -t bbs-dosemu2 doors/dosemu2

clean:
	rm -f bbs

.PHONY: all bbs test install clean doors-image
