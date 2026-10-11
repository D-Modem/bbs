Callers can come in by SSH: the text BBS, encrypted, logged in by their
BBS password or by a key.  It is what modern BBS terminals (SyncTERM,
NetRunner, MagiTerm) and a plain `ssh` speak, and the way to keep
passwords off the wire, as telnet and rlogin do not.

```ini
[listen ssh]
type = ssh
address = :2222
```

```
ssh -p 2222 alice@bbs.example.org
```

## Logging in

| SSH user | how | then |
|---|---|---|
| a user's name | their BBS password (SSH `password` or `keyboard-interactive`) | the main menu |
| a user's name | one of their keys | the main menu |
| `new` | nothing | the BBS's own login prompt: `NEW` to sign up (only when `new_users = yes`) |

A name with spaces is given with `_` or `.` for them (`Alice_Smith`).  Names are not case sensitive.  Wrong
passwords count against the caller's address as a terminal's do
(`fail_delay`, throttling), and a locked account gets in by neither
password nor key.

## Keys

A user's keys are their own to add, from the text BBS (*Your profile*,
`K`, after giving their password again), from the [HTTP API](HTTP-API)
(`POST /api/v1/me/keys` with a login's session token, not an API key), or
by the sysop:

```
bbs sshkey add alice ~alice/.ssh/id_ed25519.pub   # each line of the file (- : stdin)
bbs sshkey list [USER]
bbs sshkey del ID
```

ed25519, ECDSA (and the security-key `sk-` kinds) and RSA of 2048 bits
or more are taken; DSA is not.  A key belongs to one user only, a user
has 20 at most, and a key carries no options (`command=`, `from=`).  The
log notes each key added, deleted and logged in with (by its SHA-256
fingerprint).

## The session

Only a terminal session: a shell request, with a pty if the client asks.
The pty's terminal type and size go to the text BBS as telnet's do
(`xterm-256color` is ANSI and UTF-8, `ansi` and `syncterm` are ANSI and
CP437, anything else is asked), and `ansi`, `utf8` and `sixel` on the
listener set them as on telnet.  Commands (`ssh host ls`), SFTP and SCP,
port and agent forwarding, X11 and a second session on the connection
are all refused.  A resized window keeps the size the session began
with.

A client has 30 seconds from connecting to asking for its shell;
`max_per_ip` limits connections from one address, as on telnet.

## Host keys

```ini
host_keys = /etc/bbs/ssh_host_ed25519_key /etc/bbs/ssh_host_rsa_key
```

By default the listener has an ed25519 and an RSA key beside the
database, `ssh_host_ed25519_key` and `ssh_host_rsa_key`, made (0600) the
first time it starts if they are not there: the log says their
fingerprints, as does `bbs check`.  Keys made by `ssh-keygen` (OpenSSH or
PEM format, no passphrase) do as well; a missing one is made by its
name: RSA (3072 bits) if it says `rsa`, ECDSA P-256 if `ecdsa`, else
ed25519.  A key file others may read is refused, as sshd does.

## Old clients

```ini
legacy = yes
```

offers, beside the usual algorithms, the AES-CBC and 3DES-CBC ciphers and
the 1024-bit `diffie-hellman-group1-sha1` key exchange, for old clients
(old SyncTERM and PuTTY builds, cryptlib) that have nothing newer.  They
are weak; `bbs check` warns.  Without it the server offers curve25519,
ECDH and 2048-bit or larger DH, and AES-GCM, ChaCha20-Poly1305 and
AES-CTR.

**See also:** [Configuration](Configuration), [Security](Security), [HTTP API](HTTP-API)
