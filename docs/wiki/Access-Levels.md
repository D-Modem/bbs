Every user has an **access level** (0 to 255) and **flags** (letters A to
Z).  The sysop says what each level may do, and what message areas, file
areas, doors and menus require.  Sysops pass every check.

Without any of it nothing changes: a user's level is 20 and nothing
requires more.

## Levels

```ini
[general]
new_user_level = 10      ; a new account's level
validated_level = 20     ; what validating a user raises them to

[level 10]
name = New user
time = 30                ; minutes online a day (0: no limit)
downloads = 2            ; files a day (0: no limit)
post = no
mail = no                ; still to the sysop: to ask for validation
chat = no

[level 20]
name = Member
time = 120
downloads = 20

[level 90]
name = Trusted
```

A user's level gets what the `[level]` at or below it says: level 35 above
gets `[level 20]`'s.  A level under every `[level]` has no limits.  Each
`[level]` may say:

| | default | |
|---|---|---|
| `name` | `Level N` | shown in the profile and to the API |
| `time` | `0` | minutes online a day; the caller is warned at 5 and 1 minutes left and hung up at the end (and gets at least a minute of a call, to read and log off).  A door's time limit is cut to what is left |
| `downloads` | `0` | files a day, by any way (the BBS, FTP, 3270, the API) |
| `post` | `yes` | post in message areas |
| `mail` | `yes` | mail other users (and netmail); without it, only the sysop |
| `chat` | `yes` | chat rooms (a page's private room is the page's) |
| `page` | `yes` | page the sysop |
| `download`, `upload` | `yes` | files |
| `doors` | `yes` | doors |

The day is the server's local day; time and downloads start again at
midnight.

## Requirements

What an area, a door or a menu requires is written as terms that must all
hold:

| term | |
|---|---|
| `30` | level 30 or higher |
| `F`, `FB` | has flag F (and B) |
| `!X` | does not have flag X |
| `sysop` | sysops only |

`30 F !X` is level 30 or more, with F, without X.  Nothing is anyone
logged in.  A locked user meets none.

| where | how | |
|---|---|---|
| message area | `bbs area require AREA READ POST` | who sees it (in lists, newsgroups by NNTP, the API) and who posts |
| file area | `bbs filearea require AREA DOWNLOAD UPLOAD` | an area is listed to those who may do either |
| door | `require = 30 L` in its `[door]` | |
| menu | `require = 50` in its `[menu]` | to be taken into it |
| menu item | `x = menu:extras Extras \| 50 X` | the item is shown only to those who meet what follows `\|` |

`-` on the command line is none: `bbs area require General - 20`.

## Validation

New users start at `new_user_level`; their welcome says they wait for the
sysop's validation, and they may mail the sysop even when their level may
not mail.  The sysop validates them:

- on the BBS: `V` on the main menu (*Validate users (N)*) goes through
  those waiting: validate, set another level, lock, skip;
- by the command: `bbs user waiting`, `bbs user validate NAME`,
  `bbs user level NAME N`, `bbs user flags NAME +AB -C`;
- by the API: `PATCH /api/v1/admin/users/{name}` with `level` and
  `flags` (`"AB"`, or an edit `"+A -B"`).

Users see their own level, flags, time left and downloads in their
profile (and at `GET /api/v1/me`, `access`); others see neither.  No one
changes their own.

Drop files give doors the caller's level as their security level (255 for
sysops).

**See also:** [Administration](Administration), [Configuration](Configuration), [Security](Security)
