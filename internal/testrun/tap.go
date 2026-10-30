package testrun

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// TAPParser reads the TAP stream of `node --test --test-reporter=tap`: nested
// tests are indented four spaces per level and announced by "# Subtest: name";
// results ("ok N - name", "not ok N - name # SKIP") may carry a YAML block with
// duration, location, error and stack. The console gets a readable summary
// (✓/✗ per test, failure details) instead of raw TAP.
type TAPParser struct {
	dir   string
	nodes map[string]*Node
	order []string
	// stack holds the node ids of the open subtests by level.
	stack []string

	// pending is the node whose result line was read; its YAML block may follow.
	pending       *Node
	pendingIndent int
	yamlIndent    int
	yaml          []string
	inYAML        bool
}

// NewTAPParser creates a parser; dir is the package directory.
func NewTAPParser(dir string) *TAPParser {
	return &TAPParser{dir: dir, nodes: map[string]*Node{}}
}

var (
	tapResult = regexp.MustCompile(`^(not ok|ok)\b(?:\s+\d+)?(?:\s+-)?\s*(.*)$`)
	tapPlan   = regexp.MustCompile(`^\d+\.\.\d+`)
)

// Line implements Parser.
func (p *TAPParser) Line(line []byte) ([]byte, []Node) {
	text := strings.TrimRight(string(line), "\r\n")
	indent := len(text) - len(strings.TrimLeft(text, " "))
	body := text[indent:]

	if p.inYAML {
		if body == "..." && indent == p.yamlIndent {
			p.inYAML = false
			return p.settle()
		}
		p.yaml = append(p.yaml, text)
		return []byte{}, nil
	}
	var updates []Node
	shown := []byte{}
	if p.pending != nil {
		if body == "---" && indent == p.pendingIndent+2 {
			p.inYAML, p.yamlIndent, p.yaml = true, indent, nil
			return []byte{}, nil
		}
		shown, updates = p.settle() // no YAML block
	}
	s, u := p.parse(indent/4, body)
	if s == nil {
		s = line // not TAP: show as is
	}
	return append(shown, s...), append(updates, u...)
}

func (p *TAPParser) parse(level int, body string) ([]byte, []Node) {
	switch {
	case strings.HasPrefix(body, "# Subtest: "):
		name := unescapeTAP(strings.TrimPrefix(body, "# Subtest: "))
		if level > len(p.stack) {
			level = len(p.stack)
		}
		parent := ""
		if level > 0 {
			parent = p.stack[level-1]
		}
		id := p.uniqueID(parent, name)
		n := &Node{ID: id, Parent: parent, Name: name, Kind: KindTest, Test: p.fullName(parent, name), Status: Running, Dir: p.dir}
		p.nodes[id] = n
		p.order = append(p.order, id)
		p.stack = append(p.stack[:level], id)
		return []byte{}, []Node{*n}
	case tapResult.MatchString(body):
		m := tapResult.FindStringSubmatch(body)
		name, directive := splitDirective(m[2])
		n := p.nodeAt(level, name)
		switch {
		case strings.HasPrefix(strings.ToUpper(directive), "SKIP"), strings.HasPrefix(strings.ToUpper(directive), "TODO"):
			n.Status = Skipped
			n.Output = strings.TrimSpace(directive)
		case m[1] == "ok":
			n.Status = Passed
		default:
			n.Status = Failed
		}
		p.pending, p.pendingIndent = n, level*4
		if len(p.stack) > level {
			p.stack = p.stack[:level]
		}
		return []byte{}, nil
	case strings.HasPrefix(body, "TAP version"), tapPlan.MatchString(body):
		return []byte{}, nil
	case strings.HasPrefix(body, "# "):
		return []byte(strings.TrimPrefix(body, "# ") + "\n"), nil // test output and the summary
	}
	return nil, nil
}

// nodeAt finds the node a result line at level refers to: normally the open
// subtest of that level; reporters that skip "# Subtest:" get a node created.
func (p *TAPParser) nodeAt(level int, name string) *Node {
	if level < len(p.stack) {
		if n := p.nodes[p.stack[level]]; n != nil && n.Name == name {
			return n
		}
	}
	parent := ""
	if level > 0 && level-1 < len(p.stack) {
		parent = p.stack[level-1]
	}
	id := p.uniqueID(parent, name)
	n := &Node{ID: id, Parent: parent, Name: name, Kind: KindTest, Test: p.fullName(parent, name), Dir: p.dir}
	p.nodes[id] = n
	p.order = append(p.order, id)
	return n
}

// uniqueID: node --test reports tests of several files side by side, so equal
// names can repeat at the top level.
func (p *TAPParser) uniqueID(parent, name string) string {
	base := name
	if parent != "" {
		base = parent + " › " + name
	}
	id := base
	for i := 2; p.nodes[id] != nil; i++ {
		id = fmt.Sprintf("%s (%d)", base, i)
	}
	return id
}

func (p *TAPParser) fullName(parent, name string) string {
	if n := p.nodes[parent]; n != nil {
		return n.Test + " " + name
	}
	return name
}

