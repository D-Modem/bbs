This page lists the risks against which the BBS gives protection, and
how, for each service. Then it gives the settings to keep unwanted
callers out, and the settings for telnet over TLS.

## What is guarded, and how

- Passwords: the BBS stores them with bcrypt (cost 12). The refusal of
  an unknown user takes the same time as the refusal of an incorrect
  password. After each failure, the BBS waits `fail_delay`. After
  `attempts` failures, the BBS hangs up. `throttle_failures` refuses an
  address or caller ID that continues to fail (or continues to sign up).
- Text from users: the BBS cleans all text before it stores it, and
  again before it shows it. No escape sequences, control characters, C1
  controls, bidirectional overrides or invalid UTF-8 get to the terminal
  of a different caller. On a 3270, these items become 3270 orders after
  conversion to EBCDIC, and go3270 does not filter them.
- Limits on all items: lengths of names, subjects and lines, message
  size, telnet subnegotiations, session headers, rlogin fields, and 64 KB
  for the answer of a 3270 screen. Timeouts for each step (telnet and
  TN3270 negotiation, headers, the login, idle callers, writes to callers
  who stopped to read). `max_nodes` callers at one time and `max_per_ip`
  from one address.
- The BBS creates the database with mode 0600. It makes unix sockets
  private and then opens them to `socket_mode`. They are never wider
  between these steps.
- Only the recipient can read private mail (each query asks for the
  mail of the user). The BBS tells that an account is locked only to a
  person who knows its password.
- Telnet and rlogin send passwords in clear text, as they always did.
  Keep rlogin on 127.0.0.1 or a socket. Where security is important,
  offer [SSH](SSH) together with telnet (or put telnet behind a VPN).
- SSH:
  - The BBS makes host keys with mode 0600. It refuses them when other
    users can read them.
  - Only modern algorithms, unless `legacy = yes` (`bbs check` gives a
    warning about it).
  - The BBS checks, throttles and delays passwords as for a terminal.
  - A key logs in only its own user, and only if the user is not locked.
    The decision uses the key that the client proved (never a key that
    the client only offered).
  - Users can add keys only with the password (on the BBS) or a login
    session (the API), not with an API key.
  - The BBS refuses commands, SFTP, all types of forwarding and second
    sessions. A client has 30 seconds to get to its shell.
- A modem line is as private as the phone network.
- News:
  - The BBS posts the article of a reader as its user, for all values of
    its From. It uses the Message-ID, Date and Path of the BBS. It keeps
    only the headers that a reader can select (Subject, References, the
    body).
  - The BBS accepts `IHAVE` only from the `accept_from` addresses of a
    peer. It checks the articles of peers (the headers that RFC 5536
    requires, the sizes of the article, its headers and lines). It cleans
    them before a caller sees them.
  - `tls =
    required` refuses `AUTHINFO` before `STARTTLS`. With `no` and
    `optional`, `bbs check` gives a warning.
  - Commands, lines and articles have limits. Clients at one time and
    from one address also have limits.
- FidoNet:
  - The BBS knows a link by its address and its password (CRAM-MD5. In
    clear text only if permitted). After an incorrect password, the BBS
    waits and refuses. It refuses an address that is not a link. A system
    that calls with the address of a link but not its password gets
    nothing.
  - The BBS tosses packets only from the inbound of the link, only if
    they are from its address to our address, with its packet password
    if it has one.
  - The BBS changes received file names to a safe base name. Files,
    sessions, packets, bundles and messages have size limits.
  - The sender of netmail is the sender that the message gives, as on
    all of FidoNet. It is not a proof of the author.
- Doors: a DOS door runs in a container with no network, as the user of
  the BBS. It sees only the directory of the doors and the directory of
  its node. The line of the caller connects to it only through COM1. A
  hang-up, the time limit and the idle time stop it. The drop files
  contain no password.
- Chat: the BBS cleans the text of callers as it cleans all other text
  from them. The text has limits (200 characters for each line, 8 lines
  in 10 seconds). A caller can page a maximum of one time each minute.
  The private room of a page is only for the caller and the sysops.
  `page_command` runs without a shell, with the words of the caller as
  full arguments.
- Finger gives no addresses or caller IDs, and forwards nothing. Gopher
  shows no message or file area unless the sysop names it (and `bbs
  check` gives a warning when the sysop names one). Its selectors are
  database records, not paths. Gemini shows the same items as gopher,
  under its own `areas` and `files`, and the users of finger. All three
  have limits for connections at one time and from one address, and all
  have timeouts.
