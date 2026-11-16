The BBS can keep its own time and serve it. It can follow time servers
over NTP, NTS (NTP with keys from a TLS exchange) and PTP. It can also be
an NTP server, an NTS-KE server and a PTP grandmaster. All of these
functions are disabled until you put their section in the configuration.

The packets of the protocols come from
[facebook/time](https://github.com/facebook/time) of Meta (Apache-2.0).
The servers and clients are part of the BBS.

## Following time servers

```
[time]
servers = nts time.cloudflare.com, nts nts.netnod.se, ntp pool.ntp.org
poll = 64
set_clock = no
```

At each poll, the BBS asks all servers. It asks NTP and NTS servers four
times, and the answer with the shortest round trip counts. Then the clock
uses the median of the offsets of the servers. Thus, the other servers
outvote one incorrect server.

- `ntp HOST[:PORT]` is plain NTP.
- `nts HOST[:PORT]` is NTS. The port is the NTS-KE port (4460 if you do
  not give a port). The key exchange gives eight cookies. Each request
  uses one cookie and asks for one back. The BBS makes a new exchange
  when no cookies remain or when the server does not know them.
  `root_cas` names a PEM file to trust in place of the roots of the
  system, for a private server.
- `ptp HOST` asks a PTP grandmaster for unicast transmission (announce
  every 2 s, sync every 1 s, for 60 s at a time, renewed). It must have
  ports 319 and 320. Thus, the BBS must start as root (it binds the ports
  before it becomes `[general] user`).

With `set_clock = no` (the default), the BBS does not change the system
clock. The BBS keeps the offset that it measures and serves the
corrected time.

With `set_clock = yes` (Linux), the BBS steps the system clock when the
clock has an error of more than 128 ms. If the error is smaller, it slews
the clock. It also tells the kernel that the clock is synchronised. This
must have root, or `CAP_SYS_TIME` kept after the BBS stops its root
access (systemd: `AmbientCapabilities=CAP_SYS_TIME`). Do not enable it
with chrony or ntpd, because they will change the clock in opposite
directions. If the BBS cannot set the clock, it writes this one time in
the log. Then it corrects only its own time.

If there are no servers, the BBS reports the system clock: stratum 3
while the kernel says that the clock is synchronised, and unsynchronised
if not. `stratum` sets a different value. For example, use `stratum = 1`
and `refid = GPS` on a machine with a clock that a GPS controls.

`bbs time` asks the configured servers one time and prints each offset.
`bbs time nts time.cloudflare.com` asks only that server:

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

The server answers only client requests (mode 3). Thus, the control and
private modes that amplification attacks use get no answer. Addresses
that the guard (`[security]`) banned also get no answer. The stratum
that the server gives is one more than the stratum of the server that it
follows. The reference ID is the address of that server.

## NTS-KE server

```
[nts]
address = :4460
tls_cert = /etc/bbs/cert.pem
tls_key = /etc/bbs/key.pem
ntp_host = time.example.org   ; told to clients ("": this host)
```

NTS must also have `[ntp]`. The key exchange gives cookies, and the NTP
server answers requests that contain them, with authentication. The
cookie key changes each day. The BBS keeps the keys of the last week, so
that the cookies stay valid. A restart makes new keys. Then clients get
an NTS NAK and do the key exchange again automatically.

The BBS reads the certificate again when its file changes. Thus, a
renewal does not need a restart. The server offers AES-SIV-CMAC-256,
AES-128-GCM-SIV and AES-SIV-CMAC-512. It gives GCM-SIV only to clients
that say that they make its keys as the RFC specifies (chrony before 4.4
did not).

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
  The BBS sends the answers to delay requests directly to the client.
  This is the hybrid mode that linuxptp and other clients accept.
- **Unicast**: the BBS gives announce, sync and delay responses to
  clients that ask for them (the `unicast_master_table` of linuxptp, the
  ptp4u clients of Meta, `ptp HOST` on a different BBS). It accepts up
  to `max_clients`, not faster than `log_min_interval`.

The time on the wire is TAI (UTC + `utc_offset`, 37). The clock class is
6 while the clock of the BBS is synchronised, and 248 if not. The time
source tells the source of the time (NTP, PTP, GNSS, or the internal
oscillator).

On Linux, the kernel makes the timestamps (`SO_TIMESTAMPING`). On other
systems, the BBS stamps packets when it reads and writes them.
`timestamping = hardware` uses the clock of the network card. You must
keep that clock on the system clock
(`phc2sys -s CLOCK_REALTIME -c eth0 -O 0`). The BBS configures the card
as root, before it stops its root access.

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
  server that the BBS follows (also port negotiation).
- linuxptp 4.0 `ptp4l` as a multicast and as a unicast client of the
  grandmaster (with an error of about a microsecond, software
  timestamps, over a virtual Ethernet link), and as a unicast
  grandmaster that the BBS follows.
- The NTS-KE server of Meta. The NTP server of the BBS opens its cookies.

In that test, the BBS read a `ptp4l` grandmaster on software timestamps
with an error of 37 seconds. The cause is that this grandmaster sends the
UTC of the system clock as the TAI of PTP. A grandmaster with a card
clock on TAI does not have this problem.
