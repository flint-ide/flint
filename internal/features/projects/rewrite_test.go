package projects

import (
	"strings"
	"testing"
)

func TestSetStatus(t *testing.T) {
	for _, tc := range []struct {
		name, in, status, want, err string
	}{
		{"replace", "---\ntitle: T\nstatus: todo\n---\nbody\n", "doing", "---\ntitle: T\nstatus: doing\n---\nbody\n", ""},
		{"keeps unknown keys and comments", "---\n# planning notes\ntitle: T\nowner: me\nstatus: todo\nestimate: 3d\n---\nbody", "review",
			"---\n# planning notes\ntitle: T\nowner: me\nstatus: review\nestimate: 3d\n---\nbody", ""},
		{"crlf", "---\r\ntitle: T\r\nstatus: todo\r\n---\r\nbody\r\nmore\r\n", "done", "---\r\ntitle: T\r\nstatus: done\r\n---\r\nbody\r\nmore\r\n", ""},
		{"crlf added", "---\r\ntitle: T\r\n---\r\nbody\r\n", "doing", "---\r\ntitle: T\r\nstatus: doing\r\n---\r\nbody\r\n", ""},
		{"added before the closing line", "---\ntitle: T\npriority: high\n---\nbody\n", "doing", "---\ntitle: T\npriority: high\nstatus: doing\n---\nbody\n", ""},
		{"empty front matter", "---\n---\nbody", "doing", "---\nstatus: doing\n---\nbody", ""},
		{"closing at the end", "---\ntitle: T\nstatus: todo\n---", "done", "---\ntitle: T\nstatus: done\n---", ""},
		{"no front matter", "Just text.\n", "doing", "---\nstatus: doing\n---\nJust text.\n", ""},
		{"no front matter, crlf", "a\r\nb\r\n", "doing", "---\r\nstatus: doing\r\n---\r\na\r\nb\r\n", ""},
		{"empty file", "", "todo", "---\nstatus: todo\n---\n", ""},
		{"quoted value", "---\ntitle: T\nstatus: \"todo\"\n---\n", "doing", "---\ntitle: T\nstatus: doing\n---\n", ""},
		{"key case and spacing kept", "---\ntitle: T\nStatus:   todo\n---\n", "review", "---\ntitle: T\nStatus:   review\n---\n", ""},
		{"tab after colon", "---\ntitle: T\nstatus:\ttodo\n---\n", "review", "---\ntitle: T\nstatus:\treview\n---\n", ""},
		{"no space after colon", "---\ntitle: T\nstatus:todo\n---\n", "review", "---\ntitle: T\nstatus: review\n---\n", ""},
		{"indented", "---\ntitle: T\n  status: todo\n---\n", "done", "---\ntitle: T\n  status: done\n---\n", ""},
		{"commented status ignored", "---\ntitle: T\n# status: todo\n---\n", "done", "---\ntitle: T\n# status: todo\nstatus: done\n---\n", ""},
		{"mixed endings kept", "---\ntitle: T\r\nstatus: todo\n---\nbody\r\n", "doing", "---\ntitle: T\r\nstatus: doing\n---\nbody\r\n", ""},
		{"added like the line before", "---\ntitle: T\r\n---\r\nbody", "doing", "---\ntitle: T\r\nstatus: doing\r\n---\r\nbody", ""},
		{"body with ---", "---\ntitle: T\nstatus: todo\n---\nabove\n---\nstatus: below\n", "doing", "---\ntitle: T\nstatus: doing\n---\nabove\n---\nstatus: below\n", ""},
		{"two status lines", "---\ntitle: T\nstatus: todo\nstatus: doing\n---\n", "done", "", "two status lines"},
		{"no closing", "---\ntitle: T\nstatus: todo\n", "done", "", "no closing"},
		{"not key value", "---\ntitle: T\njust words\n---\n", "done", "", "not key: value"},
		{"unknown status", "---\ntitle: T\n---\n", "started", "", "unknown status"},
		{"not utf-8", "---\ntitle: \xff\n---\n", "done", "", "UTF-8"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SetStatus([]byte(tc.in), tc.status)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("err = %v, want %q", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}
