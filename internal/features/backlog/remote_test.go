package backlog

import "testing"

func TestRepoKey(t *testing.T) {
	for _, tc := range []struct {
		url, want string
	}{
		{"git@github.com:acme/api.git", "github.com/acme/api"},
		{"git@github.com:acme/api", "github.com/acme/api"},
		{"github.com:acme/api.git", "github.com/acme/api"},
		{"https://github.com/acme/api.git", "github.com/acme/api"},
		{"https://github.com/acme/api", "github.com/acme/api"},
		{"https://github.com/acme/api/", "github.com/acme/api"},
		{"https://github.com/acme/api.git/", "github.com/acme/api"},
		{"https://user:token@github.com/acme/api.git", "github.com/acme/api"},
		{"https://GitHub.COM/acme/api", "github.com/acme/api"},
		{"ssh://git@github.com/acme/api.git", "github.com/acme/api"},
		{"ssh://git@github.com:22/acme/api.git", "github.com/acme/api"},
		{"git+ssh://git@GITHUB.com/acme/api", "github.com/acme/api"},
		{"git://github.com/acme/api.git", "github.com/acme/api"},
		{"http://ghe.corp:8080/acme/api.git", "ghe.corp/acme/api"},
		{"ssh://git@bitbucket.corp:7999/proj/api.git", "bitbucket.corp/proj/api"},
		{"https://gitlab.com/group/sub/api.git", "gitlab.com/group/sub/api"},
		{"https://gitlab.com//group//api", "gitlab.com/group/api"},
		{"  git@github.com:acme/api.git\n", "github.com/acme/api"},
		{"/srv/git/api.git", "file:/srv/git/api"},
		{"file:///srv/git/api.git", "file:/srv/git/api"},
		{"file:///srv/git/../git/api", "file:/srv/git/api"},
		{"https://github.com/Acme/API", "github.com/Acme/API"}, // paths keep their case
	} {
		got, ok := RepoKey(tc.url)
		if !ok || got != tc.want {
			t.Errorf("RepoKey(%q) = %q, %v; want %q", tc.url, got, ok, tc.want)
		}
	}
	for _, bad := range []string{
		"", "api", "./api", "../api", "relative/path:x", "https://", "https:///acme/api", "ftp://host/acme/api",
		"https://github.com/", "git@github.com:", "git@github.com:/", "https://github.com/acme/../api",
		"git@github.com:acme/ api", "file://otherhost/srv/api", "git@[::1]:acme/api",
	} {
		if got, ok := RepoKey(bad); ok {
			t.Errorf("RepoKey(%q) = %q, want not a remote URL", bad, got)
		}
	}
}

func TestSameRepository(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		same bool
	}{
		{"git@github.com:acme/api.git", "https://github.com/acme/api", true},
		{"ssh://git@github.com/acme/api.git", "https://user@GITHUB.com/acme/api.git/", true},
		{"git@github.com:acme/api.git", "git@github.com:acme/web.git", false},
		{"git@github.com:acme/api.git", "git@github.com:other/api.git", false},
		{"git@github.com:acme/api.git", "git@gitlab.com:acme/api.git", false},
		{"https://github.com/acme/api", "https://github.com/acme/api-v2", false},
		{"https://github.com/acme/api", "https://github.com/acme/Api", false},
		{"https://github.com/acme/api", "https://github.com/acme", false},
		{"not a url", "not a url", false},
		{"", "", false},
	} {
		if got := SameRepository(tc.a, tc.b); got != tc.same {
			t.Errorf("SameRepository(%q, %q) = %v", tc.a, tc.b, got)
		}
		if got := SameRepository(tc.b, tc.a); got != tc.same {
			t.Errorf("SameRepository(%q, %q) = %v (swapped)", tc.b, tc.a, got)
		}
	}
}
