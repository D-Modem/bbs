[bbs.conf.sample](https://github.com/Neo-Desktop/bbs-sec/blob/HEAD/bbs.conf.sample) has every option.  `[general]` is the
system (name, database, log, banner, message of the day, timeouts, limits);
each `[listen NAME]` is a port or unix socket:

| `type` | what comes in |
|---|---|
| `telnet` | a telnet client; with `tn3270 = yes` (the default) 3270s get 3270 screens |
| `raw` | bytes as they come: app_dmodem's `mode = tcp`, a modemmenu `tcp` or `unix` item |
| `rlogin` | RFC 1282, as modemmenu's `rlogin` items speak it |
| `ssh` | SSH: the text BBS, logged in by password or key ([SSH](SSH)) |

`address` is `host:port`, or a path for a unix socket (`socket_mode`,
`socket_group`).  Any listener may read a session header first (`header =
yes`), and with `trust_user = yes` take the caller as logged in already: see
[Modem callers](Modem-Callers) (not `ssh`, which logs its callers in itself).  `petscii` and `rip` say whether
its callers may be Commodores or RIPscrip terminals: [PETSCII and RIP](PETSCII-and-RIP); `vt`
whether terminals are asked what they are: [Terminal identification](Terminal-Identification).

## The sections

| section | what | page |
|---|---|---|
| `[general]` | the system: name, sysop, database, log, banner, message of the day, logo, new users, limits, timeouts, file areas' directory and limits | here, [Security](Security) |
| `[listen NAME]` | a port or socket callers come in on | here, [Modem callers](Modem-Callers), [TN3270](TN3270) |
| `[ftp]` | the FTP server for the file areas | [Files and FTP](Files-and-FTP) |
| `[news]`, `[nntp]`, `[peer NAME]` | newsgroups, the NNTP server, news peers | [Newsgroups and NNTP](Newsgroups-and-NNTP) |
| `[fido]`, `[binkp]`, `[link NAME]` | FidoNet: our address, the binkp mailer, links | [FidoNet](FidoNet) |
| `[door NAME]` | a door program | [Doors](Doors) |
| `screens_dir` (in `[general]`), `[menu NAME]`, `[strings]` | the sysop's screens, menus and prompts | [Customization](Customization) |
| `[finger]`, `[gopher]` | the finger and gopher servers | [Finger and gopher](Finger-and-Gopher) |
| `[level N]`, `new_user_level`, `validated_level` (in `[general]`) | access levels | [Access levels](Access-Levels) |
| `[http]` | the HTTP API, its WebSockets, the web terminal and metrics | [HTTP API](HTTP-API) |
| `page_command` (in `[general]`) | telling the sysop of a page when they are not on | [Chat and paging](Chat-and-Paging) |

`bbs check` reads the file and says what it found, with a warning for
anything risky (a password that may cross in the clear, a socket that
trusts whoever connects, a link without a password).

**See also:** [Modem callers](Modem-Callers), [TN3270](TN3270), [Security](Security)
