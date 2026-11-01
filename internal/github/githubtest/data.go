package githubtest

import (
	_ "embed"
	"strings"
)

// The scenario the fake serves: acme/widgets with two open pull requests, a
// review thread, and a failed GitHub Actions run with a log.
const (
	Owner      = "acme"
	Repo       = "widgets"
	Login      = "octocat"
	HeadSHA    = "785d8f7a66d3403ed32421b45c87ede4fbda8bda"
	BaseSHA    = "1d9a1c3b0a8e4c5f6d7e8f9a0b1c2d3e4f5a6b7c"
	MergeBase  = "0c8b7a6f5e4d3c2b1a0f9e8d7c6b5a4f3e2d1c0b"
	DocsSHA    = "aa11bb22cc33dd44ee55ff66aa77bb88cc99dd00"
	RunID      = 4242
	FailedJob  = 5151
	PassedJob  = 5152
	SuiteID    = 91
	UserCode   = "WDJB-MJHT"
	deviceCode = "3584d83530557fdd1f46af8289938c8ef79f9dc5"
)

// Blob ids of the files of #7.
const (
	MainBlob = "b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1"
	NewBlob  = "b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2"
)

// JobLog is the raw log of the failed job, as GitHub serves it (a BOM, then
// one timestamped line per output line).
//
//go:embed testdata/job_log.txt
var JobLog string

// MainHead and MainBase are main.go after and before #7.
const (
	MainHead = "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"
	MainBase = "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"
	NewDoc   = "# Rate limits\ntext\n"
)

const mainPatch = "@@ -1,5 +1,7 @@\n package main\n \n+import \"fmt\"\n+\n func main() {\n-\tprintln(\"hi\")\n+\tfmt.Println(\"hello\")\n }"

type comment struct {
	ID, Author, Body, State string
	DatabaseID              int64
}

type thread struct {
	ID, Path, Side       string
	Line, OriginalLine   int
	Resolved, Outdated   bool
	ResolvedBy           string
	Comments             []comment
	CanResolve, CanReply bool
}

type pull struct {
	Number                        int
	Title, Body, Author, Head     string
	HeadOwner, State, Decision    string
	Draft                         bool
	HeadSHA                       string
	Threads                       []thread
	PendingReview                 string
	PendingComments               int
	Files                         []map[string]any
	ReviewRequested, AuthoredByMe bool
}

func scenario() []*pull {
	return []*pull{
		{
			Number: 7, Title: "Add rate limiting", Body: "Adds a token bucket.\n\n<script>alert(1)</script>\n", Author: "alice",
			Head: "feat/rate-limit", HeadOwner: Owner, State: "OPEN", Decision: "CHANGES_REQUESTED", HeadSHA: HeadSHA,
			ReviewRequested: true,
			Threads: []thread{
				{
					ID: "PRRT_kwDOAAABc84AAAAB", Path: "main.go", Side: "RIGHT", Line: 6, OriginalLine: 4, CanResolve: true, CanReply: true,
					Comments: []comment{{ID: "PRRC_kwDOAAABc84AAAAC", DatabaseID: 101, Author: "dave", Body: "Use `fmt`?", State: "SUBMITTED"}},
				},
				{
					ID: "PRRT_kwDOAAABc84AAAAD", Path: "main.go", Side: "LEFT", OriginalLine: 2, Resolved: true, Outdated: true,
					ResolvedBy: "alice", CanReply: true,
					Comments: []comment{{ID: "PRRC_kwDOAAABc84AAAAE", DatabaseID: 103, Author: "carol", Body: "old", State: "SUBMITTED"}},
				},
			},
			Files: []map[string]any{
				{"sha": MainBlob, "filename": "main.go", "status": "modified", "additions": 3, "deletions": 1, "changes": 4, "patch": mainPatch},
				{"sha": NewBlob, "filename": "docs/rate-limits.md", "status": "added", "additions": 2, "deletions": 0, "changes": 2, "patch": "@@ -0,0 +1,2 @@\n+# Rate limits\n+text"},
			},
		},
		{
			Number: 8, Title: "Docs: widgets guide", Body: "", Author: "bob", Head: "main", HeadOwner: "bob", State: "OPEN",
			Draft: true, HeadSHA: DocsSHA,
			Files: []map[string]any{
				{"sha": NewBlob, "filename": "docs/rate-limits.md", "status": "added", "additions": 2, "deletions": 0, "changes": 2, "patch": "@@ -0,0 +1,2 @@\n+# Rate limits\n+text"},
			},
		},
	}
}

func blobs() map[string]string {
	return map[string]string{MainBlob: MainHead, NewBlob: NewDoc}
}

// steps of the failed job, from a real run's REST answer.
func failedJobSteps() []map[string]any {
	step := func(n int, name, conclusion, from, to string) map[string]any {
		return map[string]any{"name": name, "status": "completed", "conclusion": conclusion, "number": n,
			"started_at": "2026-11-01T12:" + from + "Z", "completed_at": "2026-11-01T12:" + to + "Z"}
	}
	return []map[string]any{
		step(1, "Set up job", "success", "41:35", "41:37"),
		step(11, "go vet", "success", "42:20", "42:22"),
		step(12, "go test", "failure", "42:22", "42:48"),
		step(14, "govulncheck", "skipped", "42:48", "42:48"),
		step(31, "Complete job", "success", "42:49", "42:49"),
	}
}

func lower(s string) string { return strings.ToLower(s) }
