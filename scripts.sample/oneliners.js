// One-liners: the last lines callers left, and one of yours.
//
// A menu item runs it: o = script:oneliners One-liners
// It works on every terminal: text, RIP and 3270.

var MOST = 15; // lines kept

function show() {
	var lines = data.get("lines") || [];
	var shown = lines.length ? lines.map(function (l) {
		return {label: l.who + ":", text: l.text};
	}) : ["No one has left a line yet."];
	return ui.panel({
		title: "One-liners",
		lines: shown,
		actions: [{id: "add", label: "Add a line", key: "a", pf: 5, short: "Add"}]
	});
}

for (;;) {
	node.doing("Reading one-liners");
	if (show() !== "add") break;
	var v = ui.form({
		title: "Your one-liner",
		fields: [{name: "text", label: "Your line", prompt: "Your line (Enter to give up): ", width: 60}],
		submit: "Add"
	});
	if (!v || !v.text) continue;
	var lines = data.get("lines") || [];
	lines.push({who: user.name, text: v.text, when: new Date().toISOString()});
	data.set("lines", lines.slice(-MOST));
	ui.say("Added.", "good");
}
