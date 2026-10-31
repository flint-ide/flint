package github

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// maxDiffBytes caps a side of the diff sent to the webview.
const maxDiffBytes = 4 << 20

// rawAccept asks the REST API for file contents as-is instead of base64 JSON.
const rawAccept = "Accept: application/vnd.github.raw+json"

// Local reads objects from the project's own clone, which saves a round trip to
// GitHub whenever the pull request's commits are already fetched.
type Local interface {
	// Object returns a blob by sha or "<rev>:<path>"; ok is false when it isn't present.
	Object(ctx context.Context, spec string) (data []byte, ok bool)
	// MergeBase returns the merge base of two commits; ok is false when either is missing.
	MergeBase(ctx context.Context, a, b string) (sha string, ok bool)
}

// LineRange is a 1-based, inclusive range of lines.
type LineRange struct {
	From int `json:"from"`
	To   int `json:"to"`
}

// FileDiff is one file of a pull request: the merge-base version (what GitHub's diff
// is against) and the head version, for the side-by-side view.
type FileDiff struct {
	Path         string `json:"path"`
	PreviousPath string `json:"previousPath"`
	Status       string `json:"status"`
	Base         string `json:"base"`
	Head         string `json:"head"`
	Binary       bool   `json:"binary"`
	TooLarge     bool   `json:"tooLarge"`
	// Left and Right are the lines a new comment can go on: GitHub only accepts lines
	// inside the diff's hunks (changes plus their context).
	Left  []LineRange `json:"left"`
	Right []LineRange `json:"right"`
}

// FileDiff returns both sides of a changed file. The head comes from the local clone
// when it has the blob, else from GitHub; the base is rebuilt by applying GitHub's
// patch in reverse, so no second download is needed. Files without a patch (binary,
// huge) fall back to the base at the merge base.
func (c *Client) FileDiff(ctx context.Context, dir string, number int, path string, local Local) (FileDiff, error) {
	data, err := c.prData(ctx, dir, number, false)
	if err != nil {
		return FileDiff{}, err
	}
	f, ok := data.files[path]
	if !ok {
		return FileDiff{}, fmt.Errorf("%s is not changed by #%d", path, number)
	}
	d := data.details
	key := fmt.Sprintf("%s|%s|%s", prKey(d.Repo, number), d.HeadOID, path)
	return c.diffs.get(key, false, func() (FileDiff, error) {
		ctx, cancel := context.WithTimeout(ctx, readTimeout)
		defer cancel()
		return c.buildDiff(ctx, dir, d, f, local)
	})
}

func (c *Client) buildDiff(ctx context.Context, dir string, d Details, f restFile, local Local) (FileDiff, error) {
	out := FileDiff{Path: f.Filename, PreviousPath: f.PreviousFilename, Status: f.Status, Left: []LineRange{}, Right: []LineRange{}}
	var head []byte
	if f.Status != "removed" {
		var err error
		if head, err = c.blob(ctx, dir, d.Repo, f.SHA, local); err != nil {
			return FileDiff{}, err
		}
	}
	var base []byte
	rebuilt := false
	if f.Status != "added" && f.Patch != "" {
		if b, err := reversePatch(string(head), f.Patch); err == nil {
			base, rebuilt = []byte(b), true
		}
	}
	if f.Status != "added" && !rebuilt {
		basePath := f.Filename
		if f.PreviousFilename != "" {
			basePath = f.PreviousFilename
		}
		var err error
		if base, err = c.mergeBaseFile(ctx, dir, d, basePath, local); err != nil {
			return FileDiff{}, err
		}
	}
	switch {
	case bytes.IndexByte(head, 0) >= 0 || bytes.IndexByte(base, 0) >= 0:
		out.Binary = true
		return out, nil
	case len(head) > maxDiffBytes || len(base) > maxDiffBytes:
		out.TooLarge = true
		return out, nil
	}
	out.Base, out.Head = string(base), string(head)
	for _, h := range parseHunks(f.Patch) {
		if h.oldLines > 0 {
			out.Left = append(out.Left, LineRange{h.oldStart, h.oldStart + h.oldLines - 1})
		}
		if h.newLines > 0 {
			out.Right = append(out.Right, LineRange{h.newStart, h.newStart + h.newLines - 1})
		}
	}
	return out, nil
}

// blob reads a blob by sha: locally when possible, else through the REST API.
func (c *Client) blob(ctx context.Context, dir string, repo Repo, sha string, local Local) ([]byte, error) {
	if local != nil {
		if b, ok := local.Object(ctx, sha); ok {
			return b, nil
		}
	}
	return c.run(ctx, dir, nil, "api", "-H", rawAccept, fmt.Sprintf("repos/%s/git/blobs/%s", repo.NameWithOwner, sha))
}

