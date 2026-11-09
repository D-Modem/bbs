<img src="https://raw.githubusercontent.com/D-Modem/bbs/main/.github/banner.png" alt="D-Modem/bbs: A bulletin board for every caller" width="100%">

bbs is a bulletin board system in Go for telnet, SSH, TN3270 and modem
callers, and part of the [D-Modem suite](https://github.com/D-Modem/D-Modem).
This is its documentation: how to set it up, how to run it, and how to use
everything it does.

One telnet port serves every kind of terminal. A TN3270 client (x3270,
c3270, wc3270, Vista, PCOMM) gets full 3270 screens with PF keys, through
[go3270](https://github.com/D-Modem/go3270). Any other client gets a text
BBS with ANSI colour where the terminal has it. Modem callers come in
through the D-Modem suite: [app_dmodem](https://github.com/D-Modem/D-Modem)
straight from Asterisk, or [modemmenu](https://github.com/D-Modem/menu)
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

## Start here

| If you want to | Read |
|---|---|
| Try it out in a few minutes | [Building](Building), then [Running](Running) |
| Set up a board for real | [Running](Running), [Configuration](Configuration), [Databases](Databases), [Backups](Backups) |
| Look after a running board | [Administration](Administration), [Sysop tools](Sysop-Tools), [Access levels](Access-Levels) |
| Make it look like your own | [Customization](Customization), [Scripting](Scripting) |
| Take calls by modem | [Modem callers](Modem-Callers) |
| Join FidoNet, QWK networks or Usenet | [FidoNet](FidoNet), [Offline mail](Offline-Mail#qwk-networks), [Newsgroups and NNTP](Newsgroups-and-NNTP) |
| Give users e-mail addresses | [E-mail](E-mail) |
| Put the board on the web | [Web forum](Web-Forum), [Image boards](Image-Boards), [HTTP API](HTTP-API) |
| Work on the code | [Development](Development) |

Every setting is listed in the [Configuration reference](Configuration-Reference),
which is generated from `bbs.conf.sample`.

## What it does

**For callers**

- [Message areas](Message-Areas) with new scans, searching, threads, votes
  and polls, written in a [full-screen editor](Writing-Messages)
- Private mail, chat rooms, node messages and [paging the sysop](Chat-and-Paging)
- Who's online (with how each caller came in), the user list, the last
  callers, feedback to the sysop, and a profile of their own
- [File areas](Files-and-FTP) with X/Y/ZMODEM, Kermit and IND$FILE
  [transfers](File-Transfers), and an FTP (FTPS) server
- [Offline mail](Offline-Mail): QWK packets of new messages, and REP packets
  of replies
- [Doors](Doors): DOS door games in dosemu2, Linux programs, and door
  networks such as BBSLink and DoorParty

**Terminals**

- [TN3270](TN3270) screens with PF keys, and 3270 graphics where the
  emulator has them
- [SSH](SSH), with passwords or keys
- [Commodore 64s in PETSCII and RIPscrip terminals](PETSCII-and-RIP)
- [VT terminals](Terminal-Identification), with DEC line drawing, monochrome
  and VT52 support
- [Sixel graphics](Sixel-Graphics) for the logo and pictures

**Networks**

- [FidoNet](FidoNet) and other FTN networks: echomail, netmail, file
  echoes, AreaFix and a built-in binkp mailer
- [E-mail](E-mail): each user an address, their BBS mail read over IMAP
  and POP3 and sent over SMTP, to and from the internet
- [Newsgroups](Newsgroups-and-NNTP) served by NNTP and exchanged with other
  news servers
- [IRC](IRC), where the chat rooms are channels, [AIM and ICQ](AIM-and-ICQ)
  for AIM and ICQ clients on the same chat, [MSN Messenger](MSN-Messenger)
  for MSN clients, and [MRC](MRC), the chat
  network shared with other BBSes
- [Finger, gopher and Gemini](Finger-and-Gopher)
- [Time](Time): NTP, NTS and PTP, followed and served

**On the web**

- [A web forum](Web-Forum) of the message areas, in the style of classic
  forum software (Invision Power Board, vBulletin, phpBB and many more)
- [Image boards](Image-Boards) in the style of kusaba x and 4chan, also
  shown on the terminals
- [A web terminal](HTTP-API#the-web-terminal) and an [HTTP API](HTTP-API)
  with REST, server-sent events and WebSockets

**For the sysop**

- New users who sign themselves up (or not, with `new_users = no`), and
  [access levels](Access-Levels) with flags, daily limits and validation
- [Customization](Customization) with ANSI art, 3270 screens, menus and
  prompts, and [scripting](Scripting) in JavaScript
- [SQLite, PostgreSQL, MySQL or MariaDB](Databases), with `bbs db copy`
  between them
- [Backups](Backups) while callers are on, and [security](Security)
  settings to keep callers out

## All pages

| Page | What it covers |
|---|---|
| [Building](Building) | Go, the go3270 fork, `make` |
| [Running](Running) | the first start, systemd, running as its own user |
| [Administration](Administration) | the `bbs` command: users, areas, files, mail exchange |
| [Sysop tools](Sysop-Tools) | the sysop's menu inside the BBS, and scheduled events |
| [Configuration](Configuration) | `bbs.conf`: its sections, listeners and configuration screens |
| [Configuration reference](Configuration-Reference) | every section and key |
| [Databases](Databases) | SQLite, PostgreSQL, MySQL and MariaDB, and moving between them |
| [Backups](Backups) | making backups, and restoring them |
| [Customization](Customization) | screens (ANSI art, 3270), menus, prompts |
| [Scripting](Scripting) | the sysop's own screens in JavaScript, on every terminal |
| [Access levels](Access-Levels) | levels, flags, requirements, daily limits, validation |
| [SSH](SSH) | the SSH listener, keys, host keys |
| [Modem callers](Modem-Callers) | app_dmodem and modemmenu, and where callers are |
| [TN3270](TN3270) | 3270 terminals, PF keys, 3270 graphics |
| [Sixel graphics](Sixel-Graphics) | the logo and pictures on sixel terminals |
| [PETSCII and RIP](PETSCII-and-RIP) | Commodore 64s and 128s; RIPscrip graphics and buttons |
| [Terminal identification](Terminal-Identification) | how VT terminals are recognised: DEC graphics, monochrome, VT52 |
| [Message areas](Message-Areas) | reading, new scans, searching, votes and polls |
| [Writing messages](Writing-Messages) | the full-screen editor, the line editor, the 3270 editor |
| [Offline mail](Offline-Mail) | QWK packets, REP replies, QWK networks |
| [Files and FTP](Files-and-FTP) | file areas, the FTP (FTPS) server |
| [File transfers](File-Transfers) | X/Y/ZMODEM, Kermit, IND$FILE |
| [Doors](Doors) | DOS doors in dosemu2 (Docker), Linux doors, doors on other systems |
| [E-mail](E-mail) | addresses, IMAP, POP3, SMTP, the MX, a smarthost, DNS |
| [Newsgroups and NNTP](Newsgroups-and-NNTP) | the NNTP server, news peers |
| [FidoNet](FidoNet) | echomail, netmail, binkp, file echoes, AreaFix, nodelists |
| [Chat and paging](Chat-and-Paging) | chat rooms, node messages, paging the sysop |
| [IRC](IRC) | the IRC server, with the chat rooms as channels |
| [AIM and ICQ](AIM-and-ICQ) | the OSCAR server: AIM and ICQ clients, buddies, IMs and chat rooms |
| [IRC server linking](IRC-Linking) | The BBS as a hub server on an IRC network (TS6 or P10): every user reachable there |
| [AOL clients](AOL) | a host for AOL's own clients (3.0, 4.0): sign-on with BBS accounts |
| [MSN Messenger](MSN-Messenger) | the MSN server: MSN Messenger 1.0 to 4.7, contacts and IMs |
| [MSN Classic](MSN-Classic) | The Microsoft Network for Windows 95's client: sign-in, the areas as Categories, bulletin boards |
| [MRC](MRC) | Multi Relay Chat: chat rooms shared with other BBSes |
| [Finger, gopher and Gemini](Finger-and-Gopher) | who is on, and the BBS in gopherspace and Geminispace |
| [Time](Time) | NTP, NTS and PTP: the clock following time servers, and serving time |
| [Web forum](Web-Forum) | the message areas as a web forum |
| [Image boards](Image-Boards) | anonymous image boards, in the style of kusaba x and 4chan |
| [HTTP API](HTTP-API) | REST, events, WebSockets in JSON and protobuf, tokens, metrics |
| [Security](Security) | what is guarded, and how |
| [Development](Development) | the source's layout, the shared screens, the tests |

## More

- The wiki is also a website: [d-modem.github.io/bbs](https://d-modem.github.io/bbs/).
- Planned work is in [TODO.md](https://github.com/D-Modem/bbs/blob/HEAD/TODO.md).
- The other parts of the suite are listed in the
  [README](https://github.com/D-Modem/bbs#the-d-modem-suite).
