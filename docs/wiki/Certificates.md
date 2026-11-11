The BBS has its own ACME client, in the manner of acme.sh. It gets
certificates from Let's Encrypt, ZeroSSL or any other ACME CA, keeps them
beside the database, and renews them before they run out. Every TLS server
of the BBS can use them: the web server, mail (IMAP, POP3, SMTP), news,
FTP, IRC, IRC linking, NTS, telnet over TLS, and Gemini and Yahoo!
Messenger when told.

It is built on [lego](https://github.com/go-acme/lego) (MIT). It is off
unless there is an `[acme]` section.

## The short way

```
[acme]
email = sysop@example.org
domains = bbs.example.org, mail.example.org

[http]
address = :443
```

That is all a web server and mail server need. The CA checks the names
over HTTP on port 80, which the BBS answers itself. Any TLS server with no
`cert_file` and `key_file` (or `tls_cert` and `tls_key`) then uses the
certificate. The first is got at start-up, so the servers have none for
the few seconds that takes.

Port 80 and 443 need root (the BBS binds them before it gives root up,
with `user =`) or `CAP_NET_BIND_SERVICE`.

## Which certificate a server uses

| `cert_file` | the certificate |
|---|---|
| none, with `[acme]` | the ACME one whose names cover the name the client asks for (SNI), or the first |
| `acme` | the same, said outright (the way for `[listen]`'s `tls_cert`, for Gemini and for Yahoo) |
| `acme:NAME` | the `[acme NAME]` certificate |
| a path | that file, with `key_file` |

Files are read again when they change, whatever renews them. The ACME
client's are read again within a minute of each renewal, with no restart.

Two servers keep their own unless told:

- **Gemini** clients pin the certificate they first see, and an ACME
  certificate gets a new key each renewal, so Gemini keeps the one `bbs
  gemini cert` makes. Use `cert_file = acme` for a capsule whose visitors
  check it against a CA.
- **Yahoo! Messenger**'s login is `login.yahoo.com`, a name no CA will
  sign for this host. It still needs a certificate the client trusts.

## More than one certificate

`[acme]`'s own `domains` make the certificate named `default`. Each
`[acme NAME]` makes another, with its own `challenge`, `dns` and
`key_type` if they differ:

```
[acme]
email = sysop@example.org
domains = bbs.example.org

[acme mail]
domains = mail.example.org, imap.example.org, smtp.example.org
challenge = tls-alpn

[acme wild]
domains = *.example.org, example.org
challenge = dns
dns = cloudflare
dns_env_file = /etc/bbs/acme-dns.env

[irc]
tls_address = :6697
tls_cert = acme:wild
```

## Challenges

How the CA is shown the names are this host's:

- **`http`** (the default): a token at
  `http://NAME/.well-known/acme-challenge/`. A plain web server of the
  BBS's already on port 80 (`[http]` with `tls = no`, or `[ymsg]`'s
  `http_address`) answers it. Otherwise the client listens on
  `http_address`, `:80` by default.
- **`tls-alpn`**: a certificate on port 443, from whichever TLS server of
  the BBS is there. Nothing has to listen on port 80.
- **`dns`**: a TXT record at `_acme-challenge.NAME`. This is the only one
  for `*.wildcards`, and the one for a BBS behind a firewall. `dns` names
  how the record is made.

## DNS providers

`dns =` is one of:

- A provider built in: `cloudflare`, `route53`, `digitalocean`, `hetzner`,
  `gandiv5`, `ovh`, `namecheap`, `porkbun`, `godaddy`, `desec`, `duckdns`,
  `pdns` (PowerDNS), `rfc2136` (BIND and the like, by TSIG), `acme-dns`
  and `httpreq`. Their credentials are lego's environment variables, such
  as `CF_DNS_API_TOKEN` for Cloudflare. See
  [lego's DNS providers](https://go-acme.github.io/lego/dns/).
- **`acmesh:dns_xx`**: one of acme.sh's 150 or so
  [dnsapi scripts](https://github.com/acmesh-official/acme.sh/wiki/dnsapi),
  run with acme.sh's own functions loaded. Set `acmesh` to acme.sh's path.
  Their credentials are acme.sh's variables (`CF_Token`, `GD_Key`...), and
  those acme.sh saved in its `account.conf` work too.
- **`exec:PROGRAM`**: a program of the sysop's, run as `PROGRAM present
  _acme-challenge.NAME. VALUE` and then `PROGRAM cleanup ...`.

Credentials go in the environment (systemd's `Environment=` or
`EnvironmentFile=`) or in `dns_env_file`, `KEY=value` lines kept out of
`bbs.conf`:

```
# /etc/bbs/acme-dns.env, mode 0600, the BBS user's
CF_DNS_API_TOKEN=...
```

The client waits for the record to be out before the CA looks:

- `dns_check = authoritative` (the default) asks the zone's own servers.
- `dns_check = resolvers` asks `dns_resolvers`.
- `dns_check = no` only waits `dns_wait` seconds, like acme.sh's
  `--dnssleep`.

## CAs

`ca =` is `letsencrypt` (the default), `letsencrypt-staging` (for trying
it out: it has much higher limits, and its certificates are not trusted),
`zerossl`, `buypass`, `google`, `sslcom`, or a directory's URL.

ZeroSSL and Google need External Account Binding: `eab_kid` and
`eab_hmac_key` from the CA. For ZeroSSL, as with acme.sh, an `email` is
enough: the client gets the binding for it.

The account is made when first needed, agreeing to the CA's terms. It is
kept under `acme/accounts/` with the CA's host name.

## Renewal

A certificate is got again when:

- it has `renew_days` (30) or fewer days left,
- the CA's renewal information (ARI) says to,
- or its `domains` or `key_type` have changed.

The client looks every 12 hours, and an hour after a failure. After each
new certificate, `hook` is run (by `sh`) with `CERT_NAME`, `CERT_FILE`,
`KEY_FILE` and `CERT_DOMAINS` set, to hand it to other programs:

```
hook = cp "$CERT_FILE" /etc/nginx/bbs.crt && cp "$KEY_FILE" /etc/nginx/bbs.key && systemctl reload nginx
```

## Files

Under `acme/` beside the database (or `dir`):

| path | what |
|---|---|
| `certs/NAME.crt` | the certificate and its chain (PEM) |
| `certs/NAME.key` | its key (PEM, mode 0600) |
| `accounts/HOST/` | the account at that CA: its key and registration |
| `challenges/` | challenges being answered for `bbs acme renew` |

Other programs can use the certificates from `certs/`.

## Commands

```
bbs acme list               the certificates: names, challenge, CA, expiry, files
bbs acme renew              get the ones due now
bbs acme renew -force       get them all now
bbs acme renew NAME...      get these now
```

`bbs check` lists them too.

`bbs acme renew` can run while `bbs serve` does. When `bbs serve` holds
port 80 or 443, it answers that renewal's challenges itself: they are put
in `acme/challenges/` for it. `bbs serve` reads the new certificate within
a minute.

## Trying it out

Use `ca = letsencrypt-staging` until a certificate comes, then remove it.
Let's Encrypt's production limits are tight for repeated failures.

To try it with no public name at all, run
[Pebble](https://github.com/letsencrypt/pebble), Let's Encrypt's test CA,
with `ca = https://localhost:14000/dir` and
`LEGO_CA_CERTIFICATES=pebble.minica.pem` in the environment so the client
trusts it.

## The BBS's own certificate authority

No public CA will sign for `login.live.com`, `login.yahoo.com` or
`api.screenname.aol.com`, the names the old clients ask for once the
BBS's [DNS server](DNS) sends them here. For those the BBS is a
certificate authority of its own. Its certificate goes on each caller's
machine once; the BBS signs each server's certificate itself and renews
it before it runs out.

```ini
[msn]
https_address = 192.168.77.11:443
tls_cert = ca

[ymsg]
https_address = 192.168.77.12:443
tls_cert = ca

[oscar]
tls_address = 192.168.77.13:443
tls_cert = ca

[gemini]
hostname = gemini.example.org
cert_file = ca
```

`tls_cert = ca` (`cert_file = ca` for Gemini) needs no key file. At its
start `bbs serve` makes the CA if there is none (in `ca/` beside the
database), signs each server a certificate for its names, and looks again
daily.

The CA is a root and sub-CAs under it, each with its own key. The root
(`ca/ca.crt`) is the one callers' machines trust, and it signs only the
sub-CAs. The services sub-CA (`ca/services-ca.crt`) signs the BBS's own
servers' certificates, and a program beside the BBS, such as a TLS proxy,
gets a sub-CA of its own (`bbs ca subca proxy`). Each certificate is sent
with its sub-CA's after it, so clients need only the root. A sub-CA's key
signs nothing but servers' certificates, and the root's key stays with the
BBS. The BBS's servers sign with:

| Server | Names |
|---|---|
| `[msn]` | `login.passport.com` (its common name), `nexus.passport.com`, `loginnet.passport.com`, `login.live.com` |
| `[ymsg]` | `login.yahoo.com` |
| `[oscar]` | `api.screenname.aol.com`, `api.oscar.aol.com`, `kdc.uas.aol.com`, `slogin.oscar.aol.com`, `login.oscar.aol.com`, `login.messaging.aol.com`, `slogin.icq.com`, `login.icq.com`, and `tls_host`'s name |
| `[gemini]` | `hostname` |

Each server's certificate covers only its own names, so with Windows XP
clients (which name no server in TLS) give each its own IP, as above, not
one shared port 443. An HTTPS address on an IP of its own is in the certificate too, for a
client given the address. The keys are RSA 2048, which AIM 6.2 to 7.5 and
Windows XP need. A certificate is good for 397 days and signed again 30
days before its end, keeping its key. Gemini's is good for as long as the
CA, since Gemini clients pin what they first see.

### On callers' machines

`ca/install-ca.zip` puts the CA among a machine's trusted roots. Put it in
a file area for callers to download, or hand it out. It holds:

| File | For |
|---|---|
| `NAME-CA.cer` | Windows 98 and later: double-click, Install Certificate, Trusted Root Certification Authorities |
| `install-ca.inf` | Windows 95 and later: right-click, Install (the machine's roots, by the registry) |
| `install-ca.reg` | the same, double-clicked; `remove-ca.reg` takes it out |
| `install-ca.bat` | Windows Vista and later, as an administrator: `certutil`, or the `.reg` without it |
| `NAME-CA.mobileconfig` | macOS and iOS: a profile |
| `NAME-CA.crt` | Linux (`update-ca-certificates`), and programs with a store of their own: Firefox, Netscape, AIM 6 and later (its NSS database) |
| `README.txt` | which to use, and the CA's fingerprint to check |

The `.inf` and `.reg` write the certificate where Windows keeps the
machine's trusted roots (`HKLM\SOFTWARE\Microsoft\SystemCertificates\ROOT`),
which works from Windows 95 on, with no Internet Explorer needed. None of
the package has been tried on the old Windows versions yet.

### Old Windows: SHA-1

Windows 95 to 2000, and XP before Service Pack 3, know no SHA-256. Make
the CA with `bbs ca init -sha1` (before anything else makes it) and it
signs with SHA-1, as the CAs of the time did. Current systems refuse
SHA-1 certificates, so use it only for a BBS whose callers' machines are
that old.

### A TLS proxy

A TLS proxy in front of anything (stunnel, nginx, WebOne, an
SSL-stripping proxy for old browsers) is given a sub-CA of its own,
rather than the root or the services sub-CA:

```sh
bbs ca subca proxy
```

This makes `ca/proxy-ca.crt` and `ca/proxy-ca.key` (made once, then shown).
A proxy that makes its certificates as it goes, WebOne's port for one,
takes those two and sends `proxy-ca.crt` after each certificate it
signs. For a proxy that wants fixed files, `bbs ca issue -ca proxy -out
/etc/stunnel login.example.com 192.168.77.20` writes
`login.example.com.crt` (with the proxy CA's after it) and
`login.example.com.key`. Callers who installed the CA trust both, as they
do the BBS's own servers. Without `-ca`, `bbs ca issue` signs with the
services sub-CA.

### Commands

```
bbs ca                 the CA: its fingerprint, until when, its sub-CAs, what it signs
bbs ca init [-sha1]    make it now (the root, the services sub-CA, the zip)
bbs ca subca NAME      a sub-CA for a program beside the BBS (proxy), made if none
bbs ca issue [-ca NAME] [-out DIR] [-days N] NAME...   a certificate a sub-CA signs
bbs ca package [-out FILE]                  the zip again
```

Keep `ca/` with your backups, and its `.key` files private: anyone with
one can make certificates the callers' machines trust. A CA made by an
earlier BBS, before the sub-CAs, signed servers itself; the BBS refuses
it and says so. Move `ca/` away and it makes a new one, and callers
install the new `install-ca.zip`.
