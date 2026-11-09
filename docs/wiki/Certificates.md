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
