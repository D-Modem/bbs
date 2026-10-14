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
| `qwk_id` (in `[general]`), `[qwknet NAME]` | QWK packets' name; QWK networks this is a node of | [Offline mail](Offline-Mail) |
| `[door NAME]` | a door program | [Doors](Doors) |
| `screens_dir` (in `[general]`), `[menu NAME]`, `[strings]` | the sysop's screens, menus and prompts | [Customization](Customization) |
| `scripts_dir` (in `[general]`) | the sysop's JavaScript | [Scripting](Scripting) |
| `[security]` | deny lists, bans, the hack log | [Security](Security) |
| `[event NAME]` | scheduled programs | [Sysop tools](Sysop-Tools) |
| `[finger]`, `[gopher]` | the finger and gopher servers | [Finger and gopher](Finger-and-Gopher) |
| `[irc]` | the IRC server for the chat rooms | [IRC](IRC) |
| `[mrc]` | Multi Relay Chat | [MRC](MRC) |
| `database` (in `[general]`) | SQLite's file, or a PostgreSQL, MySQL or MariaDB server | [Databases](Databases) |
| `[level N]`, `new_user_level`, `validated_level` (in `[general]`) | access levels | [Access levels](Access-Levels) |
| `[http]` | the HTTP API, its WebSockets, the web forum, the web terminal and metrics | [HTTP API](HTTP-API), [Web forum](Web-Forum) |
| `[chan]`, `chan` (in `[http]`) | the image boards | [Image boards](Image-Boards) |
| `page_command` (in `[general]`) | telling the sysop of a page when they are not on | [Chat and paging](Chat-and-Paging) |
| `geoip`, `geoip_credit`, `phone_country` (in `[general]`) | where callers are: a country database for addresses, the home country of caller IDs | [Modem callers](Modem-Callers#where-callers-are), [Web forum](Web-Forum#flags) |

`bbs check` reads the file and says what it found, with a warning for
anything risky (a password that may cross in the clear, a socket that
trusts whoever connects, a link without a password).

## Editing it: the configuration screens

`bbs -config /etc/bbs/bbs.conf config` is full-screen menus on your
terminal for the file and the accounts, the BBS running or not.  The same
menus are on the [sysop's tools](Sysop-Tools) (`F`, Configuration): full
screen on ANSI terminals, with PF keys on a 3270, and numbered lines on a
terminal with neither.

| menu | what is in it |
|---|---|
| Board configuration | the BBS's name, the sysop's, the location, postal address, dial-in numbers and internet addresses; the banner, message of the day, goodbye, logo; menus and prompts |
| System configuration | the database and log; where files are kept and how large they may be; deny lists, the hack log, bans; scheduled events; every section of the file as it is |
| Connections and modems | modem lines and every listener; callers at once, logins, time-outs, lock-outs; the caller ID country and GeoIP |
| Services | telnet, TN3270, SSH and RLogin listeners; finger, gopher, FTP, IRC, news, the web (API, forum, image boards); MRC, FidoNet, QWK networks, doors |
| Users and groups | the accounts (level, flags, sysop, locked, a new password; new ones; deleted ones) and the access levels |
| Maintenance | disk space; what message areas keep, old messages taken out; the image boards' archive; backups: one now, those made, `[backup]` ([Backups](Backups)) |

Running as another user and group, and PAM and LDAP logins, are shown as
not available yet: they are to come.

| key | |
|---|---|
| arrows, PgUp, PgDn, Home, End | move (a letter goes to the next line beginning with it) |
| Enter | open a menu, change a value (typed on the foot line, Enter to keep it), do an action |
| Space | turn a yes/no over, or go to the next choice |
| Esc, left arrow | back |
| F1 or `?` | the whole help for the line (from bbs.conf.sample) |
| F9 | quit without saving (after a yes, if anything changed) |
| F10 | save and exit |
| `A`, `D` | add one (a listener, a door, a user ...), delete it |

On a 3270, `S` by a menu opens it, values are typed over and sent with
Enter, PF1 is help, PF3 back, PF9 and PF10 as F9 and F10.  On a terminal
without ANSI, a number opens or changes a line, `S` saves and exits, `X`
leaves without saving.

Nothing changes until you save: a changed line is marked `*` and the
foot counts what is not saved.  The main menu's Review changes lists it
all, and Check the configuration reads the file as the BBS would.  On
saving, the file is checked first (it is not saved if the BBS would not
start with it, or if someone changed it since it was read), written whole
with its permissions and owner kept and the last one as `bbs.conf.bak`,
every comment kept where it was (a new key goes below the sample's
commented example of it); then the accounts' and areas' changes are made.
On the sysop's menu, what may change while callers are on is put in force
at once, and the screen says what waits for a restart.  With no file, the
screens offer to begin it from the sample.

A key at a time, for scripts:

    bbs -config /etc/bbs/bbs.conf config get general name
    bbs -config /etc/bbs/bbs.conf config get "listen telnet"
    bbs -config /etc/bbs/bbs.conf config set "listen ssh" address :2222
    bbs -config /etc/bbs/bbs.conf config unset general motd

each checked as the BBS would read it before it is written, and what
changed printed with when it takes effect.

## Changing it while callers are on

`systemctl reload bbs` (`ExecReload` in bbs.service.sample: SIGHUP) reads
the file again.  A file the BBS would not start with changes nothing (the
log says why).  Otherwise these take effect at once, and the log lists the
rest, which wait for a restart:

| section | live keys |
|---|---|
| `[general]` | `name`, `sysop`, `location`, `postal_address`, `phone`, `addresses`, `banner`, `banner_file`, `motd`, `motd_file`, `goodbye`, `new_users`, `max_nodes`, `attempts`, `fail_delay`, `login_timeout`, `idle_timeout`, `throttle_failures`, `throttle_time`, `page_command`, `qwk_id`, `geoip`, `geoip_credit`, `geoip_credit_url`, `phone_country`, `new_user_level`, `validated_level` |
| `[level N]` | all, and levels added or taken out |
| `[backup]` | all ([Backups](Backups)) |
| `[menu NAME]`, `[strings]` | all (a caller in a menu has it until they leave it) |
| `[chan]` | all but `dir` |
| `[http]` | `forum_guests`, `forum_theme`, `forum_flags` |

Listeners, servers (FTP, NNTP, IRC, the HTTP API ...), networks, doors, the
database and the log are made when the BBS starts, so they wait for a
restart; so does what a caller's session took when it began (its idle
timeout).  The sysop's Configuration screen puts the file in force itself
when it writes it, and says which changes took effect and which wait.
bbs.service.sample's `ProtectSystem=strict` keeps the service from writing
/etc: give it `ReadWritePaths=/etc/bbs` (and the bbs user the file) for the
screen on the BBS to write; `bbs config` as root works either way.  Mind that a file the BBS may write is one any sysop may give
commands to run (doors, events, `page_command`): leave it read-only where
sysops are not all trusted as much as root.

**See also:** [Modem callers](Modem-Callers), [TN3270](TN3270), [Security](Security), [Sysop tools](Sysop-Tools)
