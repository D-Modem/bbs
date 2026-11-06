A door is a program, often a game, that the BBS hands the caller's line
to. Text callers (telnet, modem, rlogin) open doors with *D* on the main
menu. This page covers DOS doors, Linux doors, and doors on other
systems.

When a caller opens a door, the BBS writes the drop files that doors read
(DOOR.SYS, DORINFO1.DEF, DOOR32.SYS and CHAIN.TXT, in the node's own
directory), runs the door, and connects the caller to it. The door runs
until it ends, the caller hangs up (which stops the door), the door's time
limit runs out, or the caller types nothing for the idle time. A door's
CP437 output is shown as UTF-8 on UTF-8 terminals, and what those callers
type goes back as CP437. A CP437 terminal (SyncTERM, NetRunner, a modem
caller) gets the bytes unchanged. 3270 callers do not see doors, because
doors are ANSI programs.

## DOS doors

DOS doors run in dosemu2, in Docker. `doors/dosemu2` builds the image:
dosemu2 with its fdpp kernel and comcom64 shell, built from source on
Ubuntu (no PPA needed):

```
make doors-image                 ; docker build -t bbs-dosemu2 doors/dosemu2
```

In the container, the door's drive is F: (`/doors`; mount your doors
there) and the node's drop files are on G: (`/node`). The caller is on
COM1, behind dosemu2's own FOSSIL driver, which is what DOS doors talk to.
The image's `rundoor DOSDIR COMMAND` loads the FOSSIL driver, changes to
the door's directory and runs it. Anything the door shows on its own
screen (rather than COM1), and dosemu2's messages, go to `dosemu.out` in
the node's directory; errors from the command itself (Docker's) go to
`door.log` there.

```
[door lord]
description = Legend of the Red Dragon
command = docker run --rm -i --network none --name bbs-door{node} --user {uid}:{gid} -v /srv/doors:/doors -v {dir}:/node bbs-dosemu2 'F:\LORD' 'START.BAT {node}'
dir = /var/lib/bbs/doors/node{node}
time_limit = 60
cleanup = docker rm -f bbs-door{node}
```

The BBS fills in `{node}`, `{dir}` (the node's directory), `{uid}` and
`{gid}` (the BBS's own, so the door writes the node's files as the BBS),
`{userid}`, `{minutes}`, `{rows}`, `{cols}` and `{baud}`. Most doors are
told where the drop file is on their command line or in their own
configuration; here that is `G:\DOOR.SYS` (or `G:\DORINFO1.DEF`, and so
on). Each node has its own directory, so multi-node doors work, and
`single = yes` lets only one caller at a time into a door that is not
multi-node. dosemu2 runs the door under its CPU emulator unless the host
gives the container `/dev/kvm` (`--device /dev/kvm`, not yet tried). Under
the emulator, a door starts in about a second.

The BBS runs `docker` itself, so its user must be allowed to. That means
the `docker` group (which is as powerful as root, so think twice), or
rootless Docker or Podman (`command = podman run ...`). The door's
container has no network (`--network none`), runs as the BBS's user rather
than root, and is removed when the door ends or the caller hangs up.

This was tested with a FOSSIL door written for the purpose
(`doors/dosemu2/testdoor`, which reads DOOR.SYS, writes CP437 and ANSI,
and echoes keys) through the whole BBS: the caller types, the door
answers, and when the caller hangs up inside it, the container goes.

## Linux doors

A Linux door is any program that talks to a terminal on its standard
input and output. Set `pty = yes` for a program that wants a terminal
(most do), and `charset = utf-8` for one that writes UTF-8. DOOR32.SYS
tells these doors `door32_comm = local`.

## Doors elsewhere

A door can also be on another system, with the caller's line connected to
it over the network instead of to a program here. Such a door has a
`type` and no command or drop files. It is listed and opened like any
other door, with the same time limit, idle time, `require`, `sysop_only`
and character set (CP437 by default, translated for UTF-8 terminals).

| `type` | What it is | Settings |
|---|---|---|
| `telnet` | a telnet door server | `address` (host:port) |
| `rlogin` | an RLogin door server (RFC 1282) | `address`; `client_user`, `server_user` (default `{user}`, the caller's name), `terminal` (`ansi`) |
| `bbslink` | [BBSLink](http://www.bbslink.net) | `syscode`, `authcode`, `schemecode` (the codes BBSLink gave you), `door` (a door's code); `address` (`games.bbslink.net:23`), `url` (`http://games.bbslink.net`) |
| `doorparty` | [DoorParty](http://wiki.throwbackbbs.com/) | `ssh_user`, `ssh_password` (its SSH account), `tag` (your system tag), `door` (a door's code; none: its menu), `host_key`; `address` (`dp.throwbackbbs.com:2022`), `rlogin_address` (`dp.throwbackbbs.com:513`) |

```ini
[door bbslink-lord]
description = LORD on BBSLink
type = bbslink
syscode = XXXXXX
authcode = XXXXXX
schemecode = XXXXXX
door = lord

[door doorparty]
description = DoorParty
type = doorparty
ssh_user = myuser
ssh_password = secret
tag = XB
host_key = SHA256:...
```

How the BBS connects to each:

- **Telnet**: the BBS answers the server's negotiation itself, presenting
  a binary ANSI terminal of the caller's size (`TERMINAL-TYPE`, `NAWS`),
  and lets the server echo. None of this reaches the caller.
- **RLogin**: the BBS sends the client and server user names and the
  terminal (with the line's speed), then starts the session.
- **BBSLink**: the BBS gets a token from BBSLink's web server, which then
  authenticates the caller with your codes (sent as MD5s with the token),
  and connects by telnet. BBSLink knows the caller by their user number.
- **DoorParty**: the BBS opens an SSH tunnel with its account (no
  doorparty-connector is needed), then connects through it by RLogin as
  `[TAG]name`, with the door's code as the terminal type. Without
  `host_key`, the server's key is not checked; `bbs check` warns about
  this, and each call logs the key's fingerprint so you can add it.

**See also:** [Security](Security), [Future work](https://github.com/D-Modem/bbs/blob/HEAD/TODO.md)
