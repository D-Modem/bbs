// A poll: one question, a vote each, the results as bars.
//
// The sysop sets the question from the script itself (S on its screen).
// A menu item runs it: p = script:poll Today's poll

var poll = data.get("poll") || {question: "Which terminal do you call with?",
	answers: ["An ANSI terminal", "A Commodore", "A 3270", "Something else"]};
var votes = data.get("votes") || {};

function results() {
	var total = 0, i;
	for (i = 0; i < poll.answers.length; i++) total += votes[i] || 0;
	var lines = [{text: poll.question, tone: "bright"}, ""];
	for (i = 0; i < poll.answers.length; i++) {
		var n = votes[i] || 0;
		lines.push({label: poll.answers[i], text: String(n), bar: total ? n / total : 0});
	}
	lines.push("", total + (total === 1 ? " vote" : " votes"));
	ui.show("Poll results", lines);
}

function vote() {
	var c = ui.table({
		title: poll.question,
		pick: "Your answer",
		columns: [{title: "Answer"}],
		rows: poll.answers.map(function (a) { return [a]; })
	});
	if (c.action !== ui.PICK || c.row < 0) return;
	votes[c.row] = (votes[c.row] || 0) + 1;
	data.set("votes", votes);
	data.user.set("voted", poll.question);
	ui.say("Your vote is counted.", "good");
}

function setQuestion() {
	var v = ui.form({
		title: "A new poll",
		fields: [{name: "q", label: "Question", width: 70},
			{name: "a", label: "Answers", prompt: "Answers (separated by |): ", width: 200}]
	});
	if (!v || !v.q || !v.a) return;
	var answers = v.a.split("|").map(function (s) { return s.trim(); }).filter(Boolean);
	if (answers.length < 2) {
		ui.say("A poll needs two answers or more.", "bad");
		return;
	}
	poll = {question: v.q, answers: answers};
	votes = {};
	data.set("poll", poll);
	data.set("votes", votes);
	ui.say("The new poll is up.", "good");
}

node.doing("At the poll");
if (data.user.get("voted") !== poll.question) vote();
results();
if (user.sysop && ui.confirm("Set a new question?", false)) setQuestion();
