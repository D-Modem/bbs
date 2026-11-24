PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
# where make install puts bbs.service: $(PREFIX)/lib/systemd/system, which
# systemd reads for /usr/local and /usr (the same as D-Modem's units)
SYSTEMD_UNIT_DIR ?= $(PREFIX)/lib/systemd/system
GO ?= go
# go3270 is the suite's fork, github.com/D-Modem/go3270 (go.mod): fetch it
# from GitHub directly, not through the public proxy and checksum database
export GOPRIVATE ?= github.com/D-Modem/*

all: bbs bbs.service

bbs:
	$(GO) build -o bbs ./cmd/bbs

# the systemd unit, with the path of the installed program
bbs.service: bbs.service.in Makefile
	sed -e 's|@BINDIR@|$(BINDIR)|g' bbs.service.in > $@

test:
	$(GO) vet ./...
	$(GO) test -race ./...

# the program, the unit (always: changes go in drop-ins, systemctl edit
# bbs), and bbs.conf if there is none.  It does not run systemctl: then
# systemctl daemon-reload && systemctl enable --now bbs
install: bbs bbs.service
	install -D -m 0755 bbs $(DESTDIR)$(BINDIR)/bbs
	install -D -m 0644 bbs.service $(DESTDIR)$(SYSTEMD_UNIT_DIR)/bbs.service
	@if [ ! -e $(DESTDIR)/etc/bbs/bbs.conf ]; then \
		install -D -m 0640 bbs.conf.sample $(DESTDIR)/etc/bbs/bbs.conf; \
	fi

# the dosemu2 image DOS doors run in (Docker; it builds dosemu2 from source)
doors-image:
	docker build -t bbs-dosemu2 doors/dosemu2

# regenerates internal/api/pb from proto/bbs.proto (needs protoc and
# protoc-gen-go; the generated code is committed)
proto:
	protoc --go_out=. --go_opt=module=github.com/D-Modem/bbs proto/bbs.proto

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
	rm -f bbs bbs.service

.PHONY: all bbs test install clean doors-image proto phone-data ruffle
