MSN 2.0 (late 1996) and the 2.5 and 2.6 that came with Windows 98 turned
The Microsoft Network into internet services: web pages in the MSN
Program Viewer (Internet Explorer inside), newsgroups for the bulletin
boards, IRC for chat, and internet mail. Members signed in to each with
MSN's own security package, which is NTLM under another name ("MSN", once
codenamed Sicily): the password never travels, only an answer to a
challenge worked out from its hash. The BBS answers that sign-in on its
mail, news and web servers, so MSN 2.x's programs sign in to the BBS as
they did to MSN. This is early work, not yet tried with a real client.

## Turning it on

```ini
[security]
ntlm = yes
```

The sign-in checks each password's LAN Manager and NT hashes, which the
BBS keeps once this is on (or MSN Classic is served, whose 1996 client
signs in the same way). They are made when the password is next typed in
the clear: when its owner signs in any other way (telnet, the web form, a
mail program's plain password) or sets a password. Until then, that
member's NTLM sign-in fails, and the log says why. The hashes are password
equivalents, the LM hash a weak one, so the database needs guarding as a
password file does.

## What signs in

The member ID is the BBS name (or the mail address, or the name with any
domain), and the password the BBS password.

- **Mail**: POP3 and IMAP, and SMTP on the submission listener, offer
  `NTLM` and `MSN` beside `PLAIN`. In Outlook Express (or Internet Mail),
  give the account the BBS's servers and tick "Log on using Secure
  Password Authentication" for incoming mail, and under the outgoing
  server's "My server requires authentication", the same.
- **Newsgroups**: NNTP's `AUTHINFO GENERIC NTLM` (or `MSN`), as
  Microsoft's news servers had it. In Internet News (or Outlook Express),
  tick "This server requires me to log on" and "Log on using Secure
  Password Authentication".
- **The web forum**: `/forum/login/ntlm` asks the browser for NTLM (or
  MSN), and the sign-in page links to it. Internet Explorer, the Program
  Viewer's browser, answers with the name and password it already has
  (MSN's or Windows'), or asks for them. The exchange needs one connection from start to
  end, so a proxy in front of the BBS has to keep connections as they are.
- **Chat**: Comic Chat, MSN 2.x's chat program, is an IRC client: point it
  at the BBS's IRC server ([IRC](IRC)), which it signs in to by password.

A server with `tls = required` asks for TLS before any sign-in, NTLM's
too, and Windows 95 and 98 programs can't speak the TLS that a modern
server does, so leave TLS optional on the servers these clients use.

## Not served yet

- The Program Viewer's own pages: MSN's start page, its channels and
  OnStage shows, and the MSN Sign-In dialer. What the Viewer asks for
  first isn't documented anywhere found so far; it needs the client
  itself looked at.
- DPA (Distributed Password Authentication), the other package MSN's
  later Windows 98 mail used for Secure Password Authentication. It isn't
  NTLM, and isn't documented.
- MSN's own 16-character passwords: the security package spread the 15th
  and 16th characters over the first 14 for the LM hash. Passwords longer
  than 14 characters sign in by the NT hash, which needs the client to
  send it.

MSN 2.0 kept MSN Classic's Exchange mail for its members; that is served
by [MSN Classic](MSN-Classic).
