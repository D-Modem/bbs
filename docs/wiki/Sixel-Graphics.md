## Sixel graphics

Telnet callers whose terminal has sixel graphics (xterm `-ti vt340`,
mlterm, foot, WezTerm, Windows Terminal, iTerm2, SyncTERM, ...) see the
logo as a picture above the system's name: `logo = /etc/bbs/logo.png`, or
without one the name drawn in a pixel font.  The BBS asks every ANSI
terminal for its device attributes (`CSI c`; sixel is attribute 4) and its
character cell in pixels (`CSI 16 t`, to fit the logo to the window), so
nothing changes for terminals without sixel.  A caller turns it off (or on,
if their terminal has it) in their profile.

`sixel = auto` is the default for telnet listeners and `no` for raw and
rlogin ones: a picture is slow over a modem.

**See also:** [TN3270 graphics](TN3270), [Configuration](Configuration)
