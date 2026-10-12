Text callers (telnet, modem, rlogin) open doors with *D* on the main menu.
A door is a program the BBS hands the caller's line to: it writes the drop
files doors read (DOOR.SYS, DORINFO1.DEF, DOOR32.SYS and CHAIN.TXT, in the
node's own directory), runs the door, and joins the two until the door
ends, the caller hangs up (the door is stopped), the door's time limit
runs out, or the caller types nothing for the idle time.  A door's output
in CP437 is shown as UTF-8 to UTF-8 terminals, and what they type goes
back as CP437; a CP437 terminal (SyncTERM, NetRunner, a modem caller) gets
the bytes as they are.  3270 callers do not see doors: doors are ANSI.

**DOS doors** run in dosemu2, in Docker.  `doors/dosemu2` builds the image:
dosemu2 with its fdpp kernel and comcom64 shell, built from source on
Ubuntu (no PPA needed):

```
make doors-image                 ; docker build -t bbs-dosemu2 doors/dosemu2
```

In the container the door's drive is F: (`/doors`, mount your doors there)
and the node's drop files are on G: (`/node`); the caller is on COM1 behind
dosemu2's own FOSSIL driver, which is what DOS doors talk to.  The image's
`rundoor DOSDIR COMMAND` loads the FOSSIL, goes to the door's directory and
runs it; what the door shows on its own screen (not COM1) and dosemu2's
messages go to `dosemu.out` in the node's directory, and the command's
own errors (Docker's) to `door.log` there.

```
[door lord]
description = Legend of the Red Dragon
command = docker run --rm -i --network none --name bbs-door{node} --user {uid}:{gid} -v /srv/doors:/doors -v {dir}:/node bbs-dosemu2 'F:\LORD' 'START.BAT {node}'
dir = /var/lib/bbs/doors/node{node}
time_limit = 60
cleanup = docker rm -f bbs-door{node}
```

`{node}`, `{dir}` (the node's directory), `{uid}` and `{gid}` (the BBS's
own: the door writes the node's files as the BBS), `{userid}`,
`{minutes}`, `{rows}`, `{cols}` and `{baud}` are filled in.  Most doors are
told where the drop file is on their command line or in their own
configuration: here that is `G:\DOOR.SYS` (or `G:\DORINFO1.DEF`, ...).
Each node has its own directory, so multi-node doors work; `single = yes`
lets one caller at a time into a door that is not.  dosemu2 runs the door
under its CPU emulator unless the host offers `/dev/kvm` to the container
(`--device /dev/kvm`, untried here); a door starts in about a second
under the emulator.

**Linux doors**: any program that talks to a terminal on its standard
input and output, with `pty = yes` for one that wants a terminal (most
do), and `charset = utf-8` for one that writes UTF-8.  DOOR32.SYS says
`door32_comm = local` to them.

The BBS runs `docker` itself, so its user must be allowed to: the
`docker` group (which is as good as root: think twice), or rootless Docker
or Podman (`command = podman run ...`).  The door's container has no
network (`--network none`), runs as the BBS's user, not root, and is
removed when the door ends or the caller hangs up.

It was tried with a FOSSIL door of its own (`doors/dosemu2/testdoor`,
which reads DOOR.SYS, writes CP437 and ANSI and echoes keys) through the
whole BBS: the caller types, the door answers, the caller hangs up in it
and the container goes.

## Doors elsewhere

A door can be on another system: the caller's line is joined to it over
the network rather than to a program here. Such a door has a `type` and
no command or drop files. It is shown and opened like any other, with
the same time limit, idle time, `require`, `sysop_only` and character
set (CP437 by default, translated for UTF-8 terminals).

| `type` | | settings |
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

How each gets in:

- **Telnet**: the server's negotiation is answered here: the BBS is a
  binary ANSI terminal of the caller's size (`TERMINAL-TYPE`, `NAWS`), and
  the server may echo. None of it reaches the caller.
- **RLogin**: the client and server user names and the terminal (with the
  line's speed), then the session.
- **BBSLink**: a token from BBSLink's web server, which then authenticates
  the caller with your codes (as MD5s with the token), then telnet. It
  knows the caller by their user number.
- **DoorParty**: an SSH tunnel with its account, no
  doorparty-connector needed, then RLogin through it as `[TAG]name`, the
  door's code as the terminal type. Without `host_key`, the server's key
  isn't checked: `bbs check` warns, and each call logs the key's
  fingerprint to put there.

**See also:** [Security](Security), [Future work](https://github.com/Neo-Desktop/bbs-sec/blob/HEAD/TODO.md)
