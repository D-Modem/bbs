Callers can connect to the text BBS over SSH, with encryption. They log
in with their BBS password or with a key. Modern BBS terminals
(SyncTERM, NetRunner, MagiTerm) and a plain `ssh` client all use SSH.
Telnet and rlogin send passwords in clear text, but SSH does not.

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
| the name of a user | the BBS password of the user (SSH `password` or `keyboard-interactive`) | the main menu |
| the name of a user | one of the keys of the user | the main menu |
| `new` | nothing | the login prompt of the BBS: `NEW` to sign up (only when `new_users = yes`) |

For a name with spaces, use `_` or `.` in place of the spaces
(`Alice_Smith`). Names are not case-sensitive. Incorrect passwords count
against the address of the caller, as on a terminal (`fail_delay` and
throttling). A locked account cannot log in with a password or with a
key.

## Keys

Users add their own keys in one of these ways:

- On the text BBS, in *Your profile* (`K`), after they type their
  password again. For an account that modemmenu made, this is the
  modemmenu password or a
  [sign-in code](Web-Forum#sign-in-codes).
- Through the [HTTP API](HTTP-API), with `POST /api/v1/me/keys` and the
  session token of a login (not an API key).

The sysop can also add keys:

```
bbs sshkey add alice ~alice/.ssh/id_ed25519.pub   # each line of the file (- : stdin)
bbs sshkey list [USER]
bbs sshkey del ID
```

The BBS accepts ed25519, ECDSA (also the security-key `sk-` types) and
RSA keys of 2048 bits or more. It does not accept DSA. A key belongs to
one user only. A user can have up to 20 keys. Keys have no options
(`command=`, `from=`). The log records each key that a user adds,
deletes or uses to log in, by its SHA-256 fingerprint.

## The session

The listener offers only a terminal session: a shell request, with a pty
if the client asks for one. The terminal type and size of the pty go to
the text BBS, as for telnet:

- `xterm-256color` means ANSI and UTF-8.
- `ansi` and `syncterm` mean ANSI and CP437.
- For all other types, the BBS asks the caller.

`ansi`, `utf8` and `sixel` on the listener set these values, as they do
on telnet. The listener refuses these items: commands (`ssh host ls`),
SFTP and SCP, port and agent forwarding, X11 and a second session on the
same connection. If the caller changes the size of the window, the size
of the session does not change.

After a client connects, it has 30 seconds to ask for its shell.
`max_per_ip` sets the limit of connections from one address, as on
telnet.

## Host keys

```ini
host_keys = /etc/bbs/ssh_host_ed25519_key /etc/bbs/ssh_host_rsa_key
```

By default, the listener uses an ed25519 key and an RSA key adjacent to
the database, `ssh_host_ed25519_key` and `ssh_host_rsa_key`. If they are
not there, the listener creates them (mode 0600) when it starts for the
first time. The log and `bbs check` show their fingerprints. Keys from
`ssh-keygen` (OpenSSH or PEM format, without a passphrase) also operate.

The listener creates a missing key according to its name:

- RSA (3072 bits) if the name contains `rsa`.
- ECDSA P-256 if the name contains `ecdsa`.
- ed25519 for all other names.

As sshd does, the BBS refuses a key file that other users can read.

## Old clients

```ini
legacy = yes
```

This setting adds some algorithms to the usual algorithms: the AES-CBC
and 3DES-CBC ciphers and the 1024-bit `diffie-hellman-group1-sha1` key
exchange. These algorithms are for old clients that have no newer
algorithms (old SyncTERM and PuTTY builds, cryptlib). These algorithms
are weak, and `bbs check` gives a warning about them. Without `legacy`,
the server offers curve25519, ECDH and DH of 2048 bits or more, with
AES-GCM, ChaCha20-Poly1305 and AES-CTR.

**See also:** [Configuration](Configuration), [Security](Security), [HTTP API](HTTP-API)
