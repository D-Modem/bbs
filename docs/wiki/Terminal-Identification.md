The BBS asks each caller's terminal what it is, in the way DEC's terminals
were asked, and adapts to the answer. Real VTs (and emulators set up as
one) get their own line drawing and character set, monochrome terminals
get bold instead of colour, and a VT52 gets its own escape codes. The
terminal's answer appears in *Who's online* through the
[HTTP API](HTTP-API) (`terminal`), in the caller's profile, and in the
log:

```
node 3: terminal VT220 (DA1 62;1;2;6;7;8;9, DA2 1;10;0)
node 4: terminal XTerm(390) (DA1 64;1;2;4;6;9;15;16;17;18;21;22;28, DA2 41;390;0, version XTerm(390))
node 5: terminal tmux 3.4 (DA1 1;2;4, DA2 84;0;0, version tmux 3.4)
```

## The questions

An ANSI terminal is sent all of these questions at once:

| Sent | Asks for | Answered by |
|---|---|---|
| `ENQ` | the answerback message | terminals with one set (PuTTY says `PuTTY`) |
| `CSI c` | primary device attributes (DA1): the VT level and options | every VT100 and later, xterm, most emulators; SyncTERM in its CTerm form |
| `CSI > c` | secondary device attributes (DA2): the model and firmware | VT220 and later, xterm, VTE, tmux, screen, mintty |
| `CSI > 0 q` | XTVERSION: name and version | xterm, tmux, foot, kitty, WezTerm, iTerm2 |
| `CSI 16 t` | the character cell in pixels (for sixel) | xterm and others |
| `CSI 6 n` | the cursor position | every ANSI terminal; by the time it arrives, the other answers have too |

The position report comes last, so the BBS waits only for it rather than
for a timeout, even on a terminal that answers none of the others. If a
terminal is not ANSI (it did not answer the BBS's first question, the
cursor position), the BBS sends it a VT52's identify request, `ESC Z`. A
VT52 answers with `ESC / Z`; any other terminal shows a `Z`, which the BBS
then wipes.

## What it is called

The BBS names the terminal from the first of these that applies:

1. the name in its XTVERSION answer;
2. SyncTERM, from its CTerm attributes;
3. a VT52;
4. a DEC terminal, from its DA2 model (VT100, VT220, VT240, VT320, VT330,
   VT340, VT382, VT420, VT510, VT520, VT525, DECterm) with a DEC firmware
   version;
5. an emulator, from its DA2 (tmux, screen, mintty, rxvt, xterm by its
   patch number, VTE);
6. its answerback;
7. the VT class from DA1 (VT100, VT102, VT200 class ... VT500 class).

## What it changes

- **DEC character sets.** A VT (DA1 says VT100 or later) whose terminal
  type does not say UTF-8 and which is not a PC's BBS terminal (`ansi`,
  `syncterm` and the like, which want CP437) gets line drawing and
  blocks in DEC Special Graphics (`ESC ( 0`, the `lqkxjm` characters),
  Latin-1 letters as 8-bit characters on a VT220 or later (DEC
  Multinational has them) on an 8-bit line, and ASCII look-alikes for
  the rest. ANSI art in CP437 is drawn the same way. Typed Latin-1
  letters are read as such. The profile says `DEC graphics`.
- **Monochrome.** A DEC terminal by its model and firmware (not an
  emulator) without ANSI colour (DA1 option 22), from the VT100 to the
  VT520, gets bold for the bright colours and plain text for the rest.
- **Sixel** is known from DA1 option 4 (a VT240, VT330, VT340, xterm
  with it on, foot, WezTerm, mlterm), and the cell size from `CSI 16 t`.
- **VT52.** A VT52 (or a VT100 in VT52 mode) gets clearing and cursor
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

With `vt = no` the BBS asks none of these questions and changes nothing
(it still checks for sixel with DA1 alone). `auto`, the default, asks.

## What a caller sets

Settings a caller changes in their profile are kept with their account
and apply from their next call:

| Setting | Values | At the next call |
|---|---|---|
| `ansi` | `on`, `off` | ANSI as they set it, whatever the terminal answered (`off` turns RIP off too) |
| `utf8` | `on`, `off` | UTF-8, or CP437 (ASCII where there is no ANSI) |
| `sixel` | `on`, `off` | `off` keeps sixel off; `on` only where the terminal says it has it |
| `rip` | `on`, `off` | the same for RIPscrip |
| `width` | `40`, `80` | a Commodore's columns |
| `editor` | `full`, `line` | the full-screen editor or the line editor |
| `expert` | `on`, `off` | menus as their prompt alone, `?` for the rest |

A Commodore or a VT52 uses only the settings that apply to it. The
settings appear as `prefs` at `GET /api/v1/me` and are changed with
`PATCH /api/v1/me` (see [HTTP API](HTTP-API)).

**See also:** [PETSCII and RIP](PETSCII-and-RIP), [Sixel graphics](Sixel-Graphics), [Configuration](Configuration)
