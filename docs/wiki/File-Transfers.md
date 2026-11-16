Callers can use FTP. They can also move files over their own connection
to the BBS. This page gives the transfer protocols for text callers, and
IND$FILE for 3270 callers.

## Text callers

Text callers (telnet, modem, rlogin) use this procedure:

1. To download a file, open the details of the file and push *D*.
2. To upload a file, push *U* in an area.
3. Select a protocol:

| Key | Protocol | Notes |
|---|---|---|
| X | XMODEM | CRC, or checksum if the terminal program asks for it. 128-byte blocks |
| 1 | XMODEM-1K | 1K blocks |
| Y | YMODEM | batch: names and exact sizes |
| Z | ZMODEM | batch, CRC-32, error recovery. Most terminal programs start it automatically |
| K | Kermit | for 7-bit lines, and for all other cases where the other protocols fail |

XMODEM does not send a file name. Thus, the BBS asks for a name. The BBS
removes the path from names that a terminal program sends
(`C:\DOS\GAME.ZIP` becomes `GAME.ZIP`). It also removes the characters
that the file areas do not accept. An upload goes through the same
checks, size limit and daily allowance as an FTP upload. The BBS keeps the
upload only if it is complete.

XMODEM, YMODEM and ZMODEM must have an 8-bit clean line: a modem, rlogin,
or telnet in binary mode. The BBS asks for binary mode. If a client does
not accept binary mode, the BBS shows a warning to the client.

## 3270 callers

3270 callers use the file transfer of their emulator, IND$FILE. Use this
procedure:

1. Go to a file area (8 on the main menu, then S adjacent to an area).
2. Put the cursor on the command line.
3. Start a transfer. Use *send* to upload into the area, or *receive* to
   download one of its files. Use the name of the file as the host file
   name.

The emulator types `IND$FILE PUT name` or `IND$FILE GET name`. The BBS
does the work of the host in DFT mode (structured fields), as the
IND$FILE of TSO does.

Binary mode keeps a file exactly as it is. ASCII with CR/LF stores text
with CR LF line ends, and gives it back in the format that the PC
expects. Both the TSO and VM/CMS host types operate. The emulator must
support DFT. x3270, c3270, wc3270 and PCOMM support it. *Your terminal*
shows reply 95, Distributed Data Management.

**See also:** [Files and FTP](Files-and-FTP), [TN3270](TN3270)
