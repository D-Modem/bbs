With a `[mail]` section each user has an e-mail address, and their BBS
mail is their e-mail. A letter written on a 3270, a modem or the web goes
out to the internet, and mail from the internet arrives in the same inbox
the CompuServe and BBS mail commands read. Mail programs (Thunderbird,
Apple Mail, mutt, K-9) read it over IMAP or POP3 and send over SMTP, logged
in with the BBS name and password.

```
; bbs.conf
[mail]
domain = example.org
hostname = mail.example.org
dkim_key_file = /etc/bbs/dkim.pem
cert_file = /etc/bbs/cert.pem
key_file = /etc/bbs/key.pem

[imap]
address = :993
tls = implicit

[pop3]
address = :995
tls = implicit

[smtp]                       ; submission: users send mail, logged in
address = :587
tls = required

[mx]                         ; mail from the internet
address = :25
```

Every server runs only if its section is there. All of them use the BBS's
guard (bans, throttling), and each has `max_clients`, `max_per_ip` and
`idle_timeout`. `tls` is `no`, `optional` (STARTTLS offered), `required`
(no login before STARTTLS) or `implicit` (TLS from the first byte). Ports
below 1024 are bound before the BBS drops root, as the others are.

## Addresses

A user's address is their name in lower case, with spaces as dots, at the
domain: `Alice Smith` is `alice.smith@example.org`. `bbs mail address
NAME` prints it. `+tags` are ignored (`alice.smith+lists@` reaches Alice).
`postmaster@`, `abuse@`, `hostmaster@` and `webmaster@` go to `[mail]
postmaster`, or the first sysop.

When writing mail on the BBS, the recipient can be a user's name, a FidoNet
address (`Name@1:234/5`) or an internet address (`bob@example.net`). In
CompuServe Mail, `>INTERNET:bob@example.net` works as it did there. Mail to
FidoNet's internet form (`john.doe@f5.n234.z1.fidonet.org`) goes as
netmail.

## Folders

Mail is kept once. Each user has their own entry for it, with its read and
starred marks, so the same mail sent to five users is stored once. Every
mail program sees the same folders:

| Folder | Holds | Special-use |
|---|---|---|
| INBOX | mail received, not deleted | |
| Sent | mail sent | `\Sent` |
| Drafts | drafts saved by a mail program | `\Drafts` |
| Starred | starred mail (`\Flagged`) | `\Flagged` |
| Unread | unread mail received | |
| All Mail | everything not deleted | `\All` |
| Trash | deleted mail | `\Trash` |

Starred, Unread and All Mail are views: starring a mail in the inbox puts
it in Starred, and taking it out of Starred unstars it. Deleting moves mail
to the Trash, where a mail program can restore it (by moving it out) or
expunge it. Mail in the trash longer than `trash_days` (30) is removed for
good. Folders cannot be created or renamed.

POP3 sees the inbox. Mail retrieved is marked read, and `DELE` puts it in
the trash.

## Sending to the internet

`[mail] outbound` says how mail for other domains leaves:

- `direct` (the default): the BBS looks up each domain's MX and delivers
  on port 25, with STARTTLS when the other side offers it. Many home and
  cloud networks block port 25 out, and receivers want SPF, DKIM and a
  reverse DNS name for the host, so this needs some DNS work (below).
- `smarthost`: everything goes through another mail server (your
  provider's, a relay such as Postfix, or a service like Mailgun), given in
  `[smarthost]`. That server does the delivering. This is the easy route
  when one is at hand.
- `none`: only mail between users here, and FidoNet.

```
[mail]
domain = example.org
outbound = smarthost

[smarthost]
address = smtp.example.net:587
tls = starttls               ; or implicit (465), or no
username = bbs@example.org
password = secret
```

Mail out waits in a queue, tried again with longer gaps (five minutes, then
up to four hours) for `queue_days` (5). When a server refuses a recipient for
good, or the days run out, the sender gets a note in their inbox saying
why. `bbs mail queue` lists what is waiting, and `bbs mail send` tries it
all now.

## DNS for a domain of its own

For mail to arrive, and to be believed when sent directly:

```
example.org.              MX  10 mail.example.org.
mail.example.org.         A   203.0.113.5
example.org.              TXT "v=spf1 mx -all"
bbs._domainkey.example.org. TXT "v=DKIM1; k=rsa; p=..."
_dmarc.example.org.       TXT "v=DMARC1; p=quarantine"
```

and a reverse DNS (PTR) name for 203.0.113.5 of `mail.example.org`, which
the hosting provider sets. `bbs mail dkim /etc/bbs/dkim.pem` makes the DKIM
key and prints its record. With a smarthost, use the SPF and DKIM its
provider gives instead.

## Mail from the internet

`[mx]` takes mail for the domain on port 25, without a login, and only for
users here: it is never an open relay. With `checks = yes` (the default) it
checks SPF, DKIM and DMARC, and adds an `Authentication-Results` header. A
mail is refused when it fails a DMARC policy of `p=reject`, or, for a domain
with no DMARC record, when SPF fails and no signature of that domain holds.

### Spam: DNS blocklists

A DNSBL is a list of addresses known to send spam, asked by DNS. `[mx]
dnsbl` names the lists to ask; none are asked unless it is set:

```
[mx]
address = :25
dnsbl = zen.spamhaus.org, bl.spamcop.net
```

The connecting address is looked up in every list at once, at the first
`MAIL FROM`. If any list has it, the mail is refused with `554 5.7.1` and
the list's reason (its TXT record, often a link where the sender can ask to
be taken off), and the refusal is logged. A list that does not answer in
ten seconds, or answers with an error, refuses no one: mail is not lost
because a list is down. Addresses on this machine and private networks are
never looked up, and nor are users sending through `[smtp]`, who log in:
a user on a listed home connection can still send.

Spamhaus answers nothing useful to queries through public resolvers
(8.8.8.8, 1.1.1.1, a cloud provider's): the BBS logs `refused the query`
when that happens. Run a resolver of your own (unbound, or the system's
with no forwarders) on the mail host, and see Spamhaus's terms: heavy or
commercial use needs their paid feed. Lists to consider: `zen.spamhaus.org`
(Spamhaus SBL, XBL and PBL in one), `bl.spamcop.net`, `b.barracudacentral.org`
(free, after registering the resolver's address with them).

DNSBLs only look at the sending address. SPF, DKIM and DMARC (above) check
that mail claiming a domain comes from it. Together they keep out most
spam; for content filtering (SpamAssassin, rspamd) put Postfix in front
and hand mail in by LMTP, below.

### LMTP

If another mail server already takes the domain's mail (Postfix, with its
own spam filtering), it can hand mail to the BBS by LMTP instead:

```
[lmtp]
address = /run/bbs/lmtp.sock
```

```
# Postfix main.cf
virtual_mailbox_domains = example.org
virtual_transport = lmtp:unix:/run/bbs/lmtp.sock
```

## On the terminal

Mail from the internet is shown as text: HTML mail is turned into plain
text, and attachments are listed by name and size (they stay whole for
mail programs). Messages larger than `max_message` (10M) are refused.

## Databases

Mail lives in the BBS's database, whichever it is: SQLite, PostgreSQL,
MySQL or MariaDB. `bbs db copy` carries the mail, the folders and the
queue along with the rest.
