If you give a newsgroup name to a message area, the area becomes a
newsgroup. Then its messages are also news articles. Newsreaders
(Thunderbird, slrn, tin, Pan) read and post them through the NNTP server
of the BBS. Peer news servers exchange them with the BBS. Thus, a
message that a caller posts from a modem shows on INN. A reply from a
newsreader shows on a 3270.

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

The NNTP server uses RFC 3977 with these commands:

- The commands for readers (`GROUP`, `LISTGROUP`,
  `ARTICLE`/`HEAD`/`BODY`/`STAT`, `NEXT`/`LAST`, `OVER`/`XOVER`,
  `HDR`/`XHDR`, `LIST ACTIVE`/`NEWSGROUPS`/`OVERVIEW.FMT`, `NEWNEWS`,
  `POST`).
- `IHAVE` for peers.
- `AUTHINFO USER/PASS` (RFC 4643) with the login, failure delay and
  throttling of the BBS.
- `STARTTLS` (RFC 4642).

Each message has a Message-ID. The BBS changes a message that a caller
posts on the BBS into an article. The article has From, Date, Path,
References from the thread to which it replies, and MIME UTF-8 text. The
BBS keeps an article from a different source exactly as it arrived. It
shows the article on the BBS as text. It decodes the poster, subject and
body (quoted-printable, base64, the charset, the text part of a
multipart). It cleans them as it cleans all other text from callers.

## Peers

The BBS syncs with its peers every `interval` seconds, and each time that
you run `bbs news sync`:

- *pull*: for each of our newsgroups that the peer carries (and that
  `groups` matches: a wildmat, `bbs.*,!bbs.test`), the BBS gets the
  articles after the last article that it got, with `OVER` and
  `ARTICLE`. The first time, it gets only the latest `initial` articles
  of each group.
- *push*: the BBS sends with `IHAVE` our articles that came after the last
  push. These are articles from the BBS, from a reader, or from a
  different peer. The BBS never sends an article back to the peer that
  sent it. If a server refuses `IHAVE` from us (a reader-only account),
  the BBS uses `POST`. The first time, the BBS only records the start
  point. It does not send the full history to a new peer.
- The peer can *feed* us with `IHAVE` from the addresses in
  `accept_from`. No other system can use `IHAVE`. The BBS looks up the
  names there when it loads the configuration (at the start and at each
  reload), not on each connection. If the address of a peer changes, do
  a reload, or list its addresses.

The BBS takes each article only one time. It keeps a history of
Message-IDs, also of the articles that it refused. It never takes an
article if this site is already in its Path. Thus, two peers that carry
the same group do not make a loop. The BBS stores the article in the
first of its newsgroups that this site carries. It puts the name of this
site at the start of the Path. We tested this with INN 2.7 in the two
directions: push to it, pull from it, and innxmit as the feed to the BBS.

## Not supported

The BBS refuses control messages (cancel, newgroup, rmgroup). It does
not do the action of these messages. When you delete a message on the
BBS, the message is not cancelled on other servers. A crossposted article
is in only one area here. There is no `NEWGROUPS` (the sysop creates
newsgroups). There is no streaming (`CHECK`/`TAKETHIS`). This is
satisfactory for the volume of a BBS.

**See also:** [FidoNet (the echo-newsgroup gateway)](FidoNet), [Security](Security)
