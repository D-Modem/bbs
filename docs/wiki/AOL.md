The BBS can be an AOL host for the Windows clients of AOL (3.0, 4.0 and
later) over TCP. A caller signs on in the client with the BBS name and
password. Then the BBS draws its Welcome window. These functions of AOL
connect to the BBS:

- People Connection is the chat of the BBS.
- Instant Messages are the private messages of the BBS. They go to and
  from callers on all other routes.
- Mail is BBS mail.
- Keywords go to each of these functions.

This is early work. We did not try it with a real client yet.

```ini
[aol]
address = :5190
```

| Setting | Default | What it sets |
|---|---|---|
| `address` | `:5190` | the address on which to listen |
| `max_clients` | `256` | the maximum number of clients at one time |
| `max_per_ip` | `4` | the maximum number of clients from one address |

The [AIM and ICQ](AIM-and-ICQ) server also uses port 5190. If `[oscar]`
is on the same address, the two servers share it. If a connection starts
as an AOL client connection, it goes to the AOL host. All other
connections stay with OSCAR.

## Pointing the client at the BBS

The TCP connection of the client (`TCP.CCL` in its folder) dials
`AmericaOnline.aol.com` on port 5190. Do one of these steps:

- Point that name at the BBS in the hosts file of the machine of the
  client (`C:\Windows\hosts` on Windows 95 and 98).
- Give the [DNS server](DNS) of the BBS to the machine.
- Change the name in `TCP.CCL`.

Then, in the Setup of the client, select "TCP/IP" as the connection.

## Signing on

The screen name and password on the Welcome window of the client are
those of a BBS account. "New User" does not make an account. Callers
must join by telnet or the web first. The client shows a failed sign-on.
It counts against the address, as failed logins do on all other routes.
The deny list and bans in `[security]` apply.

A client that signed on is a node, as all callers are. It shows in Who's
On as `AOL`. Sign Off in the client signs off and hangs up.

## Chat and Instant Messages

People Connection takes the client into the main chat room of the BBS,
in the room window of the client. The window lists the people in the
room. Lines come and go with callers in the same room on terminals, IRC,
AIM, ICQ and MSN. A line that starts with `/me ` is an action.

List Rooms in the room window (or the keyword Rooms) lists the chat
rooms of the BBS, with the number of people in each. A button goes into
a room. The keyword Room and a name, for example `room tea`, goes into
that room. If no one is in the room yet, the BBS makes it, as `/join`
does on other routes.

An Instant Message goes to a BBS user as a private message, on all
routes where the user is on (an IM in AIM, a PRIVMSG on IRC, a line on a
terminal). If the user is away, the user gets it at the next sign-on, as
on all other routes. The BBS tells the sender. Messages to the client
open its Instant Message window, one window for each person. The BBS
compares screen names without their spaces. Thus, `CarolAnn` is the user
Carol Ann (AOL screen names have no spaces).

## Mail

Compose Mail in the client sends BBS mail to each name in the address,
by screen name as for IMs. Formatted mail from 4.0 arrives as plain
text. The Welcome window shows the quantity of new mail. If new mail
arrives while the client is signed on, the client shows it in a message
box.

The keyword Mail opens the mailbox window of the BBS. The newest mail is
first, and new mail has the mark `*`. A button reads a mail and marks it
as read. The window of the mail can delete it.

The mail windows of the client also operate: Read New Mail on the
toolbar (`m7`), Old Mail (`m9`, mail that the user read) and Sent Mail
(`m8`). The BBS fills each one with a MIP list, newest first. When the
user opens a mail (`m3`), the BBS sends all of it and marks it as read.
Ignore marks a mail as read. Keep As New marks it as unread again.
Delete puts it in the BBS trash (`m2`). We found these answers from the
mail tool of the client. We did not try them with a real client yet.

## Message boards

The keyword Boards lists the message areas of the BBS that the caller
can read, with their new messages. A board lists its topics, with the
topic that has the latest post first. A `*` shows the topics with posts
that the caller did not read. A topic shows its posts in sequence and
marks them as read. Start a topic and Reply open a window with a subject
and the text. Send posts it, as from all other routes. A long topic shows
its first 30 posts. Read the other posts by telnet or the web.

## Keywords

The Keyword window of the client (Ctrl+K) accepts these keywords:

| Keyword | Goes to |
|---|---|
| `Chat` | People Connection |
| `Mail` | the mailbox |
| `Rooms` | the chat rooms |
| `Room` *name* | that chat room |
| `Boards` | the message boards |
| `Who` | the users who are on now, by all routes |
| `Help` | the list of keywords |

## How it is built

- `internal/aol/p3`: P3, the link layer of the client (packets,
  checksums, acknowledgements, resends).
- `internal/aol/fdo`: FDO91, the forms language that the client uses to
  draw windows. It has a compiler from FDO91 source. Thus, we write the
  windows of the host as source.
- `internal/aol`: the host, which processes each token that the client
  sends.
