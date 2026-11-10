Callers on different nodes can talk to each other in three ways: in chat
rooms, by private messages, and by paging the sysop. Each user also keeps
a [friends list](#friends).

## Chat rooms

*C* on the text main menu (*Chat* on a 3270's) goes into the main room.
Anyone can start another room with `/join NAME`. A room closes when its
last caller leaves, unless the sysop has made it
[permanent](IRC#permanent-rooms). Anyone who joins a room sees its last
20 lines.

| Command | What it does |
|---|---|
| (a line) | say it to the room |
| `/me ACTION` | `* alice waves` |
| `/who` | who is in this room |
| `/rooms` | the rooms in use, and who is in them |
| `/join ROOM` | go to another room (letters, digits and `-`, up to 20) |
| `/msg WHO TEXT` | a private message to a user (or to a node, by its number), [kept](#private-messages) if they are away |
| `/history [WHO]` | those you have talked with, or your last lines with one |
| `/friends` | your [friends list](#friends), and who on it is on |
| `/friend WHO`, `/unfriend WHO` | put a user on your friends list, or take them off |
| `/block WHO`, `/unblock WHO` | refuse a user's private messages, or take them again ([blocking](#blocking)) |
| `/topic [TEXT]` | the room's topic; a [registered room](IRC#chanserv)'s ops set it, the sysop a [permanent room](IRC#permanent-rooms)'s |
| `/cs COMMAND` | ChanServ: [register a room](IRC#chanserv), its access list (`/cs HELP`) |
| `/kick WHO [WHY]` | put one out of the room (a registered room's ops; `Carol_Ann` for Carol Ann) |
| `/quit` | leave the chat |
| `/help` | these |

On the text BBS, a line someone else says appears the moment it arrives,
above what you are typing, which is then redrawn. A 3270 shows only what
it is sent, and is best not sent anything while its caller types, so the
chat screen shows new lines each time you press Enter (to say something,
or with nothing typed) or PF5.

A caller can say up to 8 lines in 10 seconds; more than that is refused
("slow down").

Rooms whose names begin with `mrc-` belong to [MRC](MRC) and are shared
with other BBSes. With [IRC server linking](IRC-Linking), rooms beginning
`irc-` are an IRC network's channels, and `/msg NICK TEXT` reaches
someone on that network. The rooms are also channels on the BBS's [IRC](IRC)
server (`#main` and the rest), with IRC clients in them alongside the
callers.

## Private messages

On the text BBS, *Who's online* ends by offering to send a line to a
node, and `/msg WHO TEXT` sends one from chat (on a 3270 too), to a user
by name or to a node by its number. The line reaches the caller
wherever they are: on the text BBS, at whatever prompt they are at; on a
3270, on the message line of their next screen (with `(+N)` when more
are waiting); on [IRC](IRC) as a `PRIVMSG`; on [AIM or ICQ](AIM-and-ICQ)
or [MSN](MSN-Messenger) or [Yahoo](Yahoo-Messenger) as an IM.

Private messages are kept, however they were sent: the BBS's chat, IRC,
AIM, ICQ, MSN or Yahoo. A message to a user who isn't on is kept for them, and they
get it when they next log in, with the time it was sent (an ICQ client
gets them as its offline messages). `/history WHO` (on IRC, `/msg
BBSServ HISTORY nick`) shows your last lines with someone. Only the two
users in a conversation can read it back.

## Friends

Every user has one friends list, of BBS users. It is the same list
everywhere: `/friends` in chat, `FRIENDS` to BBSServ on IRC, and the
buddy list on [AIM and ICQ](AIM-and-ICQ#buddies), the contact list on
[MSN](MSN-Messenger#contacts) and [Yahoo](Yahoo-Messenger#buddies). A friend added on one shows on the others;
an AIM, MSN or Yahoo client that is on gets them at once, in their group
(`Friends` on AIM if they have none).

## Blocking

Every user also has one blocked list. `/block WHO` puts a user on it
(and `/unblock WHO` takes them off), as do MSN's "Block" and Yahoo's
ignore list. A blocked
user's private messages are refused, however they send them (chat, IRC,
AIM, MSN, Yahoo) and whether or not you are on, and MSN and Yahoo show
you offline to them.

## Paging the sysop

*S* on the text main menu (*Page the sysop* on a 3270) asks what the page
is about, and tells every sysop who is logged in and not busy:

```
** alice (node 2) is paging you: my upload failed  (Chat, then /answer) **
```

The caller waits in a private room of their own (`sysop-2`), which only
they and sysops can join. A sysop answers with `/answer` in chat (which
picks the caller who has waited longest) or `/answer NODE`. `/pages`
lists who is waiting, and `/busy` turns pages to that sysop off and on.
The sysop's main menu shows `Chat (1 paging)` while someone waits. If the
caller gives up (`/quit`, PF3), the page is cancelled.

If no sysop is on, the caller is offered the chance to leave the sysop a
message, and `page_command` (in `[general]`) runs to tell the sysop some
other way:

```
[general]
page_command = /usr/local/bin/notify-sysop {user} {node} {reason}
```

`{user}`, `{node}` and `{reason}` are filled in as whole arguments, and
are also given as `BBS_PAGE_USER`, `BBS_PAGE_NODE` and `BBS_PAGE_REASON`.
There is no shell, so nothing a caller typed is ever run. The command can
take up to 30 seconds. For example, with [ntfy](https://ntfy.sh):

```
page_command = curl -s -d "{user} on node {node}: {reason}" https://ntfy.sh/my-bbs-pages
```

A caller can page at most once a minute.

**See also:** [Administration](Administration), [Security](Security)
