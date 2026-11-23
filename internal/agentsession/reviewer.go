package agentsession

import "context"

// A reviewer chat reviews code another agent wrote in its folder (a
// workflow's review step). That folder's project settings, hooks and
// instruction files were written by the agent under review, so a reviewer
// chat is started without them where the engine has a switch for it (the
// drivers read Reviewer), and the approval policy answers none of its
// permission requests: every edit, read and command asks the user.
// Like WithFolders, the mark travels in the context of Start, decided by
// Flint, never by the UI or an agent.

type reviewerKey struct{}

// WithReviewer returns ctx marking the chat started with it as a reviewer.
func WithReviewer(ctx context.Context) context.Context {
	return context.WithValue(ctx, reviewerKey{}, true)
}

// Reviewer reports whether ctx marks a reviewer chat (WithReviewer).
func Reviewer(ctx context.Context) bool {
	v, _ := ctx.Value(reviewerKey{}).(bool)
	return v
}
