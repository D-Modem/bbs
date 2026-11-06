As well as using FTP, callers can move files over their own connection to
the BBS. This page covers the transfer protocols for text callers and
IND$FILE for 3270 callers.

## Text callers

Text callers (telnet, modem, rlogin) open a file's details and press *D*
to download it, or press *U* in an area to upload, and then choose a
protocol:

| Key | Protocol | Notes |
|---|---|---|
| X | XMODEM | CRC, or checksum if the terminal program asks; 128-byte blocks |
| 1 | XMODEM-1K | 1K blocks |
| Y | YMODEM | batch: names and exact sizes |
| Z | ZMODEM | batch, CRC-32, error recovery; most terminal programs start it by themselves |
| K | Kermit | for 7-bit lines and anything else that fails |

XMODEM does not send a file name, so the BBS asks for one. Names that a
terminal program sends lose any path (`C:\DOS\GAME.ZIP` becomes
`GAME.ZIP`) and any characters the file areas do not accept. An upload
goes through the same checks, size limit and daily allowance as an FTP
upload, and is kept only if it arrived complete. XMODEM, YMODEM and ZMODEM
need an 8-bit clean line: a modem, rlogin, or telnet in binary mode (the
BBS asks for binary mode, and warns a client that refuses).

## 3270 callers

3270 callers use their emulator's file transfer, IND$FILE. In a file area
(8 on the main menu, then S beside an area), with the cursor on the
command line, start a transfer: *send* to upload into the area, or
*receive* to download one of its files, with the file's name as the host
file name. The emulator types `IND$FILE PUT name` or `IND$FILE GET name`,
and the BBS plays the host's part in DFT mode (structured fields), as
TSO's IND$FILE does.

Binary mode keeps a file exactly as it is. ASCII with CR/LF stores text
with CR LF line ends and gives it back the way the PC expects. Both the
TSO and VM/CMS host types work. The emulator must support DFT (x3270,
c3270, wc3270 and PCOMM do; *Your terminal* shows reply 95, Distributed
Data Management).

**See also:** [Files and FTP](Files-and-FTP), [TN3270](TN3270)
