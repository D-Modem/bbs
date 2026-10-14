## Straight from app_dmodem

app_dmodem's `[bbs]` profile hands each call to a TCP server, raw.  The
`[listen modem]` of the sample is that server:

```
; dmodem.conf
[bbs]
mode = tcp
host = 127.0.0.1
port = 2323

; bbs.conf
[listen modem]
type = raw
address = 127.0.0.1:2323
```

The caller logs in to the BBS (or signs up).  Over a raw line the BBS
asks the terminal for its cursor position: an ANSI terminal (SyncTERM,
Telix, Qmodem, HyperTerminal in ANSI mode) answers, and the answer gives
its size too; anything else gets plain text.  Text is 7-bit ASCII unless
the caller turns UTF-8 on.  An app_dmodem that sends a session header
(`header = yes`) gives the BBS the call's rate and caller ID: set `header
= yes` on the listener too.

## Behind modemmenu

modemmenu logs the caller in first and offers the BBS in its menu.  Three
ways to hand the caller over, from the simplest to the closest:

**rlogin**, modemmenu's default for a BBS: it sends the caller's password
as the rlogin client user and the name as the server user (Synchronet's
convention).  The BBS checks them against its own accounts; if they are
not right there (a user with no account here, or another password) the
caller is asked to log in, with the name typed in already.

```
; modemmenu.conf
[item bbs]
key = B
label = Bulletin board
mode = rlogin
host = 127.0.0.1
port = 2513
after = menu

; bbs.conf
[listen rlogin]
type = rlogin
address = 127.0.0.1:2513
```

**A unix socket with the session header**: modemmenu has checked the
caller, and the BBS believes it (`trust_user`).  Callers need no account
of their own here first: `create_users = yes` makes one for each new name
(it has no password; it logs in only this way).  The header's rate,
caller ID and number show in the log and who's online.

```
; modemmenu.conf
[item bbs]
key = B
label = Bulletin board
mode = unix
path = /run/bbs/menu.sock
header = yes
pass_password = no
after = menu

; bbs.conf
[listen menu]
type = raw
address = /run/bbs/menu.sock
header = yes
trust_user = yes
create_users = yes
socket_mode = 0660
socket_group = asterisk
```

`trust_user` believes whoever can connect: keep the socket to modemmenu's
user (Asterisk's group, mode 0660, or 0600 and the same user).  `bbs
check` warns about a trusting socket or port that others can reach.

**telnet** works too (`mode = telnet`, `port = 2300`): modemmenu's telnet
client says it is an `ANSI` terminal, and the caller logs in again.

**See also:** [Configuration](Configuration), [File transfers](File-Transfers)

## Where callers are

`[general] phone_country` reads modem callers' numbers by every
country's numbering plan: the country (the shared calling codes told
apart: Jersey from Britain, Kazakhstan from Russia, Jamaica from the US),
what the number is (fixed line, mobile, toll-free, premium rate, VoIP...)
and the place it is in, to the exchange where the plan says (*Jersey
City, NJ*, *Ontario*, *London*, *Berlin*, *Tokyo*: 151 calling codes have
places). International numbers (`+44 20...`) are read whatever it is;
national ones, as exchanges mostly send them, are read as that country
dials, its trunk and international prefixes taken off (`4155550123`,
`1 415 555 0123` and `011 44...` with `US`; `020 7123 4567` and
`00 1 415...` with `GB`; `8 495 ...` and `810 ...` with `RU`). A local
number with no area code gives the country alone; withheld numbers
nothing.

```ini
[general]
phone_country = US
;geoip = /var/lib/bbs/dbip-country-lite.mmdb   ; for callers by address
```

The log says it with the call, the number as it is written abroad:

```
node 3: modem on [listen modem]: rate 33600/31200, from 4155550123 (+1 415-555-0123), in San Francisco, CA, United States
node 4: modem on [listen modem]: rate 26400/26400, from 08005550123 (+44 800 555 0123), in United Kingdom (toll-free)
```

Who's online shows callers' countries (by caller ID, else by address with
`[general] geoip`); the sysop's node list (spy, chat, kick) their number
(and whether it is a mobile's) and place too. Posts keep the poster's
country for the [web forum's flags](Web-Forum#flags).

The plans are Google's [libphonenumber](https://github.com/google/libphonenumber)'s
(Apache License 2.0), and North America's area codes NANPA's own
([its area code report](https://nanpa.com/reports/npa-reports)): an
area code NANPA has in service is a number even where libphonenumber
has not caught up, its state or province is NANPA's, and one announced
for a later day becomes a number on that day. Both are built into the
BBS: nothing is looked up elsewhere. To make the tables again from newer
ones (`internal/phone/gen`):

```sh
git clone --depth 1 https://github.com/google/libphonenumber
curl -O https://reports.nanpa.com/public/npa_report.csv
make phone-data LIBPHONENUMBER=libphonenumber NANPA=npa_report.csv
```

They were last made in October 2026 (NANPA's report of 14 October;
libphonenumber's data of 8 October, then missing thirteen area codes
NANPA had in service), and checked against libphonenumber's Go port
(nyaruka/phonenumbers) on 34,000 numbers: the same country, kind and
place for every one.
