MSN 2.0 (late 1996), and the 2.5 and 2.6 that came with Windows 98,
changed The Microsoft Network into internet services:

- Web pages in the MSN Program Viewer (Internet Explorer inside).
- Newsgroups for the bulletin boards.
- IRC for chat.
- Internet mail.

Members signed in to each service with the MSN security package. This
package is NTLM with a different name ("MSN", with the old codename
Sicily). The password never goes on the network. Only an answer to a
challenge goes, which the client calculates from the hash of the
password. The BBS answers this sign-in on its mail, news and web servers.
Thus, the programs of MSN 2.x sign in to the BBS as they did to MSN. This
is early work. We did not try it with a real client yet.

## Turning it on

```ini
[security]
ntlm = yes
```

The sign-in checks the LAN Manager and NT hashes of each password. The
BBS keeps these hashes when this setting is on, or when it serves MSN
Classic (the 1996 client of MSN Classic signs in in the same way). The
BBS makes the hashes the next time that the user types the password in
clear text. That occurs when the user signs in with a different method
(telnet, the web form, the plain password of a mail program) or sets a
password. Until then, the NTLM sign-in of that member fails, and the log
gives the reason.

The hashes are equivalent to passwords, and the LM hash is a weak hash.
Thus, you must protect the database as you protect a password file.

## What signs in

The member ID is the BBS name (or the mail address, or the name with a
domain). The password is the BBS password.

- **Mail**: POP3 and IMAP, and SMTP on the submission listener, offer
  `NTLM` and `MSN` adjacent to `PLAIN`. In Outlook Express (or Internet
  Mail), give the servers of the BBS to the account. For incoming mail,
  select "Log on using Secure Password Authentication". For the outgoing
  server, select the same option under "My server requires
  authentication".
- **Newsgroups**: the `AUTHINFO GENERIC NTLM` (or `MSN`) of NNTP, as on
  the news servers of Microsoft. In Internet News (or Outlook Express),
  select "This server requires me to log on" and "Log on using Secure
  Password Authentication".
- **The web forum**: `/forum/login/ntlm` asks the browser for NTLM (or
  MSN). The sign-in page has a link to it. Internet Explorer, the browser
  of the Program Viewer, answers with the name and password that it
  already has (from MSN or Windows), or it asks for them. The exchange
  must use one connection from start to end. Thus, a proxy in front of
  the BBS must keep the connections as they are.
- **Chat**: Comic Chat, the chat program of MSN 2.x, is an IRC client.
  Point it at the IRC server of the BBS ([IRC](IRC)). It signs in with a
  password.

A server with `tls = required` asks for TLS before all sign-ins, also
before NTLM. Programs for Windows 95 and 98 cannot use the TLS of a
modern server. Thus, keep TLS optional on the servers that these clients
use.

## Not served yet

- The pages of the Program Viewer: the MSN start page, its channels and
  OnStage shows, and the MSN Sign-In dialer. We did not find a document
  that tells what the Viewer asks for first. It is necessary to examine
  the client.
- DPA (Distributed Password Authentication), the other package that the
  later Windows 98 mail of MSN used for Secure Password Authentication.
  DPA is not NTLM, and there is no document for it.
- The 16-character passwords of MSN: for the LM hash, the security
  package spread the 15th and 16th characters over the first 14.
  Passwords longer than 14 characters sign in with the NT hash. For this,
  the client must send the NT hash.

MSN 2.0 kept the Exchange mail of MSN Classic for its members.
[MSN Classic](MSN-Classic) serves that mail.
