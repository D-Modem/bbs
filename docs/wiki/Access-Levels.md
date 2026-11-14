Each user has an **access level** (0 to 255) and **flags** (letters A to
Z). The sysop sets what each level can do. The sysop also sets the level
and flags that each message area, file area, door and menu requires.
Sysops pass all checks.

This configuration is optional. If you set nothing, the level of all
users is 20, and nothing requires more.

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

A user gets the settings of the highest `[level]` that is at or below the
level of the user. Thus, in the example above, level 35 gets the settings
of `[level 20]`. A level below all `[level]` sections has no limits. Each
`[level]` can set:

| Key | Default | What it sets |
|---|---|---|
| `name` | `Level N` | the name that the profile and the API show |
| `time` | `0` | minutes online each day. The BBS gives a warning to the caller when 5 and 1 minutes remain. At the end, it hangs up (the caller gets a minimum of one minute of a call, to read and log out). The BBS decreases the time limit of a door to the time that remains |
| `downloads` | `0` | files each day, by all methods (the BBS, FTP, 3270, the API) |
| `ratio` | `0` | the number of files that a user can download for each file that the user uploaded (0: no ratio). The uploaded files are those that are still in the areas |
| `ratio_free` | `0` | the number of files that a user can download before the ratio starts. With `ratio = 3` and `ratio_free = 10`, a user with 2 uploads can download 16 |
| `post` | `yes` | post in message areas |
| `mail` | `yes` | send mail to other users (and netmail). Without it, the user can send mail only to the sysop |
| `chat` | `yes` | chat rooms (the private room of a page follows the page) |
| `page` | `yes` | page the sysop |
| `download`, `upload` | `yes` | files |
| `doors` | `yes` | doors |

Days follow the local time of the server. Thus, the time and download
allowances start again at midnight.

## Requirements

A requirement for an area, door or menu is a list of terms. The user
must meet all of them:

| Term | Meaning |
|---|---|
| `30` | level 30 or higher |
| `F`, `FB` | has flag F (and B) |
| `!X` | does not have flag X |
| `sysop` | sysops only |

Thus, `30 F !X` means level 30 or more, with flag F and without flag X.
An empty requirement lets all logged-in users in. A locked user meets no
requirement.

| Where | How | Notes |
|---|---|---|
| message area | `bbs area require AREA READ POST` | the users who see it (in lists, in newsgroups through NNTP, in the API) and the users who post |
| file area | `bbs filearea require AREA DOWNLOAD UPLOAD` | the BBS lists an area to the users who can do one of the two |
| door | `require = 30 L` in its `[door]` | |
| menu | `require = 50` in its `[menu]` | to go into it |
| menu item | `x = menu:extras Extras \| 50 X` | the BBS shows the item only to the users who meet the terms after `\|` |

On the command line, `-` means no requirement: `bbs area require General - 20`.

## Validation

New users start at `new_user_level`. Their welcome tells them that they
wait for the sysop to validate them. They can send mail to the sysop,
also if their level cannot send mail. The sysop validates them in one of
three ways:

- On the BBS: `V` on the main menu (*Validate users (N)*) shows the
  users who wait, one at a time. For each, you can validate, set a
  different level, lock or skip.
- With the commands: `bbs user waiting`, `bbs user validate NAME`,
  `bbs user level NAME N`, `bbs user flags NAME +AB -C`.
- With the API: `PATCH /api/v1/admin/users/{name}` with `level` and
  `flags` (`"AB"`, or an edit `"+A -B"`).

Users can see their own level, flags, time that remains and downloads in
their profile (and in `access` at `GET /api/v1/me`). They cannot see the
values of other users. Users cannot change their own values.

Drop files give the level of the caller to doors as the security level
(255 for sysops).

**See also:** [Administration](Administration), [Configuration](Configuration), [Security](Security)
