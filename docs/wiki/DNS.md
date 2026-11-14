The BBS can be a DNS server for its callers, mainly for dial-up users. It
answers the names of the servers of the old services with the address of
the BBS. Thus, the clients of that time find the BBS with no hosts file
and no registry changes:

| Front end | Names answered |
|---|---|
| [MSN Messenger](MSN-Messenger) (`[msn]`) | `messenger.hotmail.com`, `nexus.passport.com`, `login.passport.com`, `loginnet.passport.com`, `login.live.com` |
| [MSN Classic](MSN-Classic) (`[marvel]`, or a listener with `frontend = marvel`) | `gateway.moswest.msn.net` |
| [AIM and ICQ](AIM-and-ICQ) (`[oscar]`) | `login.oscar.aol.com`, `slogin.oscar.aol.com`, `login.messaging.aol.com`, `kdc.uas.aol.com`, `api.screenname.aol.com`, `api.oscar.aol.com`, `login.icq.com`, `slogin.icq.com` |
| [AOL clients](AOL) (`[aol]`) | `AmericaOnline.aol.com` |
| [Yahoo! Messenger](Yahoo-Messenger) (`[ymsg]`) | `scs.msg.yahoo.com`, `scsa`/`scsb`/`scsc.msg.yahoo.com`, `cs.yahoo.com`, `vcs1`/`vcs2.msg.yahoo.com`, `insider.msg.yahoo.com`, `relay.msg.yahoo.com`, `relay.yahoo.com`, `filetransfer.msg.yahoo.com`, `login.yahoo.com` |
| [CompuServe](CompuServe) (a listener with `frontend = compuserve`) | `compuserve.com`, `cis.compuserve.com` |

The BBS resolves all other names. It asks the DNS servers of the host,
or a primary and backup server that the sysop sets. It keeps each answer
for the time that its TTL gives.

```ini
[dns]
address = :53
; none: the host's DNS servers
primary = 1.1.1.1
backup = 8.8.8.8
```

| Setting | Default | What it sets |
|---|---|---|
| `address` | `:53` | the address on which to listen, UDP and TCP |
| `primary`, `backup` | those of the host | the servers that the BBS asks for other names, by IP address (`:port` if not 53). The BBS asks the backup when the primary fails |
| `answer` | the address of the BBS at which the caller connects | the IPv4 address that the BBS gives for the names of the services |
| `answer6` | the same, for a caller that came by IPv6 | the IPv6 address that the BBS gives |
| `msn_answer`, `ymsg_answer`, `oscar_answer`, `aol_answer`, `marvel_answer`, `compuserve_answer` | the IP of the HTTPS (TLS) server or port of the service, if it has its own IP | the address of that service (a maximum of one IPv4 and one IPv6 address), which the BBS gives for its names in place of `answer` |
| `services` | `auto` | the services whose names the BBS answers: `auto` (the front ends configured here), `none`, or some of `msn`, `marvel`, `oscar`, `aol`, `ymsg`, `compuserve` |
| `names` | none | more names to answer with the address of the BBS. `*.example.org` is all names under it |
| `allow` | this machine and private networks | the networks that can resolve other names |
| `cache_size` | `10000` | the number of answers kept (0: none) |
| `max_ttl` | `86400` | the maximum time to keep an answer, in seconds |
| `negative_ttl` | `300` | the maximum time to keep a "no such name" |
| `ttl` | `60` | the TTL given with the names of the services |
| `timeout` | `2` | seconds for a server to answer before the BBS asks the next server |
| `rate`, `burst` | `50`, `200` | the UDP queries that one address can make each second (0: no limit), and at one time |

## Dial-up users

A PPP link gives the caller a DNS server. With pppd (the `[ppp]` profile
of D-Modem), `ms-dns` names it. Give it the address of the BBS on the PPP
side, as the `dmodem.conf.sample` of D-Modem does:

