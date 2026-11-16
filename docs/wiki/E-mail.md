With a `[mail]` section, each user has an e-mail address, and the BBS
mail of the user is the e-mail of the user. A letter written on a 3270,
a modem or the web goes out to the internet. Mail from the internet
arrives in the same inbox that the CompuServe and BBS mail commands
read. Mail programs (Thunderbird, Apple Mail, mutt, K-9) read it over
IMAP or POP3 and send over SMTP. They log in with the BBS name and
password.

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

Each server runs only if its section is there. All of them use the guard
of the BBS (bans, throttling). Each has `max_clients`, `max_per_ip` and
`idle_timeout`. `tls` has these values:

- `no`.
- `optional` (the server offers STARTTLS).
- `required` (no login before STARTTLS).
- `implicit` (TLS from the first byte).

The BBS binds ports below 1024 before it stops its root access, as it
does for the other ports.

## Addresses

The address of a user is the name in lower case, with dots in place of
spaces, at the domain: `Alice Smith` is `alice.smith@example.org`.
`bbs mail address
NAME` prints it. The BBS ignores `+tags` (`alice.smith+lists@` goes to
Alice). `postmaster@`, `abuse@`, `hostmaster@` and `webmaster@` go to
`[mail]
postmaster`, or to the first sysop.

When you write mail on the BBS, the recipient can be the name of a user,
a FidoNet address (`Name@1:234/5`) or an internet address
(`bob@example.net`). In CompuServe Mail, `>INTERNET:bob@example.net`
operates as it did on CompuServe. Mail to the internet form of FidoNet
(`john.doe@f5.n234.z1.fidonet.org`) goes as netmail.

## Folders

The BBS keeps each mail one time. Each user has an entry for it, with
its read and starred marks. Thus, if you send the same mail to five
users, the BBS stores it one time. All mail programs see the same
folders:

| Folder | Holds | Special-use |
|---|---|---|
| INBOX | mail received, not deleted | |
| Sent | mail sent | `\Sent` |
| Drafts | drafts that a mail program saved | `\Drafts` |
| Starred | starred mail (`\Flagged`) | `\Flagged` |
| Unread | unread mail received | |
| All Mail | all mail that is not deleted | `\All` |
| Trash | deleted mail | `\Trash` |

Starred, Unread and All Mail are views. If you star a mail in the inbox,
it shows in Starred. If you remove it from Starred, its star is removed.
When you delete mail, the BBS moves it to the Trash. There, a mail
program can restore it (it moves it out) or expunge it. The BBS
permanently removes mail that is in the trash for longer than
`trash_days` (30). You cannot create folders or change their names.

POP3 sees the inbox. The BBS marks retrieved mail as read. `DELE` puts
mail in the trash.

## Sending to the internet

`[mail] outbound` sets how mail for other domains goes out:

- `direct` (the default): the BBS looks up the MX of each domain and
  delivers on port 25. It uses STARTTLS when the other server offers it.
  Many home and cloud networks block outgoing port 25. Also, receivers
  want SPF, DKIM and a reverse DNS name for the host. Thus, this method
  needs some DNS work (refer to the text below).
- `smarthost`: all mail goes through a different mail server (that of
  your provider, a relay such as Postfix, or a service like Mailgun),
  that you give in `[smarthost]`. That server does the delivery. If you
  have such a server, this is the easy method.
- `none`: only mail between users on this BBS, and FidoNet.

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

Outgoing mail waits in a queue. The BBS tries it again with longer
intervals (five minutes, then up to four hours) for `queue_days` (5).
When a server permanently refuses a recipient, or when the days end, the
sender gets a note in the inbox with the reason. `bbs mail queue` lists
the mail that waits. `bbs mail send` tries all of it now.

## DNS for a domain of its own

These records are necessary for mail to arrive, and for receivers to
trust mail that the BBS sends directly:

```
example.org.              MX  10 mail.example.org.
mail.example.org.         A   203.0.113.5
example.org.              TXT "v=spf1 mx -all"
bbs._domainkey.example.org. TXT "v=DKIM1; k=rsa; p=..."
_dmarc.example.org.       TXT "v=DMARC1; p=quarantine"
```

Also, a reverse DNS (PTR) name of `mail.example.org` for 203.0.113.5 is
necessary. The hosting provider sets it.
`bbs mail dkim /etc/bbs/dkim.pem` makes the DKIM key and prints its
record. With a
smarthost, use the SPF and DKIM that its provider gives.

## Mail from the internet

`[mx]` accepts mail for the domain on port 25, without a login, and only
for users on this BBS. It is never an open relay. With `checks = yes`
(the default), it checks SPF, DKIM and DMARC, and adds an
`Authentication-Results` header. It refuses a mail in these conditions:

- The mail fails a DMARC policy of `p=reject`.
- For a domain with no DMARC record: SPF fails and no signature of that
  domain is valid.

### Spam: DNS blocklists

A DNSBL is a list of addresses that send spam. You ask it by DNS. `[mx]
dnsbl` names the lists to ask. If you do not set it, the BBS asks no
lists:

```
[mx]
address = :25
dnsbl = zen.spamhaus.org, bl.spamcop.net
```

At the first `MAIL FROM`, the BBS looks up the address of the connection
in all the lists at the same time. If a list has it, the BBS refuses the
mail with `554 5.7.1` and the reason of the list. The reason is the TXT
record of the list, which often has a link where the sender can ask for
removal. The BBS logs the refusal.

If a list does not answer in ten seconds, or answers with an error, the
BBS refuses no mail because of that list. Thus, mail is not lost when a
list does not operate. The BBS never looks up addresses on this machine
and on private networks. It also does not look up users who send
through `[smtp]`, because they log in. Thus, a user on a listed home
connection can still send.

Spamhaus gives no useful answer to queries through public resolvers
(8.8.8.8, 1.1.1.1, the resolvers of a cloud provider). When this occurs,
the BBS logs `refused the query`. Run your own resolver (unbound, or the
resolver of the system with no forwarders) on the mail host. Read the
terms of Spamhaus: heavy or commercial use must have their paid feed.
Some lists that you can use: `zen.spamhaus.org` (Spamhaus SBL, XBL and
PBL in one), `bl.spamcop.net`, `b.barracudacentral.org` (free, after you
register the address of the resolver with them).

DNSBLs look only at the address of the sender. SPF, DKIM and DMARC
(above) check that mail which claims a domain comes from that domain.
Together, they stop most spam. For content filters (SpamAssassin,
rspamd), put Postfix in front and give mail to the BBS by LMTP (refer to
the text below).

### LMTP

If a different mail server already accepts the mail of the domain
(Postfix, with its own spam filters), it can give mail to the BBS by
LMTP:

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

The BBS shows mail from the internet as text. It changes HTML mail into
plain text. It lists attachments by name and size (they stay complete
for mail programs). The BBS refuses messages larger than `max_message`
(10M).

## Databases

Mail is in the database of the BBS, for all engines: SQLite, PostgreSQL,
MySQL or MariaDB. `bbs db copy` copies the mail, the folders and the
queue with the other data.
