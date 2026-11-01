package git

import (
	"reflect"
	"testing"
)

func TestParseConflictMarkers(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []ConflictBlock
	}{
		{
			name: "merge style",
			text: "a\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> feature\nz\n",
			want: []ConflictBlock{{Current: "ours\n", Incoming: "theirs\n"}},
		},
		{
			name: "diff3 style keeps the base",
			text: "<<<<<<< HEAD\nours\n||||||| base\norig\n=======\ntheirs\n>>>>>>> feature\n",
			want: []ConflictBlock{{Current: "ours\n", Base: "orig\n", HasBase: true, Incoming: "theirs\n"}},
		},
		{
			name: "empty sides and two blocks",
			text: "<<<<<<< HEAD\n=======\nx\n>>>>>>> b\nmid\n<<<<<<< HEAD\ny\n=======\n>>>>>>> b",
			want: []ConflictBlock{{Current: "", Incoming: "x\n"}, {Current: "y\n", Incoming: ""}},
		},
		{
			name: "CRLF line endings",
			text: "<<<<<<< HEAD\r\nours\r\n=======\r\ntheirs\r\n>>>>>>> b\r\n",
			want: []ConflictBlock{{Current: "ours\r\n", Incoming: "theirs\r\n"}},
		},
		{
			name: "a Markdown heading underline is not a conflict",
			text: "Title\n=======\n\ntext\n",
		},
		{
			name: "an unfinished block is not counted",
			text: "<<<<<<< HEAD\nours\n=======\ntheirs\n",
		},
		{
			name: "longer runs are not markers",
			text: "<<<<<<<< x\n========\n>>>>>>>> y\n",
		},
		{
			name: "a new opening marker restarts the block",
			text: "<<<<<<< HEAD\nstray\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> b\n",
			want: []ConflictBlock{{Current: "ours\n", Incoming: "theirs\n"}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseConflictMarkers(c.text)
			if len(got) == 0 && len(c.want) == 0 {
				if HasConflictMarkers(c.text) {
					t.Fatal("HasConflictMarkers = true, want false")
				}
				return
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %+v\nwant %+v", got, c.want)
			}
			if !HasConflictMarkers(c.text) {
				t.Fatal("HasConflictMarkers = false, want true")
			}
		})
	}
}
