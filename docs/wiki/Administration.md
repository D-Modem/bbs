Everything is done with the `bbs` command, on the configuration the BBS
runs with (`-config FILE`; without it, the defaults and `bbs.db` here).
These are safe to run while the BBS is serving.

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

Users add their own from their profile, or by the API: [SSH](SSH).

## Message areas

```
bbs area list
bbs area add NAME "DESCRIPTION" [-sysop-only]
bbs area del NAME                   # and its messages
bbs area newsgroup AREA GROUP       # make it a newsgroup (- : no longer)
bbs area echo AREA TAG              # make it a FidoNet echo (- : no longer)
```

A sysop also adds areas from the BBS (`+` in the message areas) and
deletes any message.  Newsgroups: [Newsgroups and NNTP](Newsgroups-and-NNTP);
echoes: [FidoNet](FidoNet).

## File areas

```
bbs filearea list
bbs filearea add NAME "DESCRIPTION" [-sysop-upload]
bbs filearea del NAME
bbs file add AREA PATH ["DESCRIPTION"]   # as the first sysop
bbs file list AREA
bbs file del AREA NAME
```

See [Files and FTP](Files-and-FTP).

## Mail exchange

```
bbs news sync [PEER]                # exchange articles with news peers now
bbs fido poll [LINK]                # call FidoNet links now
bbs fido scan [LINK]                # pack new mail into the outbound
bbs fido toss [LINK]                # take in what has come
```

The server does all of these on its own schedule; these are for now.

## The rest

```
bbs check                           # read the configuration, show its warnings
bbs init                            # make the database, with two areas
bbs [serve]                         # answer callers
```

**See also:** [Running](Running), [Configuration](Configuration)
