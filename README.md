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
     1  Message areas
     2  E-mail   (1 new)
     3  Who's online
     4  User list
     5  Your profile
     6  System info
     7  Your terminal
     8  Chat
     9  Page the sysop
     X  Log off
   Option ===>
 Enter=Select  PF3=Log off
```

What there is:

- message areas: read new, list, read by number, post, reply (quoting),
  reply by mail; areas only the sysop posts in; authors and the sysop
  delete messages
- private mail: inbox, read, reply, delete
- who's online (and how each caller came: telnet, tn3270, rlogin, modem
  with its rate), the user list, system info
- your profile: where you call from, your password, ANSI and UTF-8
- [file areas](https://github.com/Neo-Desktop/bbs-sec/wiki/Files-and-FTP): files listed with their size, uploader, downloads and
  SHA-256, moved by XMODEM, YMODEM, ZMODEM or Kermit on the text BBS, by
  IND$FILE on a 3270, or by FTP (FTPS); deleted by their uploader or the
  sysop
- [newsgroups](https://github.com/Neo-Desktop/bbs-sec/wiki/Newsgroups-and-NNTP): message areas given a newsgroup name are served by NNTP to
  newsreaders and exchanged with other news servers (INN and the like)
- [FidoNet](https://github.com/Neo-Desktop/bbs-sec/wiki/FidoNet) (or any FTN network): message areas that are echoes, netmail
  as private mail, and a binkp mailer to exchange packets with links
- [doors](https://github.com/Neo-Desktop/bbs-sec/wiki/Doors): DOS door games in dosemu2 (in Docker), with a FOSSIL on COM1
  and the usual drop files, or any Linux program, for text callers
- [chat](https://github.com/Neo-Desktop/bbs-sec/wiki/Chat-and-Paging): rooms, messages to a node, paging the sysop
- [finger and gopher](https://github.com/Neo-Desktop/bbs-sec/wiki/Finger-and-Gopher): who is on, and chosen areas in gopherspace
- [customization](https://github.com/Neo-Desktop/bbs-sec/wiki/Customization): ANSI art and 3270 screens, menus of the sysop's, @-codes, the prompts' words
- new users sign themselves up (or not: `new_users = no`)
- SQLite storage, one file

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
| [Configuration](https://github.com/Neo-Desktop/bbs-sec/wiki/Configuration) | bbs.conf: its sections, listeners |
| [Customization](https://github.com/Neo-Desktop/bbs-sec/wiki/Customization) | screens (ANSI art, 3270), menus, prompts |
| [Modem callers](https://github.com/Neo-Desktop/bbs-sec/wiki/Modem-Callers) | app_dmodem and modemmenu |
| [TN3270](https://github.com/Neo-Desktop/bbs-sec/wiki/TN3270) | 3270 terminals, PF keys, 3270 graphics |
| [Sixel graphics](https://github.com/Neo-Desktop/bbs-sec/wiki/Sixel-Graphics) | the logo on sixel terminals |
| [Files and FTP](https://github.com/Neo-Desktop/bbs-sec/wiki/Files-and-FTP) | file areas, the FTP (FTPS) server |
| [File transfers](https://github.com/Neo-Desktop/bbs-sec/wiki/File-Transfers) | X/Y/ZMODEM, Kermit, IND$FILE |
| [Doors](https://github.com/Neo-Desktop/bbs-sec/wiki/Doors) | DOS doors in dosemu2 (Docker), Linux doors |
| [Newsgroups and NNTP](https://github.com/Neo-Desktop/bbs-sec/wiki/Newsgroups-and-NNTP) | the NNTP server, news peers |
| [FidoNet](https://github.com/Neo-Desktop/bbs-sec/wiki/FidoNet) | echomail, netmail, binkp |
| [Chat and paging](https://github.com/Neo-Desktop/bbs-sec/wiki/Chat-and-Paging) | chat rooms, node messages, paging the sysop |
| [Finger and gopher](https://github.com/Neo-Desktop/bbs-sec/wiki/Finger-and-Gopher) | who is on, and the BBS in gopherspace |
| [Security](https://github.com/Neo-Desktop/bbs-sec/wiki/Security) | what is guarded, and how |
| [Development](https://github.com/Neo-Desktop/bbs-sec/wiki/Development) | the source's layout, the tests |

Its pages are kept in [`docs/wiki`](docs/wiki): edit them there (the
[Wiki](.github/workflows/wiki.yml) workflow publishes them, or
`docs/wiki/publish.sh`).  Future work is in [TODO.md](TODO.md).
