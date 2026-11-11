The BBS can be a DNS server for its callers, dial-up users above all. It
answers the names of the old services' servers with the BBS's own
address, so the clients of the time find the BBS without any hosts file
or registry edits:

| Front end | Names answered |
|---|---|
| [MSN Messenger](MSN-Messenger) (`[msn]`) | `messenger.hotmail.com`, `nexus.passport.com`, `login.passport.com`, `loginnet.passport.com`, `login.live.com` |
| [MSN Classic](MSN-Classic) (`[marvel]`, or a listener with `frontend = marvel`) | `gateway.moswest.msn.net` |
| [AIM and ICQ](AIM-and-ICQ) (`[oscar]`) | `login.oscar.aol.com`, `slogin.oscar.aol.com`, `login.messaging.aol.com`, `kdc.uas.aol.com`, `api.screenname.aol.com`, `api.oscar.aol.com`, `login.icq.com`, `slogin.icq.com` |
| [AOL clients](AOL) (`[aol]`) | `AmericaOnline.aol.com` |
| [Yahoo! Messenger](Yahoo-Messenger) (`[ymsg]`) | `scs.msg.yahoo.com`, `scsa`/`scsb`/`scsc.msg.yahoo.com`, `cs.yahoo.com`, `vcs1`/`vcs2.msg.yahoo.com`, `insider.msg.yahoo.com`, `relay.msg.yahoo.com`, `relay.yahoo.com`, `filetransfer.msg.yahoo.com`, `login.yahoo.com` |
| [CompuServe](CompuServe) (a listener with `frontend = compuserve`) | `compuserve.com`, `cis.compuserve.com` |

Every other name is resolved: the BBS asks the host's DNS servers, or a
primary and backup server of the sysop's, and keeps each answer for as
long as its TTL says.

```ini
[dns]
address = :53
; none: the host's DNS servers
primary = 1.1.1.1
backup = 8.8.8.8
```

| Setting | Default | What it sets |
|---|---|---|
| `address` | `:53` | where to listen, UDP and TCP |
| `primary`, `backup` | the host's | the servers other names are asked of, by IP address (`:port` if not 53); the backup is asked when the primary fails |
| `answer` | the BBS's address as the caller reaches it | the IPv4 address the services' names are given |
| `answer6` | likewise, for a caller that came by IPv6 | the IPv6 address they are given |
| `msn_answer`, `ymsg_answer`, `oscar_answer`, `aol_answer`, `marvel_answer`, `compuserve_answer` | the IP the service's HTTPS (TLS) server or port is given, when it has one of its own | that service's own address (an IPv4 and an IPv6 one at most), given for its names instead of `answer` |
| `services` | `auto` | whose names: `auto` (the front ends set up here), `none`, or some of `msn`, `marvel`, `oscar`, `aol`, `ymsg`, `compuserve` |
| `names` | none | more names answered with the BBS's address; `*.example.org` is every name under it |
| `allow` | this machine and private networks | the networks that may resolve other names |
| `cache_size` | `10000` | answers kept (0: none) |
| `max_ttl` | `86400` | the longest an answer is kept, in seconds |
| `negative_ttl` | `300` | the longest a "no such name" is kept |
| `ttl` | `60` | the TTL given with the services' names |
| `timeout` | `2` | seconds for a server to answer before the next is asked |
| `rate`, `burst` | `50`, `200` | UDP queries a second one address may make (0: no limit), and at once |

## Dial-up users

A PPP link gives the caller a DNS server. With pppd (D-Modem's `[ppp]`
profile), `ms-dns` names it; give it the BBS's address on the PPP side,
as D-Modem's `dmodem.conf.sample` does:

```
command = exec /usr/sbin/pppd nodetach noauth local lock passive 192.168.77.1:192.168.77.2 ms-dns 192.168.77.1
```

With the BBS on the same machine, the PPP side's address (192.168.77.1)
is the machine's own while a call is up, and `address = :53` answers it.

Windows 95 to XP's Dial-Up Networking takes the server the link gives
when "Server assigned name server addresses" is on (the default). The
caller's machine then sends MSN Messenger, AIM, Yahoo! Messenger and the
rest to the BBS, and everything else to the internet as usual.

By default the services' names are answered with the address the caller
reached the DNS server at, so a dial-up user is given the PPP side's
address and a LAN user the LAN's. Set `answer` when that is not the one
to give (behind NAT, a public address).

## Where the clients come

The clients come to the old services' ports, so each front end must be
on its service's port:

