Telnet callers whose terminal has sixel graphics (xterm `-ti vt340`,
mlterm, foot, WezTerm, Windows Terminal, iTerm2, SyncTERM and others) see
the logo as a picture above the system's name. The logo is
`logo = /etc/bbs/logo.png`, or, if that is not set, the system's name drawn
in a pixel font.

The BBS asks every ANSI terminal for its device attributes (`CSI c`;
sixel is attribute 4) and for its character cell size in pixels
(`CSI 16 t`, to fit the logo to the window), so nothing changes for
terminals without sixel. A caller can turn sixel off (or on, if their
terminal has it) in their profile, and the choice is kept for their later
calls.

`sixel = auto` is the default for telnet listeners, and `no` for raw and
rlogin listeners, because a picture is slow over a modem.

## Pictures

Pictures on the [image boards](Image-Boards) are shown with sixel too.
Terminals without sixel get the best they can show: kitty's graphics
protocol on kitty and Ghostty, iTerm2's inline images on iTerm2 and
mintty, and coloured half blocks on any other UTF-8 terminal with colour.
*Pictures on/off* (**M**) in the profile turns them off for a caller.
3270s have their own ways of showing pictures, with vector graphics or
blocks (see [TN3270](TN3270#pictures)).

**See also:** [TN3270 graphics](TN3270), [Configuration](Configuration), [Image boards](Image-Boards)
