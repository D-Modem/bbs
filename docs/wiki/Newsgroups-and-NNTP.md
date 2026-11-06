A message area becomes a newsgroup when it is given a newsgroup name. Its
messages are then also news articles. Newsreaders (Thunderbird, slrn, tin,
Pan) read and post them through the BBS's NNTP server, and peer news
servers exchange them with it. So a message posted from a modem shows up
on INN, and a reply from a newsreader shows up on a 3270.

```
bbs area newsgroup General bbs.general
bbs area newsgroup General -          ; no longer a newsgroup
bbs news sync                         ; sync with every peer now
bbs news sync upstream                ; or one
```

```
; bbs.conf
[news]
hostname = news.example.org    ; this site in Path and Message-IDs
organization = D-Modem BBS
max_article = 64K

[nntp]
address = :563
tls = implicit                 ; or required: STARTTLS before AUTHINFO
cert_file = /etc/bbs/cert.pem
key_file = /etc/bbs/key.pem
read = anyone                  ; or users: log in first
post = yes

[peer upstream]
address = news.example.net:119
tls = starttls
username = bbs
password = secret
groups = bbs.*
accept_from = news.example.net ; may feed us by IHAVE
```

## The server

The NNTP server speaks RFC 3977 with the readers' commands (`GROUP`,
`LISTGROUP`, `ARTICLE`/`HEAD`/`BODY`/`STAT`, `NEXT`/`LAST`, `OVER`/`XOVER`,
`HDR`/`XHDR`, `LIST ACTIVE`/`NEWSGROUPS`/`OVERVIEW.FMT`, `NEWNEWS`, `POST`),
`IHAVE` for peers, `AUTHINFO USER/PASS` (RFC 4643) with the BBS's login,
failure delay and throttling, and `STARTTLS` (RFC 4642). Every message has
a Message-ID. A message posted on the BBS is turned into an article (with
From, Date, Path, References from the thread it replies to, and MIME UTF-8
text). An article that came from elsewhere is kept exactly as it arrived,
and shown on the BBS as text: its poster, subject and body are decoded
(quoted-printable, base64, the charset, the text part of a multipart) and
cleaned like anything else a caller writes.

## Peers

The BBS syncs with its peers every `interval` seconds, and whenever you
run `bbs news sync`:

- *pull*: for each of our newsgroups the peer carries (and `groups`
  matches: a wildmat, `bbs.*,!bbs.test`), the articles after the last one
  fetched, by `OVER` and `ARTICLE`. The first time, only the latest
  `initial` of each group.
- *push*: our articles posted since the last push (on the BBS, by a reader,
  or from another peer, never back to the peer they came from), by `IHAVE`;
  a server that refuses `IHAVE` to us (a reader-only account) gets `POST`.
  The first time only records where to start: the history is not dumped
  on a new peer.
- the peer may *feed* us by `IHAVE` from the addresses in `accept_from`;
  nobody else may use `IHAVE`. Names there are looked up when the
  configuration is loaded (at the start and each reload), not on each
  connection: if a peer's address changes, reload, or list its addresses.

Each article is taken only once (the BBS keeps a history of Message-IDs,
including refused ones), and never if this site is already in its Path, so
two peers that carry the same group do not loop. It is stored in the first
of its newsgroups that this site carries, and this site's name goes at the
head of its Path. This was tested against INN 2.7 in both directions:
pushing to it, pulling from it, and innxmit feeding the BBS.

## Not supported

Control messages (cancel, newgroup, rmgroup) are refused rather than acted
on, and deleting a message on the BBS does not cancel it elsewhere. A
crossposted article lives in only one area here. There is no `NEWGROUPS`
(the sysop creates newsgroups) and no streaming (`CHECK`/`TAKETHIS`),
which suits a BBS's volume.

**See also:** [FidoNet (the echo-newsgroup gateway)](FidoNet), [Security](Security)
