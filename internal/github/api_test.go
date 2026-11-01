package github

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestEveryAPIRequestCarriesTheHeaders(t *testing.T) {
	fx := newFixture(t)
	if _, err := fx.c.List(ctx(), fx.dir, ListFilter{}, false); err != nil {
		t.Fatal(err)
	}
	for _, r := range fx.fake.Requests() {
		if r.Auth != "Bearer "+fx.fake.Token {
			t.Errorf("%s %s: Authorization = %q", r.Method, r.Path, r.Auth)
		}
	}
}

func TestUnauthorizedMeansSignIn(t *testing.T) {
	fx := newFixture(t)
	fx.fake.Token = "something-else"
	st := fx.c.Check(ctx(), fx.dir, true)
	if st.State != "unauthenticated" || !strings.Contains(st.Message, "Sign in with GitHub") && !strings.Contains(st.Message, "sign in with GitHub") {
		t.Fatalf("status = %+v", st)
	}
	if strings.Contains(st.Message, fx.auth.Token) {
		t.Error("the token is in the message")
	}
}

func TestNoTokenMeansSignIn(t *testing.T) {
	c := New(Config{})
	st := c.Check(ctx(), t.TempDir(), false)
	if st.State != "unauthenticated" {
		t.Fatalf("status = %+v", st)
	}
}

func TestSecondaryRateLimitIsRetriedOnce(t *testing.T) {
	fx := newFixture(t)
	fx.fake.FailNext("/api/v3/repos/acme/widgets", http.StatusForbidden, map[string]string{"Retry-After": "1"},
		`{"message":"You have exceeded a secondary rate limit. Please wait a few minutes before you try again."}`)
	start := time.Now()
	if _, err := fx.c.Repo(ctx(), fx.dir); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < time.Second {
		t.Error("Retry-After not honoured")
	}
}

func TestRateLimitBeyondTheWaitIsReported(t *testing.T) {
	fx := newFixture(t)
	fx.fake.FailNext("/api/v3/repos/acme/widgets", http.StatusTooManyRequests, map[string]string{"Retry-After": "120"}, `{"message":"slow down"}`)
	_, err := fx.c.Repo(ctx(), fx.dir)
	var rl *RateLimitError
	if !errors.As(err, &rl) || !rl.Secondary || !strings.Contains(err.Error(), "2m0s") {
		t.Fatalf("err = %v", err)
	}
	reset := time.Now().Add(30 * time.Minute).Unix()
	fx.fake.FailNext("/api/v3/repos/acme/widgets", http.StatusForbidden,
		map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": itoa(reset)}, `{"message":"API rate limit exceeded"}`)
	fx.c.Reset()
	_, err = fx.c.Repo(ctx(), fx.dir)
	if !errors.As(err, &rl) || rl.Secondary || !strings.Contains(err.Error(), "rate limit is used up until") {
		t.Fatalf("primary limit: err = %v", err)
	}
}

func TestMutationsAreNotRetried(t *testing.T) {
	fx := newFixture(t)
	if _, err := fx.c.Details(ctx(), fx.dir, 7, false); err != nil {
		t.Fatal(err)
	}
	fx.fake.FailNext("/api/graphql", http.StatusForbidden, map[string]string{"Retry-After": "1"}, `{"message":"secondary rate limit"}`)
	err := fx.c.Reply(ctx(), fx.dir, 7, "PRRT_kwDOAAABc84AAAAB", "ok")
	var rl *RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("err = %v", err)
	}
	if len(fx.fake.Mutations()) != 0 {
		t.Error("a mutation was retried")
	}
}

func TestTokenNeverLeavesTheAPIHost(t *testing.T) {
	var got []string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("Authorization"))
		_, _ = io.WriteString(w, `{}`)
	}))
	defer other.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/elsewhere", http.StatusFound)
	}))
	defer api.Close()
	e, _ := LocalEndpoints(api.URL)
	a := Auth{Endpoints: e, Token: "gho_secret"}
	c := New(Config{})
	err := c.getJSON(ctx(), a, call{url: rest(a, "/repos/a/b")}, &struct{}{})
	if err == nil || len(got) != 0 {
		t.Fatalf("cross-host redirect followed: err=%v auth=%v", err, got)
	}
	if strings.Contains(err.Error(), "gho_secret") {
		t.Error("token in error")
	}
	// A URL outside the API is refused before anything is sent.
	if err := c.getJSON(ctx(), a, call{url: other.URL + "/x"}, nil); err == nil || len(got) != 0 {
		t.Fatalf("call outside the API: err=%v auth=%v", err, got)
	}
}

