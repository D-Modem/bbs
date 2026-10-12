The sysop's own screens and features, in JavaScript.  A script draws the
same screens as the rest of the BBS (menus, tables, forms, panels,
messages, the editor), so it works on every terminal: a text terminal in
ANSI, ASCII or PETSCII, a RIP terminal (whose menus are buttons), and a
3270.  It acts as its caller, with their access.

```
; bbs.conf
[general]
scripts_dir = /etc/bbs/scripts

[menu main]
o = script:oneliners One-liners
p = script:poll Today's poll
```

`NAME.js` in `scripts_dir` is the script `script:NAME` runs (names are
lower case letters, digits, `_` and `-`).  A `logon.js` there runs after
every login, before the menus.  `bbs check` compiles every script and
refuses a configuration whose scripts do not compile or whose menus name
a script that is not there; a script changed while the BBS runs is
compiled again at its next run.  `scripts.sample` has two to start from:
one-liners and a poll.

## What a script is

[goja](https://github.com/dop251/goja)'s JavaScript: ECMAScript 5.1 and
much of 6 (`let`, arrow functions, classes, `Map`, template strings), in
strict mode.  A script has no files, network, processes or timers: only
the objects below.  It may compute for two seconds between one screen and
the next (the caller's time at a screen is not counted); past that it is
stopped and the caller told.  A script that fails is logged and the
caller told (a sysop is told why); the caller hanging up or going idle
ends it, whatever it catches.  `exit()` ends it early.

```js
// a guestbook in eight lines
var lines = data.get("lines") || [];
var k = ui.menu({title: "Guestbook", intro: lines.slice(-10),
	items: [{key: "s", label: "Sign it"}, {key: "q", label: "Quit"}]});
if (k === "S") {
	var v = ui.form({title: "Sign", fields: [{name: "line", label: "Your words", width: 60}]});
	if (v && v.line) data.set("lines", lines.concat(user.name + ": " + v.line).slice(-100));
}
```

## ui: the screens

| | returns | |
|---|---|---|
| `ui.menu({title, intro, items: [{key, label}], prompt, screen})` | the key chosen (upper case), `""`, or `ui.BACK` (a 3270's PF3) | `intro`: lines before the items; `screen`: a display file in their place |
| `ui.table({title, note, columns: [{title, width, right, wide}], rows, empty, pick, prompt, actions, more, less})` | `{action, row, num, text}` | `rows`: arrays of cells, or `{num, cells, mark, hot}`; `pick` makes rows choosable (`action` is `ui.PICK`, `row` the index, -1 for a number not shown, `num` what was typed); `wide` columns only on screens of 60 columns or more; `more`/`less` say there are pages after and before (`ui.MORE`, `ui.LESS`) |
| `ui.message({where, from, to, subject, date, body, actions, default, quit})` | the action chosen | |
| `ui.form({title, intro, fields: [{name, label, prompt, width, secret, value}], submit})` | the values by name, or `null` if the caller gave up | one question a field on a text terminal, one screen on a 3270 |
| `ui.panel({title, lines, actions})` | the action chosen, `ui.BACK`, or `""` | |
| `ui.edit({title, to, subject, body, quote})` | `{to, subject, body}`, or `null` | the full-screen editor, the line editor or the 3270's; `quote` is for the quote window |
| `ui.confirm(question, default)` | true or false | |
| `ui.say(text, tone)` | | a line now (a 3270: on the next screen's message line) |
| `ui.show(title, lines)` | | lines until read |
| `ui.screen(name)` | whether there was one | a display file of `screens_dir` |
| `ui.kind`, `ui.pageSize`, `ui.narrow` | | `"text"` or `"3270"`; rows a table shows; under 60 columns |

Lines are strings (`\n` makes more) or `{label, text, tone, bar}`: a
label and its value, a tone (`plain`, `dim`, `bright`, `good`, `bad`,
`warn`), a bar from 0 to 1.  Actions are `{id, label, key, pf, short}`: a
text terminal's key, a 3270's PF key (an action without one is not on a
3270), `short` for the 3270's key line.  On a text terminal `Q` (and Enter
on an empty prompt) goes back; on a 3270, PF3.

## The BBS

| | |
|---|---|
| `bbs.name`, `bbs.sysop`, `bbs.location`, `bbs.log(text)` | the system; a line in its log |
| `user.id`, `.name`, `.location`, `.level`, `.flags`, `.sysop`, `.calls`, `.posts`, `.since`, `.lastCall`, `user.has(require)` | the caller; `has` takes an [access requirement](Access-Levels) such as `"20 A"` (level 20 or more, and flag A) |
| `node.number`, `node.doing(text)`, `node.who()`, `node.message(n, text)` | the caller's node: what *Who's online* says they are doing; the nodes; a line to a node |
| `msg.areas()`, `msg.list(area, after, limit)`, `msg.get(id)` | the message areas the caller reads, their messages (up to 200 at a time, from after an id), one message (`null` if not theirs to read) |
| `msg.post(area, {to, subject, body, replyTo})`, `msg.mail({to, subject, body})` | post (the message's id) and mail, as the caller: refusals are thrown |
| `data.get(name)`, `data.set(name, value)`, `data.remove(name)`, `data.keys(prefix)` | what the script keeps for everyone: any JSON value up to 64 KB under a name of up to 100 characters, 1000 names |
| `data.user.get` ... | the same, for the caller alone (100 names) |
| `console.log(...)` | the BBS's log |

What a script keeps is in the database (`script_data`), by the script's
name: a renamed script starts afresh.  A user's data goes with their
account.

**See also:** [Customization](Customization), [Development](Development#screens)
