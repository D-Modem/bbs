PREFIX ?= /usr/local
GO ?= go

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

clean:
	rm -f bbs

.PHONY: all bbs test install clean