| Front end | Ports |
|---|---|
| `[msn]` | `address` 1863; `https_address` 443 (Passport, for MSN 5.0 to 7.5) |
| `[ymsg]` | `address` 5050; `http_address` 80; `https_address` 443 (Y!M 9's login) |
| `[oscar]`, `[aol]` | `address` 5190 (they share it); `tls_address` 443 (AIM 6.2 and later, clientLogin) |
| `[marvel]` | `address` 569 |
| CompuServe | a listener with `frontend = compuserve` on 23 (terminals); CIM's `HostIPAddress` names the host, its port the connector's |

`bbs check` warns of a front end elsewhere. Ports under 1024 (53, 80,
443, 569) need root: the BBS binds them all before it becomes
`[general]`'s user.

### Shared ports

MSN's Passport, Yahoo!'s login and AIM's TLS logins all want port 443,
and Yahoo!'s web half wants 80. When two or more front ends are given the
same address, they share it: the BBS takes the connections there and
hands each to its front end by the name the client asked for, its TLS
server name (SNI) and then its HTTP `Host`. A client that was given the
BBS's address rather than a name is told apart by the path it asks for
(`/RST.srf` is MSN's, `/config/pwtoken_get` Yahoo!'s, `/auth/clientLogin`
AIM's). A TLS connection that is not HTTP (OSCAR over TLS, AIM 6.2's SSL
2.0-format hello) goes to AIM.

```ini
[msn]
https_address = :443
tls_cert = /etc/bbs/legacy.crt
tls_key = /etc/bbs/legacy.key

[ymsg]
http_address = :80
https_address = :443
tls_cert = /etc/bbs/legacy.crt
tls_key = /etc/bbs/legacy.key

[oscar]
tls_address = :443
tls_cert = /etc/bbs/legacy.crt
tls_key = /etc/bbs/legacy.key
```

The BBS can sign these certificates itself, with a certificate authority
of its own and a package that installs it on callers' machines: see
[Certificates](Certificates#the-bbss-own-certificate-authority)
(`tls_cert = ca`).

Windows XP's clients send no server name, so a TLS client there gets the
first front end's certificate (MSN's, then Yahoo!'s, then AIM's). Give
the front ends one certificate with all their names, an RSA one (AIM 6.2
to 7.5 take only RSA), from a small certificate authority of your own
that the callers' machines trust:

```sh
openssl req -x509 -newkey rsa:2048 -nodes -days 3650 -keyout ca.key \
  -out ca.crt -subj "/CN=BBS Legacy CA"
openssl req -newkey rsa:2048 -nodes -keyout legacy.key \
  -out legacy.csr -subj "/CN=login.live.com"
printf 'subjectAltName=%s\n' "DNS:nexus.passport.com,DNS:login.passport.com,\
DNS:loginnet.passport.com,DNS:login.live.com,DNS:login.yahoo.com,\
DNS:api.screenname.aol.com,DNS:api.oscar.aol.com,DNS:kdc.uas.aol.com,\
DNS:slogin.oscar.aol.com" > san.txt
openssl x509 -req -in legacy.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -days 3650 -sha256 -extfile san.txt -out legacy.crt
```

`bbs check` warns when front ends sharing a TLS port have certificates
of their own. A port that is TLS for one front end and plain for another
can't be shared, and is refused.

### An IP for each HTTPS service

When each service needs a self-signed certificate of its own (an old
client that checks only the certificate's common name, not a SAN list),
give each its own IP instead of sharing one. Put the addresses on the
BBS machine (a dummy interface, or more addresses on the LAN one), and
give each front end's HTTPS server one:

```ini
[msn]
address = :1863
https_address = 192.168.77.11:443
tls_cert = /etc/bbs/passport.crt
tls_key = /etc/bbs/passport.key

[ymsg]
address = :5050
http_address = :80
https_address = 192.168.77.12:443
tls_cert = /etc/bbs/login.yahoo.com.crt
tls_key = /etc/bbs/login.yahoo.com.key

[oscar]
address = :5190
tls_address = 192.168.77.13:443
tls_cert = /etc/bbs/aim.crt
tls_key = /etc/bbs/aim.key

[dns]
```

`[dns]` then gives each service's names its own address (MSN's names
192.168.77.11, Yahoo!'s 192.168.77.12, AIM's 192.168.77.13), with no
more settings: a front end's HTTPS (or TLS) address, when it names an IP,
is the one its names get. `msn_answer` and the like set it otherwise (an
address behind NAT, say). Keep the plain ports (`address`, `http_address`)
on every address, as above, so a client reaches them at the service's IP
too. Dial-up callers reach the extra addresses through the BBS machine,
which is their PPP link's far end.

One port can't be both on every address and on an IP of its own: with
`https_address = 192.168.77.11:443` for one front end, give the others an
IP of their own too (not `:443`). `bbs check` refuses the mix, and the
configuration editor (`bbs config`, and the sysop's menu) won't take an
edit that makes it: the old value is put back and the reason shown. The
editor has the DNS server under Internet services, "DNS for callers".

## Who may use it

The services' names are answered to anyone who asks. Other names are
resolved only for `allow`'s networks (by default this machine and the
private ranges, 10/8, 172.16/12, 192.168/16, 100.64/10 and their IPv6
kin), so the BBS is not an open resolver for the internet; others are
refused. Each address may send `rate` queries a second by UDP.

## Running beside the system's resolver

The BBS reads the host's servers from `/etc/resolv.conf` (again when it
changes). When that lists only systemd-resolved's stub (127.0.0.53), the
servers resolved follows itself are read instead, from
`/run/systemd/resolve/resolv.conf`. The BBS never asks itself: an
address that is its own is left out.

systemd-resolved listens on 127.0.0.53:53, so `address = :53` may be
refused. Give `address` the address dial-up users reach, the PPP side's
(`192.168.77.1:53`), or turn off resolved's stub listener
(`DNSStubListener=no` in `/etc/systemd/resolved.conf`).

## What is not done

- No DNSSEC checking of its own: answers are passed on as the upstream
  servers give them (the DO and CD bits are kept, and kept apart in the
  cache).
- Only A and AAAA records are given for the services' names; any other
  type (HTTPS, MX ...) is answered with none.
- None of this has been tried with the real clients on a dial-up line
  yet.
