# Offline mail (QWK)

Callers can read and answer their messages offline with a QWK reader
(MultiMail, OLX, SLMR, Blue Wave's QWK mode, Synchronet's own...). The
BBS packs a caller's new messages into a **packet**. The caller reads it
and writes their answers offline, then brings them back as a **REP**.

## The packet

A packet is `BBSID.QWK`: a ZIP of `CONTROL.DAT`, `MESSAGES.DAT`, an index
file for each conference, `PERSONAL.NDX` (the messages to the caller),
`DOOR.ID` and `HELLO` (the message of the day). It holds:

- the caller's **unread mail**, in conference 0;
- the **new messages** in the areas of their new scan (the areas they
  left out of it stay out of the packet), each area a conference
  numbered by its ID. Every area the caller reads is listed, so a reader
  can post in one that isn't scanned.

A packet takes at most 500 new messages from one area, and 3000 in all.
A caller with more waiting takes another packet.

The messages count as read only once the packet has gone: the areas'
pointers move past them and the mail is marked read. A transfer that
fails leaves everything new, for the next packet.

Names and subjects longer than QWK's 25 characters travel as QWKE lines
(`Subject: ...`) at the head of the text, which most readers understand.
Text is CP437.

`BBSID` is made from the system's name: its first 8 letters and digits,
in capitals (`D-Modem BBS` is `DMODEMBB`). `qwk_id` in `[general]` sets
another:

```ini
[general]
qwk_id = DMODEM
```

Changing it later breaks callers' readers, which file packets under it.

## The REP

The caller's answers come back as `BBSID.REP`: a ZIP holding `BBSID.MSG`
(or the `.MSG` file on its own). Each message is posted, or mailed, as
if the caller had written it on the BBS:

- conference 0, or a message marked private, goes as mail to its To
  (a user, or `Name@zone:net/node` as netmail);
- any other conference is posted in that area, with the caller's access
  checked as for any post. The message it answers becomes its reply-to
  when that message is in the same area.

The REP's From is ignored: the message is from the caller. Messages refused (an
area they can't post in, a user who isn't there) are listed, and the
rest still go. A REP uploaded twice posts once: what each REP brought is
remembered for 90 days.

A REP is at most 16 MB and 1000 messages.

## Getting it

| how | the packet | the REP |
|---|---|---|
| text BBS: *O* on the main menu | *D*ownload, by XMODEM, YMODEM, ZMODEM or Kermit | *U*pload, likewise |
| 3270: *O* on the main menu | `IND$FILE GET BBSID.QWK` from the emulator's file transfer, typed on the screen's command line | `IND$FILE PUT BBSID.REP` |
| [FTP](Files-and-FTP) | `get /qwk/BBSID.QWK` | `put BBSID.REP` into `/qwk/` |
| [HTTP API](HTTP-API) | `GET /api/v1/qwk` | `POST /api/v1/qwk/rep` |

The FTP directory `/qwk` is there for logged-in users, not anonymous
ones. It hides a file area named `qwk` from them. The packet listed
there is made as it is asked for (and again after a minute). It counts
as taken once all of it has been read.

A sysop's own menus offer it with `qwk` ([customization](Customization)).
