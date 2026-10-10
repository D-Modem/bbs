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
