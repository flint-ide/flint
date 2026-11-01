// Package cilog turns a CI log (gh run view --log-failed) into what an agent
// needs to fix the failure: the failing step's error lines and tail, and the
// file:line references in it mapped to files of the workspace. Pure functions
// over text plus file existence checks; no network, no gh.
package cilog

import (
	"regexp"
	"strconv"
	"strings"
)

// maxLineRunes cuts very long lines (minified output, base64 blobs).
const maxLineRunes = 400

// StepLog is the log of one failed step of one job.
type StepLog struct {
	Job   string
	Step  string
	Lines []string
}

var (
	// timestampRE: the time GitHub puts before every line.
	timestampRE = regexp.MustCompile(`^\x{FEFF}?\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?Z ?`)
	// ansiRE: colour and cursor escapes.
	ansiRE = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]|\x1b\][^\x07]*\x07`)
)

// Parse splits --log-failed output ("job<TAB>step<TAB>time message" lines) into
// steps, in order of appearance, with timestamps, colours and GitHub's
// ##[group] markers removed. A line without the columns belongs to the step
// before it.
func Parse(raw string) []StepLog {
	var steps []StepLog
	index := map[[2]string]int{}
	cur := -1
	for line := range strings.SplitSeq(raw, "\n") {
		line = strings.TrimSuffix(line, "\r")
		msg := line
		if parts := strings.SplitN(line, "\t", 3); len(parts) == 3 && timestampRE.MatchString(parts[2]) {
			key := [2]string{parts[0], parts[1]}
			i, ok := index[key]
			if !ok {
				i = len(steps)
				index[key] = i
				steps = append(steps, StepLog{Job: parts[0], Step: parts[1]})
			}
			cur = i
			msg = timestampRE.ReplaceAllString(parts[2], "")
		}
		msg, keep := clean(msg)
		if !keep {
			continue
		}
		if cur < 0 {
			steps = append(steps, StepLog{})
			cur = 0
		}
		steps[cur].Lines = append(steps[cur].Lines, msg)
	}
	return steps
}

// clean strips colours and turns GitHub's workflow commands into plain text.
func clean(msg string) (string, bool) {
	msg = strings.TrimRight(ansiRE.ReplaceAllString(msg, ""), " \t")
	switch {
	case msg == "", strings.HasPrefix(msg, "##[endgroup]"), strings.HasPrefix(msg, "##[debug]"):
		return "", false
	case strings.HasPrefix(msg, "##[group]"):
		msg = strings.TrimPrefix(msg, "##[group]")
	case strings.HasPrefix(msg, "##[error]"):
		msg = "Error: " + strings.TrimPrefix(msg, "##[error]")
	case strings.HasPrefix(msg, "##[warning]"):
		msg = "Warning: " + strings.TrimPrefix(msg, "##[warning]")
	}
	return msg, msg != ""
}

// Find returns the log of job's step, else the job's last step, else nil.
func Find(steps []StepLog, job, step string) *StepLog {
	var last *StepLog
	for i := range steps {
		if steps[i].Job != job {
			continue
		}
		if steps[i].Step == step {
			return &steps[i]
		}
		last = &steps[i]
	}
	return last
}

// Excerpted is the part of a step's log kept for the agent.
type Excerpted struct {
	// Lines in log order; "…" marks where lines were left out.
	Lines []string
	// Omitted counts the lines left out.
	Omitted int
}

// Excerpt keeps the last tail lines plus up to maxErrors error lines (test
// failures, compiler errors, file:line references) from before them.
func Excerpt(lines []string, tail, maxErrors int) Excerpted {
	n := len(lines)
	tailStart := max(0, n-max(0, tail))
	keep := make([]int, 0, min(n, tail+maxErrors))
	for i := 0; i < tailStart && len(keep) < maxErrors; i++ {
		if isErrorLine(lines[i]) {
			keep = append(keep, i)
		}
	}
	for i := tailStart; i < n; i++ {
		keep = append(keep, i)
	}
	out := Excerpted{Lines: make([]string, 0, len(keep)+4), Omitted: n - len(keep)}
	next := 0
	for _, i := range keep {
		if i != next {
			out.Lines = append(out.Lines, "…")
		}
		out.Lines = append(out.Lines, shorten(lines[i]))
		next = i + 1
	}
	return out
}

func shorten(s string) string {
	if len(s) <= maxLineRunes {
		return s
	}
	r := []rune(s)
	if len(r) <= maxLineRunes {
		return s
	}
	return string(r[:maxLineRunes]) + "…"
}

// errorRE: lines that tell what failed, across common toolchains (go test,
// compilers, node:test/jest/mocha, pytest, cargo, npm, GitHub's ##[error]).
var errorRE = regexp.MustCompile(`^\s*(?:--- FAIL|FAIL(?:\s|:|$)|FAILED\b|panic:|fatal error:|npm ERR!|not ok \d|Traceback \(most recent|E {2,}\S|[✖✗×✘]\s)` +
	`|\b(?:[Ee]rror|ERROR)(?:\[\w+\])?(?::|\s\[)|\b\w+(?:Error|Exception)\b[:\s\[]|\berror TS\d+|: error\b`)

func isErrorLine(s string) bool {
	return errorRE.MatchString(s) || refRE.MatchString(s)
}

// Ref is a file:line reference found in a log.
type Ref struct {
	// Text is the reference as written ("bucket.go:42:9", "app.ts(12,5)").
	Text   string
	File   string
	Line   int
	Column int
}

// refRE: a path with an extension followed by :line[:col] or (line,col). The
// extension starts with a letter so versions (1.2.3:4) don't count, and the
// lead characters keep URLs out.
var refRE = regexp.MustCompile(`(?:^|[\s'"(\[<=])((?:[A-Za-z]:)?[\w./\\@+-]*[\w@+-]\.[A-Za-z][A-Za-z0-9]{0,9})(?::(\d+)(?::(\d+))?|\((\d+),(\d+)\))`)

// Refs finds the file references in lines, unique, in order, at most limit.
func Refs(lines []string, limit int) []Ref {
	var out []Ref
	if limit <= 0 {
		return out
	}
	seen := map[string]bool{}
	for _, l := range lines {
		for _, m := range refRE.FindAllStringSubmatchIndex(l, -1) {
			r := Ref{File: l[m[2]:m[3]], Text: l[m[2]:m[1]]}
			if m[4] >= 0 {
				r.Line = atoi(l[m[4]:m[5]])
				if m[6] >= 0 {
					r.Column = atoi(l[m[6]:m[7]])
				}
			} else {
				r.Line, r.Column = atoi(l[m[8]:m[9]]), atoi(l[m[10]:m[11]])
			}
			if r.Line == 0 || seen[r.Text] {
				continue
			}
			seen[r.Text] = true
			out = append(out, r)
			if len(out) >= limit {
				return out
			}
		}
	}
	return out
}

func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
