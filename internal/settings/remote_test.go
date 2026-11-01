package settings

import (
	"slices"
	"strings"
	"testing"
)

func TestRemoteDefaultsAreOff(t *testing.T) {
	r := Default().Remote
	if r.Enabled || r.Port != DefaultRemotePort || len(r.AllowedHosts) != 0 || r.ReplayEvents != 2000 || r.ReplayKB != 4096 || r.RequestsPerMinute != 120 {
		t.Fatalf("defaults %+v", r)
	}
	if r.AllowedHosts == nil {
		t.Fatal("allowed hosts must be [] (the frontend expects a list)")
	}
}

func TestRemoteLoadKeepsValidValuesAndDefaultsTheRest(t *testing.T) {
	got, err := Load(write(t, `{"remote":{"enabled":true,"port":80,"allowedHosts":["Laptop.tail1234.ts.net","bad host","laptop.tail1234.ts.net"],"replayEvents":5,"requestsPerMinute":600}}`))
	if err != nil {
		t.Fatal(err)
	}
	r := got.Remote
	if !r.Enabled || r.Port != DefaultRemotePort || r.ReplayEvents != 2000 || r.RequestsPerMinute != 600 || r.ReplayKB != 4096 {
		t.Fatalf("loaded %+v", r)
	}
	if !slices.Equal(r.AllowedHosts, []string{"laptop.tail1234.ts.net"}) {
		t.Fatalf("hosts = %q", r.AllowedHosts)
	}
}

func TestRemoteValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Remote)
		want   string
	}{
		{"low port", func(r *Remote) { r.Port = 1023 }, "port"},
		{"high port", func(r *Remote) { r.Port = 70000 }, "port"},
		{"host with space", func(r *Remote) { r.AllowedHosts = []string{"a b"} }, "host"},
		{"host with port", func(r *Remote) { r.AllowedHosts = []string{"a.ts.net:443"} }, "host"},
		{"host with scheme", func(r *Remote) { r.AllowedHosts = []string{"https://a.ts.net"} }, "host"},
		{"wildcard", func(r *Remote) { r.AllowedHosts = []string{"*.ts.net"} }, "host"},
		{"leading dash", func(r *Remote) { r.AllowedHosts = []string{"-a.ts.net"} }, "host"},
		{"empty label", func(r *Remote) { r.AllowedHosts = []string{"a..ts.net"} }, "host"},
		{"duplicate", func(r *Remote) { r.AllowedHosts = []string{"a.ts.net", "a.ts.net"} }, "twice"},
		{"too many", func(r *Remote) {
			for i := range maxRemoteHosts + 1 {
				r.AllowedHosts = append(r.AllowedHosts, strings.Repeat("a", i+1)+".ts.net")
			}
		}, "hosts"},
		{"replay events", func(r *Remote) { r.ReplayEvents = 10 }, "replay"},
		{"replay size", func(r *Remote) { r.ReplayKB = 1 << 20 }, "replay"},
		{"rate", func(r *Remote) { r.RequestsPerMinute = 1 }, "requests"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := Default()
			tc.change(&s.Remote)
			if err := Validate(s); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate = %v, want an error about %q", err, tc.want)
			}
		})
	}
	s := Default()
	s.Remote.AllowedHosts = []string{"laptop.tail1234.ts.net", "devbox"}
	s.Remote.Port = 34160
	if err := Validate(s); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteCloneCopiesHosts(t *testing.T) {
	s := Default()
	s.Remote.AllowedHosts = []string{"a.ts.net"}
	c := s.Clone()
	c.Remote.AllowedHosts[0] = "b.ts.net"
	if s.Remote.AllowedHosts[0] != "a.ts.net" {
		t.Fatal("Clone shares the allowed hosts")
	}
}

func TestCheckRemoteHost(t *testing.T) {
	if CheckRemoteHost("laptop.tail1234.ts.net") != nil || CheckRemoteHost("bad host") == nil {
		t.Fatal("CheckRemoteHost")
	}
}
