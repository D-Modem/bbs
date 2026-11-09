The BBS can keep its own time and serve it. It can follow time servers
over NTP, NTS (NTP secured with keys from a TLS exchange) and PTP. It can
also be an NTP server, an NTS-KE server and a PTP grandmaster. All of it
is off unless its section is in the configuration.

The protocols' packets come from Meta's
[facebook/time](https://github.com/facebook/time) (Apache-2.0). The
servers and clients are the BBS's own.

## Following time servers

```
[time]
servers = nts time.cloudflare.com, nts nts.netnod.se, ntp pool.ntp.org
poll = 64
set_clock = no
```

Each poll, every server is asked. NTP and NTS servers are asked four
times, and the answer with the shortest round trip counts. The clock then
takes the median of the servers' offsets, so one wrong server is outvoted.

- `ntp HOST[:PORT]` is plain NTP.
- `nts HOST[:PORT]` is NTS. The port is the NTS-KE port (4460 if none).
  The key exchange gives eight cookies, each request spends one and asks
  for one back, and a new exchange is made when cookies run out or the
  server stops knowing them. `root_cas` names a PEM file to trust instead
  of the system's roots, for a private server.
- `ptp HOST` asks a PTP grandmaster for unicast transmission (announce
  every 2 s, sync every 1 s, for 60 s at a time, renewed). It needs ports
  319 and 320, so the BBS must start as root (it binds them before it
  becomes `[general] user`).

With `set_clock = no` (the default), the system clock is left alone. The
BBS keeps the offset it measures and serves the corrected time. With
`set_clock = yes` (Linux), it steps the system clock when it is more than
128 ms out and slews it otherwise, and tells the kernel the clock is
synchronised. That needs root, or `CAP_SYS_TIME` kept after the BBS gives
up root (systemd: `AmbientCapabilities=CAP_SYS_TIME`). Don't turn it on
next to chrony or ntpd, which would fight it. If it can't set the clock,
it says so once in the log and falls back to correcting its own time.

With no servers, the BBS tells of the system clock: stratum 3 while the
kernel says it is synchronised, unsynchronised otherwise. `stratum` says
otherwise, for example `stratum = 1` and `refid = GPS` on a machine whose
clock a GPS keeps.

`bbs time` asks the configured servers once and prints each offset.
`bbs time nts time.cloudflare.com` asks one server instead:

```
$ bbs time nts time.cloudflare.com ntp pool.ntp.org
nts time.cloudflare.com          offset +412µs, round trip 9.8ms, stratum 3, leap 0
ntp pool.ntp.org                 offset +380µs, round trip 21ms, stratum 2, leap 0
```

## NTP server

```
[ntp]
address = :123
rate = 2       ; requests a second an address may make (0: no limit)
burst = 16     ; and at once
```

Only client requests (mode 3) are answered, so the control and private
modes used for amplification attacks get nothing. Addresses banned by the
guard (`[security]`) get nothing either. The stratum told is one past the
server followed, and the reference ID is that server's address.

## NTS-KE server

```
[nts]
address = :4460
tls_cert = /etc/bbs/cert.pem
tls_key = /etc/bbs/key.pem
ntp_host = time.example.org   ; told to clients ("": this host)
```

NTS needs `[ntp]` too: the key exchange hands out cookies, and the NTP
server answers requests that carry them, authenticated. The cookie key
changes each day, and the last week's keys are kept, so cookies stay good.
A restart makes new keys. Clients then get an NTS NAK and exchange keys
again by themselves. The certificate is read again when its file changes,
so a renewal needs no restart. AES-SIV-CMAC-256, AES-128-GCM-SIV and
AES-SIV-CMAC-512 are offered. GCM-SIV goes only to clients that say they
make its keys as the RFC says (chrony before 4.4 did not).

A chrony client:

```
server time.example.org iburst nts
```

## PTP grandmaster

```
[ptp]
interface = eth0
multicast = yes
unicast = yes
domain = 0
timestamping = software
```

The BBS is a two-step, end-to-end grandmaster on UDP ports 319 and 320.

- **Multicast** (with `interface`): announce and sync go to 224.0.1.129.
  Delay requests are answered to the client directly, the hybrid mode
  linuxptp and others accept.
- **Unicast**: clients that ask (linuxptp's `unicast_master_table`,
  Meta's ptp4u clients, `ptp HOST` on another BBS) are granted announce,
  sync and delay responses, up to `max_clients`, no faster than
  `log_min_interval`.

Time on the wire is TAI (UTC + `utc_offset`, 37). The clock class is 6
while the BBS's clock is synchronised, 248 otherwise. The time source
says where the time comes from (NTP, PTP, GNSS, or the internal
oscillator).

Timestamps are the kernel's on Linux (`SO_TIMESTAMPING`). Elsewhere
packets are stamped as they are read and written. `timestamping =
hardware` uses the network card's clock, which has to be kept on the
system clock (`phc2sys -s CLOCK_REALTIME -c eth0 -O 0`). The card is set
up as root, before the BBS gives root up.

A linuxptp client, by unicast:

```
[global]
clientOnly 1
[unicast_master_table]
table_id 1
logQueryInterval 2
UDPv4 192.0.2.10
[eth0]
unicast_master_table 1
```

## Tested with

- chrony 4.5 as an NTP and NTS client of the BBS, and as an NTP and NTS
  server the BBS follows (port negotiation included).
- linuxptp 4.0 `ptp4l` as a multicast and as a unicast client of the
  grandmaster (within about a microsecond, software timestamps, over a
  virtual Ethernet link), and as a unicast grandmaster the BBS follows.
- Meta's NTS-KE server, whose cookies the BBS's NTP server opens.

In that test, the BBS read a `ptp4l` grandmaster on software timestamps
37 seconds off: that grandmaster sends the system clock's UTC as PTP's
TAI. A grandmaster whose card's clock is on TAI doesn't have this problem.
