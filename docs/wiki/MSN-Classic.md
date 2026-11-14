The BBS can be The Microsoft Network as it was from 1995 to 1998, for the
MSN client that came with Windows 95 ("MSN Classic"). These are the
functions:

- A caller signs in with the BBS name and password.
- MSN Central is the BBS.
- The Categories folder holds the message areas of the BBS (a folder for
  each area group).
- Each area opens as an MSN bulletin board. It lists the messages with
  their replies below them. The reader opens a message. The Compose
  window makes new posts and replies. A member can delete the messages
  of that member.
- MSN Today lists the boards with the newest messages.
- The Inbox (Microsoft Exchange, with the MSN service) is the BBS mail of
  the caller.
- The Chat folder holds the chat rooms of the BBS. MSN callers talk with
  all other users in these rooms.
- The Files folder holds the file areas. Callers can download their
  files.
- Find searches all of these items.

This is early work. We did not try it with a real client yet.

## Over TCP

```ini
[marvel]
address = :569
```

| Setting | Default | What it sets |
|---|---|---|
| `address` | `:569` | the address on which to listen |
| `max_clients` | `64` | the maximum number of clients at one time |
| `max_per_ip` | `4` | the maximum number of clients from one address |

You cannot give a port to the client: it always uses 569. It connects to
the name in its Dial-Up Networking settings for "The Microsoft Network"
(by default `gateway.moswest.msn.net`). Point that name at the BBS in the
hosts file of the machine of the client (`C:\Windows\hosts`), or give the
[DNS server](DNS) of the BBS to the machine.

## By modem

The client of a modem caller sends a carriage return and waits for
`COM`. Then it uses the MSN packets. A listener with `frontend = marvel`
answers in that way (telnet, in binary, or raw). Thus, a modem bridge
such as D-Modem can give the call to it, as it does for the text BBS:

```ini
[listen msn]
type = raw
address = 127.0.0.1:2325
frontend = marvel
```

Point the phone number of the client at the line that connects to that
listener.

## What callers get

- **Signing in**: the member ID and password of the Sign In dialog are
  those of a BBS account. An incorrect password, an unknown name, a
  locked account and a throttled address each get the applicable message
  of the client. Failed sign-ins count against the address, as on all
  other routes. A client that signed in is a node, and it shows in Who's
  On as `MSN`. The Password dialog changes the BBS password. The billing
  pages show the name and location of the account, and keep nothing.
- **The 1996 client** (Windows 95 OSR2, MSN 1.3) signs in with NTLM. It
  never sends the password. It sends only an answer to a challenge, which
  it calculates from the LAN Manager hash of the password. The BBS keeps
  that hash and the NT hash for each account. It makes them each time
  that the user types the password in clear text (a sign-in with a
  different method, or a new password). The hashes are bound to the
  password. Thus, if a password changes in a location that does not make
  the hashes, the BBS does not use them until the user types the
  password again.

  If the BBS does not keep the hashes of an account yet, this client
  gets "Password not valid". In that case, first sign in one time by
  telnet, the web or the 1995 client. The BBS keeps the hashes only
  while it serves MSN Classic (a `[marvel]` section or a
  `frontend = marvel` listener), or while `[security] ntlm` is on
  ([MSN 2.x](MSN-2)). The hashes are equivalent to passwords, and the LM
  hash is a weak hash. Thus, you must protect the database as you
  protect a password file. After the sign-in, the Password dialog and
  the billing pages go sealed (RC4) with the key of the sign-in, as the
  client expects.
- **The directory**: the BBS lists only the areas that the caller can
  read. Go words are the names of the areas and groups (with upper or
  lower case), and `today`.
- **Bulletin boards**: up to the newest some hundred messages of an area
  (the quantity that fits in one reply). Bodies are plain text. If the
  caller cannot post on a board, the board is read-only in the client. A
  post can come as plain or rich text (compressed or not). The BBS keeps
  it as plain text, with its reply threading. The BBS refuses
  attachments.
- **E-mail**: the Exchange Inbox gets the BBS mail of the caller. When
  the caller reads a message there, the BBS marks it as read. Mail sent
  from Exchange goes out as BBS mail: to a member by name, or to an
  internet address if the BBS sends e-mail. The BBS keeps rich text as
  plain text. It removes an attachment and adds a line that tells this.
  If the BBS cannot send to some recipients, the other recipients still
  get the mail. The sender gets a note from Postmaster with the names of
  the recipients that did not get it. When the caller deletes a message,
  the BBS moves it to the trash of the BBS.
- **The address book**: the members of the BBS (not locked accounts or
  QWK nodes). The BBS uses them for the member list of Exchange, for
  names typed into To:, and for the Member Properties sheet (name,
  location and the About text). The address book also has the saved
  addresses of the caller. Thus, a saved name typed into To: finds its
  address on all PCs. The saved addresses are the same as those in the
  Address Book of CompuServe. When the caller sends mail from Exchange
  to an internet address, the BBS adds the address to them (unless its
  name or address is already there). The Personal Address Book of
  Exchange stays on the PC of the caller. The client never tells the
  server what it adds there.
- **Chat**: the Chat folder (go word `chat`, in MSN Central and in
  Categories) lists the main room, the permanent rooms and the rooms in
  use. It shows the topic of each room and the number of users in it.
  When the caller opens a room, the caller joins it in the chat window,
  with a list of all the users that are already there. MSN callers share
  rooms with telnet, IRC and all other callers, and with linked rooms on
  other networks. `/me` at the start of a line is an action. The BBS
  does not list sysop rooms. Callers cannot make rooms from the client.

- **Files**: the Files folder (go word `files`, in MSN Central and in
  Categories) has a folder for each file area from which the caller can
  download. Each file is a Download-and-Run file, as the downloads of
  MSN were. When the caller opens it, the File Transfer Manager of the
  client gets it, with its progress window. Then the client opens it as
  Explorer does (a program runs. A `.zip` opens in the program that the
  PC uses for these files). The file goes into the temporary folder of
  the PC. Thus, if a caller wants to keep it, the caller must save it
  from there. Each download counts against the daily limit and ratio of
  the caller, as on all other routes.
- **Find**: Find > The Microsoft Network searches the names and
  descriptions of the directory: folders, boards, chat rooms, file
  libraries and files (not the messages in a board). The words and
  wildcards operate as the help of the dialog tells. A word matches full
  words. `*` and `?` replace letters. A quoted phrase matches its words
  in sequence. The "of type" list makes the search narrower (bulletin
  boards, chat rooms, "Kiosks and other files" for the files, newsgroups
  for the areas that carry one). No item here has a place. Thus, a
  search with Place filled in finds nothing. A search gives a maximum of
  200 results.

Not served yet: the sign-up wizard and the Media Viewer title of MSN
Today.

## Where the protocol comes from

The [marvel-msn](https://github.com/gabspeck/marvel-msn) project
reverse-engineered the protocol (codename Marvel). We wrote this server
from the notes and wire captures of that project. We do not use code
from that project.
