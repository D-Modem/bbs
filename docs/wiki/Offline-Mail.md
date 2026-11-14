Callers can read and answer their messages offline with a QWK reader
(MultiMail, OLX, SLMR, the QWK mode of Blue Wave, the reader of
Synchronet, and others). The BBS puts the new messages of a caller into a
**packet**. The caller reads it and writes the answers offline. Then the
caller sends the answers back as a **REP**.

## The packet

A packet is `BBSID.QWK`. It is a ZIP of `CONTROL.DAT`, `MESSAGES.DAT`, an
index file for each conference, `PERSONAL.NDX` (the messages to the
caller), `DOOR.ID` and `HELLO` (the message of the day). It contains:

- The **unread mail** of the caller, in conference 0.
- The **new messages** in the areas of the new scan of the caller. The
  areas that the caller removed from the new scan are not in the packet.
  Each area is a conference, with its ID as the number. The packet lists
  all areas that the caller reads. Thus, a reader can post in an area
  that is not in the scan.

A packet has a maximum of 500 new messages from one area, and 3000 in
total. If more messages wait, the caller gets another packet.

The messages count as read only after the packet goes. Then the pointers
of the areas move past them, and the BBS marks the mail as read. If a
transfer fails, all items stay new for the next packet.

Names and subjects longer than the 25 characters of QWK go as QWKE lines
(`Subject: ...`) at the start of the text. Most readers know these
lines. Text is CP437.

The BBS makes `BBSID` from the name of the system: its first 8 letters
and digits, in capitals (`D-Modem BBS` is `DMODEMBB`). `qwk_id` in
`[general]` sets a different ID:

```ini
[general]
qwk_id = DMODEM
```

If you change it later, the readers of callers do not operate correctly,
because they file packets under this ID.

## The REP

The answers of the caller come back as `BBSID.REP`. This is a ZIP that
contains `BBSID.MSG` (or the `.MSG` file alone). The BBS posts or mails
each message, as if the caller wrote it on the BBS:

- Conference 0, or a message marked private, goes as mail to its To (a
  user, or `Name@zone:net/node` as netmail).
- The BBS posts all other conferences in their area. It checks the
  access of the caller, as for all posts. If the message that it answers
  is in the same area, that message becomes its reply-to.

The BBS ignores the From of the REP: the message is from the caller. The
BBS lists the messages that it refused (an area where the caller cannot
post, a user who does not exist). The other messages still go. If a REP
is uploaded two times, the BBS posts it one time only. The BBS keeps a
record of the contents of each REP for 90 days.

A REP has a maximum of 16 MB and 1000 messages.

## Getting it

| Where | The packet | The REP |
|---|---|---|
| text BBS: *O* on the main menu | *D*ownload, by XMODEM, YMODEM, ZMODEM or Kermit | *U*pload, in the same way |
| 3270: *O* on the main menu | `IND$FILE GET BBSID.QWK` from the file transfer of the emulator, typed on the command line of the screen | `IND$FILE PUT BBSID.REP` |
| [FTP](Files-and-FTP) | `get /qwk/BBSID.QWK` | `put BBSID.REP` into `/qwk/` |
| [HTTP API](HTTP-API) | `GET /api/v1/qwk` | `POST /api/v1/qwk/rep` |

The FTP directory `/qwk` is for logged-in users, not for anonymous users.
It hides a file area with the name `qwk` from them. The BBS makes the
packet in that directory when the user asks for it (and again after a
minute). The packet counts as taken when the user has read all of it.

The menus of a sysop can offer it with `qwk` ([customization](Customization)).

## QWK networks

QWK networks (DOVE-Net, the QWK side of FSXNet, and others) connect
BBSes through a **hub**. Each **node** calls the hub as a user. It puts a
REP of the messages that its callers wrote in the conferences of the
network. Then it takes a packet of the messages from all other systems.
This BBS can be a node or a hub.

### As a node

Give each network a `[qwknet NAME]` section. Put in it the FTP server of
the hub, the account that the hub gave you, and the area here for each
conference of the hub.

```ini
[qwknet dovenet]
address = hub.example.org:21
tls = explicit            ; FTPS (AUTH TLS); implicit, or no
user = MYBBS
password = secret
dir = /qwk                ; where the hub keeps its packet ("/": the root)
every = 60                ; minutes between calls (0: only when asked)
2001 = DOVE General       ; CONFERENCE = AREA
2002 = DOVE Chat
```

The areas must exist (`bbs area add "DOVE General"`). On each call, the
BBS does these steps:

1. It puts `HUBID.REP` in the directory of the hub. This file contains
   the messages written here in those areas since the last call (a
   maximum of 1000 for each call).
2. It takes `HUBID.QWK` and posts its contents in the areas of its
   conferences, with the name of each writer. It ignores conferences
   that are not in the list. It also ignores mail to the account of the
   node.

`HUBID` is the packet that the BBS finds in the directory of the hub, or
`hub_id`. The first call sends no messages that are already here. Only
messages written on this BBS go up. Messages that came by a network, a
news peer or FidoNet stay here. The BBS knows each message from the hub
by its conference and number. Thus, if it takes a packet two times, it
posts nothing two times. If the answered message came by the network,
answers keep their threads in the two directions.

`bbs qwknet call [NET]` (or `POST /api/v1/admin/qwknet/call`) calls now.

### As a hub

Make an account for each node and give it the flag:

```
bbs user add MYNODE
bbs user qwknode MYNODE
```

The account of a node takes its packet and puts its REP in the same way
as all callers. [FTP](Files-and-FTP) (`/qwk`) is the best method. The
BBS posts the contents of its REP with the names of the writers on the
node, not the name of the account. The BBS still checks its access on
each area. Its packet does not contain the messages that came from the
node. A node gets the areas of the new scan of its account.

**See also:** [Message areas](Message-Areas), [Files and FTP](Files-and-FTP), [File transfers](File-Transfers)
