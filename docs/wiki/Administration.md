This page lists the `bbs` commands a sysop uses day to day. Each command
works on the configuration the BBS runs with (`-config FILE`; without it,
the defaults and `bbs.db` in the current directory). All of them are safe
to run while the BBS is serving callers.

## Users

```
bbs user list
bbs user add NAME [-sysop]          # asks for the password (or reads it from a pipe)
bbs user passwd NAME
bbs user lock NAME                  # and unlock
bbs user sysop NAME                 # and unsysop
bbs user del NAME                   # their mail goes; their messages stay
```

## The database

```
bbs db copy DATABASE                # into a new one: a path, postgres://..., mysql://...
```

See [Databases](Databases).

## Access

```
bbs user level NAME N               # 0-255
bbs user flags NAME +AB -C          # or AB: exactly those
bbs user waiting                    # under validated_level
bbs user validate NAME
bbs area require AREA READ POST     # "30 F", sysop, - for none
bbs filearea require AREA DOWNLOAD UPLOAD
```

See [Access levels](Access-Levels).

## SSH keys

```
bbs sshkey add USER FILE            # each authorized_keys line in FILE (- : stdin)
bbs sshkey list [USER]
bbs sshkey del ID
```

Users can also add their own keys from their profile or through the API: see [SSH](SSH).

## Message areas

```
bbs area list
bbs area add NAME "DESCRIPTION" [-sysop-only]
bbs area del NAME                   # and its messages
bbs area newsgroup AREA GROUP       # make it a newsgroup (- : no longer)
bbs area echo AREA TAG              # make it a FidoNet echo (- : no longer)
bbs area group AREA GROUP           # list it under a group (- : none)
bbs area aliases AREA yes|no        # whether its posters may use their aliases
bbs area keep AREA MESSAGES DAYS    # keep the newest MESSAGES, for DAYS (0: no limit)
bbs area purge                      # take away what areas keep no more, now
```

The BBS applies each area's limits every hour while it runs, and `bbs
area purge` applies them at once. Messages beyond the newest `MESSAGES`,
and messages older than `DAYS`, are deleted along with their votes and
polls.

A sysop can also add areas from inside the BBS (`+` in the message areas)
and delete any message. For newsgroups, see
[Newsgroups and NNTP](Newsgroups-and-NNTP); for echoes, see
[FidoNet](FidoNet).

## File areas

```
bbs filearea list
bbs filearea add NAME "DESCRIPTION" [-sysop-upload]
bbs filearea del NAME
bbs filearea group NAME GROUP|-          # a CompuServe forum's library (- : none)
bbs file add AREA PATH ["DESCRIPTION"]   # as the first sysop
bbs file list AREA
bbs file del AREA NAME
```

See [Files and FTP](Files-and-FTP).

## Image boards

```
bbs chan board add DIR TITLE [DESCRIPTION] [-nsfw]   # /DIR/ at /chan/DIR/
bbs chan board set DIR KEY VALUE    # pages, per_page, bump_limit, mods, nsfw, oekaki...
bbs chan board list
bbs chan board del DIR              # its posts and images too
bbs chan ban add ADDRESS DAYS [BOARD|-] [REASON...]
bbs chan ban list | del ID
```

See [Image boards](Image-Boards).

## Mail exchange

The BBS exchanges mail with its peers on its own schedule. These commands
do it now.

```
bbs news sync [PEER]                # exchange articles with news peers now
bbs fido poll [LINK]                # call FidoNet links now
bbs fido scan [LINK]                # pack new mail into the outbound
bbs fido toss [LINK]                # take in what has come
bbs fido node ADDRESS               # a system in the nodelist
bbs fido nodes WORDS...             # systems by name, place or sysop
bbs fido subs LINK                  # the echoes and file echoes a link has
bbs fido link|unlink LINK TAG...    # link it to echoes (filelink, fileunlink)
bbs fido areafix|filefix LINK CMD...  # a request to a link's AreaFix (FileFix)
bbs qwknet call [NET]               # call QWK networks' hubs now
bbs user qwknode|unqwknode NAME     # an account a QWK node calls this hub as
```

## Chat rooms

```
bbs room list                       # the permanent rooms, with their topics
bbs room add ROOM ["TOPIC"]         # kept open when empty (IRC: held by BBSServ)
bbs room topic ROOM ["TOPIC"]       # set its topic (none: clear it)
bbs room del ROOM                   # it goes with its last caller
```

A running BBS picks up these changes at its next start; BBSServ's `ROOM`
command makes them at once. See [IRC](IRC#permanent-rooms).

## The rest

```
bbs check                           # read the configuration, show its warnings
bbs init                            # make the database, with two areas
bbs [serve]                         # answer callers
```

**See also:** [Running](Running), [Configuration](Configuration)