- The HTTP API:
  - TLS by default (`bbs check` gives a warning when TLS is disabled on
    an address that others can get to).
  - Tokens of 32 random bytes. The BBS keeps them only as hashes, never
    in URLs. They have a scope (`sysop` only for sysops), they expire,
    and you can revoke them.
  - Rate limits for each token and each address.
  - JSON bodies have limits and strict rules.
  - A maximum of one CORS origin. WebSockets only from that origin and
    from the host of the API.
  - Metrics are on a different address.
- Databases: the BBS makes the SQLite file with mode 0600. It never logs
  the password of a server (`bbs check` and the log show `xxxxx`). The
  configuration file that contains it must be 0640. Each query takes its
  values as parameters.
- Access levels: the core of the BBS checks them. Thus, the text BBS,
  3270s, FTP, NNTP and the API apply them in the same way. If a user
  cannot read an area, the BBS does not list it or open it, and does not
  find it by the number or Message-ID of a message. No user can change
  their own level or flags (`PATCH /api/v1/me` refuses them. Only the
  tokens of sysops get to `/admin`). If the BBS cannot read a
  requirement, it lets only the sysop in.
- The web terminal:
  - The scripts of the page come only from the BBS (a Content Security
    Policy: no inline scripts, no other hosts, no framing).
  - Its WebSocket accepts connections only from the pages of the BBS (or
    the `cors` origin), with the TLS, connection and rate limits of the
    HTTP server.
  - The caller logs in at the prompt of the BBS, as all terminal callers
    do.
- Screens: the BBS reads display files only from `screens_dir`, by names
  of letters, digits, `_` and `-` (no paths), with a maximum of 256 KB.
  The BBS cleans the values of @-codes. Thus, no escape sequence gets
  into a screen through the name or location of a user.
- The log has logins, failures, posts and mail sent (never passwords or
  message text).

## Keeping callers out

```
; bbs.conf
[security]
deny_file = /etc/bbs/deny.txt          ; addresses refused
name_deny_file = /etc/bbs/names.txt    ; names no new account may have
hack_log = /var/log/bbs/hack.log       ; who tried what
ban_minutes = 60                       ; a throttled address is banned this long
```

- `deny_file` lists addresses and networks, one on each line
  (`192.0.2.7`, `198.51.100.0/24`, `2001:db8::/32`. `#` or `;` starts a
  comment). The BBS closes a connection from one of them immediately on
  all services: the terminal listeners and SSH, FTP, NNTP, the HTTP API
  and web terminal, finger, gopher, Gemini and binkp. The BBS never
  refuses callers through unix sockets (a modem server, an upstream
  menu). The BBS reads the file again when it changes. `bbs check` gives
  a warning about lines that are not addresses.
- The BBS can also check mail from the internet (`[mx]`) against DNS
  blocklists (`dnsbl = zen.spamhaus.org`). It also checks SPF, DKIM and
  DMARC. Refer to [E-mail](E-mail#spam-dns-blocklists).
- `ban_minutes`: if `throttle_failures` refuses an address, the BBS bans
  it fully for that time, from all services. This does not apply to a
  caller ID: caller IDs are the callers of a modem server, and a ban
  does not stop them. The BBS keeps bans in memory.
  `GET /api/v1/admin/bans` lists them. `DELETE
  /api/v1/admin/bans/{ip}` removes one.
- `name_deny_file` has patterns, one on each line, with upper or lower
  case. `*` is for a sequence of characters (`*sysop*`, `admin*`). No
  new user, and no account that an upstream menu makes, can have a name
  that matches. The BBS always refuses names such as `sysop`, `root` and
  `new`.
- The hack log has failed logins (and the names that callers tried),
  logins to locked accounts, throttled and banned addresses, refused
  connections and refused names. Each has one line with the time.
  `GET /api/v1/admin/hacklog` shows its end. The BBS opens the file for
  each line. Thus, you can rotate it.

## Telnet over TLS

```
[listen telnets]
type = telnet
address = :992
tls_cert = /etc/bbs/cert.pem
tls_key = /etc/bbs/key.pem
```

A telnet or raw listener with `tls_cert` and `tls_key` accepts TLS (1.2
or later). Then it accepts telnet (or TN3270) as usual. SyncTERM
(TelnetS) and `c3270 L:host:992` both use it. A client has 15 seconds to
complete the handshake. `tls_cert = acme` uses the certificate of the
[ACME client](Certificates) in place of files.

## Certificates

Each TLS server reads its certificate again when the file changes. Thus,
a renewal does not need a restart. The [ACME client](Certificates) of
the BBS gets and renews certificates from Let's Encrypt or ZeroSSL for
all of them.

**See also:** [SSH](SSH), [Configuration](Configuration), [Sysop tools](Sysop-Tools)
