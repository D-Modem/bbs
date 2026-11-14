The text BBS serves ANSI terminals. It also serves two other types of
terminal from the BBS era:

- Commodore 64s and 128s in **PETSCII**.
- **RIPscrip** terminals, which draw graphics and buttons that the
  caller can click.

Both types get the same BBS as all other callers: messages, mail, files,
chat, paging, and the menus and screens of the sysop.

## PETSCII: Commodores

Commodore callers use CCGMS, Novaterm, CGTerm, a C64 with a WiFi modem,
SyncTERM or a different terminal in C64 mode. They can also use a real
C64 that calls a modem line through [app_dmodem](Modem-Callers).

Each listener tells which of its callers are Commodores:

```ini
[listen c64]
type = telnet
address = :6400
petscii = yes
```

| `petscii` | Meaning |
|---|---|
| `no` (the default) | no Commodores on this listener |
| `yes` | all callers are Commodores (for a port or modem line only for C64s) |
| `auto` | the BBS monitors a caller whose terminal is not ANSI (and not known by its type) during the login, as the text below tells |

On all listeners, the BBS reads a telnet terminal type of `petscii`,
`c64`, `c128`, `commodore` or `cbm` as a Commodore.

### How `auto` finds a Commodore

The terminal of a Commodore answers no questions (a C64 has no status
reports, attributes or answerback). Thus, the BBS monitors its keys. Until
the BBS knows, the logon screen and the name prompt are in capitals.
Both a C64 in its power-on character set and all other terminals can
read capitals. Then:

| If the caller | The BBS |
|---|---|
| pushes a key that only a Commodore has: DEL (0x14), a cursor key (not down, 0x11, which is XON), F1-F8 (0x85-0x8C), CLR (0x93), or types a shifted letter (0xC1-0xDA, unless a UTF-8 continuation byte comes immediately after it) | changes to PETSCII immediately. It clears the screen (also the remaining characters of the questions), and shows the logon screen and the name prompt again |
| types a name in lower case | knows that it is not a Commodore (the unshifted letters of a Commodore arrive as ASCII capitals). It asks nothing |
| types a name all in capitals | asks `PRESS DEL (OR BACKSPACE)`. The DEL of a Commodore changes it to PETSCII (and the BBS reads the name as lower case, as it was typed). All other keys keep ASCII |

If the BBS does not know at the end of the login, the caller gets ASCII
(for example, a caller who logged in by rlogin or a trusted header). The
BBS sends questions before the name prompt to find an ANSI terminal or a
VT52. These questions leave some characters on the screen of a C64. The
change to PETSCII clears them.

A Commodore caller gets:

- The lower/upper case character set, and a cleared screen. The BBS
  prevents shift + C= from a change of the character set.
- 40 columns and 25 rows (the `[W]idth` of the profile changes to the 80
  columns of a C128). Menus are in one column. Lists fit the screen
  (who's online, users, areas, messages, mail, files). Text wraps at 39.
- Colours as the colour codes of the C64, reverse, clear, and cursor
  movement. Box drawing and block characters as its graphics characters.
  Letters with accents without the accents. `?` for characters that it
  does not have.
- Typed text read back: letters as the set has them, RETURN, and DEL to
  erase.
- Display files: the BBS sends `NAME.seq` (PETSCII art, as C64 editors
  save it) as it is. If not, it translates `NAME.asc` or `.txt` (refer to
  [Customization](Customization)).

The BBS does not offer doors, because they are ANSI programs. File
transfers use the usual XMODEM, YMODEM, ZMODEM and Kermit (CCGMS and
Novaterm have XMODEM). Punter is not supported.

## RIPscrip

RIPterm, and SyncTERM with RIP enabled, draw RIPscrip 1.54. This is
vector graphics on a 640x350 EGA screen, with buttons that send keys when
the caller clicks them.

```ini
[listen telnet]
type = telnet
address = :2300
rip = auto
```

| `rip` | Meaning |
|---|---|
| `auto` (the default) | the BBS sends `ESC [ !` to an ANSI terminal that is not a modern terminal (it does not ask `xterm`, `screen`, `putty` and similar terminals). A terminal that answers `RIPSCRIP...` has RIP |
| `yes` | all ANSI callers have RIP |
| `no` | the BBS asks no terminal |

The question adds one round trip. The BBS sends a position request
immediately after it, which all ANSI terminals answer. The answer of a
RIP terminal arrives first.

A RIP caller gets:

- Each menu (the main menu, and the menus of the sysop) as a scene. A
  scene has a title bar, a button for each item, and the prompt in a text
  window below. The key of each button has an underline, and a click on
  the button is the same as a push of the key. When the caller makes a
  selection, the BBS clears the scene and continues in text, full screen.
  The BBS waits for a key before the next menu covers that text.
- Display files: first `NAME.rip` (RIPscrip, with ANSI in it, in CP437,
  with @-codes filled), then `.ans`, `.asc`, `.txt`. If a menu shows a
  `.rip` screen from the sysop, the menu has the buttons of that screen,
  not the built-in buttons.
- All other items as on all ANSI terminals.

A caller can disable and enable RIP in the profile (`[R]IP`). The caller
can enable it only if the terminal answered. The BBS keeps the
selection for the next call, as it keeps the width of a Commodore
([Terminal identification](Terminal-Identification#what-a-caller-sets)).

**See also:** [Customization](Customization), [Configuration](Configuration), [Modem callers](Modem-Callers)
