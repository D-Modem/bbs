# bbs: a bulletin board for telnet, TN3270 and modem callers

`bbs` is a bulletin board system in Go.  One telnet port serves both kinds
of terminal: a TN3270 client (x3270, c3270, wc3270, Vista, PCOMM) gets
full 3270 screens with PF keys, through
[go3270](https://github.com/racingmars/go3270); any other telnet client
gets a text BBS with ANSI color where the terminal has it.  Modem callers
come in through the D-Modem suite: [app_dmodem](https://github.com/Neo-Desktop/D-Modem-RE)
straight from Asterisk, or [modemmenu](https://github.com/Neo-Desktop/PBX-Modem)
after its login.

```
D-Modem BBS
Node 2, modem at 26400 bit/s

Name (or NEW): alice
Password: **********

Welcome, alice.

Main menu
  [M] Message areas         [E] E-mail (1 new)
  [W] Who's online          [U] User list
  [P] Your profile          [I] System info
  [C] Chat                  [S] Page the sysop
  [G] Goodbye
```

The same caller on a 3270:

```
 D-Modem BBS                                    Main menu                 Node 1

   Welcome, alice.

   ┌───────────────────────────────────────────┐
   │ M  Message areas                          │
   │ E  E-mail (1 new)                         │
   │ W  Who's online                           │
   │ U  User list                              │
   │ P  Your profile                           │
   │ I  System info                            │
   │ C  Chat                                   │
   │ S  Page the sysop                         │
   │ T  Your terminal                          │
   │ G  Goodbye                                │
   └───────────────────────────────────────────┘

   Option ===>

 Enter=Select  PF3=Back
```

What there is:

- message areas: read new, list, read by number, post, reply (quoting),
  reply by mail; areas only the sysop posts in; authors and the sysop
  delete messages
- private mail: inbox, read, reply, delete
- [SSH](https://github.com/Neo-Desktop/bbs-sec/wiki/SSH): logging in by password or key, the text BBS encrypted
- [Commodore 64s and RIPscrip terminals](https://github.com/Neo-Desktop/bbs-sec/wiki/PETSCII-and-RIP): PETSCII at 40 columns; menus as RIP buttons
- [VT terminals identified](https://github.com/Neo-Desktop/bbs-sec/wiki/Terminal-Identification) (answerback, DA1, DA2, XTVERSION, VT52): DEC line drawing and Latin-1, bold on monochrome VTs, VT52 escapes
- who's online (and how each caller came: ssh, telnet, tn3270, rlogin, modem
  with its rate), the user list, system info
- the last callers (*L*), feedback to the sysop (*K*, which any caller may
  send), the user list with each user's details and a line about themselves
- your profile: where you call from, a line about yourself, expert menus
  (the prompt alone, `?` for the rest), your password, SSH keys, ANSI and UTF-8
- [file areas](https://github.com/Neo-Desktop/bbs-sec/wiki/Files-and-FTP): files listed with their size, uploader, downloads and
  SHA-256, moved by XMODEM, YMODEM, ZMODEM or Kermit on the text BBS, by
  IND$FILE on a 3270, or by FTP (FTPS); deleted by their uploader or the
  sysop
- [newsgroups](https://github.com/Neo-Desktop/bbs-sec/wiki/Newsgroups-and-NNTP): message areas given a newsgroup name are served by NNTP to
  newsreaders and exchanged with other news servers (INN and the like)
- [offline mail](https://github.com/Neo-Desktop/bbs-sec/wiki/Offline-Mail): QWK packets of new messages and mail, REP
  replies brought back (by a transfer protocol, IND$FILE, FTP or the API); QWK networks as a hub or a node
- [FidoNet](https://github.com/Neo-Desktop/bbs-sec/wiki/FidoNet) (or any FTN network): message areas that are echoes, netmail
  as private mail, and a binkp mailer to exchange packets with links
- [IRC](https://github.com/Neo-Desktop/bbs-sec/wiki/IRC): the chat rooms as IRC channels, built on parts of
  [ergo](https://github.com/ergochat/ergo); logins by SASL or the server password, TLS
- [MRC](https://github.com/Neo-Desktop/bbs-sec/wiki/MRC): Multi Relay Chat, the BBSes' chat network, its rooms as chat rooms here
- [doors](https://github.com/Neo-Desktop/bbs-sec/wiki/Doors): DOS door games in dosemu2 (in Docker), with a FOSSIL on COM1
  and the usual drop files, or any Linux program, for text callers; doors
  elsewhere by telnet or RLogin, and the door networks BBSLink and DoorParty
- [chat](https://github.com/Neo-Desktop/bbs-sec/wiki/Chat-and-Paging): rooms, messages to a node, paging the sysop
- [finger and gopher](https://github.com/Neo-Desktop/bbs-sec/wiki/Finger-and-Gopher): who is on, and chosen areas in gopherspace
- [a web forum](https://github.com/Neo-Desktop/bbs-sec/wiki/Web-Forum): the message areas as a bulletin board in the browser, in the manner of Invision Power Board 3, or of IPB 4, 2 or 1, Ikonboard, vBulletin 2 or Lite, or phpBB 3.3 back to 1 (themes)
- [a web terminal](https://github.com/Neo-Desktop/bbs-sec/wiki/HTTP-API#the-web-terminal): the BBS in a browser (xterm.js, built in)
- [an HTTP API](https://github.com/Neo-Desktop/bbs-sec/wiki/HTTP-API): REST, server-sent events, and WebSockets (JSON or protobuf) where web clients chat with terminal callers; Prometheus metrics
- [customization](https://github.com/Neo-Desktop/bbs-sec/wiki/Customization): ANSI art and 3270 screens, menus of the sysop's, @-codes, the prompts' words
- [access levels](https://github.com/Neo-Desktop/bbs-sec/wiki/Access-Levels): levels and flags, areas, doors and menus that require them, time and download limits a day, new users validated by the sysop
- new users sign themselves up (or not: `new_users = no`)
- [SQLite, PostgreSQL, MySQL or MariaDB](https://github.com/Neo-Desktop/bbs-sec/wiki/Databases), and `bbs db copy` between them

## Quick start

Go 1.24 or later:

```
make                                    # or: go build ./cmd/bbs
bbs -config /etc/bbs/bbs.conf check     # the configuration, its warnings
bbs -config /etc/bbs/bbs.conf init      # the database, with two areas
bbs -config /etc/bbs/bbs.conf user add sysop -sysop
bbs -config /etc/bbs/bbs.conf serve
```

[bbs.conf.sample](bbs.conf.sample) has every option, and
[bbs.service.sample](bbs.service.sample) runs it under systemd.  go3270 is
replaced by the private [go3270-sec](https://github.com/Neo-Desktop/go3270-sec)
fork: see [Building](https://github.com/Neo-Desktop/bbs-sec/wiki/Building).

## Documentation

The documentation is in the [wiki](https://github.com/Neo-Desktop/bbs-sec/wiki/):

| | |
|---|---|
| [Building](https://github.com/Neo-Desktop/bbs-sec/wiki/Building) | Go, the go3270-sec fork, make |
| [Running](https://github.com/Neo-Desktop/bbs-sec/wiki/Running) | first start, systemd, the defaults |
| [Administration](https://github.com/Neo-Desktop/bbs-sec/wiki/Administration) | the bbs command: users, areas, files, mail exchange |
| [Databases](https://github.com/Neo-Desktop/bbs-sec/wiki/Databases) | SQLite, PostgreSQL, MySQL, MariaDB; moving between them |
| [Configuration](https://github.com/Neo-Desktop/bbs-sec/wiki/Configuration) | bbs.conf: its sections, listeners |
| [Access levels](https://github.com/Neo-Desktop/bbs-sec/wiki/Access-Levels) | levels, flags, what areas, doors and menus require, daily limits, validation |
| [Customization](https://github.com/Neo-Desktop/bbs-sec/wiki/Customization) | screens (ANSI art, 3270), menus, prompts |
| [SSH](https://github.com/Neo-Desktop/bbs-sec/wiki/SSH) | the SSH listener, keys, host keys |
| [Modem callers](https://github.com/Neo-Desktop/bbs-sec/wiki/Modem-Callers) | app_dmodem and modemmenu |
| [TN3270](https://github.com/Neo-Desktop/bbs-sec/wiki/TN3270) | 3270 terminals, PF keys, 3270 graphics |
| [Sixel graphics](https://github.com/Neo-Desktop/bbs-sec/wiki/Sixel-Graphics) | the logo on sixel terminals |
| [PETSCII and RIP](https://github.com/Neo-Desktop/bbs-sec/wiki/PETSCII-and-RIP) | Commodore 64s and 128s; RIPscrip graphics and buttons |
| [Terminal identification](https://github.com/Neo-Desktop/bbs-sec/wiki/Terminal-Identification) | VT terminals asked what they are: DEC graphics, monochrome, VT52 |
| [Files and FTP](https://github.com/Neo-Desktop/bbs-sec/wiki/Files-and-FTP) | file areas, the FTP (FTPS) server |
| [File transfers](https://github.com/Neo-Desktop/bbs-sec/wiki/File-Transfers) | X/Y/ZMODEM, Kermit, IND$FILE |
| [Doors](https://github.com/Neo-Desktop/bbs-sec/wiki/Doors) | DOS doors in dosemu2 (Docker), Linux doors |
| [Newsgroups and NNTP](https://github.com/Neo-Desktop/bbs-sec/wiki/Newsgroups-and-NNTP) | the NNTP server, news peers |
| [FidoNet](https://github.com/Neo-Desktop/bbs-sec/wiki/FidoNet) | echomail, netmail, binkp |
| [IRC](https://github.com/Neo-Desktop/bbs-sec/wiki/IRC) | the IRC server: the chat rooms as channels |
| [MRC](https://github.com/Neo-Desktop/bbs-sec/wiki/MRC) | Multi Relay Chat: chat rooms shared with other BBSes |
| [Offline mail](https://github.com/Neo-Desktop/bbs-sec/wiki/Offline-Mail) | QWK packets, REP replies, QWK networks |
| [Chat and paging](https://github.com/Neo-Desktop/bbs-sec/wiki/Chat-and-Paging) | chat rooms, node messages, paging the sysop |
| [Finger and gopher](https://github.com/Neo-Desktop/bbs-sec/wiki/Finger-and-Gopher) | who is on, and the BBS in gopherspace |
| [Web forum](https://github.com/Neo-Desktop/bbs-sec/wiki/Web-Forum) | the message areas as a web forum |
| [HTTP API](https://github.com/Neo-Desktop/bbs-sec/wiki/HTTP-API) | REST, events, WebSockets in JSON and protobuf, tokens, metrics |
| [Security](https://github.com/Neo-Desktop/bbs-sec/wiki/Security) | what is guarded, and how |
| [Development](https://github.com/Neo-Desktop/bbs-sec/wiki/Development) | the source's layout, the tests |

Its pages are kept in [`docs/wiki`](docs/wiki): edit them there (the
[Wiki](.github/workflows/wiki.yml) workflow publishes them, or
`docs/wiki/publish.sh`).  Future work is in [TODO.md](TODO.md).
