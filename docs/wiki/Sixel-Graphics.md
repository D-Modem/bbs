Some terminals have sixel graphics (xterm `-ti vt340`, mlterm, foot,
WezTerm, Windows Terminal, iTerm2, SyncTERM and others). Telnet callers
with these terminals see the logo as a picture above the name of the
system. The logo is `logo = /etc/bbs/logo.png`. If you do not set it, the
logo is the name of the system, drawn in a pixel font.

The BBS asks each ANSI terminal for its device attributes (`CSI c`.
Sixel is attribute 4). It also asks for the size of the character cell in
pixels (`CSI 16 t`), to fit the logo to the window. Thus, nothing changes
for terminals without sixel. A caller can disable sixel (or enable it, if
the terminal has it) in the profile. The BBS keeps this selection for the
later calls of the caller.

`sixel = auto` is the default for telnet listeners. `no` is the default
for raw and rlogin listeners, because a picture is slow over a modem.

## Pictures

The BBS also uses sixel to show pictures on the [image boards](Image-Boards).
Terminals without sixel get the best format that they can show:

- The graphics protocol of kitty on kitty and Ghostty.
- The inline images of iTerm2 on iTerm2 and mintty.
- Coloured half blocks on all other UTF-8 terminals with colour.

*Pictures on/off* (**M**) in the profile disables pictures for a caller.
3270s have their own methods to show pictures, with vector graphics or
blocks (refer to [TN3270](TN3270#pictures)).

**See also:** [TN3270 graphics](TN3270), [Configuration](Configuration), [Image boards](Image-Boards)
