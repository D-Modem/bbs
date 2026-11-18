The sysop can change the appearance of the BBS: their own screens (ANSI
art, text and 3270 screens in colour), their own menus, and the words of
the prompts. All of this is optional. Without it, the BBS uses its
built-in screens and menus.

## Screens

Display files go in one directory:

```
[general]
screens_dir = /etc/bbs/screens
```

A screen has a name (lower-case letters, digits, `_` and `-`) and one or
more files, one for each type of terminal:

| File | What it is | Shown to |
|---|---|---|
| `NAME.ans` | ANSI art in CP437, as ANSI editors (PabloDraw, Moebius, TheDraw) save it. The BBS removes a SAUCE record after ^Z | ANSI terminals |
| `NAME.asc` | text (UTF-8 or ASCII) with [tags](#tags) for colour, one page at a time | all text terminals. 3270s when there is no `.3270` |
| `NAME.txt` | the same as `.asc` | the same |
| `NAME.3270` | text with colour tags (below) | 3270s |
| `NAME.rip` | RIPscrip (graphics and ANSI) in CP437. Its buttons send keys to the menu that shows it | [RIP terminals](PETSCII-and-RIP) |
| `NAME.seq` | PETSCII, as C64 editors save it, sent as it is (no @-codes) | [Commodores](PETSCII-and-RIP) |

Each terminal gets the first file that it can show:

- An ANSI terminal: `.ans`, then `.asc`, then `.txt`.
- A RIP terminal: `.rip`, then the same as ANSI.
- A plain terminal: `.asc` or `.txt`.
- A Commodore: `.seq`, then `.asc` or `.txt` (translated).
- A 3270: `.3270`, `.asc` or `.txt`.

The BBS sends ANSI art to a UTF-8 terminal as UTF-8. It sends it to all
other terminals as its original CP437 bytes, as SyncTERM, NetRunner and
DOS terminal programs expect. The BBS reads files each time that it
shows them. Thus, you can change them while the BBS runs. The largest
file that the BBS reads is 256 KB.

The BBS shows these screens automatically:

| Name | When | Replaces |
|---|---|---|
| `logon` | when a caller connects, before the name prompt (on a 3270, in the upper part of the logon screen) | the name of the system, the banner and the route of the caller |
| `newuser` | before the questions for a new user | (added) |
| `welcome` | after the login | the welcome lines (the message of the day still comes after it) |
| `goodbye` | at a logout | the `goodbye` line |

Menus show the other screens, with `screen = NAME` on a menu or a
`screen:NAME` item.
[screens.sample](https://github.com/D-Modem/bbs/tree/HEAD/screens.sample)
has a set that you can use as a start.

### @-codes

Screens, menu titles, labels, prompts and strings can contain codes. The
BBS fills them in when it shows them:

| Code | Filled in with |
|---|---|
| `@BBS@` `@SYSOP@` `@LOCATION@` | the system |
| `@PHONE@` `@ADDRESSES@` `@POSTAL@` | how to contact it: `phone`, `addresses`, `postal_address` of `[general]` |
| `@USER@` `@USERLOC@` | the name and the location of the caller |
| `@CALLS@` `@POSTS@` `@LASTON@` `@SINCE@` | the calls, posts, last call and join date of the caller |
| `@NEWMAIL@` | the unread mail of the caller |
| `@NODE@` `@VIA@` `@BAUD@` | this call: node, route (telnet, modem, tn3270), the rate of a modem |
| `@ONLINE@` | the callers that are on now |
| `@DATE@` `@TIME@` | now |
| `@CLS@` | clears the screen |
| `@PAUSE@` | waits for a key (on a 3270, a new page) |

`@CODE:20@` pads or cuts the value to 20 characters. `@CODE:-20@` aligns
it to the right. Thus, art keeps its shape for all lengths of a name.
The BBS does not change other text between two @ signs (for example, an
email address). The BBS cleans values as it cleans all text from
callers. Thus, no escape sequences can get into art through the name of
a user.

### Tags

Text screens (`.asc`, `.txt`), the [strings](#strings), and the title,
labels and prompt of a [menu](#menus) can contain tags in braces. Tags
are shortcuts for ANSI escape sequences, similar to the tags that
`.3270` files use for the colours of a 3270.

```
{clear}{cyan}{bright}*** @BBS@ ***{reset}
{yellow}1.{default} The board is open.   {on blue}{white} NEW {reset}
{pause}
```

| Tag | What it does |
|---|---|
| `{clear}` `{cls}` | clears the screen (as `@CLS@`) |
| `{pause}` | waits for a key (as `@PAUSE@`. In a string, the BBS removes it) |
| `{home}` `{eol}` | moves the cursor to the top left. Clears to the end of the line |
| `{black}` `{red}` `{green}` `{yellow}` `{blue}` `{magenta}` `{cyan}` `{white}` | the colour of the letters (`{pink}` and `{turquoise}`, the names of the 3270, are magenta and cyan) |
| `{on black}` ... `{on white}` | the colour behind the letters |
| `{bright}` `{bold}` / `{normal}` | bright, or not bright |
| `{blink}` `{underline}` `{reverse}` | |
| `{default}` `{reset}` | plain again |

Each terminal gets the items that it can show:

- An ANSI terminal gets the escape sequences.
- A terminal without colour (`Mono`) gets bright, underline and similar
  items, but no colours.
- A Commodore gets its own colours (but no backgrounds, blink or
  underline).
- A VT52 gets the clears and cursor movements.
- A plain terminal gets nothing (a `{clear}` becomes a new line).

A screen always ends plain, for all values of its last colour. On a
3270, a text screen keeps its colours (each change uses a column, as the
text below tells), and the BBS removes the other tags. The labels of a
menu show their colours in the same way, but its title and prompt are
plain. Where the BBS cannot show tags (on the buttons of a RIP terminal,
or the title of a menu in Who's online), it also removes them. A menu
label stays in its column for all tags, and ends plain. In a text
screen, a line that contains only `{clear}` or `{pause}` (or `@CLS@` or
`@PAUSE@`) does not leave a blank line.

Tags operate only in the text of the sysop. If a caller uses the name
`{red}bob`, the BBS shows `{red}bob`. ANSI art (`.ans`) keeps its braces
as they are, because it has its own escape sequences.

### 3270 screens

A `.3270` file is lines of text with tags. A tag changes the colour for
the remainder of the line:

```
{turquoise}{bright}*** @BBS@ ***
{blue}A bulletin board, by telnet, modem and TN3270.{default} plain again
```

The tags are `{blue}` `{red}` `{pink}` `{green}` `{turquoise}` `{yellow}`
`{white}` `{default}`, `{bright}` and `{normal}`, and also `{clear}` and
`{pause}` for a new page. The names of the text terminals (`{magenta}`,
`{cyan}`, `{reset}`) also operate. On a 3270, each change of colour uses
a blank column, because the 3270 keeps the attribute byte of the field
there.

## Menus

The sysop can replace the main menu and add other menus. Callers go
from the main menu to the other menus, and from these menus to more
menus. If the sysop does not give a main menu, the BBS uses its built-in
main menu. The built-in main menu is a menu file of the Classic theme
([`internal/app/menus/main.menu`](https://github.com/D-Modem/bbs/tree/HEAD/internal/app/menus/main.menu)).
You can copy it to `menus_dir` and change it.

There are two ways to write a menu:

- A `[menu NAME]` section in `bbs.conf`, with one line for each item.
  This way is short. Each item does one command.
- A menu file `NAME.menu` in `menus_dir`. This way gives all the
  functions: an item can do more than one command, a key can be a word,
  and there are special keys and hidden items.

A menu name is lower-case letters, digits, `_` and `-`. If two menus
have the same name, `bbs check` refuses the configuration.

### Menus in bbs.conf

```
[menu main]
title = {cyan}{bright}@BBS@{reset}
screen = mainmenu          ; shown instead of the list of items (optional)
prompt = "Command: "       ; (optional)
m = areas Message areas
e = mail E-mail@NOTE@
d = doors Doors
l = door:lord Legend of the Red Dragon
b = screen:bulletins Bulletins
g = area:general Straight to General
x = menu:extras Extras
s = sysop Sysop tools | sysop
q = logoff Goodbye

[menu extras]
title = Extras
u = users User list
z = back Back
```

Each item is `KEY = COMMAND Label`. The key is one character (not `;` or
`#`, which start comments). The command is one of the
[commands](#commands). Write its data after a colon: `door:lord`. After
`|`, an item can have an [access requirement](Access-Levels). Only
callers who meet the requirement see the item.

### Menu files

Set the directory of the menu files:

```
[general]
menus_dir = /etc/bbs/menus
```

The file `NAME.menu` is the menu `NAME`. Thus, `main.menu` replaces the
built-in main menu. A menu file has one `[menu]` section and one
`[item KEY]` section for each item:

```
; /etc/bbs/menus/messages.menu
[menu]
title = {cyan}Messages{reset}
prompt = "{yellow}@USER@{default}> "
screen = messages          ; shown instead of the list of items (optional)
require = 20               ; who can go into the menu (optional)
fail = main                ; where other callers go (optional)

[item @first]              ; done one time, when the menu opens
do = screen:msgnews

[item r]
label = Read the areas
do = areas

[item n]
label = Areas, then mail
do = areas ; mail

[item JOIN]                ; a word: the caller types it, then Enter
label = Join an area
do = area:general

[item q]
label = Back
do = menu.return

[item x]                   ; the key operates, but it is not in the list
hidden = yes
do = menu.main
```

The `[menu]` section can have these keys. All of them are optional.

| Key | What it is |
|---|---|
| `title` | the title of the menu, with @-codes and tags. The default is the name of the menu |
| `prompt` | the prompt. The default is the `command_prompt` string |
| `screen` | a screen that the BBS shows instead of the list of items |
| `require` | the [access requirement](Access-Levels) to go into the menu. The main menu does not use it |
| `fail` | the menu for callers who do not meet `require`. If there is no `fail`, the caller stays in the menu before |

An `[item KEY]` section can have these keys:

| Key | What it is |
|---|---|
| `do` | the [commands](#commands), with `;` between them. This key is necessary |
| `label` | the text in the list, with @-codes and tags. It is necessary, but not for a special key or a hidden item |
| `require` | the [access requirement](Access-Levels) to see and use the item |
| `hidden` | `yes`: the key operates, but the BBS does not show it in the list |

The key of an item is one of these:

- One character, for example `r` or `*`. The case of a letter is not
  important.
- A word of 2 to 12 letters and digits, for example `JOIN`. If a menu
  has a word, the caller types a line and pushes Enter. Then a key of one
  character also needs Enter.
- A special key:
  - `@first`: the BBS does it one time, when the caller goes into the
    menu.
  - `@every`: the BBS does it each time before it shows the menu.
  - `@enter`: the BBS does it when the caller pushes Enter and types no
    key.

The BBS does the commands of an item in sequence. A command that goes
to a different menu (`menu.gosub`, `menu.goto`, `menu.return`,
`menu.main`) is the last command that the BBS does.

### Commands

| Command | What it does |
|---|---|
| `areas` | the message areas |
| `mail` | e-mail |
| `who` | who is online |
| `users` | the user list |
| `profile` | the profile of the caller |
| `info` | system information |
| `files` | the file areas |
| `boards` | the [image boards](Image-Boards) |
| `doors` | the list of doors |
| `chat` | chat |
| `page` | pages the sysop |
| `callers` | the last callers |
| `feedback` | mail to the sysop |
| `qwk` | [offline mail](Offline-Mail) |
| `news` | news, weather and stock quotes |
| `headlines` | the news headlines |
| `weather` | the weather of the caller |
| `quotes` | stock quotes |
| `validate` | the users that wait for validation (for sysops) |
| `sysop` | the [sysop tools](Sysop-Tools) (for sysops) |
| `terminal` | the terminal details of a 3270 |
| `logoff` | logs out (on a text terminal, it asks first) |
| `door:NAME` | goes directly into a door |
| `screen:NAME` | shows a screen |
| `area:NAME` | goes directly into a message area |
| `script:NAME` | runs a script of `scripts_dir` ([Scripting](Scripting)) |
| `menu.gosub NAME` | goes to the menu `NAME`. `menu.return` in that menu comes back. The short name is `menu:NAME` |
| `menu.goto NAME` | goes to the menu `NAME`, in place of this menu |
| `menu.return` | goes back to the menu before. The short name is `back` |
| `menu.main` | goes back to the main menu |

In a menu file, write the data after a space or after a colon:
`menu.gosub extras` and `menu.gosub:extras` are the same.

These names are also correct: `msg.areas` (`areas`), `mail.read`
(`mail`), `mail.feedback` (`feedback`), `info.who` (`who`), `info.users`
(`users`), `info.system` (`info`), `info.callers` (`callers`),
`user.profile` (`profile`), `file.areas` (`files`).

Some commands are not always available. The BBS does not show an item
if its first command is not available to the caller:

- `files`: only if there are file areas.
- `doors`: only if a door is open to the caller.
- `boards` and `news`: only if the BBS has them.
- `chat` and `page`: only if the [level](Access-Levels) of the caller
  lets the caller chat or page.
- `sysop`: only for sysops.
- `validate`: only for sysops, and only if users wait for validation.
- `terminal`: only on a 3270.

`@NOTE@` in a label gives a note from the first command of the item:

| Command | `@NOTE@` |
|---|---|
| `mail` | ` (3 new)`: the new mail. Nothing if there is no new mail |
| `chat` | ` (2 paging)`: callers who page the sysop. Only for sysops |
| `validate` | ` (4)`: the users that wait |
| `news` | the name of what there is, for example `News and weather` |

A caller can go into a maximum of 10 menus, each from the menu before.

### Menus on a 3270 and on RIP terminals

With `screen =`, a text terminal shows the screen, which shows the keys
itself. A 3270 shows the `.3270` (or `.asc` or `.txt`) version of the
screen if there is one. If not, it shows the list of items in a box.
Thus, a menu that is drawn only as ANSI art still operates on a 3270.

On a 3270, the caller types the key in the option field. PF3 goes back
(on the main menu, it logs out if there is a `logoff` item). Doors are
only for text terminals. On a 3270, a door item tells this. A RIP
terminal shows the items as buttons.

`bbs check` refuses a menu that names a command, menu, door, screen or
script that does not exist. It lists the menus that it read.

## Strings

```
[strings]
name_prompt = "Handle: "
new_name_prompt = "Handle (or NEW): "
password_prompt = "Password: "
command_prompt = "@USER@ @ @BBS@ > "
pause = "-- more --"
logoff_confirm = "Leaving so soon, @USER@?"
welcome = "Hi @USER@, @NEWMAIL@ new letters."
```

Each string can contain @-codes and [tags](#tags). To keep spaces at the
end of a value, put the value in quotes. `welcome` is also the greeting
on the 3270 main menu (there, without its tags).

```
command_prompt = "{yellow}@USER@{default} @ {cyan}@BBS@{default} > "
pause = "{blue}-- more --{reset}"
```

**See also:** [Configuration](Configuration), [Doors](Doors)
