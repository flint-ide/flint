// Package profiling turns pprof profiles (`go test -cpuprofile`, net/http/pprof)
// into what the flame graph tab shows: a call tree with flat and cumulative
// values, trimmed of tiny frames so the payload to the WebView stays small, and
// a top table of functions. It also owns the temporary directories profiles
// are written to and fetches profiles from a running program's pprof endpoint.
package profiling

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/google/pprof/profile"
)

// Defaults of a Report.
const (
	// TrimFraction drops frames below this share of the total (0.5%).
	TrimFraction = 0.005
	// MaxDepth caps the stack depth sent to the UI (deeper frames are folded
	// into their ancestor at the cap).
	MaxDepth = 256
	// TopN is how many functions the top table gets, per sort order (flat, cum).
	TopN = 150
	// maxFileSize guards against parsing something that is clearly no profile.
	maxFileSize = 256 << 20
)

// Frame is one node of the call tree: a function called through the path of
// its ancestors. Total includes the children (also those trimmed away).
type Frame struct {
	Name string `json:"name"`
	// File and Line locate the function (its first line), when known.
	File     string  `json:"file,omitempty"`
	Line     int     `json:"line,omitempty"`
	Self     int64   `json:"self"`
	Total    int64   `json:"total"`
	Children []Frame `json:"children,omitempty"`
}

// TopEntry is one function of the top table: Flat counts samples where it was
// the leaf, Cum samples where it was anywhere on the stack (once per sample,
// also when recursive).
type TopEntry struct {
	Name string `json:"name"`
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
	Flat int64  `json:"flat"`
	Cum  int64  `json:"cum"`
}

// Report is a profile prepared for the flame graph.
type Report struct {
	Path string `json:"path"`
	// SampleTypes lists the values recorded per sample ("samples", "cpu" for
	// CPU profiles; "alloc_space", "inuse_space", … for heap profiles).
	SampleTypes []string `json:"sampleTypes"`
	SampleType  string   `json:"sampleType"`
	// Unit of the values: "nanoseconds", "bytes", "count", …
	Unit  string `json:"unit"`
	Total int64  `json:"total"`
	// DurationNs is how long the profile was recorded (0 when unknown).
	DurationNs int64      `json:"durationNs"`
	Root       Frame      `json:"root"`
	Top        []TopEntry `json:"top"`
	// Frames is the number of frames in Root; Trimmed those left out as too small.
	Frames  int `json:"frames"`
	Trimmed int `json:"trimmed"`
}

// Load reads the profile at path and builds a report for sampleType ("" = the
// profile's default).
func Load(path, sampleType string) (Report, error) {
	p, err := cache.get(path)
	if err != nil {
		return Report{}, err
	}
	r, err := Build(p, sampleType)
	if err != nil {
		return Report{}, fmt.Errorf("%s: %w", path, err)
	}
	r.Path = path
	return r, nil
}

// Parse reads a profile file (gzipped protobuf, as written by runtime/pprof).
func Parse(path string) (*profile.Profile, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a directory", path)
	}
	if info.Size() > maxFileSize {
		return nil, fmt.Errorf("%s is larger than %d MB", path, maxFileSize>>20)
	}
	if info.Size() == 0 {
		return nil, fmt.Errorf("%s is empty (the program wrote no profile)", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	p, err := profile.Parse(f)
	if err != nil {
		return nil, fmt.Errorf("%s is not a pprof profile: %w", path, err)
	}
	return p, nil
}

// Build aggregates p into a report for sampleType ("" = the default one).
func Build(p *profile.Profile, sampleType string) (Report, error) {
	if len(p.SampleType) == 0 {
		return Report{}, errors.New("the profile has no sample types")
	}
	index, err := sampleIndex(p, sampleType)
	if err != nil {
		return Report{}, err
	}
	types := make([]string, len(p.SampleType))
	for i, t := range p.SampleType {
		types[i] = t.Type
	}
	agg := aggregate(p, index)
	root, frames, trimmed := agg.frames(TrimFraction)
	return Report{
		SampleTypes: types,
		SampleType:  p.SampleType[index].Type,
		Unit:        p.SampleType[index].Unit,
		Total:       agg.root.total,
		DurationNs:  p.DurationNanos,
		Root:        root,
		Top:         agg.top(TopN),
		Frames:      frames,
		Trimmed:     trimmed,
	}, nil
}

// sampleIndex picks the value index of sampleType, defaulting to the
// profile's DefaultSampleType and then to the last type (pprof's convention:
// "cpu" for CPU profiles, "inuse_space" for heap profiles).
func sampleIndex(p *profile.Profile, sampleType string) (int, error) {
	want := sampleType
	if want == "" {
		want = p.DefaultSampleType
	}
	if want != "" {
		if i := slices.IndexFunc(p.SampleType, func(t *profile.ValueType) bool { return t.Type == want }); i >= 0 {
			return i, nil
		}
		if sampleType != "" {
			return 0, fmt.Errorf("no sample type %q in the profile", sampleType)
		}
	}
	return len(p.SampleType) - 1, nil
}

// parsedCache keeps the last few parsed profiles, so switching sample types or
// reopening a tab doesn't parse the file again.
type parsedCache struct {
	mu      sync.Mutex
	entries []cached
}

type cached struct {
	path    string
	modTime time.Time
	size    int64
	p       *profile.Profile
}

const cacheSize = 4

var cache parsedCache

func (c *parsedCache) get(path string) (*profile.Profile, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	for _, e := range c.entries {
		if e.path == path && e.modTime.Equal(info.ModTime()) && e.size == info.Size() {
			c.mu.Unlock()
			return e.p, nil
		}
	}
	c.mu.Unlock()
	p, err := Parse(path)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entries := slices.DeleteFunc(slices.Clone(c.entries), func(e cached) bool { return e.path == path })
	entries = append([]cached{{path: path, modTime: info.ModTime(), size: info.Size(), p: p}}, entries...)
	if len(entries) > cacheSize {
		entries = entries[:cacheSize]
	}
	c.entries = entries
	return p, nil
}

// forget drops cached profiles under dir (their files are being deleted).
func (c *parsedCache) forget(match func(path string) bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = slices.DeleteFunc(slices.Clone(c.entries), func(e cached) bool { return match(e.path) })
}
