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

- [message areas](Message-Areas): a new scan of the areas you choose, messages to you, finding
  messages by their words, read by number, post, reply (quoting
  from a window in the [full-screen editor](Writing-Messages)),
  reply by mail; areas only the sysop posts in; authors and the sysop
  delete messages
- private mail: inbox, read, reply, delete
- [offline mail](Offline-Mail): QWK packets of new messages and mail, REP packets of replies
  brought back (by a transfer protocol, IND$FILE, FTP or the API); QWK networks as a hub or a node
- [SSH](SSH): logging in by password or key, the text BBS encrypted
- [Commodore 64s and RIPscrip terminals](PETSCII-and-RIP): PETSCII at 40 columns; menus as RIP buttons
- [VT terminals identified](Terminal-Identification) (answerback, DA1, DA2, XTVERSION, VT52): DEC line drawing and Latin-1, bold on monochrome VTs, VT52 escapes
- who's online (and how each caller came: ssh, telnet, tn3270, rlogin, modem
  with its rate), the user list, system info
- the last callers (*L*), feedback to the sysop (*K*, which any caller may
  send), the user list with each user's details and a line about themselves
- your profile: where you call from, a line about yourself, expert menus
  (the prompt alone, `?` for the rest), your password, SSH keys, ANSI and UTF-8
- [file areas](Files-and-FTP): files listed with their size, uploader, downloads and
  SHA-256, moved by XMODEM, YMODEM, ZMODEM or Kermit on the text BBS, by
  IND$FILE on a 3270 ([file transfers](File-Transfers)), or by FTP (FTPS); deleted by their uploader or the
  sysop
- [newsgroups](Newsgroups-and-NNTP): message areas given a newsgroup name are served by NNTP to
  newsreaders and exchanged with other news servers (INN and the like)
- [FidoNet](FidoNet) (or any FTN network): message areas that are echoes, netmail
  as private mail, and a binkp mailer to exchange packets with links
- [doors](Doors): DOS door games in dosemu2 (in Docker), with a FOSSIL on COM1
  and the usual drop files, or any Linux program, for text callers; doors
  elsewhere by telnet or RLogin, and the door networks BBSLink and DoorParty
- [chat](Chat-and-Paging): rooms, messages to a node, paging the sysop; the rooms are
  [IRC](IRC) channels too, for IRC clients, and [MRC](MRC)'s rooms join the
  BBSes' Multi Relay Chat
- [finger and gopher](Finger-and-Gopher): who is on, and chosen areas in gopherspace
- [a web forum](Web-Forum): the message areas as a bulletin board in the browser, in the manner of Invision Power Board 3, or of IPB 5, 4, 2 or 1, Ikonboard, vBulletin 3, 2 or Lite, or phpBB 3.3 back to 1 (themes)
- [a web terminal](HTTP-API#the-web-terminal): the BBS in a browser (xterm.js, built in)
- [an HTTP API](HTTP-API): REST, server-sent events, and WebSockets (JSON or protobuf) where web clients chat with terminal callers; Prometheus metrics
- [customization](Customization): ANSI art and 3270 screens, menus of the sysop's, @-codes, the prompts' words
- [access levels](Access-Levels): levels and flags, areas, doors and menus that require them, time and download limits a day, new users validated by the sysop
- new users sign themselves up (or not: `new_users = no`)
- [SQLite, PostgreSQL, MySQL or MariaDB](Databases), and `bbs db copy` between them

## The pages

| | |
|---|---|
| [Building](Building) | Go, the go3270-sec fork, `make` |
| [Running](Running) | first start, systemd, the defaults |
| [Administration](Administration) | the `bbs` command: users, areas, files, mail exchange |
| [Databases](Databases) | SQLite, PostgreSQL, MySQL, MariaDB; moving between them |
| [Configuration](Configuration) | `bbs.conf`: its sections, listeners |
| [Access levels](Access-Levels) | levels, flags, what areas, doors and menus require, daily limits, validation |
| [Customization](Customization) | screens (ANSI art, 3270), menus, prompts |
| [Scripting](Scripting) | the sysop's own screens in JavaScript, on every terminal |
| [SSH](SSH) | the SSH listener, keys, host keys |
| [Modem callers](Modem-Callers) | app_dmodem and modemmenu |
| [TN3270](TN3270) | 3270 terminals, PF keys, 3270 graphics |
| [Sixel graphics](Sixel-Graphics) | the logo on sixel terminals |
| [PETSCII and RIP](PETSCII-and-RIP) | Commodore 64s and 128s; RIPscrip graphics and buttons |
| [Terminal identification](Terminal-Identification) | VT terminals asked what they are: DEC graphics, monochrome, VT52 |
| [Files and FTP](Files-and-FTP) | file areas, the FTP (FTPS) server |
| [File transfers](File-Transfers) | X/Y/ZMODEM, Kermit, IND$FILE |
| [Offline mail](Offline-Mail) | QWK packets, REP replies, QWK networks |
| [Doors](Doors) | DOS doors in dosemu2 (Docker), Linux doors |
| [Newsgroups and NNTP](Newsgroups-and-NNTP) | the NNTP server, news peers |
| [FidoNet](FidoNet) | echomail, netmail, binkp |
| [Chat and paging](Chat-and-Paging) | chat rooms, node messages, paging the sysop |
| [IRC](IRC) | the IRC server: the chat rooms as channels |
| [MRC](MRC) | Multi Relay Chat: chat rooms shared with other BBSes |
| [Finger and gopher](Finger-and-Gopher) | who is on, and the BBS in gopherspace |
| [Web forum](Web-Forum) | the message areas as a web forum |
| [HTTP API](HTTP-API) | REST, events, WebSockets in JSON and protobuf, tokens, metrics |
| [Security](Security) | what is guarded, and how |
| [Development](Development) | the source's layout, the tests |

Future work is in [TODO.md](https://github.com/Neo-Desktop/bbs-sec/blob/HEAD/TODO.md).
