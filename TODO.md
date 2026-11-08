# Future work

Ideas not yet started, with what is known of them.

## Doors for 3270 callers: an ANSI to 3270 layer

*Status: being thought about; other ways to solve it may win.*

Doors are ANSI programs, so 3270 callers do not see the Doors menu.  A
layer between a door and a 3270 could show them:

- **Emulate, then draw.**  A small ANSI-BBS emulator (80x25: cursor
  movement, ED/EL, SGR, save/restore cursor, and answers to `ESC[6n`, which
  many doors use to detect ANSI) keeps the door's screen; every ~50 ms the
  cells that changed go to the 3270 as SBA/SA orders and characters
  (Write with keyboard restore).  go3270-sec already has the raw writes,
  the query replies and Load PS.
- **Characters.**  ASCII and Latin-1 letters are in EBCDIC CP037.  What
  CP437 has beyond them (box drawing single/double/mixed, ░▒▓█ ▀▄▌▐,
  ☺♥♦♣♠•◘○ and arrows: about 70 glyphs) goes in one programmed-symbol set,
  drawn by program at the terminal's cell size (from its query reply), as
  the logo is: no font file.  Each cell selects the set with an SA
  character-set attribute, which takes no screen position.  Without
  programmed symbols (x3270, wc3270, Vista): GE / CP310 line drawing and
  ASCII for the blocks.
- **Colours.**  Seven 3270 colours for ANSI's eight; bright and normal the
  same (or the intensified attribute for bold); ANSI backgrounds by the SA
  background-colour attribute where the emulator has it (PCOMM, it
  seems), else reverse video.  Blink and underscore map directly.
- **Input: the hard part.**  A 3270 sends nothing until Enter, a PF/PA key
  or Clear.  An unformatted screen lets the caller type at the door's
  cursor; on Enter the BBS diffs the screen against what it drew to find
  the keys, and sends them and a CR.  PF keys for arrows, PgUp/PgDn,
  Home/End; PA1 for Esc; an empty Enter for a lone CR.  Menu-driven doors
  (LORD, BRE, Usurper, TradeWars) would be fine; hotkey and real-time ones
  sluggish.
- **Size.**  Doors want 25 lines: a model 3 (32x80) or bigger fits, with
  room for a help line; a model 2 (24x80) loses or scrolls one.
- **Testing.**  The emulator, diffing and data stream by unit tests; the
  fallback path with s3270; a whole DOS door session by a scripted 3270
  client.  Programmed symbols only against a fake terminal here: PCOMM to
  be tried by hand.
- **Shape.**  An option on a `[door]` (`tn3270 = yes`) that shows the door
  on the 3270 Doors menu and puts this layer between them.
