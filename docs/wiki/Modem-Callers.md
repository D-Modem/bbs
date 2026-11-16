Modem callers connect to the BBS through the D-Modem suite, in one of two
ways:

- Directly from the call through
  [app_dmodem](https://github.com/D-Modem/D-Modem/blob/main/docs/asterisk.md),
  the Asterisk application of D-Modem.
- Through [modemmenu](https://github.com/D-Modem/menu) after its login.

This page tells about the two ways. It also tells how to give callers
the same password on all routes, and how the BBS finds the locations of
callers.

## Straight from app_dmodem

The `[bbs]` profile of app_dmodem gives each call to a TCP server as raw
bytes. The `[listen modem]` of the sample is that server:

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

Then the caller logs in to the BBS, or signs up. Over a raw line, the BBS
asks the terminal for its cursor position. An ANSI terminal (SyncTERM,
Telix, Qmodem, HyperTerminal in ANSI mode) answers. The answer also tells
the BBS the screen size. All other terminals get plain text. Text is
7-bit ASCII unless the caller enables UTF-8. If app_dmodem sends a
session header (`header = yes`), the BBS gets the rate and caller ID of
the call. Also set `header = yes` on the listener.

## Behind modemmenu

modemmenu logs in the caller first, and offers the BBS as an item in its
menu. There are three ways for modemmenu to give the caller to the BBS.
The list starts with the simplest and ends with the most closely linked.

**rlogin** is the default of modemmenu for a BBS. It sends the password
of the caller as the rlogin client user, and the name as the server user
(the convention of Synchronet). The BBS checks them against its own
accounts. If they do not agree (the user has no account on the BBS, or a
different password), the BBS asks the caller to log in, with the name
already filled in.

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

**A unix socket with the session header** lets the BBS trust the login of
modemmenu (`trust_user`). Callers do not need an account on the BBS
first: `create_users = yes` creates one for each new name. This type of
account has no BBS password of its own. It logs in in this way, or with
the modemmenu password (refer to the text below). The rate, caller ID and
number from the header show in the log and in Who's online.

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

`trust_user` trusts all processes that can connect. Thus, make sure that
only the user of modemmenu can get to the socket (the group of Asterisk
with mode 0660, or mode 0600 and the same user). `bbs check` gives a
warning about a trusting socket or port that others can get to.

**telnet** also operates (`mode = telnet`, `port = 2300`). The telnet
client of modemmenu says that it is an `ANSI` terminal, and the caller
logs in again.

## The same password for telnet, SSH and the web

Accounts that modemmenu logs in have no password on the BBS. With the
auth socket of modemmenu, the same persons can log in to the BBS by all
other routes (telnet, SSH, TN3270, the web forum and terminal, the API,
FTP, NNTP, IRC) with their modemmenu password. The BBS asks modemmenu.
modemmenu asks its own backends (its users file, LDAP, SQL, or the
accounts of the system through PAM), as it does at its own login prompt.
The BBS keeps no part of the password.

```
; modemmenu.conf: answered by modemmenu -serve, or modemmenu -authd alone
[authd]
listen = /run/modemmenu/auth.sock
socket_mode = 0660
socket_group = bbs
allow_users = bbs

; bbs.conf
[modemmenu]
auth_socket = /run/modemmenu/auth.sock
```

| `[modemmenu]` key | Default | What it sets |
|---|---|---|
| `auth_socket` | none | the `[authd] listen` of modemmenu |
| `create_users` | `yes` | if modemmenu knows a user and the BBS does not, the user gets an account at the first login (by any route), as `create_users` does for a listener. The name must be a name that the BBS accepts, and `name_deny_file` must not refuse it |
| `timeout` | `15` | seconds for the answer of modemmenu (the answer for an incorrect password comes after its `fail_delay`) |

The BBS asks modemmenu only about accounts that modemmenu created
(external accounts with no BBS password). An account with its own BBS
password keeps it, and the modemmenu password does not open it. An
incorrect password counts as a failed login, as all others do
(throttling, the hack log). If modemmenu does not answer, the BBS tells
the caller that there is a problem, and does not count it against the
caller. Change these passwords in modemmenu (or its backend), not in the
BBS.

Callers can also sign in without a password. While a caller is signed in
on the BBS, the sign-in code in *Your profile* (`C`) signs in to the web
forum one time, within five minutes (refer to
[Web forum](Web-Forum#sign-in-codes)). SSH keys added there (`K`) log in
by SSH with no password.

## Where callers are

If `[general] phone_country` is set, the BBS reads the numbers of modem
callers with the numbering plan of each country. From a number, it
finds:

- The country, also where countries share a calling code (Jersey and
  Britain, Kazakhstan and Russia, Jamaica and the US).
- The type of number (fixed line, mobile, toll-free, premium rate, VoIP
  and other types).
- The place of the number, down to the exchange if the plan gives it
  (*Jersey City, NJ*, *Ontario*, *London*, *Berlin*, *Tokyo*. 151 calling
  codes have places).

The BBS reads international numbers (`+44 20...`) for all values of
`phone_country`. It reads national numbers, the usual format of
exchanges, as that country dials them, with its trunk and international
prefixes removed (`4155550123`, `1 415 555 0123` and `011 44...` with
`US`. `020 7123 4567` and `00 1 415...` with `GB`. `8 495 ...` and
`810 ...` with `RU`). A local number without an area code gives only the
country. A withheld number gives nothing.

```ini
[general]
phone_country = US
;geoip = /var/lib/bbs/dbip-country-lite.mmdb   ; for callers by address
```

The log records this with each call. It writes the number as callers
from other countries write it:

```
node 3: modem on [listen modem]: rate 33600/31200, from 4155550123 (+1 415-555-0123), in San Francisco, CA, United States
node 4: modem on [listen modem]: rate 26400/26400, from 08005550123 (+44 800 555 0123), in United Kingdom (toll-free)
```

Who's online shows the country of each caller (from the caller ID, or
from the address with `[general] geoip`). The node list of the sysop
(spy, chat, kick) also shows the number, if it is a mobile, and the
place. Posts keep the country of the poster for the
[flags of the web forum](Web-Forum#flags).

### By address: countries, regions and cities

The BBS finds the location of callers by telnet, SSH, the web terminal
and the forum from their address, with the database in
`[general] geoip`. A country database gives a country. A **city**
database also gives a region (state, province or county) and a city. The
log and the node list of the sysop show them
(*Mountain View, California, United States*). Who's online, the API and
the flags of the forum still show only the country. The two types of
database operate, in the two formats:

| Format | Country databases | City databases |
|---|---|---|
| MaxMind DB (`.mmdb`, gzipped or not) | GeoLite2/GeoIP2 Country, DB-IP *IP to Country Lite*, the database of IPinfo | GeoLite2/GeoIP2 City, DB-IP *IP to City Lite* |
| CSV of ranges (gzipped or not) | the database of DB-IP (`start,end,CC`), IP2Location LITE DB1 | the city database of DB-IP (`start,end,continent,CC,region,city,lat,lon`), IP2Location LITE DB3, DB5, DB9, DB11 |

```ini
[general]
geoip = /var/lib/bbs/dbip-city-lite.mmdb
geoip_credit = IP geolocation by DB-IP
geoip_credit_url = https://db-ip.com
```

The free city database of DB-IP is a monthly download
(`https://download.db-ip.com/free/dbip-city-lite-YYYY-MM.mmdb.gz`, 60 MB).
It can stay gzipped. It uses about 120 MB of memory. Its CSV version (86
MB gzipped, 7.8 million ranges) uses 135 MB and about 15 seconds to
read. The `.mmdb` loads immediately. The BBS reads the file when it
starts, not at the first call. If you replace the file, the BBS reads it
again in the background within one minute. The old file answers during
that time.

We checked the reader against the reader of MaxMind on the city database
of DB-IP of October 2026, in the two formats. It gave the same country,
region and city for 200,000 addresses. The flags of the image boards use
the same file (loaded one time) unless `[chan] geoip` sets a different
file.

The numbering plans come from
[libphonenumber](https://github.com/google/libphonenumber) of Google
(Apache License 2.0). The area codes of North America come from the
[area code report](https://nanpa.com/reports/npa-reports) of NANPA. The
BBS recognises an area code that NANPA has in service, also if
libphonenumber does not have it yet. The BBS takes its state or province
from NANPA. An area code that NANPA announced for a later date starts to
operate on that date. The two data sets are part of the BBS. Thus, the
BBS looks up nothing online. To build the tables again from newer data
(`internal/phone/gen`):

```sh
git clone --depth 1 https://github.com/google/libphonenumber
curl -O https://reports.nanpa.com/public/npa_report.csv
make phone-data LIBPHONENUMBER=libphonenumber NANPA=npa_report.csv
```

We last built the tables in October 2026, from the report of NANPA of 14
October and the data of libphonenumber of 8 October (this data did not
have thirteen area codes that NANPA had in service). We checked them
against the Go port of libphonenumber (nyaruka/phonenumbers) on 34,000
numbers. They gave the same country, type and place for all numbers.

### A modem call by VoIP without a caller ID

If a call has no caller ID, the BBS uses the address from which the call
came over VoIP, if the modem server gives it (`DMODEM_PEER` in the
session header: `203.0.113.5`, `203.0.113.5:5060` or `sip:...@203.0.113.5`
all operate). The BBS looks up the address in `[general] geoip`, as all
other addresses. This is only an estimate: a call through a VoIP provider
gives the address of the provider, not the address of the caller.

### New users, and users who share where they are

For a new user, the BBS fills in *Where are you calling from?* with the
location from the caller ID or the address (*San Francisco, CA, United
States*, *London, England, United Kingdom*). The user can change it or
clear it. The sign-up form of the web forum does the same.
`geoip_prefill = no` disables this.

Then the BBS asks new users if the sysop can see their location. The
answer is no unless the user says yes. The user can change it at all
times in the profile (*Share where you are with the sysop*), or through
the API (`PATCH /api/v1/me` with `share_location`). For a user who says
yes, the BBS keeps the place at each login, with the method (caller ID,
address, VoIP address) and the date. Sysops see it on the page of the
user (in the user list, in the user editor of the sysop and in
`bbs config`) and in the `geo` of the API. If a user stops the share, the
BBS deletes the kept data immediately. `geoip_users = no` stops the
question, and stops the records. The BBS asks the question only if it
has a method to find the place (`geoip` or `phone_country`).

```ini
[general]
geoip_users = yes     ; ask users whether the sysop may see where they are
geoip_prefill = yes   ; fill in a new user's location
```

**See also:** [Configuration](Configuration), [File transfers](File-Transfers)
