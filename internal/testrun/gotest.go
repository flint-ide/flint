package testrun

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// goEvent is one line of `go test -json` (cmd/test2json).
type goEvent struct {
	Action      string
	Package     string
	Test        string
	Output      string
	Elapsed     float64
	ImportPath  string // build-output / build-fail
	FailedBuild string
}

type goNode struct {
	node   Node
	output strings.Builder
}

// GoParser reads `go test -json` output.
type GoParser struct {
	// dirOf maps an import path to its directory ("" when unknown).
	dirOf func(importPath string) string
	// cwd is where go test runs; build errors are printed relative to it.
	cwd   string
	nodes map[string]*goNode
	order []string
	// build output per import path, attached to the package when its build fails.
	build map[string]*strings.Builder
}

// NewGoParser creates a parser; dirOf resolves import paths to directories.
func NewGoParser(cwd string, dirOf func(string) string) *GoParser {
	return &GoParser{dirOf: dirOf, cwd: cwd, nodes: map[string]*goNode{}, build: map[string]*strings.Builder{}}
}

// Line implements Parser.
func (p *GoParser) Line(line []byte) ([]byte, []Node) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, nil // e.g. "go: downloading ..."
	}
	var ev goEvent
	if json.Unmarshal(trimmed, &ev) != nil {
		return nil, nil
	}
	switch ev.Action {
	case "build-output":
		pkg := strings.TrimSpace(strings.SplitN(ev.ImportPath, " [", 2)[0])
		b := p.build[pkg]
		if b == nil {
			b = &strings.Builder{}
			p.build[pkg] = b
		}
		if b.Len() < maxOutput {
			b.WriteString(ev.Output)
		}
		return []byte(ev.Output), nil
	case "start":
		return []byte{}, p.update(p.ensure(ev.Package, ""), Running, 0)
	case "run":
		return []byte{}, p.update(p.ensure(ev.Package, ev.Test), Running, 0)
	case "output":
		n := p.ensure(ev.Package, ev.Test)
		if n.output.Len() < maxOutput && !isProgressLine(ev.Output) {
			n.output.WriteString(ev.Output)
		}
		return []byte(ev.Output), nil
	case "pass", "fail", "skip":
		n := p.ensure(ev.Package, ev.Test)
		status := Passed
		switch ev.Action {
		case "fail":
			status = Failed
		case "skip":
			status = Skipped
		}
		if status == Passed {
			n.output = strings.Builder{} // only failures and skips show output; free it
		} else {
			out := n.output.String()
			if ev.Test == "" && ev.FailedBuild != "" {
				if b := p.build[ev.Package]; b != nil {
					out = b.String() + out
				}
				n.node.Dir = p.cwd // build errors are relative to the go command's directory
			}
			n.node.Output = capOutput(out)
			n.node.Location = p.location(out, n.node.Dir)
		}
		return []byte{}, p.update(n, status, ev.Elapsed*1000)
	}
	return []byte{}, nil // pause, cont, bench
}

// Finish implements Parser.
func (p *GoParser) Finish(exitCode int, stopped bool) []Node {
	var out []Node
	for _, id := range p.order {
		if n := p.nodes[id]; n.node.Status == Running {
			n.node.Output = capOutput(n.output.String())
			out = append(out, p.update(n, unfinished(exitCode, stopped), 0)...)
		}
	}
	return out
}

func (p *GoParser) update(n *goNode, status Status, elapsedMs float64) []Node {
	n.node.Status = status
	if elapsedMs > 0 {
		n.node.ElapsedMs = elapsedMs
	}
	return []Node{n.node}
}

// ensure returns the node of a package (test == "") or test, creating it and
// its parents.
func (p *GoParser) ensure(pkg, test string) *goNode {
	id := pkg
	if test != "" {
		id = pkg + "::" + test
	}
	if n, ok := p.nodes[id]; ok {
		return n
	}
	n := &goNode{node: Node{ID: id, Name: pkg, Kind: KindPackage, Dir: p.dirOf(pkg), Status: Running}}
	if test != "" {
		parent := pkg
		if i := strings.LastIndexByte(test, '/'); i >= 0 {
			parent = p.ensure(pkg, test[:i]).node.ID
		} else {
			p.ensure(pkg, "")
		}
		n.node.Parent = parent
		n.node.Name = test[strings.LastIndexByte(test, '/')+1:]
		n.node.Kind = KindTest
		n.node.Test = test
	}
	p.nodes[id] = n
	p.order = append(p.order, id)
	return n
}

// isProgressLine: test2json's own markers, noise in a test's failure output.
func isProgressLine(s string) bool {
	return strings.HasPrefix(s, "=== RUN") || strings.HasPrefix(s, "=== PAUSE") || strings.HasPrefix(s, "=== CONT") || strings.HasPrefix(s, "=== NAME")
}

// goLocation matches "    foo_test.go:12: message" and "./x.go:3:5: undefined: y".
var goLocation = regexp.MustCompile(`(?m)^\s*((?:\.{0,2}/)?[\w./@+-]*\.go):(\d+)(?::(\d+))?:`)

// location finds the first file:line in output that exists on disk.
func (p *GoParser) location(out, dir string) *Location {
	for _, m := range goLocation.FindAllStringSubmatch(out, 8) {
		path := m[1]
		var candidates []string
		if filepath.IsAbs(path) {
			candidates = []string{path}
		} else {
			candidates = []string{filepath.Join(dir, path), filepath.Join(p.cwd, path)}
		}
		if found := existingPath(candidates...); found != "" {
			line, _ := strconv.Atoi(m[2])
			col, _ := strconv.Atoi(m[3])
			return &Location{Path: found, Line: line, Column: col}
		}
	}
	return nil
}
