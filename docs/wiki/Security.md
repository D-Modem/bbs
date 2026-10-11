- Passwords are bcrypt (cost 12); unknown users take as long to refuse as
  wrong passwords; failures wait `fail_delay`; `attempts` failures hang
  up; `throttle_failures` turns away an address or caller ID that keeps
  failing (or signing up).
- Everything users write is cleaned before it is stored and again before
  it is shown: no escape sequences, control characters, C1 controls,
  bidirectional overrides or invalid UTF-8 reach another caller's
  terminal.  On a 3270 these would become 3270 orders once in EBCDIC; go3270
  does not filter them itself.
- Limits everywhere: name, subject and line lengths, message size, telnet
  subnegotiations, session headers, rlogin fields, 64 KB for a 3270
  screen's answer; timeouts for every step (telnet and TN3270 negotiation,
  headers, the login, idle callers, writes to callers who stopped
  reading); `max_nodes` callers at once and `max_per_ip` from one address.
- The database is created 0600; unix sockets are made private and then
  opened to `socket_mode`, never wider in between.
- Private mail is read only by its recipient (every query asks for the
  user's own); a locked account is said to be locked only to someone who
  knows its password.
- Telnet and rlogin carry passwords in the clear, as they always have:
  keep rlogin to 127.0.0.1 or a socket, and offer [SSH](SSH) beside
  telnet (or put telnet behind a VPN) where that matters.
- SSH: host keys made 0600 and refused when others may read them; modern
  algorithms only unless `legacy = yes` (which `bbs check` warns about);
  passwords checked, throttled and delayed as a terminal's; a key logs in
  only its own user, only if not locked, and is decided by the key the
  client proved (never one it merely offered); keys are added only with
  the password (on the BBS) or a login's session (the API), not by an
  API key.  Commands, SFTP, forwarding of any kind and second sessions
  are refused, and a client has 30 seconds to get to its shell.  A modem line is as private as the phone
  network.
- News: a reader's article is posted as its user whatever its From says,
  with the BBS's own Message-ID, Date and Path; only the headers a reader
  may choose (Subject, References, the body) are kept.  `IHAVE` is taken
  only from a peer's `accept_from` addresses, and peers' articles are
  checked (the headers RFC 5536 requires, sizes of the article, its
  headers and lines) and cleaned before a caller sees them.  `tls =
  required` refuses `AUTHINFO` before `STARTTLS`; `no` and `optional` make
  `bbs check` warn.  Commands, lines and articles are limited, and so are
  clients at once and from one address.
- FidoNet: a link is known by its address and its password (CRAM-MD5;
  in the clear only if allowed); a wrong password waits and is refused,
  an address that is not a link is turned away, and a system calling with
  a link's address but not its password gets nothing.  Packets are tossed
  only from a link's own inbound, only if from its address to ours, with
  its packet password if it has one.  Received file names are reduced to a
  safe base name; files, sessions, packets, bundles and messages have size
  limits.  Netmail's sender is what the message says, as anywhere in
  FidoNet: it is not proof of who wrote it.
- Doors: a DOS door runs in a container with no network, as the BBS's
  user, seeing only the doors' directory and its node's; the caller's
  line is joined to it only through COM1.  Hanging up, the time limit and
  the idle time stop it.  The drop files carry no password.
- Chat: what callers say is cleaned like anything else they write, and
  limited (200 characters a line, 8 lines in 10 seconds); a caller pages
  at most once a minute; a page's private room is the caller's and the
  sysops' only.  `page_command` runs without a shell, the caller's words
  as whole arguments.
- Finger tells no addresses or caller IDs and forwards nothing; gopher
  shows no message or file area unless the sysop names it (and `bbs
  check` warns when they do), and its selectors are database records, not
  paths.  Both limit connections at once and from one address, and time
  out.
- The HTTP API: TLS by default (`bbs check` warns when it is off on an
  address others reach); tokens of 32 random bytes kept only as hashes,
  never in URLs, limited by scope (`sysop` only for sysops), expiring
  and revocable; rate limits per token and per address; JSON bodies
  limited and strict; one CORS origin at most, and WebSockets only from
  it and the API's own host.  Metrics are on an address of their own.
- Screens: display files are read only from `screens_dir`, by names of
  letters, digits, `_` and `-` (no paths), at most 256 KB.  The values of
  @-codes are cleaned, so no escape sequence gets into a screen through
  a user's name or location.
- The log has logins, failures, posts and mail sent (never passwords or
  message text).
