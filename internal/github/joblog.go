package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// maxLogDownload bounds the bytes read of one job's log (its tail is kept):
// a security limit against a log, or a server, that never ends.
const maxLogDownload = 512 << 20

// errNoLog: GitHub has no log for the job (expired, or still being uploaded).
var errNoLog = errors.New("GitHub has no log for this job (it expired, or is still being uploaded)")

// newDownloadHTTP fetches the log file a job's log URL redirects to. It never
// carries the token (no request it sends has one), and follows https only.
func newDownloadHTTP() *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "https" && req.URL.Scheme != via[0].URL.Scheme {
				return fmt.Errorf("refusing a redirect to %s", req.URL.Scheme)
			}
			req.Header.Del("Authorization")
			return nil
		},
	}
}

// jobLog downloads a job's log: GET …/actions/jobs/{id}/logs answers with a
// redirect to a short-lived URL on another host, which is fetched without the
// token. The last maxLogBytes are kept.
func (c *Client) jobLog(ctx context.Context, a Auth, repo Repo, jobID int64) (*tailBuffer, error) {
	u := rest(a, "/repos/%s/%s/actions/jobs/%d/logs", repo.Owner, repo.Name, jobID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Authorization", "Bearer "+a.Token)
	res, err := c.http.Do(req) // follows redirects within the API's origin only
	if err != nil {
		return nil, fmt.Errorf("could not reach GitHub at %s: %w", hostOf(u), stripURL(err))
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusOK:
		return readTail(res.Body)
	case res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusGone:
		return nil, errNoLog
	case res.StatusCode >= 300 && res.StatusCode < 400:
	default:
		body, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
		if err := statusError(res, body); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("unexpected answer from GitHub (HTTP %d)", res.StatusCode)
	}
	loc, err := res.Location()
	if err != nil {
		return nil, errors.New("GitHub sent no log location")
	}
	if loc.Scheme != "https" && !(loc.Scheme == "http" && req.URL.Scheme == "http") {
		return nil, fmt.Errorf("refusing to download the log over %s", loc.Scheme)
	}
	dl, err := http.NewRequestWithContext(ctx, http.MethodGet, loc.String(), nil)
	if err != nil {
		return nil, err
	}
	dl.Header.Set("User-Agent", c.userAgent) // and nothing else: no token
	resp, err := c.download.Do(dl)
	if err != nil {
		return nil, fmt.Errorf("could not download the log from %s: %w", loc.Host, stripURL(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return nil, errNoLog
	}
	return readTail(resp.Body)
}

func readTail(r io.Reader) (*tailBuffer, error) {
	out := &tailBuffer{max: maxLogBytes}
	n, err := io.Copy(out, io.LimitReader(r, maxLogDownload))
	if err != nil {
		return nil, fmt.Errorf("reading the log: %w", stripURL(err))
	}
	if n == maxLogDownload {
		out.cut = true
	}
	return out, nil
}

// failedSteps picks the lines of a job's failed steps from its log and writes
// them as the GitHub CLI's --log-failed does: "job<TAB>step<TAB>line". The
// job log has no step markers, so a line belongs to a failed step when its
// timestamp falls within the step's start and end (whole seconds: a line at
// a boundary second may also belong to the step before, which is harmless).
// Without step times (or failed steps), the whole log stands for the job.
func failedSteps(j Job, raw string) string {
	type span struct {
		name     string
		from, to time.Time // to zero: still running
	}
	var spans []span
	for _, s := range j.Steps {
		if !failed(s.Conclusion) {
			continue
		}
		from, err := time.Parse(time.RFC3339, s.StartedAt)
		if err != nil {
			continue
		}
		sp := span{name: s.Name, from: from.Truncate(time.Second)}
		if to, err := time.Parse(time.RFC3339, s.CompletedAt); err == nil {
			sp.to = to.Truncate(time.Second).Add(time.Second)
		}
		spans = append(spans, sp)
	}
	whole := ""
	if st := j.FailedStep(); st != nil {
		whole = st.Name
	}
	job := tabless(j.Name)
	var b strings.Builder
	keep := false // the line before was kept (continuation lines follow it)
	wrote := false
	for line := range strings.SplitSeq(raw, "\n") {
		ts, ok := lineTime(line)
		if !ok {
			if keep && strings.TrimSpace(line) != "" {
				b.WriteString(line + "\n")
			}
			continue
		}
		step, found := "", len(spans) == 0
		if found {
			step = whole
		}
		for _, sp := range spans {
			if !ts.Before(sp.from) && (sp.to.IsZero() || ts.Before(sp.to)) {
				step, found = sp.name, true
				break
			}
		}
		keep = found
		if found {
			b.WriteString(job + "\t" + tabless(step) + "\t" + strings.TrimSuffix(line, "\r") + "\n")
			wrote = true
		}
	}
	if !wrote && len(spans) > 0 {
		// Step times that don't match the log (clock skew): keep the whole log.
		return failedSteps(Job{Name: j.Name, Steps: nil}, raw)
	}
	return b.String()
}

// lineTime reads the timestamp GitHub puts before every log line.
func lineTime(line string) (time.Time, bool) {
	line = strings.TrimPrefix(line, "\uFEFF")
	stamp, _, _ := strings.Cut(line, " ")
	if len(stamp) < 20 || stamp[4] != '-' || stamp[10] != 'T' {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, stamp)
	return t, err == nil
}

func tabless(s string) string { return strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(s) }