// settle applies the pending node's YAML block and reports it.
func (p *TAPParser) settle() ([]byte, []Node) {
	n := p.pending
	p.pending = nil
	details := parseTAPYAML(p.yaml, p.yamlIndent)
	p.yaml = nil
	if d, err := strconv.ParseFloat(details["duration_ms"], 64); err == nil {
		n.ElapsedMs = d
	}
	if strings.Trim(details["type"], "'") == "suite" {
		n.Kind = KindSuite
	}
	if n.Status == Failed {
		n.Output = capOutput(failureText(details))
		n.Location = tapLocation(details)
	}
	return []byte(consoleLine(n, p.depth(n))), []Node{*n}
}

func (p *TAPParser) depth(n *Node) int {
	d := 0
	for parent := n.Parent; parent != ""; d++ {
		next := p.nodes[parent]
		if next == nil {
			break
		}
		parent = next.Parent
	}
	return d
}

// consoleLine renders a result for the console (ANSI colors).
func consoleLine(n *Node, depth int) string {
	pad := strings.Repeat("  ", depth)
	took := ""
	if n.ElapsedMs > 0 {
		took = fmt.Sprintf(" \x1b[2m(%.1fms)\x1b[0m", n.ElapsedMs)
	}
	switch n.Status {
	case Passed:
		return pad + "\x1b[32m✓\x1b[0m " + n.Name + took + "\n"
	case Skipped:
		return pad + "\x1b[33m↷\x1b[0m " + n.Name + " \x1b[2m# " + n.Output + "\x1b[0m\n"
	default:
		out := pad + "\x1b[31m✗ " + n.Name + "\x1b[0m" + took + "\n"
		if n.Kind == KindTest && n.Output != "" {
			for _, l := range strings.Split(strings.TrimRight(n.Output, "\n"), "\n") {
				out += pad + "    " + l + "\n"
			}
		}
		return out
	}
}

// Finish implements Parser.
func (p *TAPParser) Finish(exitCode int, stopped bool) []Node {
	var updates []Node
	if p.pending != nil {
		_, updates = p.settle()
	}
	for _, id := range p.order {
		if n := p.nodes[id]; n.Status == Running || n.Status == "" {
			n.Status = unfinished(exitCode, stopped)
			updates = append(updates, *n)
		}
	}
	return updates
}

// splitDirective separates "name # SKIP reason" at the first unescaped " #".
func splitDirective(s string) (string, string) {
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\':
			i++
		case s[i] == '#' && (i == 0 || s[i-1] == ' '):
			return unescapeTAP(strings.TrimSpace(s[:i])), strings.TrimSpace(s[i+1:])
		}
	}
	return unescapeTAP(strings.TrimSpace(s)), ""
}

func unescapeTAP(s string) string {
	return strings.NewReplacer(`\#`, "#", `\\`, `\`).Replace(s)
}

// parseTAPYAML reads the flat YAML subset node emits: "key: value" and block
// scalars ("key: |-" followed by more-indented lines).
func parseTAPYAML(lines []string, indent int) map[string]string {
	out := map[string]string{}
	key := ""
	var block []string
	flush := func() {
		if key != "" && block != nil {
			out[key] = strings.Join(block, "\n")
		}
		key, block = "", nil
	}
	for _, l := range lines {
		lineIndent := len(l) - len(strings.TrimLeft(l, " "))
		if key != "" && block != nil && (lineIndent > indent || strings.TrimSpace(l) == "") {
			cut := min(len(l), indent+2)
			block = append(block, l[cut:])
			continue
		}
		flush()
		k, v, ok := strings.Cut(strings.TrimSpace(l), ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if v == "|-" || v == "|" || v == ">-" {
			key, block = k, []string{}
			continue
		}
		out[k] = strings.Trim(v, "'")
	}
	flush()
	return out
}

func failureText(d map[string]string) string {
	var b strings.Builder
	b.WriteString(strings.TrimRight(d["error"], "\n"))
	if exp, ok := d["expected"]; ok {
		fmt.Fprintf(&b, "\nexpected: %s\nactual:   %s", exp, d["actual"])
	}
	if stack := d["stack"]; stack != "" {
		b.WriteString("\n\n" + strings.ReplaceAll(stack, "file://", ""))
	}
	return strings.TrimSpace(b.String())
}

var stackFrame = regexp.MustCompile(`(?:file://)?(/[^\s():]+):(\d+):(\d+)`)

// tapLocation prefers the first stack frame in an existing file (with source
// maps, the original source), then the reported test location.
func tapLocation(d map[string]string) *Location {
	for _, src := range []string{d["stack"], d["location"]} {
		for _, m := range stackFrame.FindAllStringSubmatch(src, 8) {
			if existingPath(m[1]) == "" || strings.Contains(m[1], "/node_modules/") {
				continue
			}
			line, _ := strconv.Atoi(m[2])
			col, _ := strconv.Atoi(m[3])
			return &Location{Path: m[1], Line: line, Column: col}
		}
	}
	return nil
}
