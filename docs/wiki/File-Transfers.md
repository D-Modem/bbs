Besides FTP, files move over the caller's own line.

**Text callers** (telnet, modem, rlogin) pick a file's details and press
*D* to download, or *U* in an area to upload, then a protocol:

| key | protocol | |
|---|---|---|
| X | XMODEM | CRC, or checksum if the terminal program asks; 128-byte blocks |
| 1 | XMODEM-1K | 1K blocks |
| Y | YMODEM | batch: names and exact sizes |
| Z | ZMODEM | batch, CRC-32, error recovery; most terminal programs start it by themselves |
| K | Kermit | for 7-bit lines and anything else that fails |

XMODEM sends no name, so the BBS asks for one.  Names a terminal program
sends lose any path (`C:\DOS\GAME.ZIP` becomes `GAME.ZIP`) and characters
the file areas do not take; an upload goes through the same checks, size
limit and daily allowance as one by FTP, and is kept only if it all came.
X/Y/ZMODEM need an 8-bit clean line: a modem, rlogin, or telnet in binary
mode (the BBS asks for it; a client that refuses is warned).

**3270 callers** use their emulator's file transfer (IND$FILE): in a file
area (8 on the main menu, then S by an area), with the cursor on the
command line, start a transfer: *send* to upload into the area, *receive*
to download a file of it, the host file name being the file's name.  The
emulator types `IND$FILE PUT name` or `IND$FILE GET name` and the BBS plays
the host's part in DFT mode (structured fields), as TSO's IND$FILE does.
Binary mode keeps a file as it is; ASCII with CR/LF stores text with CR LF
line ends and gives it back as the PC wants it.  TSO or VM/CMS host type
both work.  The emulator must do DFT (x3270, c3270, wc3270 and PCOMM do;
*Your terminal* shows reply 95, Distributed Data Management).

**See also:** [Files and FTP](Files-and-FTP), [TN3270](TN3270)
