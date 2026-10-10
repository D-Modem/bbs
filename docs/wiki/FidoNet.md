The BBS can be a node (or point) of FidoNet or any other FTN network
(fsxNet, AgoraNet, a network of your own): a message area becomes an
echo when it is given an echo tag, users write netmail by addressing mail
to `Name@zone:net/node`, and the BBS exchanges packets with its links by
binkp, calling them and answering them.  It is its own tosser, scanner and
mailer: no other FTN software is needed.

```
bbs area echo General FSX_GEN       ; General is the echo FSX_GEN
bbs area echo General -             ; no longer an echo
bbs fido poll                       ; call the links now (also: poll LINK)
bbs fido scan                       ; pack new mail into the outbound
bbs fido toss                       ; take in what has come
```

```
; bbs.conf
[fido]
address = 21:1/999              ; ours; more after it are AKAs
origin = D-Modem BBS, by phone and telnet
dir = /var/lib/bbs/fido          ; inbound, outbound, bad packets
charset = cp437                  ; or utf-8

[binkp]                          ; answer calls (port 24554)
address = :24554

[link hub]                       ; our hub: a link per system
address = 21:1/100
host = hub.example.net:24554       ; to call it ("": it calls us)
password = secret                ; the session password
echoes = FSX_*                   ; the echoes exchanged, a wildmat
route = yes                      ; netmail for anywhere goes by it
poll = 3600                      ; call every hour, mail or not
```

**Echomail**: what users post in an echo is packed for each link that
carries it (with MSGID, REPLY for threads, PID, TZUTC, CHRS, tear and
origin lines, SEEN-BY and PATH), and what links send is tossed into the
echo, decoded from its character set (CP437 and the other DOS code pages,
Latin-1, KOI8, Windows code pages, UTF-8, by its CHRS kludge) and cleaned
like anything else a caller writes.  With more than one link the BBS
passes echomail on between them, by SEEN-BY, as a hub does.  Messages are
taken once (by MSGID, or a digest of messages that lack one) and never if
this system is in their PATH already.

**Netmail**: on the text BBS and on a 3270, mail to `Name@1:2/3` (or
`Name@1:2/3.4`) is netmail, sent by the link with that address, the
link of that point's node, or else the link with `route = yes`.  Netmail
for this system goes to the user of that name, or the sysop, from
`Name@zone:net/node`, so *Reply* answers it as netmail (with a REPLY
kludge).  The BBS routes no one else's netmail: it is a leaf.

**binkp** (FTS-1026) with CRAM-MD5 passwords (FTS-1027): links are known by
their address and password, others are turned away; a password is never
sent in the clear unless `plain_password = yes`.  Packets are Type 2+,
sent as ZIP bundles (ARCmail, `packer = zip`, the default) or bare
(`none`); received ZIP bundles and packets are tossed, files that are not
mail (TIC file echoes, requests) are set aside in `dir/in/LINK/files`, and
broken packets in `dir/bad`.  New mail is packed within a minute
(netmail at once), and a link with a host is called when it has mail waiting (again every
five minutes while calls fail) and every `poll` seconds.  The first scan
for a new link only marks where to start: the history is not dumped on
it.

An area that is both an echo and a newsgroup is a gateway: echomail is
served to newsreaders as articles (From `Name <Name@f1.n2.z3.fidonet.org>`,
the Message-ID made of the MSGID as other gateways make it), and articles
from news peers go out as echomail.  Most FidoNet echoes do not want
gated traffic: ask first.

It was tried against binkd 1.1a and CrashMail II 1.7: echomail and netmail
both ways, calling binkd and called by it, with ZIP bundles and CRAM-MD5.

What is not done: ARC, ARJ and LHA bundles (ZIP only, which nearly every
hub sends), file echoes (TIC), file requests, AreaFix (the sysop sets
`echoes` by hand and asks the hub's AreaFix by netmail), nodelists, routing
others' netmail, binkp's NR mode, CRYPT and multiple batches (it speaks
binkp/1.0, which every mailer accepts), and EMSI for mailers calling by
modem.

**See also:** [Newsgroups and NNTP](Newsgroups-and-NNTP), [Security](Security)
