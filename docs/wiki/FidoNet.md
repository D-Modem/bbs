The BBS can be a node (or point) of FidoNet or a different FTN network
(fsxNet, AgoraNet, or your own network). It has these functions:

- If you give an echo tag to a message area, the area becomes an echo.
- Users send netmail when they address mail to `Name@zone:net/node`.
- The BBS exchanges packets with its links over binkp. It calls them and
  it answers their calls.

The BBS is its own tosser, scanner and mailer. Thus, no other FTN
software is necessary.

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

## Echomail

The BBS packs the messages that users post in an echo for each link
that carries the echo. The packed messages have MSGID, REPLY for
threads, PID, TZUTC, CHRS, tear and origin lines, SEEN-BY and PATH. The
BBS tosses the messages from links into the echo. It decodes them from
their character set, according to their CHRS kludge (CP437 and the
other DOS code pages, Latin-1, KOI8, Windows code pages, UTF-8). It
cleans them as it cleans all other text from callers.

With more than one link, the BBS passes echomail between them by
SEEN-BY, as a hub does. The BBS takes each message only one time (by its
MSGID, or by a digest for messages without a MSGID). It never takes a
message if this system is already in its PATH.

## Netmail

On the text BBS and on a 3270, mail to `Name@1:2/3` (or `Name@1:2/3.4`)
is netmail. The BBS sends it through the link with that address, or
through the link of the node of that point. If neither exists, it uses
the link with `route = yes`. Netmail for this system goes to the user of
that name (or to the sysop), from `Name@zone:net/node`. Thus, *Reply*
answers it as netmail (with a REPLY kludge). The BBS does not route the
netmail of other systems: it is a leaf.

## binkp

The mailer uses binkp (FTS-1026) with CRAM-MD5 passwords (FTS-1027). It
knows links by their address and password, and refuses other systems.
It never sends a password in clear text unless `plain_password = yes`.

Packets are Type 2+. The BBS sends them as ARCmail bundles
(`packer = zip`, the default, or `arc`, `arj`, `lha`) or as bare packets
(`none`). The BBS tosses received bundles of the four types and bare
packets. It does not use the names of the bundles: it reads their
contents to find the type. Broken packets go to `dir/bad`. Files that
have no TIC and are one day old (for example, file requests, or a file
that a sysop sent manually) go into `dir/in/LINK/files`.

The BBS packs new mail in one minute (netmail immediately). The BBS
calls a link with a host when mail waits for it (and again every five
minutes while calls fail), and every `poll` seconds. The first scan for
a new link only records the start point. Thus, the BBS does not send the
full history to the link.

## Gateways to newsgroups

An area that is an echo and also a newsgroup is a gateway. The BBS
serves echomail to newsreaders as articles (From
`Name <Name@f1.n2.z3.fidonet.org>`, with the Message-ID made from the
MSGID, as other gateways do). Articles from news peers go out as
echomail. Most FidoNet echoes do not want gated traffic. Thus, ask
first.

## Bundles

Almost all hubs send ARCmail as ZIP, but older systems send ARC, ARJ or
LHA. The BBS reads all four types itself. Thus, you do not have to
install archivers. The BBS reads these methods:

- ZIP: stored and deflated.
- ARC: stored, packed, squeezed, crunched and squashed.
- ARJ: methods 1 to 4.
- LHA: `-lh0-` and `-lh4-` to `-lh7-`, with header levels 0 to 2.

The BBS stores the bundles that it writes with `packer = arc`, `arj` or
`lha` without compression. Thus, all unpackers can read them. The same
readers list archives in the file areas and find their FILE_ID.DIZ.

## Nodelists

`nodelist` in `[fido]` names nodelists (FTS-5000) by path or pattern, or
as `area:NAME` for the file area that the NODELIST file echo fills. The
BBS reads the newest file of each, plain or in an archive. It reads it
again when a newer file arrives. With a nodelist:

- The BBS refuses netmail to a node that is not in the nodelist, or that
  the nodelist shows as down, when the user writes it. When the user
  sends netmail, the BBS tells the user the name and place of the
  system.
- `host = nodelist` makes the BBS call a link at the address in its
  nodelist entry (the host of its IBN flag, or its INA).
- `bbs fido node` and `bbs fido nodes` look up systems. Callers can also
  do this: **N** (PF5 on a 3270) in their mailbox searches the nodelist.

The BBS checks only the zones that the nodelists cover. It sends netmail
for other zones, and for links, without a check.

## AreaFix and FileFix

A link with `areafix = yes` manages its own echoes. From the echoes that
`echoes` (and `fileechoes`) permit, it gets the echoes that it asks for.
To ask, it sends netmail to AreaFix (or AreaMgr) and FileFix (or
FileMgr, AllFix, Raid, TicFix) with its `areafix_password` as the
subject and one command on each line:

```
+FSX_GEN        link it (a pattern too: +FSX_*)
-FSX_GEN        unlink it; -ALL unlinks every one
%LIST           what it may have, * where linked
%QUERY          what it has linked
%UNLINKED       what it has not
%HELP           this
```

The answer comes back by netmail. The sysop can do the same from the
command line (`bbs fido link`, `unlink`, `filelink`, `fileunlink`), and
see the echoes of a link (`bbs fido subs`). A link without `areafix`
gets all echoes that `echoes` permits.

In the other direction, `bbs fido areafix hub COMMAND...` (or `filefix`)
writes to the robot of a link with `areafix_password`. With
`create = yes` on that link, the BBS creates the echoes and file echoes
that the link then sends (as a message area, or a sysop-upload file
area, with the tag as the name). It does not discard them.

## File echoes

A file area becomes a file echo with `bbs filearea echo AREA TAG`. Files
arrive with TICs (FTS-5006). The BBS does these steps for each file:

1. It checks the file: that it came from the link with its
   `tic_password`, its size and CRC-32, that it is not a duplicate, and
   that it did not already go through this system.
2. It puts the file in the area with its description.
3. It sends the file with its own TIC to the other links that carry the
   echo and are not in its SEEN-BY.

The `Replaces` of a TIC removes the older files that it names, from the
files that came by a file echo. The BBS hatches files that are uploaded
here (by callers, by FTP or with `bbs file add`). This means that it
sends them to all links that carry the echo. If a file does not agree
with its TIC, it goes to `dir/bad`. A TIC waits one day for its file. As
with echomail, the first scan for a link only records the start point.

## Testing

We tested the BBS against binkd 1.1a and CrashMail II 1.7, with
echomail and netmail in the two directions. The BBS called binkd, and
binkd called the BBS, with ZIP bundles and CRAM-MD5. The BBS tosses
bundles from the real `arc`, `arj` and `jlha` correctly, and those tools
unpack the bundles that the BBS writes.

## Not supported

File requests (FREQ), the routing of netmail from other systems, the NR
mode of binkp, CRYPT and multiple batches (the BBS uses binkp/1.0, which
all mailers accept), and EMSI for mailers that call by modem.

**See also:** [Newsgroups and NNTP](Newsgroups-and-NNTP), [Security](Security)
