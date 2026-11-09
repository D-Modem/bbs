The BBS can be The Microsoft Network as it was from 1995 to 1998, for the
MSN client that came with Windows 95 ("MSN Classic"). A caller signs in
with their BBS name and password, MSN Central is the BBS, the Categories
folder holds the BBS's message areas (a folder for each area group), and
each area opens as an MSN bulletin board: the messages listed with their
replies under them, a message opened in the reader, new posts and replies
from the Compose window, and a member's own messages deleted. MSN Today
lists the boards with the newest messages. The Inbox (Microsoft Exchange,
with the MSN service) is the caller's BBS mail, and the Chat folder holds
the BBS's chat rooms, where MSN callers talk with everyone else in them.
This is early work, not yet tried with a real client.

## Over TCP

```ini
[marvel]
address = :569
```

| Setting | Default | What it sets |
|---|---|---|
| `address` | `:569` | where to listen |
| `max_clients` | `64` | clients at once |
| `max_per_ip` | `4` | from one address |

The client can't be told a port: it is always 569. It connects to the name
in its Dial-Up Networking settings for "The Microsoft Network" (by default
`gateway.moswest.msn.net`): point that name at the BBS in the hosts file of
the client's machine (`C:\Windows\hosts`).

## By modem

A modem caller's client sends a carriage return and waits for `COM`, then
speaks MSN's own packets. A listener with `frontend = marvel` answers that
way (telnet, in binary, or raw), so a modem bridge such as D-Modem can hand
the call over as it does for the text BBS:

```ini
[listen msn]
type = raw
address = 127.0.0.1:2325
frontend = marvel
```

Point the client's phone number at the line that reaches that listener.

## What callers get

- **Signing in**: the Sign In dialog's member ID and password are a BBS
  account's. A wrong password, an unknown name, a locked account and a
  throttled address each get the client's own message, and failed sign-ins
  count against the address as everywhere else. A signed-in client is a
  node and shows in Who's On as `MSN`. The Password dialog changes the BBS
  password. The billing pages show the account's name and location and
  keep nothing.
- **The directory**: only the areas the caller may read are listed. Go
  words are the areas' and groups' names (in any case), and `today`.
- **Bulletin boards**: up to the newest few hundred messages of an area
  (as many as fit in one reply). Bodies are plain text. A board the caller
  may not post in is read-only in the client. A post may come as plain or
  rich text (compressed or not); it is kept as plain text, with its reply
  threading. Attachments are refused.
- **E-mail**: Exchange's Inbox gets the caller's BBS mail, and a message
  read there is marked read. Mail sent from Exchange goes out as BBS mail:
  to a member by name, or to an internet address where the BBS sends
  e-mail. Rich text is kept as plain text, and an attachment is left out
  with a line saying so. If some recipients can't be sent to, the rest
  still get it and the sender gets a note from Postmaster naming the ones
  that didn't. Deleting a message moves it to the BBS's trash.
- **The address book**: the BBS's members (not locked accounts or QWK
  nodes), for Exchange's member list, for names typed into To:, and for
  the Member Properties sheet (name, location and the About text).
- **Chat**: the Chat folder (go word `chat`, in MSN Central and in
  Categories) lists the main room, the permanent rooms and those in use,
  with each one's topic and how many are in it. Opening one joins it in
  the chat window, with everyone already there listed. MSN callers share
  rooms with telnet, IRC and every other caller, and with linked rooms on
  other networks. `/me` at the start of a line is an action. Sysop rooms
  aren't listed, and rooms can't be made from the client.

Not served yet: Find, file downloads, the
sign-up wizard and MSN Today's Media Viewer title. The 1996 client
(Windows 95 OSR2, MSN 1.3) signs in with NTLM, which needs a password hash
the BBS doesn't keep, so it can't sign in.

## Where the protocol comes from

The protocol, codenamed Marvel, was reverse-engineered by the
[marvel-msn](https://github.com/gabspeck/marvel-msn) project, whose notes
and wire captures this server is written from. None of that project's
code is used.
