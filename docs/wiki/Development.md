## Layout

| | |
|---|---|
| `cmd/bbs` | the command: serve, check, init, user and area administration |
| `internal/server` | listeners, session headers, rlogin, telling 3270s from the rest |
| `internal/telnet` | the server side of telnet: options, terminal type, window size, the handoff to go3270 |
| `internal/textui` | the text BBS: line editing, ANSI detection, paging, the menus |
| `internal/ui3270` | the 3270 BBS: screens and PF keys on go3270, and its graphics |
| `internal/sixel` | the sixel encoder |
| `internal/files` | file areas' storage: names, uploads, limits |
| `internal/ftp` | the FTP server: the file areas as a file system |
| `internal/news` | articles: parsing, checking, making, decoding; wildmats |
| `internal/nntp` | the NNTP server and the peers' client |
| `internal/ftn` | FidoNet's formats: addresses, Type 2+ packets, messages, kludges, character sets |
| `internal/fido` | the BBS on FidoNet: tosser, scanner, netmail, the binkp mailer |
| `internal/door` | doors: drop files, running a door on the caller's line |
| `doors/dosemu2` | the Docker image DOS doors run in: dosemu2, its FOSSIL on COM1 |
| `internal/xfer` | XMODEM, YMODEM, ZMODEM and Kermit, both ways |
| `internal/logo` | the logo: a PNG, or the name in a pixel font |
| `internal/bbs` | the BBS itself: nodes, logins, new users, posting, cleaning text |
| `internal/store` | the SQLite database |
| `internal/config` | the configuration file |

## Tests

`make test` runs the unit tests (text cleaning, names and passwords, the
configuration, telnet negotiation and the 3270 handoff) and whole sessions
against a running server on unix sockets: a modem caller with a trusted
modemmenu header, one whose header is not trusted, a login and a post, a
new user, rlogin with a right and a wrong password and trusted, throttling
by caller ID, all lines busy, a telnet ANSI client, a TN3270 client
taken through go3270's negotiation to the logon screen and out with PF3,
the transfer protocols against lrzsz (`sx`/`rx`, `sb`/`rb`, `sz`/`rz`) and
C-Kermit both ways, and ZMODEM over a line that damages bytes; uploads and
downloads through the BBS by ZMODEM, Kermit, YMODEM and XMODEM-1K;
IND$FILE with a real s3270 (upload, download, a file that is not there);
the file store (names, limits, the daily allowance, sysop-only areas),
the FTP server with a real FTP client (uploads, downloads and their
count, every refused operation, path traversal, wrong logins, anonymous
access, TLS required and explicit TLS, the size limit, the per-address
limit), telnet terminals with and without sixel, the sixel encoder checked by
decoding its output with ImageMagick (when installed), and TN3270
clients that report programmed symbols (the logo is loaded and
shown), vector graphics (the logo is drawn) or neither (nothing is sent),
articles (parsing, limits, decoding, wildmats), the NNTP server with
Python's nntplib as a newsreader (login, reading, overview, posting), its
refusals (posting without a login, AUTHINFO before STARTTLS, forged
headers, control messages, oversized articles, IHAVE from a stranger), two
BBSes peered both ways, and IHAVE.  With `BBS_TEST_INN=host:port` it also
syncs with a running INN.  FidoNet: packets and messages, a hub with two
nodes exchanging echomail by binkp (SEEN-BY, PATH, threads, forwarding),
netmail and replies, wrong passwords, unknown addresses and impostors,
dupes, loops, bad and foreign packets, the echo-newsgroup gateway, and,
when binkd and CrashMail are installed, a hub run by them: echomail and
netmail both ways, calling it and called by it.  Doors: the drop files,
the runner (CP437 and UTF-8, time limit, hanging up, idle, single-caller
doors, a pseudo-terminal), a caller opening a door from the menu, and,
when the bbs-dosemu2 image is built, a real DOS door talking by FOSSIL in
it.

**See also:** [Building](Building)