// mergeBaseFile reads path at the merge base of the pull request ("" when the file
// didn't exist there).
func (c *Client) mergeBaseFile(ctx context.Context, dir string, d Details, path string, local Local) ([]byte, error) {
	base, err := c.mergeBase(ctx, dir, d, local)
	if err != nil {
		return nil, err
	}
	if local != nil {
		if b, ok := local.Object(ctx, base+":"+path); ok {
			return b, nil
		}
	}
	out, err := c.run(ctx, dir, nil, "api", "-H", rawAccept,
		fmt.Sprintf("repos/%s/contents/%s?ref=%s", d.Repo.NameWithOwner, escapePath(path), base))
	var gerr *Error
	if errors.As(err, &gerr) && strings.Contains(gerr.Msg, "HTTP 404") {
		return nil, nil
	}
	return out, err
}

func (c *Client) mergeBase(ctx context.Context, dir string, d Details, local Local) (string, error) {
	if local != nil {
		if sha, ok := local.MergeBase(ctx, d.BaseOID, d.HeadOID); ok {
			return sha, nil
		}
	}
	out, err := c.run(ctx, dir, nil, "api", fmt.Sprintf("repos/%s/compare/%s...%s", d.Repo.NameWithOwner, d.BaseOID, d.HeadOID),
		"--jq", ".merge_base_commit.sha")
	if err != nil {
		return "", err
	}
	sha := strings.TrimSpace(string(out))
	if sha == "" {
		return "", errors.New("GitHub returned no merge base")
	}
	return sha, nil
}

func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

type hunk struct{ oldStart, oldLines, newStart, newLines int }

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

func parseHunk(line string) (hunk, bool) {
	m := hunkHeader.FindStringSubmatch(line)
	if m == nil {
		return hunk{}, false
	}
	n := func(s string) int {
		if s == "" {
			return 1 // "@@ -3 +3 @@": a count of one is left out
		}
		v, _ := strconv.Atoi(s)
		return v
	}
	return hunk{oldStart: n(m[1]), oldLines: n(m[2]), newStart: n(m[3]), newLines: n(m[4])}, true
}

func parseHunks(patch string) []hunk {
	var hs []hunk
	for line := range strings.SplitSeq(patch, "\n") {
		if h, ok := parseHunk(line); ok {
			hs = append(hs, h)
		}
	}
	return hs
}

var errPatchMismatch = errors.New("patch does not match the file")

// reversePatch rebuilds the old version of a file from the new one and its unified
// diff hunks (GitHub's "patch": hunks without file headers). Context and added lines
// are checked against head, so a patch that doesn't fit is an error, never a wrong file.
func reversePatch(head, patch string) (string, error) {
	lines, headNoEOL := splitLines(head)
	var base []string
	pos := 0 // next line of head
	var prev byte
	sawMarker, baseMarker := false, false
	patchLines := strings.Split(patch, "\n")
	for i, l := range patchLines {
		if l == "" && i == len(patchLines)-1 {
			break // trailing newline of the patch itself
		}
		if strings.HasPrefix(l, "@@") {
			h, ok := parseHunk(l)
			if !ok {
				return "", fmt.Errorf("bad hunk header %q", l)
			}
			// An empty new side starts *after* newStart.
			upto := h.newStart - 1
			if h.newLines == 0 {
				upto = h.newStart
			}
			if upto < pos || upto > len(lines) {
				return "", errPatchMismatch
			}
			base = append(base, lines[pos:upto]...)
			pos = upto
			prev = '@'
			continue
		}
		kind, text := byte(' '), ""
		if l != "" { // some tools strip the space of empty context lines
			kind, text = l[0], l[1:]
		}
		switch kind {
		case ' ', '+':
			if pos >= len(lines) || lines[pos] != text {
				return "", errPatchMismatch
			}
			if kind == ' ' {
				base = append(base, text)
			}
			pos++
		case '-':
			base = append(base, text)
		case '\\': // "\ No newline at end of file" for the line before
			sawMarker = true
			if prev == '-' || prev == ' ' {
				baseMarker = true
			}
		default:
			return "", fmt.Errorf("unexpected patch line %q", l)
		}
		prev = kind
	}
	base = append(base, lines[pos:]...)
	baseNoEOL := headNoEOL
	if sawMarker {
		baseNoEOL = baseMarker
	}
	if len(base) == 0 {
		return "", nil
	}
	out := strings.Join(base, "\n")
	if !baseNoEOL {
		out += "\n"
	}
	return out, nil
}

// splitLines splits text into lines and reports whether the last one lacks "\n".
func splitLines(s string) ([]string, bool) {
	if s == "" {
		return nil, false
	}
	if strings.HasSuffix(s, "\n") {
		return strings.Split(s[:len(s)-1], "\n"), false
	}
	return strings.Split(s, "\n"), true
}
