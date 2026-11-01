package settings

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Remote configures Flint Remote (internal/remote): a read-only API on
// 127.0.0.1 that a paired phone reaches through the user's own tunnel
// (ssh -L, tailscale serve). Off by default. Paired devices are not settings:
// they live in state.json, their token hashes in the Keychain.
type Remote struct {
	Enabled bool `json:"enabled"`
	// Port on 127.0.0.1; fixed so a tunnel can target it.
	Port int `json:"port"`
	// AllowedHosts are extra host names requests may be addressed to (the
	// Host header, and Origin when a browser sends one), e.g. the name
	// `tailscale serve` gives the laptop. Loopback names are always allowed.
	AllowedHosts []string `json:"allowedHosts"`
	// ReplayEvents and ReplayKB bound the events kept per chat for a client
	// that connects mid-conversation.
	ReplayEvents int `json:"replayEvents"`
	ReplayKB     int `json:"replayKB"`
	// RequestsPerMinute is each paired device's request budget.
	RequestsPerMinute int `json:"requestsPerMinute"`
}

// Bounds of the Remote values.
const (
	DefaultRemotePort        = 7419
	minRemotePort            = 1024
	maxRemotePort            = 65535
	maxRemoteHosts           = 16
	maxRemoteHostLen         = 253
	defaultReplayEvents      = 2000
	minReplayEvents          = 100
	maxReplayEvents          = 20000
	defaultReplayKB          = 4096
	minReplayKB              = 256
	maxReplayKB              = 65536
	defaultRequestsPerMinute = 120
	minRequestsPerMinute     = 10
	maxRequestsPerMinute     = 6000
)

func defaultRemote() Remote {
	return Remote{
		Port: DefaultRemotePort, AllowedHosts: []string{},
		ReplayEvents: defaultReplayEvents, ReplayKB: defaultReplayKB, RequestsPerMinute: defaultRequestsPerMinute,
	}
}

// hostLabel is one DNS label: letters, digits and inner hyphens.
var hostLabel = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

// CheckRemoteHost reports why name can't be an allowed host: it must be a
// plain DNS name (no scheme, port, path or wildcard).
func CheckRemoteHost(name string) error {
	if name == "" || len(name) > maxRemoteHostLen {
		return fmt.Errorf("invalid remote host %q", name)
	}
	for _, label := range strings.Split(strings.TrimSuffix(name, "."), ".") {
		if !hostLabel.MatchString(label) {
			return fmt.Errorf("invalid remote host %q: use a host name like laptop.tail1234.ts.net, without scheme or port", name)
		}
	}
	return nil
}

func validateRemote(r Remote) error {
	if r.Port < minRemotePort || r.Port > maxRemotePort {
		return fmt.Errorf("remote port %d out of range %d–%d", r.Port, minRemotePort, maxRemotePort)
	}
	if len(r.AllowedHosts) > maxRemoteHosts {
		return fmt.Errorf("too many remote hosts (max %d)", maxRemoteHosts)
	}
	seen := map[string]bool{}
	for _, h := range r.AllowedHosts {
		if err := CheckRemoteHost(h); err != nil {
			return err
		}
		key := strings.ToLower(strings.TrimSuffix(h, "."))
		if seen[key] {
			return fmt.Errorf("remote host %q listed twice", h)
		}
		seen[key] = true
	}
	if r.ReplayEvents < minReplayEvents || r.ReplayEvents > maxReplayEvents {
		return fmt.Errorf("remote replay events %d out of range %d–%d", r.ReplayEvents, minReplayEvents, maxReplayEvents)
	}
	if r.ReplayKB < minReplayKB || r.ReplayKB > maxReplayKB {
		return fmt.Errorf("remote replay size %d KB out of range %d–%d", r.ReplayKB, minReplayKB, maxReplayKB)
	}
	if r.RequestsPerMinute < minRequestsPerMinute || r.RequestsPerMinute > maxRequestsPerMinute {
		return fmt.Errorf("remote requests per minute %d out of range %d–%d", r.RequestsPerMinute, minRequestsPerMinute, maxRequestsPerMinute)
	}
	return nil
}

// normalizeRemote keeps the valid values (hosts lowercased, unique, up to
// the limit) and defaults the rest.
func normalizeRemote(r Remote) Remote {
	def := defaultRemote()
	out := r
	if r.Port < minRemotePort || r.Port > maxRemotePort {
		out.Port = def.Port
	}
	out.AllowedHosts = []string{}
	for _, h := range r.AllowedHosts {
		h = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(h), "."))
		if len(out.AllowedHosts) < maxRemoteHosts && CheckRemoteHost(h) == nil && !slices.Contains(out.AllowedHosts, h) {
			out.AllowedHosts = append(out.AllowedHosts, h)
		}
	}
	if r.ReplayEvents < minReplayEvents || r.ReplayEvents > maxReplayEvents {
		out.ReplayEvents = def.ReplayEvents
	}
	if r.ReplayKB < minReplayKB || r.ReplayKB > maxReplayKB {
		out.ReplayKB = def.ReplayKB
	}
	if r.RequestsPerMinute < minRequestsPerMinute || r.RequestsPerMinute > maxRequestsPerMinute {
		out.RequestsPerMinute = def.RequestsPerMinute
	}
	return out
}
