This page tells what callers can do in the message areas, on all
terminals. The letters are the keys on a text terminal. The PF keys are
the keys of a 3270.

## The areas

```
Message areas
    Area                 Description                           Msgs   New
  1 General              Talk                                    42 *   3
  2 Other                More talk                               17

Area number, [N]ew scan, [T]o you, [F]ind, [S]can setup, [Q]uit:
```

| Action | Key | PF | What it does |
|---|---|---|---|
| an area | its number | `S` adjacent to it | shows its messages, one page at a time |
| new scan | `N` | PF5 | reads the new messages of each area in the scan, one area after the other |
| to you | `T` | PF4 | shows the newest 100 messages to you (with upper or lower case), in the areas that you read. `*` shows the messages that you did not read |
| find | `F` | PF10 | shows the messages that have the words in their subject, text or author (with upper or lower case, two characters or more). The newest come first, with a maximum of 100 |
| scan setup | `S` | PF11 | selects the areas for the new scan |
| add area | `+` | PF6 | for the sysop only |

## Scan setup

All areas are in the new scan until the caller removes them. To put an
area in the scan or remove it, type the number of the area (or `S`
adjacent to it on a 3270). `A` (PF5) puts all areas in. `X` (PF6) removes
all areas. Areas with the mark `*` are in the scan. The BBS keeps the
selection with the account.

If the sysop put areas in groups, the BBS lists them by group, with a
Group column. Areas that are not in a group come first.

## In an area

An area lists its messages one page at a time (Enter for more on a text
terminal, PF7/PF8 on a 3270). A `*` shows the messages that you did not
read yet. To read a message, type its number, also if it is not on the
page. `R` (PF5) reads from the first new message. `P` (PF6) posts a
message. `L` (PF9) posts a poll: a question and 2 to 10 answers, with
`|` between them.

## Reading

| Action | Key | PF |
|---|---|---|
| next | `N`, Enter, space | PF11, Enter |
| previous | `P` | PF10 |
| reply in the area | `R` | PF4 |
| reply by mail | `M` | PF5 |
| delete (your messages, or all messages for the sysop) | `D` | PF9 |
| vote for or against it (or remove your vote) | `V` | PF1 |
| the thread: all the messages from the first one, with replies below the messages that they answer | `T` | PF6 |
| the message that this message answers | `O` | PF2 |
| answer a poll (if you answer again, your answer changes) | `A` | PF12 |
| back | `Q` | PF3 |

The votes for a message show adjacent to its number (`+3 -1`). The
answers of a poll show below its text, with the share of each answer. The
mark `*` shows your answer.

## Aliases and signatures

In the profile, `I` sets an alias and `G` sets a signature (up to three
lines). If the sysop permits aliases in an area, the BBS asks if you want
to post with the alias. Then the message is from the alias (also in
newsgroups and echoes). But the sysop can still see the author. The BBS
checks an alias in the same way as the name of a new user. An alias
cannot be the name or alias of a different user. The signature goes
below each post and mail that you write on the BBS.

When you read in an area, the BBS marks the area as read up to that
point. When you read messages from *To you* or *Find*, the BBS does not
mark them. Thus, the new scan still shows those messages as new.

For how to write messages, refer to [Writing messages](Writing-Messages).

**See also:** [Administration](Administration#message-areas), [TN3270](TN3270)
