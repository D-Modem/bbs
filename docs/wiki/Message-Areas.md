What a caller does in the message areas, on any terminal: the letters
are a text terminal's, the PF keys a 3270's.

## The areas

```
Message areas
    Area                 Description                           Msgs   New
  1 General              Talk                                    42 *   3
  2 Other                More talk                               17

Area number, [N]ew scan, [T]o you, [F]ind, [S]can setup, [Q]uit:
```

| | key | PF | |
|---|---|---|---|
| an area | its number | `S` by it | its messages, a page at a time |
| new scan | `N` | PF5 | reads the new messages of each area in the scan, area after area |
| to you | `T` | PF4 | the newest 100 messages addressed to you (in any case), in the areas you read; `*` those you have not read |
| find | `F` | PF10 | messages whose subject, text or author has the words in them (in any case, two characters or more), newest first, up to 100 |
| scan setup | `S` | PF11 | which areas the new scan goes through |
| add area | `+` | PF6 | the sysop only |

## Scan setup

Every area is in the new scan until it is taken out: the area's number
(or `S` by it on a 3270) switches it, `A` (PF5) puts all in, `X` (PF6)
takes all out.  Areas marked `*` are in.  The choice is kept with the
account.

Areas a sysop put in groups are listed by group (those in none first),
with a Group column.

## In an area

The messages a page at a time (Enter for more on a text terminal,
PF7/PF8 on a 3270); `*` marks those not read yet.  A message's number
reads it, even one not on the page.  `R` (PF5) reads from the first new
one, `P` (PF6) posts, `L` (PF9) posts a poll: a question and 2 to 10
answers, parted by `|`.

## Reading

| | key | PF |
|---|---|---|
| next | `N`, Enter, space | PF11, Enter |
| previous | `P` | PF10 |
| reply in the area | `R` | PF4 |
| reply by mail | `M` | PF5 |
| delete (yours, or any for the sysop) | `D` | PF9 |
| vote it up or down (or take your vote back) | `V` | PF1 |
| the thread: every message from the one it began with, replies under what they answer | `T` | PF6 |
| the message this answers | `O` | PF2 |
| answer a poll (again: your answer changes) | `A` | PF12 |
| back | `Q` | PF3 |

A message's votes show by its number (`+3 -1`), a poll's answers under
its text with each one's share, yours marked `*`.

## Aliases and signatures

On the profile, `I` sets an alias and `G` a signature (up to three
lines).  In an area whose sysop allows aliases, posting asks whether to
post under the alias: the message is from the alias (in newsgroups and
echoes too), while the sysop still knows whose it is.  An alias is
checked like a new user's name and may not be anyone's name or alias.
The signature goes under every post and mail written on the BBS.

Reading in an area marks it read up to there.  Reading what *To you* or
*Find* found does not: the new scan still finds those messages new.

Writing: [Writing messages](Writing-Messages).

**See also:** [Administration](Administration#message-areas), [TN3270](TN3270)
