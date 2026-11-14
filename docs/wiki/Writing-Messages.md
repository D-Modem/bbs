Callers write posts and mail in one of three editors. The editor
changes with the terminal:

- The full-screen editor on ANSI terminals.
- The line editor on other text terminals.
- The editor screen on a 3270.

Each editor first asks for `To` and `Subject`. For `To`, Enter keeps the
value that the editor shows.

## The full-screen editor

The full-screen editor is for an ANSI terminal (not a VT52 or a
Commodore) of 40 columns and 12 rows or more:

```
To: alice  Subject: Re: The meeting
-------------------------------------------------------------------------------
alice wrote:
> Thursday at eight?
Thursday suits me.
_



^Z save  ^X give up  ^Y delete line  ^L redraw  ^Q quote                    4/5
```

| Key | What it does |
|---|---|
| typed text | goes in at the cursor. If a word goes past the edge, it wraps to the next line |
| cursor keys, Home, End, PgUp, PgDn | move the cursor |
| Enter | starts a new line (the line breaks at the cursor) |
| Backspace, Delete (or ^D) | removes a character, or joins two lines |
| ^Y | deletes the line |
| ^Q | opens the quote window (for a reply). Up and Down select a line of the initial message. Enter copies it into your message. ^Q or ESC closes the window |
| ^Z | saves the message (`/S` alone on a line, then Enter, also saves it) |
| ^X (or ^A) | stops the message without a save, after a Y |
| ^L | draws the screen again |

A line can have up to 79 characters (fewer on a narrower screen). A
message can have the same number of lines as the line editor permits.
The BBS holds messages from other nodes until you save the message.

## The line editor

The BBS uses the line editor on other terminals, or if the caller selects
it. Type each line and send it with Enter. Words wrap at the edge. These
commands operate when they are alone on a line:

| Command | What it does |
|---|---|
| `/S` | saves the message |
| `/A` | stops the message without a save |
| `/L` | lists the text that you wrote |
| `/D` | deletes the last line |
| `/C` | clears all the text |

A reply starts with a quote of the message that you answer.

## Choosing

On an ANSI terminal, `[E]ditor full/line` in *Your profile* changes
between the two editors. The BBS keeps the selection ([`editor`](Terminal-Identification#what-a-caller-sets)).

## On a 3270

A 3270 has an editor screen with `To`, `Subject` and a screen of lines
for the text. PF5 sends the message. PF3 stops the message without a
save. A reply starts with its quote.

**See also:** [TN3270](TN3270), [Terminal identification](Terminal-Identification)
