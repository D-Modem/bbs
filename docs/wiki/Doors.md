A door is a program, often a game, to which the BBS gives the line of
the caller. Text callers (telnet, modem, rlogin) open doors with *D* on
the main menu. This page tells about DOS doors, Linux doors, and doors
on other systems.

When a caller opens a door, the BBS does these steps:

1. It writes the drop files that doors read (DOOR.SYS, DORINFO1.DEF,
   DOOR32.SYS and CHAIN.TXT), in the directory of the node.
2. It runs the door.
3. It connects the caller to the door.

The door runs until one of these events occurs: the door ends, the
caller hangs up (this stops the door), the time limit of the door ends,
or the caller types nothing for the idle time. The BBS shows the CP437
output of a door as UTF-8 on UTF-8 terminals. The text that these
callers type goes back as CP437. A CP437 terminal (SyncTERM, NetRunner,
a modem caller) gets the bytes with no change. 3270 callers do not see
doors, because doors are ANSI programs.

## DOS doors

DOS doors run in dosemu2, in Docker. `doors/dosemu2` builds the image.
The image contains dosemu2 with its fdpp kernel and comcom64 shell,
built from source on Ubuntu (no PPA is necessary):

```
make doors-image                 ; docker build -t bbs-dosemu2 doors/dosemu2
```

In the container, the drive of the door is F: (`/doors`. Mount your
doors there). The drop files of the node are on G: (`/node`). The caller
is on COM1, behind the FOSSIL driver of dosemu2. DOS doors use this
driver. The `rundoor DOSDIR COMMAND` of the image loads the FOSSIL
driver, goes to the directory of the door and runs it. The door can show
text on its own screen (not on COM1). This text, and the messages of
dosemu2, go to `dosemu.out` in the directory of the node. Errors from
the command (from Docker) go to `door.log` in the same directory.

```
[door lord]
description = Legend of the Red Dragon
command = docker run --rm -i --network none --name bbs-door{node} --user {uid}:{gid} -v /srv/doors:/doors -v {dir}:/node bbs-dosemu2 'F:\LORD' 'START.BAT {node}'
dir = /var/lib/bbs/doors/node{node}
time_limit = 60
cleanup = docker rm -f bbs-door{node}
```

The BBS fills in these values:

- `{node}`.
- `{dir}` (the directory of the node).
- `{uid}` and `{gid}` (those of the BBS. Thus, the door writes the files
  of the node as the BBS).
- `{userid}`, `{minutes}`, `{rows}`, `{cols}` and `{baud}`.

Most doors get the location of the drop file on their command line or
in their own configuration. Here, the location is `G:\DOOR.SYS` (or
`G:\DORINFO1.DEF`, and other files). Each node has its own directory.
Thus, multi-node doors operate. `single = yes` lets only one caller at a
time into a door that is not multi-node.

dosemu2 runs the door under its CPU emulator, unless the host gives
`/dev/kvm` to the container (`--device /dev/kvm`, not tried yet). Under
the emulator, a door starts in about a second.

The BBS runs `docker` itself. Thus, its user must have the permission to
run it. That means the `docker` group, or rootless Docker or Podman
(`command = podman run ...`).

> **CAUTION: THE `docker` GROUP HAS THE SAME POWER AS ROOT.** Think
> carefully before you add the user of the BBS to it.

The container of the door has no network (`--network none`). It runs as
the user of the BBS, not as root. The BBS removes it when the door ends
or the caller hangs up.

We tested this with a FOSSIL door that we wrote for this purpose
(`doors/dosemu2/testdoor`). This door reads DOOR.SYS, writes CP437 and
ANSI, and echoes keys. We tested it through the full BBS: the caller
types, the door answers, and when the caller hangs up inside it, the
container stops.

## Linux doors

A Linux door is a program that uses a terminal on its standard input
and output. Set `pty = yes` for a program that must have a terminal
(most programs). Set `charset = utf-8` for a program that writes UTF-8.
DOOR32.SYS gives these doors `door32_comm = local`.

## Doors elsewhere

A door can also be on a different system. Then the BBS connects the line
of the caller to it over the network, not to a program here. This type
of door has a `type`, and no command or drop files. The BBS lists and
opens it as all other doors, with the same time limit, idle time,
`require`, `sysop_only` and character set (CP437 by default, translated
for UTF-8 terminals).

| `type` | What it is | Settings |
|---|---|---|
| `telnet` | a telnet door server | `address` (host:port) |
| `rlogin` | an RLogin door server (RFC 1282) | `address`. `client_user`, `server_user` (default `{user}`, the name of the caller), `terminal` (`ansi`) |
| `bbslink` | [BBSLink](http://www.bbslink.net) | `syscode`, `authcode`, `schemecode` (the codes that BBSLink gave you), `door` (the code of a door). `address` (`games.bbslink.net:23`), `url` (`http://games.bbslink.net`) |
| `doorparty` | [DoorParty](http://wiki.throwbackbbs.com/) | `ssh_user`, `ssh_password` (its SSH account), `tag` (your system tag), `door` (the code of a door. None: its menu), `host_key`. `address` (`dp.throwbackbbs.com:2022`), `rlogin_address` (`dp.throwbackbbs.com:513`) |

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

How the BBS connects to each type:

- **Telnet**: the BBS answers the negotiation of the server itself. It
  shows itself as a binary ANSI terminal with the size of the terminal
  of the caller (`TERMINAL-TYPE`, `NAWS`). It lets the server echo. None
  of this data goes to the caller.
- **RLogin**: the BBS sends the client and server user names and the
  terminal (with the speed of the line). Then it starts the session.
- **BBSLink**: the BBS gets a token from the web server of BBSLink. Then
  BBSLink authenticates the caller with your codes (sent as MD5s with the
  token). The BBS then connects by telnet. BBSLink knows the caller by
  the user number.
- **DoorParty**: the BBS opens an SSH tunnel with its account (no
  doorparty-connector is necessary). Then it connects through the tunnel
  by RLogin as `[TAG]name`, with the code of the door as the terminal
  type. Without `host_key`, the BBS does not check the key of the
  server. `bbs check` gives a warning about this. Each call logs the
  fingerprint of the key, so that you can add it.

**See also:** [Security](Security), [Future work](https://github.com/D-Modem/bbs/blob/HEAD/TODO.md)
