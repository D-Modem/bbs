The BBS's look is the sysop's: screens of their own (ANSI art, text, 3270
screens in color), menus of their own, and the prompts' words.  None of it
is needed: without it the built-in screens and menus are used.

## Screens

Display files go in one directory:

```
[general]
screens_dir = /etc/bbs/screens
```

A screen is a name (lower case letters, digits, `_` and `-`) and one or
more files for different terminals:

| file | what | shown to |
|---|---|---|
| `NAME.ans` | ANSI art in CP437, as ANSI editors (PabloDraw, Moebius, TheDraw) save it; a SAUCE record after ^Z is dropped | ANSI terminals |
| `NAME.asc` | text (UTF-8 or ASCII) with [tags](#tags) for color, a page at a time | any text terminal; 3270s when there is no `.3270` |
| `NAME.txt` | the same as `.asc` | the same |
| `NAME.3270` | text with color tags (below) | 3270s |
| `NAME.rip` | RIPscrip (graphics and ANSI) in CP437; its buttons send keys to the menu showing it | [RIP terminals](PETSCII-and-RIP) |
| `NAME.seq` | PETSCII, as C64 editors save it, sent as it is (no @-codes) | [Commodores](PETSCII-and-RIP) |

An ANSI terminal gets `.ans`, else `.asc`, else `.txt`; a RIP terminal
`.rip` before those; a plain one `.asc` or `.txt`; a Commodore `.seq`,
else `.asc` or `.txt` (translated); a 3270 `.3270`, `.asc` or `.txt`.  ANSI art goes to a UTF-8
terminal as UTF-8, and to any other as the CP437 bytes it is (what
SyncTERM, NetRunner and DOS terminal programs want).  Files are read
each time they are shown, so they can be changed while the BBS runs; the
largest read is 256 KB.

These are shown by themselves:

| name | when | in place of |
|---|---|---|
| `logon` | when a caller connects, before the name prompt (on a 3270, in the logon screen's upper part) | the system's name, banner and how they came |
| `newuser` | before a new user's questions | (added) |
| `welcome` | after the login | the welcome lines (the message of the day still follows) |
| `goodbye` | at a logoff | the `goodbye` line |

Others are shown by menus: `screen = NAME` on a menu, or a `screen:NAME`
item.  [screens.sample](https://github.com/Neo-Desktop/bbs-sec/tree/HEAD/screens.sample)
has a set to start from.

### @-codes

Screens, menu titles, labels and prompts, and the strings take codes
that are filled in when shown:

| code | |
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

`@CODE:20@` pads (or cuts) the value to 20 characters, `@CODE:-20@`
aligns it right: art keeps its shape whatever a name's length.  Anything
else between @s (an e-mail address) is left as it is.  The values are
cleaned like anything a caller writes: no escape sequences get into art
through a user's name.

### Tags

Text screens (`.asc`, `.txt`), the [strings](#strings) and a
[menu's](#menus) title, labels and prompt take tags in braces: shortcuts
for ANSI's escape sequences, as `.3270` files have for a 3270's colors.

```
{clear}{cyan}{bright}*** @BBS@ ***{reset}
{yellow}1.{default} The board is open.   {on blue}{white} NEW {reset}
{pause}
```

| tag | |
|---|---|
| `{clear}` `{cls}` | clear the screen (as `@CLS@`) |
| `{pause}` | wait for a key (as `@PAUSE@`; in a string, dropped) |
| `{home}` `{eol}` | the cursor to the top left; clear to the end of the line |
| `{black}` `{red}` `{green}` `{yellow}` `{blue}` `{magenta}` `{cyan}` `{white}` | the color of the letters (`{pink}` and `{turquoise}`, the 3270's names, are magenta and cyan) |
| `{on black}` ... `{on white}` | the color behind them |
| `{bright}` `{bold}` / `{normal}` | bright, or not |
| `{blink}` `{underline}` `{reverse}` | |
| `{default}` `{reset}` | plain again |

Each terminal gets what it can show: an ANSI terminal the escape
sequences; one without color (`Mono`) bright, underline and the like but
no colors; a Commodore its colors (no backgrounds, blink or underline);
a VT52 the clears and the cursor's moves; a plain terminal nothing (a
`{clear}` a new line).  A screen ends plain whatever its last color.  On
a 3270 a text screen's colors are its colors (each change taking a
column, below) and the other tags are dropped; a menu's labels show
their colors the same way, its title and prompt plain.  Where tags
cannot be shown at all (a RIP terminal's buttons, a menu's title in
Who's online) they are dropped too.  A menu's label keeps to its column
whatever its tags, and ends plain.  In a text screen a line holding only
a `{clear}` or a `{pause}` (or `@CLS@`, `@PAUSE@`) leaves no blank line
behind.

Only the sysop's text has tags: a caller who calls themselves `{red}bob`
is shown as `{red}bob`.  ANSI art (`.ans`) keeps its braces as they are:
it has its own escape sequences.

### 3270 screens

A `.3270` file is lines of text with tags that change the color for the
rest of the line:

```
{turquoise}{bright}*** @BBS@ ***
{blue}A bulletin board, by telnet, modem and TN3270.{default} plain again
```

`{blue}` `{red}` `{pink}` `{green}` `{turquoise}` `{yellow}` `{white}`
`{default}`, and `{bright}` / `{normal}` (and `{clear}` and `{pause}` for a
new page; the text terminals' names, `{magenta}`, `{cyan}`, `{reset}`, are
taken too).  On a 3270 each change of color takes a blank column (an
attribute byte, as 3270 fields work).

## Menus

`[menu NAME]` sections make menus; `[menu main]` takes the place of the
built-in main menu, on text terminals and 3270s alike, and the others are
reached from it (and from each other).

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

Each item is `KEY = ACTION Label`: one character (not `;` or `#`, which
begin comments), then what it does:

| action | |
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

With `screen =`, a text terminal sees the screen (which shows the keys
itself), and a 3270 the screen's `.3270` (or `.asc`, `.txt`) version if
it has one, else the list of items in a box: a menu drawn as ANSI art
alone still works on a 3270.  On a 3270 the key is typed in the option field;
PF3 goes back (on the main menu, logs off if a `logoff` item is there).
Doors are for text terminals: on a 3270, a door item says so.  `bbs
check` refuses a menu that names a menu, door or screen that is not
there, and lists the menus it read.

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

Each takes @-codes and [tags](#tags); quote a value to keep spaces at its
end.  `welcome` is the 3270 main menu's greeting too (shown without its
tags, on both kinds of terminal).

```
command_prompt = "{yellow}@USER@{default} @ {cyan}@BBS@{default} > "
pause = "{blue}-- more --{reset}"
```

**See also:** [Configuration](Configuration), [Doors](Doors)
