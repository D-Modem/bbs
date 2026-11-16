Sysop tools is the menu of the sysop inside the BBS. To open it, push `*`
on the main menu of a sysop (a custom menu can also have a `sysop` item).
It operates on all terminals. But to watch a node, you must have a text
terminal.

| Tool | Key | What it does |
|---|---|---|
| watch a node | `W` | shows on your screen the data that the BBS sends to a text caller, at the same time: their menus, their door, the echo of their keys. A key stops it. It also stops when the caller leaves. The screen size and character set are those of the caller |
| chat with a node | `C` | at the next menu, the BBS tells the caller that you want to talk. Then it takes the caller into a private chat with you (the room of their page, `sysop-N`). You wait there |
| kick a node | `K` | disconnects a caller, after a yes |
| users | `U` | shows the level, flags and state of all users. For one user, you can set: the level, the flags (`+AB -C` adds and removes), lock or unlock, sysop rights given or removed, a new password. You cannot change your own lock or rights |
| statistics | `S` | shows totals (users, messages, mail, files, and logins, posts, pages and failed logins since the start). It also shows calls, new users, posts and uploads for each of the last 14 days, as bars |
| hack log and bans | `H` | shows the addresses that are banned now (a number removes its ban). `L` shows the last 100 lines of the [hack log](Security#keeping-callers-out) |
| scheduled events | `E` | shows the events, their next run and the result of their last run. For one event, it shows the command and the output. `R` runs it now |
| configuration | `F` | the setup screens (blue windows and pull-down menus on ANSI terminals): the board, the system, connections, internet and online services, messengers, e-mail, networks, time, users and groups, maintenance. Nothing changes until F10 saves (F9 quits). Then the BBS applies immediately the settings that can change while callers are on ([Configuration](Configuration#editing-it-the-configuration-screens)). This tool shows only when the BBS runs with `-config` |
| available | `A` | sets if pages get to you. If you are not available, the BBS tells a caller who pages that you are not available. The caller can leave a message for you |

The validation of new users stays on the main menu (`V`, when new users
wait).

## Scheduled events

```
; bbs.conf
; every day, at this local time
[event nodelist]
command = /usr/local/bin/get-nodelist --to "/var/lib/bbs/fido/nodelist"
at = 03:30

; every 60 minutes, for at most 300 seconds (default 600)
[event fidopoll]
command = /usr/local/bin/bbs fido poll
every = 60
timeout = 300
```

A scheduled event is a program that the BBS runs for you. It runs `at` a
time each day, or `every` a number of minutes. The first run comes that
number of minutes after the BBS starts. The command is a program and its
arguments, with spaces between them. `"..."` or `'...'` keep spaces
inside one argument.

The BBS runs the command without a shell, with `BBS_EVENT` set to the
name of the event. Only one event runs at a time. If a run continues
after `timeout`, the BBS stops it.

The BBS logs each run. It keeps the result and the output (the last 4 KB)
of the last run for the sysop tools and the API. If `bbs check` cannot
find a program, it gives a warning.

## The API

| Request | What it does |
|---|---|
| `GET /api/v1/admin/events` | gives the events, and their next and last runs |
| `POST /api/v1/admin/events/{name}/run` | runs one event now, and waits for it |
| `PUT /api/v1/admin/available` | `{"available": false}`: pages go to no one |
| `GET /api/v1/admin/bans`, `DELETE /api/v1/admin/bans/{ip}`, `GET /api/v1/admin/hacklog` | bans and the hack log |
| `POST /api/v1/admin/nodes/{n}/kick` | disconnects a caller |

**See also:** [Administration](Administration), [Security](Security), [Chat and paging](Chat-and-Paging)
