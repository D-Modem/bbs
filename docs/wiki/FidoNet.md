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
bbs filearea echo Nodediff NODEDIFF ; a file area is the file echo NODEDIFF
bbs fido node 21:1/100              ; a system in the nodelist
bbs fido nodes Wellington           ; systems by name, place or sysop
bbs fido subs downlink              ; what a link has, echoes and file echoes
bbs fido link downlink FSX_GEN      ; link it to echoes (unlink; filelink,
                                    ; fileunlink for file echoes)
bbs fido areafix hub +FSX_* %LIST   ; ask the hub's AreaFix (filefix: FileFix)
```

```
; bbs.conf
[fido]
address = 21:1/999              ; ours; more after it are AKAs
origin = D-Modem BBS, by phone and telnet
dir = /var/lib/bbs/fido          ; inbound, outbound, bad packets
charset = cp437                  ; or utf-8
nodelist = /var/lib/bbs/fido/nodelist/FSXNET.*   ; the newest of each

[binkp]                          ; answer calls (port 24554)
address = :24554

[link hub]                       ; our hub: a link per system
address = 21:1/100
host = hub.example.net:24554       ; to call it ("": it calls us)
password = secret                ; the session password
echoes = FSX_*                   ; the echoes exchanged, a wildmat
route = yes                      ; netmail for anywhere goes by it
poll = 3600                      ; call every hour, mail or not
fileechoes = FSX_*               ; the file echoes exchanged (none if unset)
tic_password = secret            ; the TICs' password, both ways
areafix_password = FIXIT         ; to ask its AreaFix and FileFix
create = yes                     ; make the echoes and file echoes it sends
packer = zip                     ; or arc, arj, lha; none: bare packets

[link downlink]                  ; a node that has its echoes from us
address = 21:1/200
password = other
echoes = *                       ; what it may have...
fileechoes = *
areafix = yes                    ; ...and asks for, by AreaFix and FileFix
areafix_password = FIXIT2
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
sent as ARCmail bundles (`packer = zip`, the default, or `arc`, `arj`,
`lha`) or bare (`none`); received bundles of any of the four (whatever
they are called: the BBS reads their contents to tell) and packets are
tossed, and broken packets go to `dir/bad`.  Files with no TIC that are a
day old (file requests, a sysop's file sent by hand) are set aside in
`dir/in/LINK/files`.  New mail is packed within a minute
(netmail at once), and a link with a host is called when it has mail waiting (again every
five minutes while calls fail) and every `poll` seconds.  The first scan
for a new link only marks where to start: the history is not dumped on
it.

An area that is both an echo and a newsgroup is a gateway: echomail is
served to newsreaders as articles (From `Name <Name@f1.n2.z3.fidonet.org>`,
the Message-ID made of the MSGID as other gateways make it), and articles
from news peers go out as echomail.  Most FidoNet echoes do not want
gated traffic: ask first.

## Bundles

ARCmail comes in ZIP from nearly every hub, but older systems send ARC,
ARJ or LHA.  The BBS reads all four itself (no archivers to install):
ZIP's stored and deflated, ARC's stored, packed, squeezed, crunched and
squashed, ARJ's methods 1 to 4, and LHA's `-lh0-`, `-lh4-` to `-lh7-`
with header levels 0 to 2.  What it writes, with `packer = arc`, `arj`
or `lha`, is stored, uncompressed: every unpacker reads it.  The same
readers list archives in the file areas and find their FILE_ID.DIZ.

## Nodelists

`nodelist` in `[fido]` names nodelists (FTS-5000), by path or pattern,
or `area:NAME` for the file area the NODELIST file echo fills; the newest
file of each is read, plain or in an archive, and read again when a newer
one comes.
With a nodelist:

- netmail to a node it lacks, or has as down, is refused as it is
  written, and the writer is told the system's name and place when it is
  sent;
- `host = nodelist` has a link called where its nodelist entry says (its
  IBN flag's host, or its INA);
- `bbs fido node` and `bbs fido nodes` look systems up, and callers can
  too: **N** (PF5 on a 3270) in their mailbox searches the nodelist.

Only zones the nodelists have are checked: netmail for other zones, and
for links, goes as it did.

## AreaFix and FileFix

A link with `areafix = yes` manages its own echoes: it has, of those
`echoes` (and `fileechoes`) allow, the ones it asks for, by netmail to
AreaFix (or AreaMgr) and to FileFix (or FileMgr, AllFix, Raid, TicFix)
with its `areafix_password` as the subject and a command a line:

```
+FSX_GEN        link it (a pattern too: +FSX_*)
-FSX_GEN        unlink it; -ALL unlinks every one
%LIST           what it may have, * where linked
%QUERY          what it has linked
%UNLINKED       what it has not
%HELP           this
```

The answer comes back by netmail.  The sysop can do the same from the
command line (`bbs fido link`, `unlink`, `filelink`, `fileunlink`) and see
what a link has (`bbs fido subs`).  A link without `areafix` has every
echo `echoes` allows, as before.

The other way, `bbs fido areafix hub COMMAND...` (and `filefix`) writes
to a link's robot with `areafix_password`; `create = yes` on that link has
the echoes and file echoes it then sends made here (a message area, or a
sysop-upload file area, named by the tag) instead of dropped.

## File echoes

A file area becomes a file echo with `bbs filearea echo AREA TAG`.  Files
come with TICs (FTS-5006): each is checked (from the link, its
`tic_password`, size and CRC-32, not a dupe and not already past this
system), put in the area with its description, and passed on, with its
own TIC, to the other links that carry the echo and are not in its
SEEN-BY.  A TIC's `Replaces` removes the older files it names, of those
that came by a file echo.  Files uploaded here (by callers, FTP or `bbs
file add`) are hatched: sent to every link that carries the echo.  A file
that does not match its TIC goes to `dir/bad`; a TIC waits a day for its
file.  As with echomail, the first scan for a link only marks where to
start.

## Tried against

binkd 1.1a and CrashMail II 1.7: echomail and netmail both ways, calling
binkd and called by it, with ZIP bundles and CRAM-MD5.  Bundles made by
the real `arc`, `arj` and `jlha` are tossed, and what the BBS writes those
tools unpack.

What is not done: file requests (FREQ), routing others' netmail, binkp's
NR mode, CRYPT and multiple batches (it speaks binkp/1.0, which every
mailer accepts), and EMSI for mailers calling by modem.

**See also:** [Newsgroups and NNTP](Newsgroups-and-NNTP), [Security](Security)
