The BBS can be an AOL host for AOL's own Windows clients (3.0, 4.0 and
later) over TCP. A caller signs on in the client with their BBS name and
password, and the BBS draws its Welcome window. People Connection is the
BBS's chat, and Instant Messages are its private messages, with callers on
every other route, mail is BBS mail, and keywords go to each of these.
This is early work, not yet tried with a real client.

```ini
[aol]
address = :5190
```

| Setting | Default | What it sets |
|---|---|---|
| `address` | `:5190` | where to listen |
| `max_clients` | `256` | clients at once |
| `max_per_ip` | `4` | from one address |

Port 5190 is also the [AIM and ICQ](AIM-and-ICQ) server's. With `[oscar]`
on the same address, the two share it: a connection that begins as an AOL
client's goes to the AOL host, and everything else stays with OSCAR.

## Pointing the client at the BBS

The client's TCP connection (`TCP.CCL` in its folder) dials
`AmericaOnline.aol.com` on port 5190. Either point that name at the BBS in
the hosts file of the client's machine
(`C:\Windows\hosts` on Windows 95 and 98), or change the name in `TCP.CCL`.
Then pick "TCP/IP" as the connection in the client's Setup.

## Signing on

The screen name and password on the client's Welcome window are a BBS
account's. "New User" does not make an account; callers join by telnet or
the web first. A failed sign-on is shown in the client, and counts against
the address as failed logins do everywhere else. The deny list and bans in
`[security]` apply.

A signed-on client is a node, as any caller is, and appears in Who's On
as `AOL`. Sign Off in the client signs off and hangs up.

## Chat and Instant Messages

People Connection takes the client into the BBS's main chat room, in the
client's own room window: the people there are listed, and lines come and
go with callers on terminals, IRC, AIM, ICQ and MSN in the same room. A
line beginning `/me ` is an action.

The room window's List Rooms (or the keyword Rooms) lists the BBS's chat
rooms with how many are in each, and a button goes into one. The keyword
Room and a name, such as `room tea`, goes into that room, making it if no
one is there yet, as `/join` does elsewhere.

An Instant Message goes to a BBS user wherever they are on, as a private
message (an IM in AIM, a PRIVMSG on IRC, a line on a terminal). One away
has it when they next sign on, as on every other route, and the sender is
told. Messages to the client open its own Instant Message window, one for
each person. Screen names are compared without their spaces, so
`CarolAnn` is the user Carol Ann (AOL's screen names have no spaces).

## Mail

Compose Mail in the client sends BBS mail to each name it is addressed
to, by screen name as for IMs. 4.0's formatted mail arrives as plain text.
The Welcome window says how much new mail is waiting, and new mail while
signed on is shown in a message box.

The keyword Mail opens the BBS's mailbox window, newest first with new
mail marked `*`. A button reads a mail, which marks it read, and the mail's
window can delete it.

The client's own mail windows work too: Read New Mail on the toolbar
(`m7`), Old Mail (`m9`, mail already read) and Sent Mail (`m8`). Each is
filled with a MIP list, newest first. Opening a mail (`m3`) sends it
whole and marks it read. Ignore marks a mail read, Keep As New marks it
unread again, and Delete puts it in the BBS trash (`m2`). These answers
were worked out from the client's mail tool and have not yet been tried
with a real client.

## Message boards

The keyword Boards lists the BBS's message areas the caller may read,
with their new messages. A board lists its topics, the one written in
last first, with `*` on those with posts not yet read. A topic shows its
posts in order and marks them read. Start a topic and Reply open a window
with a subject and the text, and Send posts it, as from any other route.
A long topic shows its first 30 posts; the rest are read by telnet or the
web.

## Keywords

The client's Keyword window (Ctrl+K) takes:

| Keyword | Goes to |
|---|---|
| `Chat` | People Connection |
| `Mail` | the mailbox |
| `Rooms` | the chat rooms |
| `Room` *name* | that chat room |
| `Boards` | the message boards |
| `Who` | who is on now, by any route |
| `Help` | the list of keywords |

## How it is built

- `internal/aol/p3`: P3, the client's link layer (packets, checksums,
  acknowledgements, resends).
- `internal/aol/fdo`: FDO91, the forms language the client draws windows
  from. It has a compiler from FDO91 source, so the host's windows are
  written as source.
- `internal/aol`: the host, which handles each token the client sends.
