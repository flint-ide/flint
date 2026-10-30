package search

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// rgEvent mirrors the subset of `rg --json` output Flint consumes.
type rgEvent struct {
	Type string `json:"type"`
	Data struct {
		Path       rgText `json:"path"`
		Lines      rgText `json:"lines"`
		LineNumber int    `json:"line_number"`
		Submatches []struct {
			Start int `json:"start"`
			End   int `json:"end"`
		} `json:"submatches"`
	} `json:"data"`
}

// rgText is either {"text": "..."} or {"bytes": "<base64>"} for non-UTF-8 data;
// the latter is skipped.
type rgText struct {
	Text string `json:"text"`
}

func ripgrepArgs(opts Options) []string {
	args := []string{"--json", "--hidden", "--glob", "!.git", "--max-columns", "4000"}
	if !opts.Regex {
		args = append(args, "--fixed-strings")
	}
	if opts.CaseSensitive {
		args = append(args, "--case-sensitive")
	} else {
		args = append(args, "--ignore-case")
	}
	if opts.WholeWord {
		args = append(args, "--word-regexp")
	}
	for _, g := range splitGlobs(opts.Include) {
		args = append(args, "--glob", g)
	}
	for _, g := range splitGlobs(opts.Exclude) {
		args = append(args, "--glob", "!"+g)
	}
	return append(args, "--", opts.Query, ".")
}

func runRipgrep(ctx context.Context, rg string, opts Options, b *batcher) (Summary, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	cmd := exec.CommandContext(ctx, rg, ripgrepArgs(opts)...)
	cmd.Dir = opts.Root
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return Summary{}, fmt.Errorf("rg stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return Summary{}, fmt.Errorf("start rg: %w", err)
	}

	sum, parseErr := parseRipgrep(out, opts.MaxResults, b.add)
	if sum.Truncated {
		cancel() // stop rg early; its exit error is expected
	}
	waitErr := cmd.Wait()

	if parseErr != nil {
		return sum, parseErr
	}
	if err := ctx.Err(); err != nil && !sum.Truncated {
		return sum, err
	}
	return sum, ripgrepExitError(waitErr, sum, stderr.String())
}

// ripgrepExitError maps rg's exit status: 0 = matches, 1 = no matches, 2 = error.
// Exit 2 alongside matches usually means a few unreadable files, which isn't fatal.
func ripgrepExitError(err error, sum Summary, stderr string) error {
	if err == nil || sum.Truncated {
		return nil
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return fmt.Errorf("rg: %w", err)
	}
	if exitErr.ExitCode() == 1 || sum.Matches > 0 {
		return nil
	}
	msg := strings.TrimSpace(stderr)
	if msg == "" {
		msg = exitErr.Error()
	}
	return fmt.Errorf("search failed: %s", msg)
}

// parseRipgrep consumes `rg --json` output, calling emit for each match until max is reached.
func parseRipgrep(r io.Reader, max int, emit func(Match)) (Summary, error) {
	var sum Summary
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 256*1024), 16*1024*1024)
	for sc.Scan() {
		var ev rgEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			continue // tolerate unexpected lines rather than abort the search
		}
		switch ev.Type {
		case "begin":
			sum.Files++
		case "match":
			if ev.Data.Path.Text == "" {
				continue
			}
			text := strings.TrimRight(ev.Data.Lines.Text, "\r\n")
			spans := make([][2]int, 0, len(ev.Data.Submatches))
			for _, sm := range ev.Data.Submatches {
				spans = append(spans, [2]int{sm.Start, sm.End})
			}
			emit(newMatch(strings.TrimPrefix(ev.Data.Path.Text, "./"), ev.Data.LineNumber, text, spans))
			sum.Matches++
			if sum.Matches >= max {
				sum.Truncated = true
				return sum, nil
			}
		}
	}
	if err := sc.Err(); err != nil {
		return sum, fmt.Errorf("read rg output: %w", err)
	}
	return sum, nil
}

func splitGlobs(s string) []string {
	var out []string
	for _, g := range strings.Split(s, ",") {
		if g = strings.TrimSpace(g); g != "" {
			out = append(out, g)
		}
	}
	return out
}
