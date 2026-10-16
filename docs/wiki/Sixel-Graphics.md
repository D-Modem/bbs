## Sixel graphics

Telnet callers whose terminal has sixel graphics (xterm `-ti vt340`,
mlterm, foot, WezTerm, Windows Terminal, iTerm2, SyncTERM, ...) see the
logo as a picture above the system's name: `logo = /etc/bbs/logo.png`, or
without one the name drawn in a pixel font.  The BBS asks every ANSI
terminal for its device attributes (`CSI c`; sixel is attribute 4) and its
character cell in pixels (`CSI 16 t`, to fit the logo to the window), so
nothing changes for terminals without sixel.  A caller turns it off (or on,
if their terminal has it) in their profile, and it stays off on their
later calls.

`sixel = auto` is the default for telnet listeners and `no` for raw and
rlogin ones: a picture is slow over a modem.

## Pictures

The [image boards](Image-Boards)' pictures are shown with sixel too, and
on terminals without it as they can: kitty's graphics protocol on kitty
and Ghostty, iTerm2's inline images on iTerm2 and mintty, and coloured
half blocks on any other UTF-8 terminal with colour.  *Pictures on/off*
(**M**) on the profile turns them off for a caller.  3270s have pictures
of their own: vector graphics, or blocks (see [TN3270](TN3270#pictures)).

**See also:** [TN3270 graphics](TN3270), [Configuration](Configuration), [Image boards](Image-Boards)
