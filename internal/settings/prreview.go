package settings

import "fmt"

// PRReview configures handing a pull request's review comments to the agent
// ("Ask agent" on a thread, "Address review comments").
type PRReview struct {
	// MaxThreads is how many unresolved threads one "Address review comments"
	// draft carries; the rest are listed as left out.
	MaxThreads int `json:"maxThreads"`
	// ContextLines is how many lines of code around a commented line go into
	// the draft, on each side (0: none, the agent reads the file itself).
	ContextLines int `json:"contextLines"`
}

// Bounds of PRReview: a draft an agent's context can take.
const (
	MinPRReviewThreads      = 1
	MaxPRReviewThreads      = 50
	MaxPRReviewContextLines = 50
)

func defaultPRReview() PRReview {
	return PRReview{MaxThreads: 20, ContextLines: 8}
}

// CheckPRReview reports the first value out of range.
func CheckPRReview(p PRReview) error {
	switch {
	case p.MaxThreads < MinPRReviewThreads || p.MaxThreads > MaxPRReviewThreads:
		return fmt.Errorf("review threads per draft %d out of range %d–%d", p.MaxThreads, MinPRReviewThreads, MaxPRReviewThreads)
	case p.ContextLines < 0 || p.ContextLines > MaxPRReviewContextLines:
		return fmt.Errorf("review context lines %d out of range 0–%d", p.ContextLines, MaxPRReviewContextLines)
	}
	return nil
}

// normalizePRReview clamps each number into its range (0 or missing threads
// is the default; 0 context lines is a valid choice).
func normalizePRReview(p PRReview) PRReview {
	out := PRReview{
		MaxThreads:   max(MinPRReviewThreads, min(MaxPRReviewThreads, p.MaxThreads)),
		ContextLines: max(0, min(MaxPRReviewContextLines, p.ContextLines)),
	}
	if p.MaxThreads == 0 {
		out.MaxThreads = defaultPRReview().MaxThreads
	}
	return out
}
