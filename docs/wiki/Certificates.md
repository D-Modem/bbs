The BBS has its own ACME client, similar to acme.sh. It gets
certificates from Let's Encrypt, ZeroSSL or a different ACME CA. It
keeps them adjacent to the database, and renews them before they expire.
All TLS servers of the BBS can use them: the web server, mail (IMAP,
POP3, SMTP), news, FTP, IRC, IRC linking, NTS, telnet over TLS, and
Gemini and Yahoo! Messenger when you configure them to.

It uses [lego](https://github.com/go-acme/lego) (MIT). It is disabled
unless there is an `[acme]` section.

## The short way

```
[acme]
email = sysop@example.org
domains = bbs.example.org, mail.example.org

[http]
address = :443
```

A web server and a mail server need only these settings. The CA checks
the names over HTTP on port 80. The BBS answers these checks itself.
Then all TLS servers with no `cert_file` and `key_file` (or `tls_cert`
and `tls_key`) use the certificate. The BBS gets the first certificate
at the start. Thus, the servers have no certificate for the few seconds
that this takes.

Ports 80 and 443 must have root (the BBS binds them before it stops its
root access, with `user =`) or `CAP_NET_BIND_SERVICE`.

## Which certificate a server uses

| `cert_file` | the certificate |
|---|---|
| none, with `[acme]` | the ACME certificate whose names include the name that the client asks for (SNI), or the first |
| `acme` | the same, given explicitly (the method for the `tls_cert` of `[listen]`, for Gemini and for Yahoo) |
| `acme:NAME` | the `[acme NAME]` certificate |
| a path | that file, with `key_file` |

The BBS reads files again when they change, for all renewal methods. It
reads the files of the ACME client again within one minute of each
renewal, with no restart.

Two servers keep their own certificates unless you configure them:

- **Gemini** clients pin the first certificate that they see, and an
  ACME certificate gets a new key at each renewal. Thus, Gemini keeps the
  certificate that `bbs
  gemini cert` makes. Use `cert_file = acme` for a capsule whose visitors
  check the certificate against a CA.
- The login of **Yahoo! Messenger** is `login.yahoo.com`. No CA will sign
  this name for this host. The login still must have a certificate that
  the client trusts.

## More than one certificate

The `domains` of `[acme]` make the certificate with the name `default`.
Each `[acme NAME]` makes a different certificate, with its own
`challenge`, `dns` and `key_type` if they are different:

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

The challenge is the method that shows the CA that the names belong to
this host:

- **`http`** (the default): a token at
  `http://NAME/.well-known/acme-challenge/`. If a plain web server of the
  BBS is already on port 80 (`[http]` with `tls = no`, or the
  `http_address` of `[ymsg]`), it answers. If not, the client listens on
  `http_address`, `:80` by default.
- **`tls-alpn`**: a certificate on port 443, from the TLS server of the
  BBS that is there. No server must listen on port 80.
- **`dns`**: a TXT record at `_acme-challenge.NAME`. This is the only
  method for `*.wildcards`, and the method for a BBS behind a firewall.
  `dns` names the method that makes the record.

## DNS providers

`dns =` is one of these:

- A built-in provider: `cloudflare`, `route53`, `digitalocean`,
  `hetzner`, `gandiv5`, `ovh`, `namecheap`, `porkbun`, `godaddy`,
  `desec`, `duckdns`, `pdns` (PowerDNS), `rfc2136` (BIND and similar
  servers, by TSIG), `acme-dns` and `httpreq`. Their credentials are the
  environment variables of lego, for example `CF_DNS_API_TOKEN` for
  Cloudflare. Refer to
  [lego's DNS providers](https://go-acme.github.io/lego/dns/).
- **`acmesh:dns_xx`**: one of the approximately 150
  [dnsapi scripts](https://github.com/acmesh-official/acme.sh/wiki/dnsapi)
  of acme.sh, run with the functions of acme.sh loaded. Set `acmesh` to
  the path of acme.sh. Their credentials are the variables of acme.sh
  (`CF_Token`, `GD_Key`...). The credentials that acme.sh saved in its
  `account.conf` also operate.
- **`exec:PROGRAM`**: a program of the sysop, run as `PROGRAM present
  _acme-challenge.NAME. VALUE` and then `PROGRAM cleanup ...`.

Put credentials in the environment (the `Environment=` or
`EnvironmentFile=` of systemd) or in `dns_env_file`. This file has
`KEY=value` lines, and keeps the credentials out of `bbs.conf`:

```
# /etc/bbs/acme-dns.env, mode 0600, the BBS user's
CF_DNS_API_TOKEN=...
```

The client waits until the record is published before the CA looks:

- `dns_check = authoritative` (the default) asks the servers of the
  zone.
- `dns_check = resolvers` asks `dns_resolvers`.
- `dns_check = no` only waits `dns_wait` seconds, as the `--dnssleep` of
  acme.sh does.

## CAs

`ca =` is `letsencrypt` (the default), `letsencrypt-staging` (for tests:
it has much higher limits, and its certificates are not trusted),
`zerossl`, `buypass`, `google`, `sslcom`, or the URL of a directory.

ZeroSSL and Google must have External Account Binding: `eab_kid` and
`eab_hmac_key` from the CA. For ZeroSSL, as with acme.sh, an `email` is
sufficient: the client gets the binding for it.

The client makes the account when it first needs it, and agrees to the
terms of the CA. It keeps the account under `acme/accounts/` with the
host name of the CA.

## Renewal

The client gets a certificate again in these conditions:

- It has `renew_days` (30) or fewer days before it expires.
- The renewal information (ARI) of the CA says to renew it.
- Its `domains` or `key_type` changed.

The client looks every 12 hours, and one hour after a failure. After
each new certificate, the BBS runs `hook` (by `sh`) with `CERT_NAME`,
`CERT_FILE`, `KEY_FILE` and `CERT_DOMAINS` set, to give the certificate
to other programs:

```
hook = cp "$CERT_FILE" /etc/nginx/bbs.crt && cp "$KEY_FILE" /etc/nginx/bbs.key && systemctl reload nginx
```

## Files

Under `acme/` adjacent to the database (or `dir`):

| path | what |
|---|---|
| `certs/NAME.crt` | the certificate and its chain (PEM) |
| `certs/NAME.key` | its key (PEM, mode 0600) |
| `accounts/HOST/` | the account at that CA: its key and registration |
| `challenges/` | the challenges that `bbs acme renew` answers now |

Other programs can use the certificates from `certs/`.

## Commands

```
bbs acme list               the certificates: names, challenge, CA, expiry, files
bbs acme renew              get the ones due now
bbs acme renew -force       get them all now
bbs acme renew NAME...      get these now
```

`bbs check` also lists them.

`bbs acme renew` can run while `bbs serve` runs. When `bbs serve` holds
port 80 or 443, it answers the challenges of that renewal itself: the
renewal puts them in `acme/challenges/` for it. `bbs serve` reads the new
certificate within one minute.

## Trying it out

Use `ca = letsencrypt-staging` until you get a certificate. Then remove
it. The production limits of Let's Encrypt are strict for repeated
failures.

To try it with no public name, run
[Pebble](https://github.com/letsencrypt/pebble), the test CA of Let's
Encrypt, with `ca = https://localhost:14000/dir` and
`LEGO_CA_CERTIFICATES=pebble.minica.pem` in the environment. Thus, the
client trusts it.

## The BBS's own certificate authority

No public CA will sign for `login.live.com`, `login.yahoo.com` or
`api.screenname.aol.com`. The old clients ask for these names after the
[DNS server](DNS) of the BBS sends them here. For these names, the BBS
is its own certificate authority. You put its certificate on the machine
of each caller one time. The BBS signs the certificate of each server
itself, and renews it before it expires.

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

`tls_cert = ca` (`cert_file = ca` for Gemini) does not need a key file.
At its start, `bbs serve` makes the CA if there is none (in `ca/`
adjacent to the database). It signs a certificate for the names of each
server, and looks again each day.

The CA is a root with sub-CAs below it. Each has its own key:

- The root (`ca/ca.crt`) is the certificate that the machines of callers
  trust. It signs only the sub-CAs.
- The services sub-CA (`ca/services-ca.crt`) signs the certificates of
  the servers of the BBS.
- A program adjacent to the BBS, for example a TLS proxy, gets its own
  sub-CA (`bbs ca subca proxy`).

The BBS sends each certificate with the certificate of its sub-CA after
it. Thus, clients need only the root. The key of a sub-CA signs only the
certificates of servers. The key of the root stays with the BBS. The
servers of the BBS sign with these names:

| Server | Names |
|---|---|
| `[msn]` | `login.passport.com` (its common name), `nexus.passport.com`, `loginnet.passport.com`, `login.live.com` |
| `[ymsg]` | `login.yahoo.com` |
| `[oscar]` | `api.screenname.aol.com`, `api.oscar.aol.com`, `kdc.uas.aol.com`, `slogin.oscar.aol.com`, `login.oscar.aol.com`, `login.messaging.aol.com`, `slogin.icq.com`, `login.icq.com`, and the name of `tls_host` |
| `[gemini]` | `hostname` |

The certificate of each server includes only its own names. Thus, with
Windows XP clients (which name no server in TLS), give each server its
own IP, as above. Do not use one shared port 443. If an HTTPS address is
on its own IP, the certificate also includes that IP, for a client that
got the address. The keys are RSA 2048, which AIM 6.2 to 7.5 and Windows
XP must have. A certificate is valid for 397 days. The BBS signs it
again 30 days before it expires, and keeps its key. The certificate of
Gemini is valid for the same time as the CA, because Gemini clients pin
the first certificate that they see.

### On callers' machines

`ca/install-ca.zip` puts the CA in the trusted roots of a machine. Put it
in a file area for callers to download, or give it to them. It contains:

| File | For |
|---|---|
| `NAME-CA.cer` | Windows 98 and later: double-click, Install Certificate, Trusted Root Certification Authorities |
| `install-ca.inf` | Windows 95 and later: right-click, Install (the roots of the machine, by the registry) |
| `install-ca.reg` | the same, with a double-click. `remove-ca.reg` removes it |
| `install-ca.bat` | Windows Vista and later, as an administrator: `certutil`, or the `.reg` without it |
| `NAME-CA.mobileconfig` | macOS and iOS: a profile |
| `NAME-CA.crt` | Linux (`update-ca-certificates`), and programs with their own store: Firefox, Netscape, AIM 6 and later (its NSS database) |
| `README.txt` | the file to use, and the fingerprint of the CA to check |

The `.inf` and `.reg` write the certificate in the location where Windows
keeps the trusted roots of the machine
(`HKLM\SOFTWARE\Microsoft\SystemCertificates\ROOT`). This operates from
Windows 95, and Internet Explorer is not necessary. We did not try the
package on the old Windows versions yet.

### Old Windows: SHA-1

Windows 95 to 2000, and XP before Service Pack 3, do not know SHA-256.
Make the CA with `bbs ca init -sha1` (before other functions make it).
Then it signs with SHA-1, as the CAs of that time did. Current systems
refuse SHA-1 certificates. Thus, use it only for a BBS whose callers
have machines that old.

### A TLS proxy

Give a TLS proxy in front of a service (stunnel, nginx, WebOne, an
SSL-stripping proxy for old browsers) its own sub-CA. Do not give it the
root or the services sub-CA:

```sh
bbs ca subca proxy
```

This makes `ca/proxy-ca.crt` and `ca/proxy-ca.key` (made one time, then
shown). A proxy that makes its certificates when necessary (for example,
the port of WebOne) uses those two files, and sends `proxy-ca.crt` after
each certificate that it signs. For a proxy that must have fixed files,
`bbs ca issue -ca proxy -out
/etc/stunnel login.example.com 192.168.77.20` writes
`login.example.com.crt` (with the certificate of the proxy CA after it)
and `login.example.com.key`. Callers who installed the CA trust the two,
as they trust the servers of the BBS. Without `-ca`, `bbs ca issue`
signs with the services sub-CA.

### Commands

```
bbs ca                 the CA: its fingerprint, until when, its sub-CAs, what it signs
bbs ca init [-sha1]    make it now (the root, the services sub-CA, the zip)
bbs ca subca NAME      a sub-CA for a program beside the BBS (proxy), made if none
bbs ca issue [-ca NAME] [-out DIR] [-days N] NAME...   a certificate a sub-CA signs
bbs ca package [-out FILE]                  the zip again
```

Keep `ca/` with your backups, and keep its `.key` files private. A person
with one of them can make certificates that the machines of callers
trust. A CA that an earlier version of the BBS made, before the sub-CAs,
signed servers itself. The BBS refuses this type of CA and tells you.
Move `ca/` to a different location. Then the BBS makes a new CA, and
callers install the new `install-ca.zip`.
