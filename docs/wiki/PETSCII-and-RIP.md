As well as ANSI terminals, the text BBS serves two other kinds of
terminal from the BBS era: Commodore 64s and 128s in **PETSCII**, and
**RIPscrip** terminals, which draw graphics and clickable buttons. Both
get the same BBS as everyone else: messages, mail, files, chat, paging,
and the sysop's menus and screens.

## PETSCII: Commodores

Commodore callers use CCGMS, Novaterm, CGTerm, a C64 with a WiFi modem,
SyncTERM or another terminal in C64 mode, or a real C64 calling a modem
line through [app_dmodem](Modem-Callers).

Each listener says which of its callers are Commodores:

```ini
[listen c64]
type = telnet
address = :6400
petscii = yes
```

| `petscii` | Meaning |
|---|---|
| `no` (the default) | no Commodores on this listener |
| `yes` | every caller is a Commodore (for a port or modem line just for C64s) |
| `auto` | a caller whose terminal is not ANSI (and not known by its type) is watched while it logs in, as described below |

On any listener, a telnet terminal type of `petscii`, `c64`, `c128`,
`commodore` or `cbm` is taken as a Commodore.

### How `auto` finds a Commodore

A Commodore's terminal answers no questions (a C64 has no status
reports, attributes or answerback), so the BBS watches its keys instead.
Until it knows, the logon screen and the name prompt are in capitals,
which both a C64 in its power-on character set and any other terminal can
read. Then:

| If the caller | The BBS |
|---|---|
| presses a key only a Commodore has: DEL (0x14), a cursor key (not down, 0x11, which is XON), F1-F8 (0x85-0x8C), CLR (0x93), or types a shifted letter (0xC1-0xDA, unless a UTF-8 continuation byte follows it at once) | switches to PETSCII there and then: the screen cleared (and the questions' leftovers with it), the logon screen and the name prompt again |
| types a name in lower case | knows it is not a Commodore (whose unshifted letters arrive as ASCII's capitals); nothing is asked |
| types a name all in capitals | asks `PRESS DEL (OR BACKSPACE)`: a Commodore's DEL makes it PETSCII (and the name, which was typed in lower case, is taken so), anything else leaves it ASCII |

A caller who is still undecided at the end of the login (for example, one
logged in by rlogin or a trusted header) gets ASCII. The questions sent
before the name prompt to find an ANSI terminal or a VT52 leave a few
characters on a C64's screen; switching to PETSCII clears them.

A Commodore caller gets:

- the lower/upper case character set (and shift + C= kept from switching
  it), the screen cleared
- 40 columns and 25 rows (the profile's `[W]idth` switches to a C128's
  80): menus in one column, lists that fit (who's online, users, areas,
  messages, mail, files), text wrapped at 39
- colours as the C64's colour codes, reverse, clearing, cursor movement;
  box drawing and block characters as its graphics characters; letters
  with accents without them; what it has no character for as `?`
- what is typed read back: letters as the set has them, RETURN, DEL to
  erase
- display files: `NAME.seq` (PETSCII art, as C64 editors save it) sent as
  it is, else `NAME.asc` or `.txt` translated (see
  [Customization](Customization))

Doors are not offered, because they are ANSI programs. File transfers
use the usual XMODEM, YMODEM, ZMODEM and Kermit (CCGMS and Novaterm have
XMODEM); Punter is not supported.

## RIPscrip

RIPterm, and SyncTERM with RIP turned on, draw RIPscrip 1.54: vector
graphics on a 640x350 EGA screen, with buttons that send keys when
clicked.

```ini
[listen telnet]
type = telnet
address = :2300
rip = auto
```

| `rip` | Meaning |
|---|---|
| `auto` (the default) | an ANSI terminal that is not a modern one (`xterm`, `screen`, `putty` and friends are not asked) is asked with `ESC [ !`; one that answers `RIPSCRIP...` has RIP |
| `yes` | every ANSI caller has it |
| `no` | none is asked |

The question costs one round trip. The BBS sends a position request
straight after it, which every ANSI terminal answers, and a RIP terminal's
answer arrives first.

A RIP caller gets:

- every menu (the main menu, and the sysop's own) as a scene: a title
  bar, a button for each item (its key underlined, clicking it the same
  as pressing it) and the prompt in a text window below. When a choice
  is made the scene is cleared and the BBS goes on in text, full screen;
  whatever that printed waits for a key before the next menu covers it
- display files: `NAME.rip` first (RIPscrip, with ANSI in it, in CP437,
  @-codes filled), then `.ans`, `.asc`, `.txt`. A menu showing a `.rip`
  screen of the sysop's has its buttons, not the built-in ones
- everything else as on any ANSI terminal

A caller can turn RIP off and on in their profile (`[R]IP`; it can only
be turned on if the terminal answered). Their choice, like a Commodore's
width, is kept for their next call ([Terminal identification](Terminal-Identification#what-a-caller-sets)).

**See also:** [Customization](Customization), [Configuration](Configuration), [Modem callers](Modem-Callers)
