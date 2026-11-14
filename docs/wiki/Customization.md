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

Each `[menu NAME]` section makes a menu. `[menu main]` replaces the
built-in main menu on text terminals and on 3270s. Users get to the
other menus from the main menu and from each other.

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
`#`, which start comments). The action is what the item does:

| Action | What it does |
|---|---|
| `areas` `mail` `who` `users` `profile` `info` `files` `boards` `doors` `chat` `page` `callers` `feedback` `qwk` `sysop` | the built-in parts (`callers`: the last callers. `feedback`: mail to the sysop. `qwk`: [offline mail](Offline-Mail). `boards`: the [image boards](Image-Boards). `sysop`: the [sysop tools](Sysop-Tools), for sysops) |
| `terminal` | the terminal details of a 3270 |
| `logoff` | logs out (on a text terminal, it asks first) |
| `back` | goes back to the previous menu |
| `door:NAME` | goes directly into a door |
| `menu:NAME` | goes to a different menu |
| `screen:NAME` | shows a screen |
| `area:NAME` | goes directly into a message area |
| `script:NAME` | runs a script of `scripts_dir` ([Scripting](Scripting)) |

With `screen =`, a text terminal shows the screen, which shows the keys
itself. A 3270 shows the `.3270` (or `.asc` or `.txt`) version of the
screen if there is one. If not, it shows the list of items in a box.
Thus, a menu that is drawn only as ANSI art still operates on a 3270.

On a 3270, the caller types the key in the option field. PF3 goes back
(on the main menu, it logs out if there is a `logoff` item). Doors are
only for text terminals. On a 3270, a door item tells this. `bbs check`
refuses a menu that names a menu, door or screen that does not exist. It
lists the menus that it read.

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
