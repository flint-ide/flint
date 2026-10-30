package httpfile

// The response history is memory only and bounded three ways: a few responses per
// request, a number of requests, and a byte budget for the bodies. The newest
// response always stays (it is the one on screen).
const (
	historyPerRequest = 5
	historyRequests   = 50
	historyBytes      = 256 << 20
)

type entry struct {
	resp *Response
	raw  []byte // the whole body, for "Save to file"
	curl string
}

func (e entry) size() int { return len(e.raw) + len(e.resp.Body) + len(e.curl) }

// history holds entries oldest first. add returns a new value; entries are never
// modified in place.
type history struct {
	entries []entry
}

func (h history) add(e entry) history {
	all := append(append([]entry{}, h.entries...), e)
	perRequest := map[string]int{}
	requests := 0
	bytes := 0
	keep := make([]bool, len(all))
	for i := len(all) - 1; i >= 0; i-- {
		key := all[i].resp.File + "\x00" + all[i].resp.Key
		if perRequest[key] == 0 {
			requests++
		}
		perRequest[key]++
		bytes += all[i].size()
		newest := i == len(all)-1
		keep[i] = newest || (perRequest[key] <= historyPerRequest && requests <= historyRequests && bytes <= historyBytes)
	}
	kept := make([]entry, 0, len(all))
	for i, e := range all {
		if keep[i] {
			kept = append(kept, e)
		}
	}
	return history{entries: kept}
}

func (h history) get(id string) (entry, bool) {
	for _, e := range h.entries {
		if e.resp.ID == id {
			return e, true
		}
	}
	return entry{}, false
}
