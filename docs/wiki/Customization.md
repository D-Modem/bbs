The sysop can change how the BBS looks: their own screens (ANSI art, text
and 3270 screens in colour), their own menus, and the wording of the
prompts. None of this is required; without it, the BBS uses its built-in
screens and menus.

## Screens

Display files go in one directory:

```
[general]
screens_dir = /etc/bbs/screens
```

A screen has a name (lower-case letters, digits, `_` and `-`) and one or
more files, one for each kind of terminal:

| File | What it is | Shown to |
|---|---|---|
| `NAME.ans` | ANSI art in CP437, as ANSI editors (PabloDraw, Moebius, TheDraw) save it; a SAUCE record after ^Z is dropped | ANSI terminals |
| `NAME.asc` | text (UTF-8 or ASCII) with [tags](#tags) for colour, a page at a time | any text terminal; 3270s when there is no `.3270` |
| `NAME.txt` | the same as `.asc` | the same |
| `NAME.3270` | text with colour tags (below) | 3270s |
| `NAME.rip` | RIPscrip (graphics and ANSI) in CP437; its buttons send keys to the menu showing it | [RIP terminals](PETSCII-and-RIP) |
| `NAME.seq` | PETSCII, as C64 editors save it, sent as it is (no @-codes) | [Commodores](PETSCII-and-RIP) |

Each terminal gets the first file it can show:

- an ANSI terminal: `.ans`, then `.asc`, then `.txt`
- a RIP terminal: `.rip`, then the same as ANSI
- a plain terminal: `.asc` or `.txt`
- a Commodore: `.seq`, then `.asc` or `.txt` (translated)
- a 3270: `.3270`, `.asc` or `.txt`

ANSI art is sent to a UTF-8 terminal as UTF-8, and to any other terminal
as its original CP437 bytes, which is what SyncTERM, NetRunner and DOS
terminal programs expect. Files are read each time they are shown, so you
can change them while the BBS runs. The largest file read is 256 KB.

The BBS shows these screens by itself:

| Name | When | Replaces |
|---|---|---|
| `logon` | when a caller connects, before the name prompt (on a 3270, in the logon screen's upper part) | the system's name, banner and how they came |
| `newuser` | before a new user's questions | (added) |
| `welcome` | after the login | the welcome lines (the message of the day still follows) |
| `goodbye` | at a logoff | the `goodbye` line |

Menus show the others, with `screen = NAME` on a menu or a `screen:NAME`
item. [screens.sample](https://github.com/D-Modem/bbs/tree/HEAD/screens.sample)
has a set to start from.

### @-codes

Screens, menu titles, labels, prompts and strings can hold codes that are
filled in when they are shown:

| Code | Filled in with |
|---|---|
| `@BBS@` `@SYSOP@` `@LOCATION@` | the system |
| `@PHONE@` `@ADDRESSES@` `@POSTAL@` | how to reach it: `[general]`'s `phone`, `addresses`, `postal_address` |
| `@USER@` `@USERLOC@` | the caller's name, where they call from |
| `@CALLS@` `@POSTS@` `@LASTON@` `@SINCE@` | their calls, posts, last call, when they joined |
| `@NEWMAIL@` | their unread mail |
| `@NODE@` `@VIA@` `@BAUD@` | this call: node, how (telnet, modem, tn3270), a modem's rate |
| `@ONLINE@` | callers on now |
| `@DATE@` `@TIME@` | now |
| `@CLS@` | clear the screen |
| `@PAUSE@` | wait for a key (on a 3270, a new page) |

`@CODE:20@` pads or cuts the value to 20 characters, and `@CODE:-20@`
aligns it to the right, so art keeps its shape whatever the length of a
name. Anything else between two @ signs (such as an email address) is left
as it is. Values are cleaned like anything a caller writes, so no escape
sequences can get into art through a user's name.

### Tags

Text screens (`.asc`, `.txt`), the [strings](#strings), and a
[menu's](#menus) title, labels and prompt can hold tags in braces. Tags
are shortcuts for ANSI escape sequences, like the tags `.3270` files use
for a 3270's colours.

```
{clear}{cyan}{bright}*** @BBS@ ***{reset}
{yellow}1.{default} The board is open.   {on blue}{white} NEW {reset}
{pause}
```

| Tag | What it does |
|---|---|
| `{clear}` `{cls}` | clear the screen (as `@CLS@`) |
| `{pause}` | wait for a key (as `@PAUSE@`; in a string, dropped) |
| `{home}` `{eol}` | the cursor to the top left; clear to the end of the line |
| `{black}` `{red}` `{green}` `{yellow}` `{blue}` `{magenta}` `{cyan}` `{white}` | the colour of the letters (`{pink}` and `{turquoise}`, the 3270's names, are magenta and cyan) |
| `{on black}` ... `{on white}` | the colour behind them |
| `{bright}` `{bold}` / `{normal}` | bright, or not |
| `{blink}` `{underline}` `{reverse}` | |
| `{default}` `{reset}` | plain again |

Each terminal gets what it can show:

- an ANSI terminal gets the escape sequences;
- a terminal without colour (`Mono`) gets bright, underline and the like,
  but no colours;
- a Commodore gets its own colours (but no backgrounds, blink or
  underline);
- a VT52 gets the clears and cursor moves;
- a plain terminal gets nothing (a `{clear}` becomes a new line).

A screen always ends plain, whatever its last colour. On a 3270, a text
screen keeps its colours (each change takes a column, as explained below)
and the other tags are dropped. A menu's labels show their colours the
same way, while its title and prompt are plain. Where tags cannot be shown
at all (on a RIP terminal's buttons, or a menu's title in Who's online),
they are dropped too. A menu label stays in its column whatever its tags,
and ends plain. In a text screen, a line holding only `{clear}` or
`{pause}` (or `@CLS@` or `@PAUSE@`) leaves no blank line behind.

Tags work only in the sysop's text: a caller who names themselves
`{red}bob` is shown as `{red}bob`. ANSI art (`.ans`) keeps its braces
as they are, because it has its own escape sequences.

### 3270 screens

A `.3270` file is lines of text with tags that change the colour for the
rest of the line:

```
{turquoise}{bright}*** @BBS@ ***
{blue}A bulletin board, by telnet, modem and TN3270.{default} plain again
```

The tags are `{blue}` `{red}` `{pink}` `{green}` `{turquoise}` `{yellow}`
`{white}` `{default}`, `{bright}` and `{normal}`, plus `{clear}` and
`{pause}` for a new page. The text terminals' names (`{magenta}`, `{cyan}`,
`{reset}`) work too. On a 3270, each change of colour takes up a blank
column, because that is where the 3270 keeps the field's attribute byte.

## Menus

Each `[menu NAME]` section makes a menu. `[menu main]` replaces the
built-in main menu on text terminals and 3270s alike, and the other menus
are reached from it and from each other.

```
[menu main]
title = {cyan}{bright}@BBS@{reset}
screen = mainmenu          ; shown instead of the list of items (optional)
prompt = "Command: "       ; (optional)
m = areas Message areas
e = mail E-mail ({yellow}@NEWMAIL@{default} new)
d = doors Doors
l = door:lord Legend of the Red Dragon
b = screen:bulletins Bulletins
g = area:general Straight to General
x = menu:extras Extras
q = logoff Goodbye

[menu extras]
title = Extras
u = users User list
z = back Back
```

Each item is `KEY = ACTION Label`. The key is one character (not `;` or
`#`, which begin comments), and the action is what the item does:

| Action | What it does |
|---|---|
| `areas` `mail` `who` `users` `profile` `info` `files` `boards` `doors` `chat` `page` `callers` `feedback` `qwk` `sysop` | the built-in parts (`callers`: the last callers; `feedback`: mail to the sysop; `qwk`: [offline mail](Offline-Mail); `boards`: the [image boards](Image-Boards); `sysop`: the [sysop tools](Sysop-Tools), for sysops) |
| `terminal` | a 3270's terminal details |
| `logoff` | log off (asking first on a text terminal) |
| `back` | back to the menu before |
| `door:NAME` | straight into a door |
| `menu:NAME` | another menu |
| `screen:NAME` | show a screen |
| `area:NAME` | straight into a message area |
| `script:NAME` | run a script of `scripts_dir` ([Scripting](Scripting)) |

With `screen =`, a text terminal sees the screen, which shows the keys
itself. A 3270 sees the screen's `.3270` (or `.asc` or `.txt`) version if
there is one, or otherwise the list of items in a box, so a menu drawn
only as ANSI art still works on a 3270. On a 3270 the caller types the key
in the option field, and PF3 goes back (on the main menu, it logs off if
there is a `logoff` item). Doors are for text terminals only; on a 3270, a
door item says so. `bbs check` refuses a menu that names a menu, door or
screen that does not exist, and lists the menus it read.

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

Each string can hold @-codes and [tags](#tags). Put a value in quotes to
keep spaces at its end. `welcome` is also the greeting on the 3270 main
menu (shown there without its tags).

```
command_prompt = "{yellow}@USER@{default} @ {cyan}@BBS@{default} > "
pause = "{blue}-- more --{reset}"
```

**See also:** [Configuration](Configuration), [Doors](Doors)
