Callers on different nodes can talk to each other in three ways: in chat
rooms, with private messages, and with a page to the sysop. Each user
also has a [friends list](#friends).

## Chat rooms

*C* on the text main menu (*Chat* on the main menu of a 3270) goes into
the main room. All users can start a different room with `/join NAME`.
A room closes when its last caller leaves, unless the sysop made it
[permanent](IRC#permanent-rooms). When a user joins a room, the user
sees its last 20 lines.

| Command | What it does |
|---|---|
| (a line) | sends the line to the room |
| `/me ACTION` | `* alice waves` |
| `/who` | shows the users in this room |
| `/rooms` | shows the rooms in use, and the users in them |
| `/join ROOM` | goes to a different room (letters, digits and `-`, up to 20) |
| `/msg WHO TEXT` | sends a private message to a user (or to a node, by its number). The BBS [keeps](#private-messages) it if the user is away |
| `/history [WHO]` | shows the users that you talked with, or your last lines with one user |
| `/friends` | shows your [friends list](#friends), and which friends are on |
| `/friend WHO`, `/unfriend WHO` | puts a user on your friends list, or removes the user |
| `/block WHO`, `/unblock WHO` | refuses the private messages of a user, or accepts them again ([blocking](#blocking)) |
| `/topic [TEXT]` | the topic of the room. The ops of a [registered room](IRC#chanserv) set it. The sysop sets the topic of a [permanent room](IRC#permanent-rooms) |
| `/cs COMMAND` | ChanServ: [register a room](IRC#chanserv), its access list (`/cs HELP`) |
| `/kick WHO [WHY]` | removes a user from the room (ops of a registered room. `Carol_Ann` for Carol Ann) |
| `/mode [CHANGES]` | the [channel modes](IRC#channel-modes) of a registered room: `/mode +m`, `/mode +b alice`. `/mode +b` lists the bans |
| `/invite WHO` | invites a user into the room (past `+i`, `+k`, `+l`) |
| `/join ROOM KEY` | goes into a room with a key |
| `/knock ROOM [WHY]` | asks the ops of a room that you cannot go into to let you in |
| `/quit` | leaves the chat |
| `/help` | shows these commands |

On the text BBS, a line from a different user shows immediately when it
arrives. It shows above the text that you type, and the BBS then draws
your text again. A 3270 shows only the data that it gets. It is best to
send nothing to a 3270 while its caller types. Thus, the chat screen
shows new lines each time that you push Enter (to send a line, or with
no text) or PF5.

A caller can send up to 8 lines in 10 seconds. The BBS refuses more
lines ("slow down").

Rooms with names that start with `mrc-` are part of [MRC](MRC). Other
BBSes share them. With [IRC server linking](IRC-Linking), rooms that
start with `irc-` are the channels of an IRC network. `/msg NICK TEXT`
sends a message to a user on that network. The rooms are also channels
on the [IRC](IRC) server of the BBS (`#main` and the other rooms). IRC
clients are in them together with the callers.

## Private messages

On the text BBS, at the end of *Who's online*, the BBS offers to send a
line to a node. `/msg WHO TEXT` sends a line from chat (also on a 3270),
to a user by name or to a node by its number. The line goes to the
caller on all routes:

- On the text BBS, at the current prompt of the caller.
- On a 3270, on the message line of the next screen (with `(+N)` when
  more lines wait).
- On [IRC](IRC) as a `PRIVMSG`.
- On [AIM or ICQ](AIM-and-ICQ) or [MSN](MSN-Messenger) or
  [Yahoo](Yahoo-Messenger) as an IM.

The BBS keeps private messages from all routes: the chat of the BBS,
IRC, AIM, ICQ, MSN or Yahoo. If a user is not on, the BBS keeps the
message for the user. The user gets it at the next login, with the time
of the message (an ICQ client gets them as its offline messages).
`/history WHO` (on IRC, `/msg
BBSServ HISTORY nick`) shows your last lines with a user. Only the two
users in a conversation can read it again.

## Friends

Each user has one friends list, of BBS users. It is the same list on all
routes: `/friends` in chat, `FRIENDS` to BBSServ on IRC, the buddy list
on [AIM and ICQ](AIM-and-ICQ#buddies), and the contact list on
[MSN](MSN-Messenger#contacts) and [Yahoo](Yahoo-Messenger#buddies). A
friend that you add on one route shows on the other routes. An AIM, MSN
or Yahoo client that is on gets the friends immediately, in their group
(`Friends` on AIM if they have no group).

## Blocking

Each user also has one blocked list. `/block WHO` puts a user on it, and
`/unblock WHO` removes the user. "Block" on MSN and the ignore list of
Yahoo do the same. The BBS refuses the private messages of a blocked
user on all routes (chat, IRC, AIM, MSN, Yahoo), also when you are not
on. MSN and Yahoo show you as offline to the blocked user.

## Paging the sysop

*S* on the text main menu (*Page the sysop* on a 3270) asks for the
subject of the page. Then the BBS tells each sysop who is logged in and
not busy:

```
** alice (node 2) is paging you: my upload failed  (Chat, then /answer) **
```

The caller waits in a private room (`sysop-2`). Only the caller and
sysops can join it. A sysop answers with `/answer` in chat (this selects
the caller who waited longest) or with `/answer NODE`. `/pages` lists
the callers who wait. `/busy` disables and enables pages to that sysop.
While a caller waits, the main menu of the sysop shows
`Chat (1 paging)`. If the caller stops (`/quit`, PF3), the BBS cancels
the page.

If no sysop is on, the BBS offers the caller the option to leave a
message for the sysop. Then `page_command` (in `[general]`) runs, to
tell the sysop in a different way:

```
[general]
page_command = /usr/local/bin/notify-sysop {user} {node} {reason}
```

The BBS puts the values of `{user}`, `{node}` and `{reason}` in as full
arguments. It also gives them as `BBS_PAGE_USER`, `BBS_PAGE_NODE` and
`BBS_PAGE_REASON`. There is no shell. Thus, the BBS never runs text that
a caller typed. The command can use up to 30 seconds. For example, with
[ntfy](https://ntfy.sh):

```
page_command = curl -s -d "{user} on node {node}: {reason}" https://ntfy.sh/my-bbs-pages
```

A caller can page a maximum of one time each minute.

**See also:** [Administration](Administration), [Security](Security)