```
command = exec /usr/sbin/pppd nodetach noauth local lock passive 192.168.77.1:192.168.77.2 ms-dns 192.168.77.1
```

If the BBS is on the same machine, the address of the PPP side
(192.168.77.1) is the address of the machine while a call is up. Thus,
`address = :53` answers it.

The Dial-Up Networking of Windows 95 to XP uses the server that the link
gives when "Server assigned name server addresses" is on (the default).
Then the machine of the caller sends MSN Messenger, AIM, Yahoo! Messenger
and the other services to the BBS. It sends all other traffic to the
internet as usual.

By default, the BBS answers the names of the services with the address
at which the caller connected to the DNS server. Thus, a dial-up user
gets the address of the PPP side, and a LAN user gets the address of the
LAN. If that address is not the correct one (behind NAT, a public
address), set `answer`.

## Where the clients come

The clients come to the ports of the old services. Thus, each front end
must be on the port of its service:

| Front end | Ports |
|---|---|
| `[msn]` | `address` 1863. `https_address` 443 (Passport, for MSN 5.0 to 7.5) |
| `[ymsg]` | `address` 5050. `http_address` 80. `https_address` 443 (the login of Y!M 9) |
| `[oscar]`, `[aol]` | `address` 5190 (they share it). `tls_address` 443 (AIM 6.2 and later, clientLogin) |
| `[marvel]` | `address` 569 |
| CompuServe | a listener with `frontend = compuserve` on 23 (terminals). The `HostIPAddress` of CIM names the host, and its port is the port of the connector |

`bbs check` gives a warning about a front end on a different port. Ports
below 1024 (53, 80, 443, 569) must have root. The BBS binds all of them
before it becomes the user of `[general]`.

### Shared ports

The Passport of MSN, the login of Yahoo! and the TLS logins of AIM all
use port 443. The web part of Yahoo! uses 80. If you give the same
address to two or more front ends, they share it. The BBS accepts the
connections there. It gives each connection to its front end by the name
that the client asked for: first its TLS server name (SNI), then its
HTTP `Host`. If a client got the address of the BBS and not a name, the
BBS uses the path that it asks for (`/RST.srf` is for MSN,
`/config/pwtoken_get` for Yahoo!, `/auth/clientLogin` for AIM). A TLS
connection that is not HTTP (OSCAR over TLS, the SSL 2.0-format hello of
AIM 6.2) goes to AIM.

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

