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

# regenerates internal/api/pb from proto/bbs.proto (needs protoc and
# protoc-gen-go; the generated code is committed)
proto:
	protoc --go_out=. --go_opt=module=github.com/Neo-Desktop/bbs-sec proto/bbs.proto

# regenerates internal/phone's tables from a checkout of Google's
# libphonenumber (git clone --depth 1 https://github.com/google/libphonenumber)
# and NANPA's area code report (curl -O https://reports.nanpa.com/public/npa_report.csv)
LIBPHONENUMBER ?= ../libphonenumber
NANPA ?= npa_report.csv
phone-data:
	$(GO) run ./internal/phone/gen -src $(LIBPHONENUMBER) -nanpa $(NANPA)

# fetches Ruffle's self-hosted package (Flash for the image boards: [chan]
# ruffle = ./ruffle) from npm, unpacked into ruffle/
RUFFLE_VERSION ?= 0.7.1
ruffle:
	rm -rf ruffle && mkdir ruffle
	curl -fsSL https://registry.npmjs.org/@ruffle-rs/ruffle/-/ruffle-$(RUFFLE_VERSION).tgz | tar xz -C ruffle --strip-components=1
	@ls ruffle/ruffle.js

clean:
	rm -f bbs

.PHONY: all bbs test install clean doors-image proto phone-data ruffle
