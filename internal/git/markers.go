package git

import "strings"

// ConflictBlock is one conflict as git writes it into a file: the current side
// (ours), the base (only with merge.conflictStyle diff3 / zdiff3) and the incoming
// side (theirs). Each side keeps its line endings.
type ConflictBlock struct {
	Current  string `json:"current"`
	Base     string `json:"base"`
	HasBase  bool   `json:"hasBase"`
	Incoming string `json:"incoming"`
}

// markerSize is git's default conflict-marker-size.
const markerSize = 7

// markerKind classifies a line: '<' opens a block, '|' starts the base, '=' separates
// the sides, '>' closes it; 0 is an ordinary line. A marker is exactly seven of its
// character, alone or followed by a space and a label (the separator never has one),
// so a Markdown heading underline of another length is not a marker.
func markerKind(line string) byte {
	line = strings.TrimRight(line, "\r\n")
	if len(line) < markerSize {
		return 0
	}
	c := line[0]
	if c != '<' && c != '|' && c != '=' && c != '>' {
		return 0
	}
	if line[:markerSize] != strings.Repeat(string(c), markerSize) {
		return 0
	}
	rest := line[markerSize:]
	if rest == "" || (c != '=' && rest[0] == ' ') {
		return c
	}
	return 0
}

// ParseConflictMarkers returns the complete conflict blocks in text, in order. The
// same rules as the merge editor's parser (frontend/src/lib/mergeConflicts.ts).
func ParseConflictMarkers(text string) []ConflictBlock {
	var blocks []ConflictBlock
	var cur ConflictBlock
	var side *string // section being collected; nil outside a block
	state := byte(0) // last marker seen inside the current block
	for _, line := range strings.SplitAfter(text, "\n") {
		switch k := markerKind(line); {
		case k == '<':
			cur, state = ConflictBlock{}, '<'
			side = &cur.Current
		case k == '|' && state == '<':
			cur.HasBase, state = true, '|'
			side = &cur.Base
		case k == '=' && (state == '<' || state == '|'):
			state = '='
			side = &cur.Incoming
		case k == '>' && state == '=':
			blocks = append(blocks, cur)
			cur, state, side = ConflictBlock{}, 0, nil
		default:
			if side != nil {
				*side += line
			}
		}
	}
	return blocks
}

// HasConflictMarkers reports whether text still contains a complete conflict block.
func HasConflictMarkers(text string) bool {
	return len(ParseConflictMarkers(text)) > 0
}