The BBS can sign these certificates itself, with its own certificate
authority and a package that installs it on the machines of callers.
Refer to [Certificates](Certificates#the-bbss-own-certificate-authority)
(`tls_cert = ca`).

The clients of Windows XP send no server name. Thus, a TLS client there
gets the certificate of the first front end (MSN, then Yahoo!, then
AIM). Give the front ends one certificate with all their names. It must
be an RSA certificate (AIM 6.2 to 7.5 accept only RSA), from a small
certificate authority of your own that the machines of the callers
trust:

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

`bbs check` gives a warning when front ends that share a TLS port have
different certificates. Front ends cannot share a port that is TLS for
one front end and plain for a different one. The BBS refuses this.

### An IP for each HTTPS service

If each service must have its own self-signed certificate (an old client
that checks only the common name of the certificate, not a SAN list),
give each service its own IP. Do not share one IP. Put the addresses on
the BBS machine (a dummy interface, or more addresses on the LAN
interface). Give one to the HTTPS server of each front end:

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

Then `[dns]` gives the names of each service its own address (the names
of MSN get 192.168.77.11, those of Yahoo! get 192.168.77.12, those of AIM
get 192.168.77.13), with no more settings. If the HTTPS (or TLS) address
of a front end names an IP, its names get that IP. `msn_answer` and the
similar settings set a different value (for example, an address behind
NAT). Keep the plain ports (`address`, `http_address`) on all addresses,
as above. Thus, a client also gets to them at the IP of the service.
Dial-up callers get to the extra addresses through the BBS machine,
which is the far end of their PPP link.

One port cannot be on all addresses and also on its own IP. If one front
end has `https_address = 192.168.77.11:443`, give the other front ends
their own IP also (not `:443`). `bbs check` refuses the mix. The
configuration editor (`bbs config`, and the menu of the sysop) does not
accept an edit that makes it: it puts back the old value and shows the
reason. The editor has the DNS server under Internet services, "DNS for
callers".

## Records of your own

A `[dnsrecord NAME]` section is one DNS record. The BBS answers it as you
write it. The BBS gives these records to all users, before the names of
the services. It does not ask upstream servers about the name. Use them
for a machine on your network, a game server, or a mail server for your
dial-up users. For more than one record, give a name more than one
section:

```ini
[dnsrecord games.bbs.example.org]
value = 192.168.77.20

[dnsrecord bbs-mx]
name = bbs.example.org
type = MX
value = 10 mail.bbs.example.org

[dnsrecord bbs-txt]
name = bbs.example.org
type = TXT
value = v=spf1 mx -all

[dnsrecord lan]
name = *.lan.example.org
value = 192.168.77.5
ttl = 300
```

| key | what it is |
|---|---|
| `name` | the name (default: the NAME of the section). `*.domain` is all names under it |
| `type` | A, AAAA, CNAME, TXT, MX, SRV, NS, PTR, CAA, or a different type (default: A or AAAA, from the address in the value) |
| `value` | as a zone file writes it: an address, the target of a CNAME, the text of a TXT, the preference and host of an MX, the priority, weight, port and host of an SRV |
| `ttl` | seconds (default: `[dns]` `ttl`) |

Reverse lookups also operate. The address of each A or AAAA record
answers a PTR query (`20.77.168.192.in-addr.arpa`, or its `ip6.arpa`
name) with the name of the record. Thus, `192.168.77.20` gives back
`games.bbs.example.org`. Wildcards do not make PTRs. To give an address
a different name, write its PTR yourself
(`name = 20.77.168.192.in-addr.arpa`, `type = PTR`,
`value = arcade.bbs.example.org`). Your own PTR replaces the automatic
PTR.

A query for a type for which the name has no record gets an empty
answer. The BBS does not ask upstream servers about it. The BBS follows a
CNAME to its target when the target also has records here. A record for
one of the names of the old services replaces the answer of the BBS for
that type. The other types still get the answer of the BBS. `bbs check`
refuses a record that it cannot read. In the configuration screens,
these records are under Internet services, DNS for callers, DNS
records. You select the type from a list, and an address from the
addresses of this machine.

## Who may use it

The BBS answers the names of the services to all users who ask. It
resolves other names only for the networks of `allow`. By default, these
are this machine and the private ranges, 10/8, 172.16/12, 192.168/16,
100.64/10 and the equivalent IPv6 ranges. Thus, the BBS is not an open
resolver for the internet. It refuses other users. Each address can send
`rate` queries each second by UDP.

## Running beside the system's resolver

The BBS reads the servers of the host from `/etc/resolv.conf` (again
when it changes). If that file lists only the stub of systemd-resolved
(127.0.0.53), the BBS reads the servers that resolved follows, from
`/run/systemd/resolve/resolv.conf`. The BBS never asks itself: it
ignores addresses that are its own.

systemd-resolved listens on 127.0.0.53:53. Thus, the system can refuse
`address = :53`. Give `address` the address at which dial-up users
connect, the address of the PPP side (`192.168.77.1:53`). Or disable the
stub listener of resolved (`DNSStubListener=no` in
`/etc/systemd/resolved.conf`).

## What is not done

- The BBS does no DNSSEC checks of its own. It passes on answers as the
  upstream servers give them (it keeps the DO and CD bits, and keeps them
  separate in the cache).
- The BBS gives only A and AAAA records for the names of the services.
  It answers all other types (HTTPS, MX ...) with no records.
- We did not try any of this with the real clients on a dial-up line
  yet.
