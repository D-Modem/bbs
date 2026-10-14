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

`bbs -config /etc/bbs/bbs.conf config` is screens on your terminal for the
file, the BBS running or not: its sections, each key's value with the
sample's comment on it, its default and examples, and whether it takes
effect at once or when the BBS starts again.  The same screens are on the
[sysop's tools](Sysop-Tools) (`F`, Configuration), on a text terminal or a
3270.

| | key | |
|---|---|---|
| sections | `S` | the file's sections; one's keys, set ones first, then those it may have.  `A` adds a key (a menu's item keys, a QWK net's conferences), `D` deletes the section (commented out, to be had back with New section), `R` renames a `[KIND NAME]` |
| a key | | `E` edits the value in place, `U` unsets it (its line commented out: the default holds), `T` takes one of the sample's examples |
| new section | `N` | a kind of section, and its name for those there may be many of.  One the file has commented out (`;[http]`) is uncommented where it is, its examples below it |
| check | `C` | the file as the BBS would read it: why it would not start, or its warnings |
| view changes | `V` | what has changed since the file was read: green takes effect at once, yellow at a restart |
| write | `W` | written whole (a new file renamed over it, its permissions and owner kept), the old one as `bbs.conf.bak`.  Not written if the BBS would not start with it, or if the file changed since it was read (`U` reads it again) |

Every comment and blank line stays where it was: a key changed is changed
on its own line, a new one goes below the sample's commented example of it,
or below the section's last key.  With no file, the screens offer to begin
it from the sample.  Values with spaces at their ends, quotes around them
or control characters are written quoted.

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
| `[general]` | `name`, `sysop`, `location`, `banner`, `banner_file`, `motd`, `motd_file`, `goodbye`, `new_users`, `max_nodes`, `attempts`, `fail_delay`, `login_timeout`, `idle_timeout`, `throttle_failures`, `throttle_time`, `page_command`, `qwk_id`, `geoip`, `geoip_credit`, `geoip_credit_url`, `phone_country`, `new_user_level`, `validated_level` |
| `[level N]` | all, and levels added or taken out |
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
