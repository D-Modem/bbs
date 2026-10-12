Callers on different nodes talk to each other: in chat rooms, by a
message to one node, and by paging the sysop.

## Chat rooms

*C* on the text main menu (*Chat* on a 3270's) goes into the main room.
Anyone may make another with `/join NAME`; a room goes when its last
caller leaves, unless the sysop has made it [permanent](IRC#permanent-rooms).  Whoever comes in sees its last 20 lines.

| command | |
|---|---|
| (a line) | say it to the room |
| `/me ACTION` | `* alice waves` |
| `/who` | who is in this room |
| `/rooms` | the rooms in use, and who is in them |
| `/join ROOM` | go to another room (letters, digits and `-`, up to 20) |
| `/msg NODE TEXT` | a line to one node, wherever its caller is |
| `/topic [TEXT]` | the room's topic; the sysop sets a [permanent room](IRC#permanent-rooms)'s |
| `/quit` | leave the chat |
| `/help` | these |

On the text BBS a line others say shows the moment it comes, above what
you are typing, which is then drawn again.  A 3270 shows nothing it was
not sent, and is better not sent anything while its caller types: the
chat screen shows new lines each time you press Enter (to say something,
or with nothing typed) or PF5.

A caller may say 8 lines in 10 seconds; more is refused ("slow down").

Rooms beginning `mrc-` are [MRC](MRC)'s, shared with other BBSes.
The rooms are channels on the BBS's [IRC](IRC) server too: `#main` and
the rest, with IRC clients in them alongside the callers.

## Messages to a node

*Who's online* on the text BBS ends by offering to send a node a line;
`/msg NODE TEXT` does it from chat (on a 3270 too).  The line reaches its
caller wherever they are: on the text BBS at the prompt they are at, on a
3270 on the message line of their next screen (with `(+N)` when more are
waiting).

## Paging the sysop

*S* on the text main menu (*Page the sysop* on a 3270) asks what it is
about, and tells every sysop logged in (and not busy):

```
** alice (node 2) is paging you: my upload failed  (Chat, then /answer) **
```

The caller waits in a private room of their own (`sysop-2`), which only
they and sysops can join.  A sysop answers with `/answer` in chat (the
caller waiting longest), or `/answer NODE`; `/pages` lists who is
waiting, and `/busy` turns pages to them off (and on again).  The sysop's
main menu shows `Chat (1 paging)` while someone waits.  If the caller
gives up (`/quit`, PF3), the page goes.

If no sysop is on, the caller is offered to leave the sysop a message, and
`page_command` (in `[general]`) runs, to tell the sysop some other way:

```
[general]
page_command = /usr/local/bin/notify-sysop {user} {node} {reason}
```

`{user}`, `{node}` and `{reason}` are filled in as whole arguments, and
also given as `BBS_PAGE_USER`, `BBS_PAGE_NODE` and `BBS_PAGE_REASON`;
there is no shell, so nothing a caller typed is run.  It may take 30
seconds.  For example, with [ntfy](https://ntfy.sh):

```
page_command = curl -s -d "{user} on node {node}: {reason}" https://ntfy.sh/my-bbs-pages
```

A caller pages at most once a minute.

**See also:** [Administration](Administration), [Security](Security)
