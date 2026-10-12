The BBS asks each caller's terminal what it is, as DEC's terminals were
asked, and adapts to the answer: real VTs (and emulators set up as one)
get their line drawing and character set, monochrome ones bold instead
of color, and a VT52 its own escape codes.  What the terminal said shows
in *Who's online* through the [HTTP API](HTTP-API) (`terminal`), in the
caller's profile, and in the log:

```
node 3: terminal VT220 (DA1 62;1;2;6;7;8;9, DA2 1;10;0)
node 4: terminal XTerm(390) (DA1 64;1;2;4;6;9;15;16;17;18;21;22;28, DA2 41;390;0, version XTerm(390))
node 5: terminal tmux 3.4 (DA1 1;2;4, DA2 84;0;0, version tmux 3.4)
```

## The questions

To an ANSI terminal, all at once:

| sent | asks | answered by |
|---|---|---|
| `ENQ` | the answerback message | terminals with one set (PuTTY says `PuTTY`) |
| `CSI c` | primary device attributes (DA1): the VT level and options | every VT100 and later, xterm, most emulators; SyncTERM in its CTerm form |
| `CSI > c` | secondary device attributes (DA2): the model and firmware | VT220 and later, xterm, VTE, tmux, screen, mintty |
| `CSI > 0 q` | XTVERSION: name and version | xterm, tmux, foot, kitty, WezTerm, iTerm2 |
| `CSI 16 t` | the character cell in pixels (for sixel) | xterm and others |
| `CSI 6 n` | the cursor position | every ANSI terminal: once it comes, the rest has |

The position report comes last, so the BBS waits only for it, not for a
timeout, even on a terminal that answers none of the others.  To a
terminal that is not ANSI (it did not answer the BBS's first question,
the cursor position) the BBS sends a VT52's identify, `ESC Z`, which a
VT52 answers with `ESC / Z`; any other terminal shows a `Z`, which is
wiped.

## What it is called

In order: XTVERSION's name; SyncTERM by its CTerm attributes; a VT52; a
DEC terminal by its DA2 model (VT100, VT220, VT240, VT320, VT330, VT340,
VT382, VT420, VT510, VT520, VT525, DECterm) with a DEC firmware version;
emulators by their DA2 (tmux, screen, mintty, rxvt, xterm by its patch
number, VTE); the answerback; the VT class from DA1 (VT100, VT102, VT200
class ... VT500 class).

## What it changes

- **DEC character sets.**  A VT (DA1 says VT100 or later) whose terminal
  type does not say UTF-8 and which is not a PC's BBS terminal (`ansi`,
  `syncterm` and the like, which want CP437) gets line drawing and
  blocks in DEC Special Graphics (`ESC ( 0`, the `lqkxjm` characters),
  Latin-1 letters as 8-bit characters on a VT220 or later (DEC
  Multinational has them) on an 8-bit line, and ASCII look-alikes for
  the rest.  ANSI art in CP437 is drawn the same way.  Typed Latin-1
  letters are read as such.  The profile says `DEC graphics`.
- **Monochrome.**  A DEC terminal by its model and firmware (not an
  emulator) without ANSI color (DA1 option 22) — VT100 to VT520 — gets
  bold for the bright colors and plain text for the rest.
- **Sixel** is known from DA1 option 4 (a VT240, VT330, VT340, xterm
  with it on, foot, WezTerm, mlterm), and the cell size from `CSI 16 t`.
- **VT52.**  A VT52 (or a VT100 in VT52 mode) gets clearing and cursor
  movement as `ESC H`, `ESC J`, `ESC K`, `ESC Y`, `ESC A`-`D`, no
  attributes, ASCII only; its cursor and keypad keys are not taken as
  commands.

## Turning it off

```ini
[listen modem]
type = raw
address = 127.0.0.1:2323
vt = no
```

`vt = no` asks nothing (sixel is then asked for by DA1 alone, as before)
and changes nothing; `auto` (the default) asks.

## What a caller sets

What a caller switches in their profile is kept with their account and
begins their next call:

| setting | values | at the next call |
|---|---|---|
| `ansi` | `on`, `off` | ANSI as they set it, whatever the terminal answered (`off` turns RIP off too) |
| `utf8` | `on`, `off` | UTF-8, or CP437 (ASCII where there is no ANSI) |
| `sixel` | `on`, `off` | `off` keeps sixel off; `on` only where the terminal says it has it |
| `rip` | `on`, `off` | the same for RIPscrip |
| `width` | `40`, `80` | a Commodore's columns |
| `editor` | `full`, `line` | the full-screen editor or the line editor |
| `expert` | `on`, `off` | menus as their prompt alone, `?` for the rest |

A Commodore and a VT52 take only what applies to them.  The settings are
`prefs` at `GET /api/v1/me`, changed with `PATCH /api/v1/me`
([HTTP API](HTTP-API)).

**See also:** [PETSCII and RIP](PETSCII-and-RIP), [Sixel graphics](Sixel-Graphics), [Configuration](Configuration)