func TestAnswerSizeIsCapped(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `"`+strings.Repeat("x", 2048)+`"`)
	}))
	defer api.Close()
	e, _ := LocalEndpoints(api.URL)
	a := Auth{Endpoints: e, Token: "t"}
	c := New(Config{})
	if _, err := c.do(ctx(), a, call{method: http.MethodGet, url: rest(a, "/x"), limit: 1024}); err == nil || !strings.Contains(err.Error(), "larger") {
		t.Fatalf("err = %v", err)
	}
	resp, err := c.do(ctx(), a, call{method: http.MethodGet, url: rest(a, "/x"), limit: 1024, truncate: true})
	if err != nil || len(resp.body) != 1024 {
		t.Fatalf("truncate: %d bytes, %v", len(resp.body), err)
	}
}

func TestAPIErrorsCarryGitHubsMessage(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"message":"Validation Failed","errors":[{"resource":"PullRequest","code":"custom","message":"A pull request already exists for acme:x."}]}`)
	}))
	defer api.Close()
	e, _ := LocalEndpoints(api.URL)
	a := Auth{Endpoints: e, Token: "t"}
	err := New(Config{}).getJSON(ctx(), a, call{url: rest(a, "/x")}, nil)
	if err == nil || err.Error() != "GitHub: Validation Failed: A pull request already exists for acme:x. (HTTP 422)" {
		t.Fatalf("err = %v", err)
	}
}

func TestNextLink(t *testing.T) {
	h := `<https://api.github.com/repositories/1/pulls/7/files?per_page=100&page=2>; rel="next", <https://api.github.com/repositories/1/pulls/7/files?per_page=100&page=3>; rel="last"`
	if got := nextLink(h); got != "https://api.github.com/repositories/1/pulls/7/files?per_page=100&page=2" {
		t.Fatalf("nextLink = %q", got)
	}
	if nextLink(`<https://x>; rel="prev"`) != "" || nextLink("") != "" {
		t.Error("next link invented")
	}
}

func TestPagesStayOnTheAPIHost(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", `<https://evil.example/next>; rel="next"`)
		_, _ = io.WriteString(w, `[]`)
	}))
	defer api.Close()
	e, _ := LocalEndpoints(api.URL)
	a := Auth{Endpoints: e, Token: "t"}
	err := New(Config{}).pages(ctx(), a, rest(a, "/x"), 5, func([]byte) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "another host") {
		t.Fatalf("err = %v", err)
	}
}

func TestEndpoints(t *testing.T) {
	cases := map[string][3]string{
		"github.com":      {"https://github.com", "https://api.github.com", "https://api.github.com/graphql"},
		"GHE.corp.com":    {"https://ghe.corp.com", "https://ghe.corp.com/api/v3", "https://ghe.corp.com/api/graphql"},
		"octo.ghe.com":    {"https://octo.ghe.com", "https://api.octo.ghe.com", "https://api.octo.ghe.com/graphql"},
		"ghe.corp:8443":   {"https://ghe.corp:8443", "https://ghe.corp:8443/api/v3", "https://ghe.corp:8443/api/graphql"},
		"github.com:443x": {},
	}
	for host, want := range cases {
		e, err := EndpointsFor(host)
		if want[0] == "" {
			if err == nil {
				t.Errorf("%q accepted", host)
			}
			continue
		}
		if err != nil || e.Web != want[0] || e.API != want[1] || e.GraphQL != want[2] {
			t.Errorf("%q: %+v %v", host, e, err)
		}
	}
	for _, bad := range []string{"https://127.0.0.1:1", "http://10.0.0.1:80", "http://example.com:80", "http://127.0.0.1", "http://127.0.0.1:9/x", "http://u@127.0.0.1:9"} {
		if _, err := LocalEndpoints(bad); err == nil {
			t.Errorf("LocalEndpoints(%q) accepted", bad)
		}
	}
	if e, err := LocalEndpoints("http://127.0.0.1:34230"); err != nil || e.API != "http://127.0.0.1:34230/api/v3" || e.Host != "127.0.0.1:34230" {
		t.Errorf("LocalEndpoints = %+v, %v", e, err)
	}
}

func TestEnvToken(t *testing.T) {
	env := map[string]string{"GITHUB_TOKEN": "gh1", "GH_ENTERPRISE_TOKEN": "ghe1"}
	get := func(k string) string { return env[k] }
	if n, v := EnvToken("github.com", get); n != "GITHUB_TOKEN" || v != "gh1" {
		t.Errorf("github.com: %s %s", n, v)
	}
	env["GH_TOKEN"] = "gh0"
	if n, v := EnvToken("github.com", get); n != "GH_TOKEN" || v != "gh0" {
		t.Errorf("GH_TOKEN first: %s %s", n, v)
	}
	if n, v := EnvToken("ghe.corp", get); n != "GH_ENTERPRISE_TOKEN" || v != "ghe1" {
		t.Errorf("ghe: %s %s", n, v)
	}
	if n, _ := EnvToken("octo.ghe.com", get); n != "GH_TOKEN" {
		t.Errorf("ghe.com: %s", n)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
