A sysop's main menu has `*`, *Sysop tools* (and a custom menu may have a
`sysop` item).  It works on any terminal; watching a node needs a text
terminal.

| | key | |
|---|---|---|
| watch a node | `W` | what a text caller is sent, as it is sent, on your screen: their menus, their door, their typing echoed.  A key stops; their leaving ends it.  Their screen size and character set are theirs |
| chat with a node | `C` | the caller is told you would like a word, and taken into a private chat with you at their next menu (their page's room, `sysop-N`); you wait there |
| kick a node | `K` | throw a caller off, after a yes |
| users | `U` | every user's level, flags and state; for one: their level, flags (`+AB -C` adds and takes), lock or unlock, sysop rights given or taken, a new password.  Not your own lock or rights |
| statistics | `S` | totals (users, messages, mail, files; logins, posts, pages and failed logins since the start), and calls, new users, posts and uploads for each of the last 14 days, as bars |
| hack log and bans | `H` | the addresses banned now (a number lifts its ban), and `L` the [hack log](Security#keeping-callers-out)'s last 100 lines |
| scheduled events | `E` | the events, when they run next and how their last run went; one's command and output, and `R` runs it now |
| available | `A` | whether pages reach you; when not, a caller who pages is told you are not available and may leave you a message |

Validating new users stays on the main menu (`V`, when some wait).

## Scheduled events

```
; bbs.conf
[event backup]
command = /usr/local/bin/bbs-backup --to "/var/backups/bbs"
at = 03:30          ; every day, at this local time

[event fidopoll]
command = /usr/local/bin/bbs fido poll
every = 60          ; minutes
timeout = 300       ; seconds it may take (default 600)
```

An event is a program and its arguments (words parted by spaces; `"..."`
or `'...'` keep spaces in one), run without a shell, with `BBS_EVENT` set
to its name, at most one at a time.  It runs `at` a time each day or
`every` so many minutes (the first after that many minutes from the
start).  A run that goes past `timeout` is stopped.  The log has each
run; the last run's end and output (its last 4 KB) are kept for the
sysop tools and the API.  `bbs check` warns of a program it cannot find.

## The API

| | |
|---|---|
| `GET /api/v1/admin/events` | the events, their next and last runs |
| `POST /api/v1/admin/events/{name}/run` | run one now, and wait for it |
| `PUT /api/v1/admin/available` | `{"available": false}`: pages reach no one |
| `GET /api/v1/admin/bans`, `DELETE /api/v1/admin/bans/{ip}`, `GET /api/v1/admin/hacklog` | bans and the hack log |
| `POST /api/v1/admin/nodes/{n}/kick` | throw a caller off |

**See also:** [Administration](Administration), [Security](Security), [Chat and paging](Chat-and-Paging)
