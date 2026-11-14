The BBS asks the terminal of each caller what it is, in the same way as
DEC terminals were asked. Then it adapts to the answer:

- Real VTs (and emulators that are configured as a VT) get their own
  line drawing and character set.
- Monochrome terminals get bold in place of colour.
- A VT52 gets its own escape codes.

The answer of the terminal shows in *Who's online* through the
[HTTP API](HTTP-API) (`terminal`), in the profile of the caller, and in
the log:

```
node 3: terminal VT220 (DA1 62;1;2;6;7;8;9, DA2 1;10;0)
node 4: terminal XTerm(390) (DA1 64;1;2;4;6;9;15;16;17;18;21;22;28, DA2 41;390;0, version XTerm(390))
node 5: terminal tmux 3.4 (DA1 1;2;4, DA2 84;0;0, version tmux 3.4)
```

## The questions

The BBS sends all of these questions together to an ANSI terminal:

| Sent | Asks for | Answered by |
|---|---|---|
| `ENQ` | the answerback message | terminals that have one (PuTTY says `PuTTY`) |
| `CSI c` | primary device attributes (DA1): the VT level and options | all VT100 and later, xterm, most emulators. SyncTERM in its CTerm form |
| `CSI > c` | secondary device attributes (DA2): the model and firmware | VT220 and later, xterm, VTE, tmux, screen, mintty |
| `CSI > 0 q` | XTVERSION: name and version | xterm, tmux, foot, kitty, WezTerm, iTerm2 |
| `CSI 16 t` | the character cell in pixels (for sixel) | xterm and others |
| `CSI 6 n` | the cursor position | all ANSI terminals. When it arrives, the other answers are also there |

The position report comes last. Thus, the BBS waits only for it, and not
for a timeout, also on a terminal that does not answer the other
questions. The first question of the BBS is the cursor position. If a
terminal does not answer it, the terminal is not ANSI. Then the BBS
sends the identify request of a VT52, `ESC Z`. A VT52 answers with
`ESC / Z`. All other terminals show a `Z`, which the BBS then erases.

## What it is called

The BBS gives the terminal a name from the first of these items that
applies:

1. The name in its XTVERSION answer.
2. SyncTERM, from its CTerm attributes.
3. A VT52.
4. A DEC terminal, from its DA2 model (VT100, VT220, VT240, VT320, VT330,
   VT340, VT382, VT420, VT510, VT520, VT525, DECterm) with a DEC firmware
   version.
5. An emulator, from its DA2 (tmux, screen, mintty, rxvt, xterm by its
   patch number, VTE).
6. Its answerback.
7. The VT class from DA1 (VT100, VT102, VT200 class ... VT500 class).

## What it changes

- **DEC character sets.** These rules apply to a VT (DA1 says VT100 or
  later) if its terminal type does not say UTF-8, and if it is not a BBS
  terminal of a PC (`ansi`, `syncterm` and similar types, which use
  CP437):
  - Line drawing and blocks use DEC Special Graphics (`ESC ( 0`, the
    `lqkxjm` characters).
  - On a VT220 or later on an 8-bit line, Latin-1 letters go as 8-bit
    characters (DEC Multinational has them).
  - All other characters become ASCII characters that look almost the
    same.

  The BBS draws ANSI art in CP437 in the same way. It reads typed
  Latin-1 letters as Latin-1. The profile says `DEC graphics`.
- **Monochrome.** A DEC terminal from the VT100 to the VT520, known by
  its model and firmware (not an emulator), without ANSI colour (DA1
  option 22), gets bold for the bright colours. It gets plain text for
  the other colours.
- **Sixel.** The BBS knows of sixel from DA1 option 4 (a VT240, VT330,
  VT340, xterm with sixel enabled, foot, WezTerm, mlterm). It knows the
  cell size from `CSI 16 t`.
- **VT52.** A VT52 (or a VT100 in VT52 mode) gets the clear and cursor
  movement as `ESC H`, `ESC J`, `ESC K`, `ESC Y`, `ESC A`-`D`. It gets no
  attributes, and ASCII only. The BBS does not read its cursor and
  keypad keys as commands.

## Turning it off

```ini
[listen modem]
type = raw
address = 127.0.0.1:2323
vt = no
```

With `vt = no`, the BBS asks none of these questions and changes nothing
(it still checks for sixel with DA1 only). `auto`, the default, asks the
questions.

## What a caller sets

The BBS keeps the settings that a caller changes in the profile with the
account. They apply from the next call:

| Setting | Values | At the next call |
|---|---|---|
| `ansi` | `on`, `off` | ANSI as the caller set it, for all answers from the terminal (`off` also disables RIP) |
| `utf8` | `on`, `off` | UTF-8, or CP437 (ASCII where there is no ANSI) |
| `sixel` | `on`, `off` | `off` keeps sixel disabled. `on` enables it only if the terminal says that it has sixel |
| `rip` | `on`, `off` | the same for RIPscrip |
| `width` | `40`, `80` | the columns of a Commodore |
| `editor` | `full`, `line` | the full-screen editor or the line editor |
| `expert` | `on`, `off` | menus show only as their prompt. `?` shows the remainder |

A Commodore or a VT52 uses only the settings that apply to it. The
settings show as `prefs` at `GET /api/v1/me`. To change them, use
`PATCH /api/v1/me` (refer to [HTTP API](HTTP-API)).

**See also:** [PETSCII and RIP](PETSCII-and-RIP), [Sixel graphics](Sixel-Graphics), [Configuration](Configuration)
