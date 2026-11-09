The BBS can be an AOL host for AOL's own Windows clients (3.0, 4.0 and
later) over TCP. A caller signs on in the client with their BBS name and
password, and the BBS draws its Welcome window. People Connection is the
BBS's chat, and Instant Messages are its private messages, with callers on
every other route. This is early work: mail and keywords are still to come.

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

An Instant Message goes to a BBS user wherever they are on, as a private
message (an IM in AIM, a PRIVMSG on IRC, a line on a terminal). One away
has it when they next sign on, as on every other route, and the sender is
told. Messages to the client open its own Instant Message window, one for
each person. Screen names are compared without their spaces, so
`CarolAnn` is the user Carol Ann (AOL's screen names have no spaces).

## How it is built

- `internal/aol/p3`: P3, the client's link layer (packets, checksums,
  acknowledgements, resends).
- `internal/aol/fdo`: FDO91, the forms language the client draws windows
  from. It has a compiler from FDO91 source, so the host's windows are
  written as source.
- `internal/aol`: the host, which handles each token the client sends.
