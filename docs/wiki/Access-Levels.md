Every user has an **access level** (0 to 255) and **flags** (letters A to
Z). The sysop decides what each level may do, and what level and flags
each message area, file area, door and menu requires. Sysops pass every
check.

None of this is required. If you set nothing up, every user's level is 20
and nothing requires more.

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

A user gets the settings of the highest `[level]` at or below their own
level, so in the example above, level 35 gets `[level 20]`'s settings. A
level below every `[level]` section has no limits. Each `[level]` can set:

| Key | Default | What it sets |
|---|---|---|
| `name` | `Level N` | shown in the profile and to the API |
| `time` | `0` | minutes online a day; the caller is warned at 5 and 1 minutes left and hung up at the end (and gets at least a minute of a call, to read and log off). A door's time limit is cut to what is left |
| `downloads` | `0` | files a day, by any way (the BBS, FTP, 3270, the API) |
| `ratio` | `0` | files a user may download for each they have uploaded (0: no ratio); the files uploaded are those still in the areas |
| `ratio_free` | `0` | files a user may download before the ratio counts: with `ratio = 3` and `ratio_free = 10`, someone with 2 uploads may download 16 |
| `post` | `yes` | post in message areas |
| `mail` | `yes` | mail other users (and netmail); without it, only the sysop |
| `chat` | `yes` | chat rooms (a page's private room is the page's) |
| `page` | `yes` | page the sysop |
| `download`, `upload` | `yes` | files |
| `doors` | `yes` | doors |

Days follow the server's local time, so time and download allowances
start again at midnight.

## Requirements

A requirement for an area, door or menu is written as a list of terms,
and the user must meet all of them:

| Term | Meaning |
|---|---|
| `30` | level 30 or higher |
| `F`, `FB` | has flag F (and B) |
| `!X` | does not have flag X |
| `sysop` | sysops only |

So `30 F !X` means level 30 or more, with flag F and without flag X. An
empty requirement lets in anyone who is logged in. A locked user meets no
requirement.

| Where | How | Notes |
|---|---|---|
| message area | `bbs area require AREA READ POST` | who sees it (in lists, newsgroups by NNTP, the API) and who posts |
| file area | `bbs filearea require AREA DOWNLOAD UPLOAD` | an area is listed to those who may do either |
| door | `require = 30 L` in its `[door]` | |
| menu | `require = 50` in its `[menu]` | to be taken into it |
| menu item | `x = menu:extras Extras \| 50 X` | the item is shown only to those who meet what follows `\|` |

On the command line, `-` means no requirement: `bbs area require General - 20`.

## Validation

New users start at `new_user_level`. Their welcome tells them they are
waiting for the sysop to validate them, and they can mail the sysop even
if their level cannot send mail. The sysop validates them in any of three
ways:

- on the BBS: `V` on the main menu (*Validate users (N)*) goes through
  those waiting: validate, set another level, lock, skip;
- by the command: `bbs user waiting`, `bbs user validate NAME`,
  `bbs user level NAME N`, `bbs user flags NAME +AB -C`;
- by the API: `PATCH /api/v1/admin/users/{name}` with `level` and
  `flags` (`"AB"`, or an edit `"+A -B"`).

Users can see their own level, flags, time left and downloads in their
profile (and in `access` at `GET /api/v1/me`), but not anyone else's. No
one can change their own.

Drop files give doors the caller's level as their security level (255 for
sysops).

**See also:** [Administration](Administration), [Configuration](Configuration), [Security](Security)
