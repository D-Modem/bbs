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

- [message areas](Administration#message-areas): read new, list, read by number, post, reply (quoting),
  reply by mail; areas only the sysop posts in; authors and the sysop
  delete messages
- private mail: inbox, read, reply, delete
- [SSH](SSH): logging in by password or key, the text BBS encrypted
- who's online (and how each caller came: ssh, telnet, tn3270, rlogin, modem
  with its rate), the user list, system info
- your profile: where you call from, your password, SSH keys, ANSI and UTF-8
- [file areas](Files-and-FTP): files listed with their size, uploader, downloads and
  SHA-256, moved by XMODEM, YMODEM, ZMODEM or Kermit on the text BBS, by
  IND$FILE on a 3270 ([file transfers](File-Transfers)), or by FTP (FTPS); deleted by their uploader or the
  sysop
- [newsgroups](Newsgroups-and-NNTP): message areas given a newsgroup name are served by NNTP to
  newsreaders and exchanged with other news servers (INN and the like)
- [FidoNet](FidoNet) (or any FTN network): message areas that are echoes, netmail
  as private mail, and a binkp mailer to exchange packets with links
- [doors](Doors): DOS door games in dosemu2 (in Docker), with a FOSSIL on COM1
  and the usual drop files, or any Linux program, for text callers
- [chat](Chat-and-Paging): rooms, messages to a node, paging the sysop
- [finger and gopher](Finger-and-Gopher): who is on, and chosen areas in gopherspace
- [an HTTP API](HTTP-API): REST, server-sent events, and WebSockets (JSON or protobuf) where web clients chat with terminal callers; Prometheus metrics
- [customization](Customization): ANSI art and 3270 screens, menus of the sysop's, @-codes, the prompts' words
- new users sign themselves up (or not: `new_users = no`)
- SQLite storage, one file

## The pages

| | |
|---|---|
| [Building](Building) | Go, the go3270-sec fork, `make` |
| [Running](Running) | first start, systemd, the defaults |
| [Administration](Administration) | the `bbs` command: users, areas, files, mail exchange |
| [Configuration](Configuration) | `bbs.conf`: its sections, listeners |
| [Customization](Customization) | screens (ANSI art, 3270), menus, prompts |
| [SSH](SSH) | the SSH listener, keys, host keys |
| [Modem callers](Modem-Callers) | app_dmodem and modemmenu |
| [TN3270](TN3270) | 3270 terminals, PF keys, 3270 graphics |
| [Sixel graphics](Sixel-Graphics) | the logo on sixel terminals |
| [Files and FTP](Files-and-FTP) | file areas, the FTP (FTPS) server |
| [File transfers](File-Transfers) | X/Y/ZMODEM, Kermit, IND$FILE |
| [Doors](Doors) | DOS doors in dosemu2 (Docker), Linux doors |
| [Newsgroups and NNTP](Newsgroups-and-NNTP) | the NNTP server, news peers |
| [FidoNet](FidoNet) | echomail, netmail, binkp |
| [Chat and paging](Chat-and-Paging) | chat rooms, node messages, paging the sysop |
| [Finger and gopher](Finger-and-Gopher) | who is on, and the BBS in gopherspace |
| [HTTP API](HTTP-API) | REST, events, WebSockets in JSON and protobuf, tokens, metrics |
| [Security](Security) | what is guarded, and how |
| [Development](Development) | the source's layout, the tests |

Future work is in [TODO.md](https://github.com/Neo-Desktop/bbs-sec/blob/HEAD/TODO.md).
