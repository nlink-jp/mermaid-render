package mermaidrender

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

var mmShapeNames = []string{"default", "rect", "rounded", "circle", "cloud", "bang", "hexagon"}

// dumpMindmap prints the tree, one node a line, indented by depth.
func dumpMindmap(m *Mindmap) string {
	var b strings.Builder
	b.WriteString("mindmap")
	if m.title != "" {
		fmt.Fprintf(&b, " title=%q", m.title)
	}
	b.WriteString("\n")
	for _, n := range m.Nodes {
		fmt.Fprintf(&b, "%s%s %q section=%d line=%d\n", strings.Repeat("  ", n.Level+1), mmShapeNames[n.Shape], n.Text, n.Section, n.Line)
	}
	return b.String()
}

// mmTree is the tree in short: depth|shape|text per node.
func mmTree(t *testing.T, src string) string {
	t.Helper()
	d, err := Parse(src)
	if err != nil {
		return "error " + err.(*Error).Kind.String()
	}
	var out []string
	for _, n := range d.(*Mindmap).Nodes {
		out = append(out, fmt.Sprintf("%d|%s|%s", n.Level, mmShapeNames[n.Shape], n.Text))
	}
	return strings.Join(out, "\n")
}

// The reading, case by case as the RFP states it; each case was run
// through mermaid 12.0.0's own parser (the jison grammar and mindmapDb)
// and gave the same tree or an error.
func TestMindmapReading(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{"mindmap\n  root\n    A\n      B\n      C", "0|default|root\n1|default|A\n2|default|B\n2|default|C"},
		// The documentation's "unclear indentation": C joins A.
		{"mindmap\nRoot\n    A\n        B\n      C", "0|default|Root\n1|default|A\n2|default|B\n2|default|C"},
		{"mindmap root", "0|default|root"},
		{"mindmap\n  root((mindmap))", "0|circle|mindmap"},
		// A bare text ends at a bracket: 言語 (Go) is a rounded node "Go".
		{"mindmap\n  root\n    言語 (Go)", "0|default|root\n1|rounded|Go"},
		{"mindmap\n  root\n    A:::urgent", "0|default|root\n1|default|A:::urgent"},
		{"mindmap\n  root\n    A %% c", "0|default|root\n1|default|A %% c"},
		// The shape is the opening delimiter's. A label runs to the first
		// closing character: (-x-) is "x-", and k-)x(- is the node "k-"
		// followed by a cloud.
		{"mindmap\n  r\n    a[x]\n    b(x)\n    c((x))\n    d)x(\n    e))x((\n    f{{x}}\n    g(x(\n    h((x)\n    i{{x]\n    j(-x-)\n    k-)x(-",
			"0|default|r\n1|rect|x\n1|rounded|x\n1|circle|x\n1|cloud|x\n1|bang|x\n1|hexagon|x\n1|cloud|x\n1|circle|x\n1|hexagon|x\n1|default|x-\n1|cloud|x"},
		{"mindmap\n  r\n    [\"only\"]", "0|default|r\n1|rect|only"},
		{"mindmap\n  r\n    a[\"q (x) y\"]", "0|default|r\n1|rect|q (x) y"},
		{"mindmap\n  r\n    a[\"`m d`\"]", "0|default|r\n1|rect|m d"},
		{"mindmap\n  r\n    a[b[c]", "0|default|r\n1|rect|b[c"},
		// A label may span lines.
		{"mindmap\n  r\n    a[x\n   y]", "0|default|r\n1|rect|x\ny"},
		// Classes and icons are read and dropped.
		{"mindmap\n  root\n    A\n    :::urgent big\n    ::icon(fa fa-book)", "0|default|root\n1|default|A"},
		// Blank and comment lines between nodes separate nothing.
		{"mindmap\n  r\n    a\n\n\n    b\n  %% x\n    c   ", "0|default|r\n1|default|a\n1|default|b\n1|default|c"},
		{"%% c\nmindmap\n  r", "0|default|r"},
		// A comment line takes the blank lines right before it
		// (cleanupComments); the first line is trimmed (trimStart).
		{"mindmap\n\n%% c\n  root", "0|default|root"},
		{"  mindmap\n  r", "0|default|r"},
		// Indentation counts JavaScript whitespace characters: U+3000 is one.
		{"mindmap\n  r\n\u3000\u3000\u3000a", "0|default|r\n1|default|a"},
		{"mindmap\n", ""},
		{"---\ntitle: T\n---\nmindmap\n  r", "0|default|r"},
		// mermaid's errors.
		{"mindmap", "error syntax error"},
		{"mindmap\n\n  root", "error syntax error"},
		{"mindmap  \n  r", "error syntax error"},
		{"mindmap\n  r\n  x", "error syntax error"},
		{"mindmap\n    r\n  x", "error syntax error"},
		{"mindmap\n  r\n\tA", "error syntax error"},
		{"mindmap\n  r\n\u3000\u3000a", "error syntax error"},
		{"mindmap\n  r\n    Mindmap tools", "error syntax error"},
		{"mindmap\n  ::icon(fa)\n  r", "error syntax error"},
		{"mindmap\n  r\n    b[x] y", "error syntax error"},
		{"mindmap\n  r\n    a{x}", "error syntax error"},
		{"mindmapx\n  r", "error syntax error"},
	} {
		if got := mmTree(t, c.src); got != c.want {
			t.Errorf("%q:\n got %q\nwant %q", c.src, got, c.want)
		}
	}
}

