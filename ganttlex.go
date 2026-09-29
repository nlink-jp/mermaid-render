package mermaidrender

// The gantt lexer follows gantt.jison (mermaid 12.0.0) rule by rule, in its
// order, with jison's behaviour (see erlex.go). Its exclusive states are
// the click statement's (click, href, callbackname, callbackargs) and the
// accessibility ones. Everything a keyword rule does not take up to a ":"
// is a task's text; a ":" and what follows up to "#", ";" or the line's end
// is its data.
var ganttRules = map[string][]lexRule{
	"INITIAL": {
		// %%{ opens a directive; prepare has removed whole directive lines,
		// and an undeclared state has no rules: whatever follows is an error.
		{re: lexRE(`%%\{`), kind: "open_directive", push: "open_directive"},
		{re: lexRE(`accTitle\s*:\s*`), kind: "acc_title", push: "acc_title"},
		{re: lexRE(`accDescr\s*:\s*`), kind: "acc_descr", push: "acc_descr"},
		{re: lexRE(`accDescr\s*\{\s*`), push: "acc_descr_multiline"},
		// Comments, three ways (the (?!\{)* is a zero-width repeat).
		r(`%%[^\n]*`, ""),
		r(`[^\}]%%*[^\n]*`, ""),
		r(`%%*[^\n]*[\n]*`, ""),
		r(`[\n]+`, "NL"),
		r(`\s+`, ""),
		r(`%%[^\n]*`, ""),
		{re: lexRE(`href\s+"`), push: "href"},
		{re: lexRE(`call\s+`), push: "callbackname"},
		{re: lexRE(`click\s+`), push: "click"},
		r(`gantt\b`, "gantt"),
		r(`dateFormat\s[^#\n;]+`, "dateFormat"),
		r(`inclusiveEndDates\b`, "inclusiveEndDates"),
		r(`topAxis\b`, "topAxis"),
		r(`axisFormat\s[^#\n;]+`, "axisFormat"),
		r(`tickInterval\s[^#\n;]+`, "tickInterval"),
		r(`includes\s[^#\n;]+`, "includes"),
		r(`excludes\s[^#\n;]+`, "excludes"),
		r(`todayMarker\s[^\n;]+`, "todayMarker"),
		r(`weekday\s+monday\b`, "weekday_monday"),
		r(`weekday\s+tuesday\b`, "weekday_tuesday"),
		r(`weekday\s+wednesday\b`, "weekday_wednesday"),
		r(`weekday\s+thursday\b`, "weekday_thursday"),
		r(`weekday\s+friday\b`, "weekday_friday"),
		r(`weekday\s+saturday\b`, "weekday_saturday"),
		r(`weekday\s+sunday\b`, "weekday_sunday"),
		r(`weekend\s+friday\b`, "weekend_friday"),
		r(`weekend\s+saturday\b`, "weekend_saturday"),
		r(`\d\d\d\d-\d\d-\d\d\b`, "date"),
		r(`title\s[^\n]+`, "title"),
		r(`accDescription\s[^#\n;]+`, "accDescription"),
		r(`section\s[^\n]+`, "section"),
		r(`[^:\n]+`, "taskTxt"),
		r(`:[^#\n;]+`, "taskData"),
		r(`:`, ":"),
		r(`(DOT)`, "INVALID"),
	},
	"href": {
		{re: lexRE(`"`), pop: 1},
		r(`[^"]*`, "href"),
	},
	"callbackname": {
		{re: lexRE(`\(\s*\)`), pop: 1},
		{re: lexRE(`\(`), pop: 1, push: "callbackargs"},
		r(`[^(]*`, "callbackname"),
	},
	"callbackargs": {
		{re: lexRE(`\)`), pop: 1},
		r(`[^)]*`, "callbackargs"),
	},
	"click": {
		{re: lexRE(`\s`), pop: 1},
		r(`(NOTSPACE)*`, "click"),
	},
	"acc_title": {
		{re: lexRE(`[^\n]*`), kind: "acc_title_value", pop: 1},
	},
	"acc_descr": {
		{re: lexRE(`[^\n]*`), kind: "acc_descr_value", pop: 1},
	},
	"acc_descr_multiline": {
		{re: lexRE(`\}`), pop: 1},
		r(`[^\}]*`, "acc_descr_multiline_value"),
	},
}

// lexGantt turns the prepared lines into tokens, with the line end mermaid
// adds (Diagram.fromText).
func lexGantt(lines []srcLine) ([]erToken, error) {
	last := 0
	if len(lines) > 0 {
		last = lines[len(lines)-1].no
	}
	raw := make([]srcLine, 0, len(lines)+1)
	for _, l := range lines {
		raw = append(raw, srcLine{text: l.tail, no: l.no})
	}
	return runLexer(append(raw, srcLine{text: "", no: last}), ganttRules, "EOF")
}
