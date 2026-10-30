package search

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"unicode"
	"unicode/utf8"

	"flint/internal/workspace"
)

var errStop = errors.New("stop")

// matcher finds query matches in a line, mirroring the rg flags used.
type matcher struct {
	re        *regexp.Regexp
	wholeWord bool
}

// compilePattern turns Options into a matcher. Whole-word matching is checked against
// Unicode letters by hand, because RE2's \b only knows ASCII word characters.
func compilePattern(opts Options) (*matcher, error) {
	pattern := opts.Query
	if !opts.Regex {
		pattern = regexp.QuoteMeta(pattern)
	}
	if !opts.CaseSensitive {
		pattern = `(?i)` + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern: %w", err)
	}
	return &matcher{re: re, wholeWord: opts.WholeWord}, nil
}

// findAll returns byte spans of all matches in text.
func (m *matcher) findAll(text string) [][2]int {
	var spans [][2]int
	for _, loc := range m.re.FindAllStringIndex(text, -1) {
		if loc[0] == loc[1] {
			continue // empty regex matches aren't useful search results
		}
		if m.wholeWord && !(wordBoundaryBefore(text, loc[0]) && wordBoundaryAfter(text, loc[1])) {
			continue
		}
		spans = append(spans, [2]int{loc[0], loc[1]})
	}
	return spans
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func wordBoundaryBefore(text string, i int) bool {
	if i == 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(text[:i])
	return !isWordRune(r)
}

func wordBoundaryAfter(text string, i int) bool {
	if i >= len(text) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(text[i:])
	return !isWordRune(r)
}

// runFallback is a pure-Go search used when rg isn't installed. It is slower and
// doesn't honour .gitignore, but keeps search usable everywhere.
func runFallback(ctx context.Context, opts Options, b *batcher) (Summary, error) {
	m, err := compilePattern(opts)
	if err != nil {
		return Summary{}, err
	}
	includes, excludes := splitGlobs(opts.Include), splitGlobs(opts.Exclude)

	var sum Summary
	walkErr := filepath.WalkDir(opts.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if path != opts.Root && workspace.SkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(opts.Root, path)
		rel = filepath.ToSlash(rel)
		if !globsAllow(rel, includes, excludes) {
			return nil
		}
		found, stop := searchFile(path, rel, m, opts.MaxResults-sum.Matches, b)
		if found > 0 {
			sum.Files++
			sum.Matches += found
		}
		if stop {
			sum.Truncated = true
			return errStop
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, errStop) {
		return sum, walkErr
	}
	return sum, nil
}

// searchFile scans one file, returning the match count and whether the budget ran out.
func searchFile(path, rel string, m *matcher, budget int, b *batcher) (int, bool) {
	info, err := os.Stat(path)
	if err != nil || info.Size() > workspace.MaxFileSize {
		return 0, false
	}
	data, err := os.ReadFile(path)
	if err != nil || bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0 {
		return 0, false
	}
	found := 0
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64*1024), workspace.MaxFileSize)
	for line := 1; sc.Scan(); line++ {
		text := sc.Text()
		spans := m.findAll(text)
		if len(spans) == 0 {
			continue
		}
		b.add(newMatch(rel, line, text, spans))
		found++
		if found >= budget {
			return found, true
		}
	}
	return found, false
}

// globsAllow applies include/exclude globs to both the base name and the relative path.
func globsAllow(rel string, includes, excludes []string) bool {
	for _, g := range excludes {
		if globMatch(g, rel) {
			return false
		}
	}
	if len(includes) == 0 {
		return true
	}
	for _, g := range includes {
		if globMatch(g, rel) {
			return true
		}
	}
	return false
}

func globMatch(glob, rel string) bool {
	if ok, _ := filepath.Match(glob, filepath.Base(rel)); ok {
		return true
	}
	ok, _ := filepath.Match(glob, rel)
	return ok
}