// Labels: markdown emphasis, escapes and multi-line blocks are refused;
// what mermaid shows as written is drawn as written.
func TestMindmapLabels(t *testing.T) {
	for _, c := range []struct{ label, want string }{
		{"1. 設計", "1. 設計"},
		{"# 見出し", "# 見出し"},
		{"- item", "- item"},
		{"x `code`", "x `code`"},
		{"snake_case_name", "snake_case_name"},
		{"a  b", "a b"},
		{"a<br>b", "a\nb"},
		{"a<br/>b", "a\nb"},
		{"#quot;x#quot;", "\"x\""},
		{"#42;x#42;", "*x*"},
		{"C:\\Users", "C:\\Users"},
		{"**bold**", "error unsupported construct"},
		{"*it*", "error unsupported construct"},
		{"_it_", "error unsupported construct"},
		{"a\\*b", "error unsupported construct"},
		{"<b>x</b>", "error unsupported construct"},
		{"x\n- y", "error unsupported construct"},
		{"x\n---", "error unsupported construct"},
		{"x\n   y", "x\ny"},
		// Found by the specification's independent review against mermaid
		// 12.0.0 in a browser.
		{"_snake_case_", "error unsupported construct"},
		{"__init_db__", "error unsupported construct"},
		{"use _my_var_ here", "error unsupported construct"},
		{"user_id", "user_id"},
		{"#42;x#42;", "*x*"},
		{"#42;x#42;<br>y", "error unsupported construct"},
		{"a fa:fa-car b", "error unsupported construct"},
		{"$$x^2$$", "error unsupported construct"},
		{"l1  \n l2", "error unsupported construct"},
		{"l1\\\n l2", "error unsupported construct"},
		{"x<y", "error unsupported construct"},
		{"a</b", "error unsupported construct"},
		{"a < b", "a < b"},
		{"a</br>b", "a\nb"},
		{"a &amp; b", "a & b"},
		{"#foo;", "&foo;"},
	} {
		got := mmTree(t, "mindmap\n  r\n    a[\""+c.label+"\"]")
		if !strings.HasPrefix(got, "error") {
			got = strings.TrimPrefix(got, "0|default|r\n1|rect|")
		}
		if got != c.want {
			t.Errorf("%q: got %q, want %q", c.label, got, c.want)
		}
	}
}

// Sections: each child of the root starts one, i mod 11; descendants
// inherit it; the root has none.
func TestMindmapSections(t *testing.T) {
	var b strings.Builder
	b.WriteString("mindmap\n  root\n")
	for i := range 13 {
		fmt.Fprintf(&b, "    c%d\n      g%d\n", i, i)
	}
	d, err := Parse(b.String())
	if err != nil {
		t.Fatal(err)
	}
	m := d.(*Mindmap)
	if m.Nodes[0].Section != -1 {
		t.Errorf("the root's section is %d", m.Nodes[0].Section)
	}
	for k, c := range m.Nodes[0].Children {
		n := m.Nodes[c]
		if n.Section != k%MindmapSections || m.Nodes[n.Children[0]].Section != n.Section {
			t.Errorf("child %d: section %d, its child's %d, want %d", k, n.Section, m.Nodes[n.Children[0]].Section, k%MindmapSections)
		}
	}
}

func TestMindmapLimit(t *testing.T) {
	var b strings.Builder
	b.WriteString("mindmap\n  root\n")
	for i := range MaxMindmapNodes {
		fmt.Fprintf(&b, "    n%d\n", i)
	}
	_, err := Parse(b.String())
	var e *Error
	if !errors.As(err, &e) || e.Kind != UnsupportedConstruct {
		t.Errorf("%d nodes: %v", MaxMindmapNodes+1, err)
	}
}
