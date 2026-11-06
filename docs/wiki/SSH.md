Callers can reach the text BBS over SSH, encrypted, and log in with their
BBS password or with a key. Modern BBS terminals (SyncTERM, NetRunner,
MagiTerm) and a plain `ssh` client all speak SSH, and unlike telnet and
rlogin it keeps passwords off the wire.

```ini
[listen ssh]
type = ssh
address = :2222
```

```
ssh -p 2222 alice@bbs.example.org
```

## Logging in

| SSH user | Logs in with | Then |
|---|---|---|
| a user's name | their BBS password (SSH `password` or `keyboard-interactive`) | the main menu |
| a user's name | one of their keys | the main menu |
| `new` | nothing | the BBS's own login prompt: `NEW` to sign up (only when `new_users = yes`) |

For a name with spaces, use `_` or `.` in their place (`Alice_Smith`).
Names are not case-sensitive. Wrong passwords count against the caller's
address just as they do on a terminal (`fail_delay` and throttling), and a
locked account cannot log in with either a password or a key.

## Keys

Users add their own keys in one of these ways:

- on the text BBS, in *Your profile* (`K`), after giving their password
  again (for an account modemmenu made, its modemmenu password or a
  [sign-in code](Web-Forum#sign-in-codes));
- through the [HTTP API](HTTP-API), with `POST /api/v1/me/keys` and a
  login's session token (not an API key).

The sysop can also add them:

```
bbs sshkey add alice ~alice/.ssh/id_ed25519.pub   # each line of the file (- : stdin)
bbs sshkey list [USER]
bbs sshkey del ID
```

The BBS accepts ed25519, ECDSA (including the security-key `sk-` kinds)
and RSA keys of 2048 bits or more, but not DSA. A key belongs to one user
only, a user can have up to 20, and keys carry no options (`command=`,
`from=`). The log records each key added, deleted or used to log in, by
its SHA-256 fingerprint.

## The session

The listener offers only a terminal session: a shell request, with a pty
if the client asks for one. The pty's terminal type and size go to the
text BBS just as telnet's do (`xterm-256color` means ANSI and UTF-8,
`ansi` and `syncterm` mean ANSI and CP437, and for anything else the
caller is asked). `ansi`, `utf8` and `sixel` on the listener set them as
they do on telnet. Commands (`ssh host ls`), SFTP and SCP, port and agent
forwarding, X11 and a second session on the same connection are all
refused. Resizing the window does not change the size the session began
with.

A client has 30 seconds after connecting to ask for its shell.
`max_per_ip` limits connections from one address, as on telnet.

## Host keys

```ini
host_keys = /etc/bbs/ssh_host_ed25519_key /etc/bbs/ssh_host_rsa_key
```

By default the listener uses an ed25519 key and an RSA key beside the
database, `ssh_host_ed25519_key` and `ssh_host_rsa_key`. If they are not
there, it creates them (mode 0600) the first time it starts, and the log
and `bbs check` show their fingerprints. Keys made by `ssh-keygen`
(OpenSSH or PEM format, without a passphrase) work too. A missing key is
created according to its name: RSA (3072 bits) if the name says `rsa`,
ECDSA P-256 if it says `ecdsa`, and ed25519 otherwise. Like sshd, the BBS
refuses a key file that other users can read.

## Old clients

```ini
legacy = yes
```

This adds the AES-CBC and 3DES-CBC ciphers and the 1024-bit
`diffie-hellman-group1-sha1` key exchange to the usual algorithms, for old
clients that have nothing newer (old SyncTERM and PuTTY builds, cryptlib).
These algorithms are weak, and `bbs check` warns about them. Without
`legacy`, the server offers curve25519, ECDH and DH of 2048 bits or more,
with AES-GCM, ChaCha20-Poly1305 and AES-CTR.

**See also:** [Configuration](Configuration), [Security](Security), [HTTP API](HTTP-API)
