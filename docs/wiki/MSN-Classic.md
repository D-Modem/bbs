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
The Files folder holds the file areas, whose files download, and Find
searches all of it. This is early work, not yet tried with a real client.

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
- **The 1996 client** (Windows 95 OSR2, MSN 1.3) signs in with NTLM: it
  never sends the password, only an answer to a challenge worked out from
  the password's LAN Manager hash. The BBS keeps that hash and the NT
  hash for each account, made whenever the password is typed in the clear
  (signing in any other way, or setting a password), and bound to the
  password, so a password changed somewhere they aren't made leaves them
  unused until it is typed again. An account whose hashes aren't kept yet
  gets "Password not valid" from this client: sign in once by telnet, the
  web or the 1995 client first. The hashes are kept only while MSN Classic
  is served (a `[marvel]` section or a `frontend = marvel` listener) or
  `[security] ntlm` is on ([MSN 2.x](MSN-2)), and
  they are password equivalents, the LM hash a weak one, so the database
  needs guarding as a password file does. After signing in, the Password
  dialog and the billing pages travel sealed (RC4) under the sign-in's
  key, as the client expects.
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
  the Member Properties sheet (name, location and the About text). With
  them come the caller's own saved addresses, so a saved name typed into
  To: finds its address on any PC. The saved addresses are the same ones
  as CompuServe's Address Book, and an internet address the caller mails
  from Exchange is added to them (unless its name or address is there
  already). Exchange's own Personal Address Book stays on the caller's
  PC: the client never tells the server what it adds there.
- **Chat**: the Chat folder (go word `chat`, in MSN Central and in
  Categories) lists the main room, the permanent rooms and those in use,
  with each one's topic and how many are in it. Opening one joins it in
  the chat window, with everyone already there listed. MSN callers share
  rooms with telnet, IRC and every other caller, and with linked rooms on
  other networks. `/me` at the start of a line is an action. Sysop rooms
  aren't listed, and rooms can't be made from the client.

- **Files**: the Files folder (go word `files`, in MSN Central and in
  Categories) has a folder for each file area the caller may download
  from. Each file is a Download-and-Run file, as MSN's own downloads
  were: opening it has the client's File Transfer Manager fetch it, with
  its progress window, and then the client opens it as Explorer would (a
  program runs; a `.zip` opens in whatever the PC opens those with). The
  file lands in the PC's temporary folder, so a caller who wants to keep
  it saves it from there. Each download counts against the caller's
  daily limit and ratio, as it does everywhere else.
- **Find**: Find > The Microsoft Network searches the directory's names
  and descriptions: folders, boards, chat rooms, file libraries and files
  (not the messages in a board). The words and wildcards work as the
  dialog's help says: a word matches whole words, `*` and `?` stand in
  for letters, and a quoted phrase matches its words in order. The "of
  type" list narrows it (bulletin boards, chat rooms, "Kiosks and other
  files" for the files, newsgroups for the areas that carry one). Nothing
  here has a place, so a search with Place filled in finds nothing. At
  most 200 results come back.

Not served yet: the sign-up wizard and MSN Today's Media Viewer title.

## Where the protocol comes from

The protocol, codenamed Marvel, was reverse-engineered by the
[marvel-msn](https://github.com/gabspeck/marvel-msn) project, whose notes
and wire captures this server is written from. None of that project's
code is used.
