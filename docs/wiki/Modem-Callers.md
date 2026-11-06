Modem callers reach the BBS through the D-Modem suite, in one of two ways:
straight from the call through
[app_dmodem](https://github.com/D-Modem/D-Modem/blob/main/docs/asterisk.md),
D-Modem's Asterisk application, or through
[modemmenu](https://github.com/D-Modem/menu) after its login. This page
covers both, how to give callers the same password everywhere, and how
the BBS works out where callers are.

## Straight from app_dmodem

app_dmodem's `[bbs]` profile hands each call to a TCP server as raw
bytes. The sample's `[listen modem]` is that server:

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

The caller then logs in to the BBS, or signs up. Over a raw line the BBS
asks the terminal for its cursor position. An ANSI terminal (SyncTERM,
Telix, Qmodem, HyperTerminal in ANSI mode) answers, and the answer also
tells the BBS the screen size; any other terminal gets plain text. Text is
7-bit ASCII unless the caller turns UTF-8 on. If app_dmodem sends a session
header (`header = yes`), the BBS learns the call's rate and caller ID; set
`header = yes` on the listener too.

## Behind modemmenu

modemmenu logs the caller in first and offers the BBS as an item in its
menu. There are three ways for it to hand the caller over, from the
simplest to the most closely linked.

**rlogin** is modemmenu's default for a BBS. It sends the caller's
password as the rlogin client user and their name as the server user
(Synchronet's convention). The BBS checks them against its own accounts.
If they do not match (the user has no account on the BBS, or a different
password), the caller is asked to log in, with their name already filled
in.

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

**A unix socket with the session header** lets the BBS trust modemmenu's
login (`trust_user`). Callers do not need an account on the BBS first:
`create_users = yes` creates one for each new name. Such an account has
no BBS password of its own; it logs in this way, or with the modemmenu
password (described below). The rate, caller ID and number from the
header appear in the log and in Who's online.

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

`trust_user` trusts whoever can connect, so make sure only modemmenu's
user can reach the socket (Asterisk's group with mode 0660, or mode 0600
and the same user). `bbs check` warns about a trusting socket or port that
others can reach.

**telnet** works too (`mode = telnet`, `port = 2300`). modemmenu's telnet
client says it is an `ANSI` terminal, and the caller logs in again.

## The same password for telnet, SSH and the web

Accounts that modemmenu logs in have no password on the BBS. With
modemmenu's auth socket, the same people can log in to the BBS any other
way (telnet, SSH, TN3270, the web forum and terminal, the API, FTP, NNTP,
IRC) with their modemmenu password. The BBS asks modemmenu, and modemmenu
asks its own backends (its users file, LDAP, SQL, or the system's accounts
through PAM), just as it does at its own login prompt. The BBS keeps
nothing of the password.

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
| `auth_socket` | none | modemmenu's `[authd] listen` |
| `create_users` | `yes` | a user modemmenu knows and the BBS does not gets an account at their first login (any way in), as `create_users` does for a listener: a name the BBS takes, not refused by `name_deny_file` |
| `timeout` | `15` | seconds for modemmenu's answer (a wrong password comes after its `fail_delay`) |

The BBS asks modemmenu only about accounts modemmenu created (external
accounts with no BBS password). An account with its own BBS password keeps
it, and the modemmenu password does not open it. A wrong password counts
as a failed login like any other (throttling, the hack log). If modemmenu
does not answer, the caller is told there is a problem, and it is not
counted against them. These passwords are changed in modemmenu (or its
backend), not in the BBS.

Callers can also sign in without typing a password. While signed in on
the BBS, the sign-in code in *Your profile* (`C`) signs in to the web
forum once, within five minutes (see
[Web forum](Web-Forum#sign-in-codes)), and SSH keys added there (`K`) log
in by SSH with no password.

## Where callers are

With `[general] phone_country` set, the BBS reads modem callers' numbers
using each country's numbering plan. From a number it learns:

- the country, even where countries share a calling code (Jersey and
  Britain, Kazakhstan and Russia, Jamaica and the US);
- what kind of number it is (fixed line, mobile, toll-free, premium rate,
  VoIP and so on);
- the place it is in, down to the exchange where the plan says (*Jersey
  City, NJ*, *Ontario*, *London*, *Berlin*, *Tokyo*; 151 calling codes have
  places).

International numbers (`+44 20...`) are read whatever `phone_country` is.
National numbers, which is how exchanges mostly send them, are read the
way that country dials, with its trunk and international prefixes removed
(`4155550123`, `1 415 555 0123` and `011 44...` with `US`; `020 7123 4567`
and `00 1 415...` with `GB`; `8 495 ...` and `810 ...` with `RU`). A local
number without an area code gives only the country, and a withheld number
gives nothing.

```ini
[general]
phone_country = US
;geoip = /var/lib/bbs/dbip-country-lite.mmdb   ; for callers by address
```

The log records this with each call, writing the number as it is written
from abroad:

```
node 3: modem on [listen modem]: rate 33600/31200, from 4155550123 (+1 415-555-0123), in San Francisco, CA, United States
node 4: modem on [listen modem]: rate 26400/26400, from 08005550123 (+44 800 555 0123), in United Kingdom (toll-free)
```

Who's online shows each caller's country (from the caller ID, or else
from their address with `[general] geoip`). The sysop's node list (spy,
chat, kick) also shows their number, whether it is a mobile, and the
place. Posts keep the poster's country for the
[web forum's flags](Web-Forum#flags).

### By address: countries, regions and cities

Callers by telnet, SSH, the web terminal and the forum are placed by
their address, using the database in `[general] geoip`. A country database
gives a country. A **city** database also gives a region (state, province
or county) and a city, which the log and the sysop's node list show
(*Mountain View, California, United States*); Who's online, the API and
the forum's flags still show only the country. Either kind works, in
either format:

| Format | Country databases | City databases |
|---|---|---|
| MaxMind DB (`.mmdb`, gzipped or not) | GeoLite2/GeoIP2 Country, DB-IP *IP to Country Lite*, IPinfo's | GeoLite2/GeoIP2 City, DB-IP *IP to City Lite* |
| CSV of ranges (gzipped or not) | DB-IP's (`start,end,CC`), IP2Location LITE DB1 | DB-IP's city (`start,end,continent,CC,region,city,lat,lon`), IP2Location LITE DB3, DB5, DB9, DB11 |

```ini
[general]
geoip = /var/lib/bbs/dbip-city-lite.mmdb
geoip_credit = IP geolocation by DB-IP
geoip_credit_url = https://db-ip.com
```

DB-IP's free city database is a monthly download
(`https://download.db-ip.com/free/dbip-city-lite-YYYY-MM.mmdb.gz`, 60 MB),
and it can stay gzipped. It takes about 120 MB of memory. Its CSV version
(86 MB gzipped, 7.8 million ranges) takes 135 MB and about 15 seconds to
read, while the `.mmdb` loads at once. The BBS reads the file when it
starts, not at the first call. If you replace the file, it is read again
in the background within a minute, and the old one answers meanwhile. The
reader was checked against MaxMind's own reader on DB-IP's city database
of October 2026, in both formats, and gave the same country, region and
city for 200,000 addresses. The image boards' flags use the same file
(loaded once) unless `[chan] geoip` says otherwise.

The numbering plans come from Google's
[libphonenumber](https://github.com/google/libphonenumber) (Apache License
2.0), and North America's area codes from NANPA's own
[area code report](https://nanpa.com/reports/npa-reports). An area code
NANPA has in service is recognised even where libphonenumber has not
caught up, its state or province is taken from NANPA, and an area code
announced for a later date starts working on that date. Both are built
into the BBS, so nothing is looked up online. To rebuild the tables from
newer data (`internal/phone/gen`):

```sh
git clone --depth 1 https://github.com/google/libphonenumber
curl -O https://reports.nanpa.com/public/npa_report.csv
make phone-data LIBPHONENUMBER=libphonenumber NANPA=npa_report.csv
```

The tables were last rebuilt in October 2026, from NANPA's report of 14
October and libphonenumber's data of 8 October (which was then missing
thirteen area codes NANPA had in service). They were checked against
libphonenumber's Go port (nyaruka/phonenumbers) on 34,000 numbers and gave
the same country, kind and place for every one.

**See also:** [Configuration](Configuration), [File transfers](File-Transfers)
