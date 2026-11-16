[Multi Relay Chat](https://wiki.mysticbbs.com/doku.php?id=mrc) is a chat
network of BBSes. Mystic, Synchronet, ENiGMA½ and other BBSes share its
rooms. The BBS connects to it as its own MRC connector. One connection to
the MRC server carries all the users of the BBS. You do not have to
install a connector program.

The MRC rooms are the chat rooms of the BBS that start with the prefix
(`mrc-`). `mrc-lobby` is the MRC lobby. All users in that room on the
BBS are in the MRC lobby, with their names. This applies to users from a
terminal, a 3270, the web or IRC (`#mrc-lobby`).

```ini
[mrc]
address = mrc.bottomlessabyss.net:5001
tls = yes
web = https://bbs.example.org/
telnet = bbs.example.org:23
```

| Setting | Default | What it sets |
|---|---|---|
| `address` | `mrc.bottomlessabyss.net:5000` | the MRC server (5001 with TLS) |
| `tls` | `no` | TLS to the MRC server |
| `prefix` | `mrc-` | the start of the names of MRC rooms on this BBS |
| `system_name` | the name of the BBS | the name of the BBS that MRC shows (the `\|NN` colour codes of Mystic are permitted) |
| `web`, `telnet`, `ssh`, `sysop`, `description` | (the name of the sysop, the BBS) | the data about the BBS that the MRC `INFO` gives |
| `require` | (all users who can chat) | the users who can go into the MRC rooms ([access levels](Access-Levels)) |

## Chatting

- `/join mrc-lobby` in the chat of the BBS (`JOIN #mrc-lobby` on IRC)
  puts the user in the MRC lobby. Other users on the network see the
  user arrive. They also see the user leave.
- Lines that the user writes there go to MRC as `<name> text`. `/me`
  goes as an action. Lines from other systems show as
  `carol@Far_BBS: text` (on IRC, from `carol_Far_BBS!mrc@far_bbs`). The
  BBS removes the MRC colour codes.
- `!COMMAND` is a command for the MRC server, not a line for the room.
  Examples are `!motd`, `!list` (its rooms), `!chatters`, `!bbses`,
  `!info N` and other commands. The server tells you which commands it
  does not know. Its answers go only to the user who asked. `!!text` sends
  a line that starts with `!`.
- Private messages to a user on this BBS go only to that user.

On MRC, a user is in one room at a time. Thus, if a user is in two MRC
rooms on this BBS (an IRC client in two channels), the user is in one
room on MRC. This is the last room in which the user wrote.

Each minute, the BBS tells MRC that each user is present (`IAMHERE`). If
the connection stops, the BBS connects again after 30 seconds. Then it
puts back all the users who were there.

A sysop can make `mrc-lobby` a [permanent room](IRC#permanent-rooms).
Then the BBS lists the room when it has no users on this BBS.

## The protocol

The BBS uses the MRC protocol in the same way as the `mrc-connector.js` and `mrc-session.js` of Synchronet:

- The handshake `SYSTEM~BBSSEC/os_arch/1.3.5`, then the hello of the server.
- Lines of `FromUser~FromSite~FromRoom~ToUser~MsgExt~ToRoom~Body~`.
- `INFO*`, `IMALIVE` and `CAPABILITIES` for the BBS.
- `IMALIVE` as the answer to the `PING` of the server.
- `IAMHERE` for each user, `NEWROOM:old:new` to move, `LOGOFF` to leave.
- Names without spaces (underscores), with a maximum of 30 characters.

**See also:** [Chat and paging](Chat-and-Paging), [IRC](IRC)
