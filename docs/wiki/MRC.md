# MRC (Multi Relay Chat)

[Multi Relay Chat](https://wiki.mysticbbs.com/doku.php?id=mrc) is a chat
network of BBSes: Mystic, Synchronet, ENiGMA½ and others share its rooms.
The BBS joins it as its own MRC connector, with one connection to the MRC
server carrying all its users. No connector program is needed.

MRC's rooms are the BBS's chat rooms that begin with the prefix (`mrc-`):
`mrc-lobby` is MRC's lobby. Whoever is in that room on the BBS, from a
terminal, a 3270, the web or IRC (`#mrc-lobby`), is in MRC's lobby, under
their name.

```ini
[mrc]
address = mrc.bottomlessabyss.net:5001
tls = yes
web = https://bbs.example.org/
telnet = bbs.example.org:23
```

| setting | default | |
|---|---|---|
| `address` | `mrc.bottomlessabyss.net:5000` | the MRC server (5001 with TLS) |
| `tls` | `no` | TLS to it |
| `prefix` | `mrc-` | what begins MRC's rooms here |
| `system_name` | the BBS's name | the BBS as MRC shows it (Mystic's `\|NN` colors allowed) |
| `web`, `telnet`, `ssh`, `sysop`, `description` | (the sysop's name, the BBS's) | what MRC's `INFO` tells of the BBS |
| `require` | (anyone who may chat) | who may go into MRC's rooms ([access levels](Access-Levels)) |

## Chatting

- `/join mrc-lobby` in the BBS's chat (`JOIN #mrc-lobby` on IRC) goes into
  MRC's lobby. Others on the network see the user arrive, and see them
  leave when they go.
- Lines said there go to MRC as `<name> text`, and `/me` as an action.
  Lines from other systems show as `carol@Far_BBS: text` (on IRC, from
  `carol_Far_BBS!mrc@far_bbs`). MRC's colors are taken out.
- `!COMMAND` is a command for the MRC server, not a line for the room:
  `!motd`, `!list` (its rooms), `!chatters`, `!bbses`, `!info N`
  and the rest (the server says which it does not know). Its answers come to whoever asked. `!!text`
  says a line that begins with `!`.
- Private messages to a user here come to them alone.

A user in two of MRC's rooms here (an IRC client in two channels) is in
one on MRC, the last they spoke in. MRC has a user in one room at a time.

Each user is kept present (`IAMHERE`) every minute. If the connection
drops, the BBS connects again after 30 seconds and puts back everyone
who was there.

A sysop can make `mrc-lobby` a [permanent room](IRC#permanent-rooms), so
that it is listed even when no one here is in it.

## The protocol

As Synchronet's `mrc-connector.js` and `mrc-session.js` speak it:

- the handshake `SYSTEM~BBSSEC/os_arch/1.3.5`, then the server's hello;
- lines of `FromUser~FromSite~FromRoom~ToUser~MsgExt~ToRoom~Body~`;
- `INFO*`, `IMALIVE` and `CAPABILITIES` for the BBS;
- `IMALIVE` in answer to the server's `PING`;
- `IAMHERE` for each user, `NEWROOM:old:new` to move, `LOGOFF` to go;
- names without spaces (underscores), at most 30 characters.
