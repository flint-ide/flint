package accounts

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// memSecrets stands in for the keyring; tests never touch the real one.
type memSecrets struct {
	mu    sync.Mutex
	v     map[string]string
	reads int
	fail  error
}

func (m *memSecrets) Get(k string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reads++
	return m.v[k], m.fail
}

func (m *memSecrets) Set(k, v string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	if m.v == nil {
		m.v = map[string]string{}
	}
	if v == "" {
		delete(m.v, k)
	} else {
		m.v[k] = v
	}
	return nil
}

func TestStoreRoundTrip(t *testing.T) {
	sec := &memSecrets{}
	s := NewStore(sec)
	if _, ok, err := s.Token("github", "github.com"); ok || err != nil {
		t.Fatalf("empty store: ok=%v err=%v", ok, err)
	}
	if err := s.Save("github", "github.com", Token{Access: "gho_abc", Scopes: []string{"repo"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sec.v["github:github.com"], "gho_abc") {
		t.Fatalf("keyring = %v", sec.v)
	}
	// A fresh store reads the keyring once, then answers from its cache.
	s2 := NewStore(sec)
	before := sec.reads
	for range 3 {
		tok, ok, err := s2.Token("github", "github.com")
		if !ok || err != nil || tok.Access != "gho_abc" {
			t.Fatalf("Token = %+v %v %v", tok, ok, err)
		}
	}
	if sec.reads-before != 1 {
		t.Errorf("keyring read %d times", sec.reads-before)
	}
	if err := s2.Delete("github", "github.com"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s2.Token("github", "github.com"); ok {
		t.Error("token still there after Delete")
	}
	if _, found := sec.v["github:github.com"]; found {
		t.Error("keyring entry not deleted")
	}
}

func TestStoreRejectsBadTokens(t *testing.T) {
	s := NewStore(&memSecrets{})
	for _, tok := range []string{"", "a b", "a\nb", strings.Repeat("x", 9000), "tök"} {
		if err := s.Save("github", "github.com", Token{Access: tok}); err == nil {
			t.Errorf("token %q accepted", tok)
		}
	}
	sec := &memSecrets{v: map[string]string{"github:github.com": "not json"}}
	if _, _, err := NewStore(sec).Token("github", "github.com"); err == nil {
		t.Error("garbage in the keyring accepted")
	}
	broken := &memSecrets{fail: errors.New("locked")}
	if _, _, err := NewStore(broken).Token("github", "github.com"); err == nil || !strings.Contains(err.Error(), "keyring") {
		t.Errorf("keyring error = %v", err)
	}
}

func TestClean(t *testing.T) {
	a := Clean(Account{
		Provider: "github", Host: "github.com", Login: "octo\ncat", Name: strings.Repeat("n", 300),
		AvatarURL: "javascript:alert(1)", Scopes: []string{"repo", "repo", "", "a b"},
	})
	if a.Login != "octo cat" || len([]rune(a.Name)) != maxNameLen || a.AvatarURL != "" || len(a.Scopes) != 1 {
		t.Fatalf("Clean = %+v", a)
	}
	if Clean(Account{AvatarURL: "https://avatars.githubusercontent.com/u/1?v=4"}).AvatarURL == "" {
		t.Error("https avatar dropped")
	}
}

func TestUpsertWithout(t *testing.T) {
	list := Upsert(nil, Account{Provider: "github", Host: "github.com", Login: "a"})
	list = Upsert(list, Account{Provider: "github", Host: "ghe.corp", Login: "b"})
	list2 := Upsert(list, Account{Provider: "github", Host: "github.com", Login: "c"})
	if len(list2) != 2 || list[0].Login != "a" {
		t.Fatalf("Upsert = %+v (input %+v)", list2, list)
	}
	if a, ok := Find(list2, "github", "github.com"); !ok || a.Login != "c" {
		t.Errorf("Find = %+v", a)
	}
	if got := Without(list2, "github", "github.com"); len(got) != 1 || got[0].Host != "ghe.corp" {
		t.Errorf("Without = %+v", got)
	}
}

func TestRegistry(t *testing.T) {
	var r Registry
	p := Provider{
		ID: "github", Name: "GitHub", Host: func() string { return "github.com" },
		Flow:     func() (DeviceFlow, error) { return DeviceFlow{}, nil },
		Identify: func(context.Context, string, Token) (Account, error) { return Account{}, nil },
	}
	if err := r.Register(p); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(p); err == nil {
		t.Error("duplicate provider registered")
	}
	if err := r.Register(Provider{ID: "Bad Id"}); err == nil {
		t.Error("invalid provider registered")
	}
	if got, ok := r.Get("github"); !ok || got.Name != "GitHub" || len(r.List()) != 1 {
		t.Error("Get/List")
	}
}

// deviceServer is a fake authorization server: the token endpoint answers
// from a script, one answer per poll.
type deviceServer struct {
	*httptest.Server
	mu      sync.Mutex
	answers []string
	polls   int
	forms   []url.Values
}

func newDeviceServer(t *testing.T, codeAnswer string, answers ...string) *deviceServer {
	d := &deviceServer{answers: answers}
	d.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept = %q", r.Header.Get("Accept"))
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		d.forms = append(d.forms, form)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/login/device/code":
			_, _ = io.WriteString(w, strings.ReplaceAll(codeAnswer, "HOST", r.Host))
		case "/login/oauth/access_token":
			a := d.answers[min(d.polls, len(d.answers)-1)]
			d.polls++
			_, _ = io.WriteString(w, a)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(d.Close)
	return d
}

const codeOK = `{"device_code":"3584d83530557fdd1f46af8289938c8ef79f9dc5","user_code":"WDJB-MJHT","verification_uri":"http://HOST/login/device","expires_in":900,"interval":5}`

func (d *deviceServer) flow(sleeps *[]time.Duration) DeviceFlow {
	return DeviceFlow{
		CodeURL: d.URL + "/login/device/code", TokenURL: d.URL + "/login/oauth/access_token",
		ClientID: "Ov23liTEST", Scopes: []string{"repo", "read:org"}, UserAgent: "Flint/test",
		Sleep: func(ctx context.Context, dur time.Duration) error {
			*sleeps = append(*sleeps, dur)
			return ctx.Err()
		},
	}
}

func TestDeviceFlowSignIn(t *testing.T) {
	d := newDeviceServer(t, codeOK,
		`{"error":"authorization_pending"}`,
		`{"error":"slow_down","interval":10}`,
		`{"error":"authorization_pending"}`,
		`{"access_token":"gho_16C7e42F292c6912E7710c838347Ae178B4a","token_type":"bearer","scope":"repo,read:org"}`,
	)
	var sleeps []time.Duration
	f := d.flow(&sleeps)
	code, err := f.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if code.UserCode != "WDJB-MJHT" || code.Interval != 5*time.Second || !strings.HasSuffix(code.VerificationURI, "/login/device") {
		t.Fatalf("code = %+v", code)
	}
	if got := d.forms[0].Get("scope"); got != "repo read:org" {
		t.Errorf("scope = %q", got)
	}
	tok, err := f.Poll(context.Background(), code)
	if err != nil {
		t.Fatal(err)
	}
	if tok.Access != "gho_16C7e42F292c6912E7710c838347Ae178B4a" || len(tok.Scopes) != 2 {
		t.Fatalf("token = %+v", tok)
	}
	want := []time.Duration{5 * time.Second, 5 * time.Second, 10 * time.Second, 10 * time.Second}
	if len(sleeps) != len(want) {
		t.Fatalf("sleeps = %v", sleeps)
	}
	for i := range want {
		if sleeps[i] != want[i] {
			t.Errorf("sleep %d = %v, want %v (slow_down honoured)", i, sleeps[i], want[i])
		}
	}
	last := d.forms[len(d.forms)-1]
	if last.Get("grant_type") != deviceGrantType || last.Get("device_code") == "" || last.Get("client_id") != "Ov23liTEST" {
		t.Errorf("poll form = %v", last)
	}
}

func TestDeviceFlowSlowDownWithoutInterval(t *testing.T) {
	d := newDeviceServer(t, codeOK, `{"error":"slow_down"}`, `{"access_token":"gho_x","token_type":"bearer","scope":""}`)
	var sleeps []time.Duration
	f := d.flow(&sleeps)
	code, _ := f.Start(context.Background())
	if _, err := f.Poll(context.Background(), code); err != nil {
		t.Fatal(err)
	}
	if len(sleeps) != 2 || sleeps[1] != 10*time.Second {
		t.Errorf("sleeps = %v (RFC 8628: +5 s)", sleeps)
	}
}

func TestDeviceFlowEnds(t *testing.T) {
	for answer, want := range map[string]error{
		`{"error":"access_denied"}`: ErrDenied,
		`{"error":"expired_token"}`: ErrExpired,
	} {
		d := newDeviceServer(t, codeOK, answer)
		var sleeps []time.Duration
		f := d.flow(&sleeps)
		code, _ := f.Start(context.Background())
		if _, err := f.Poll(context.Background(), code); !errors.Is(err, want) {
			t.Errorf("%s: err = %v", answer, err)
		}
	}
	d := newDeviceServer(t, codeOK, `{"error":"authorization_pending"}`)
	var sleeps []time.Duration
	f := d.flow(&sleeps)
	code, _ := f.Start(context.Background())
	code.Expires = time.Now().Add(-time.Second)
	if _, err := f.Poll(context.Background(), code); !errors.Is(err, ErrExpired) {
		t.Errorf("past expiry: err = %v", err)
	}
}

func TestDeviceFlowStartErrors(t *testing.T) {
	cases := map[string]string{
		`{"error":"device_flow_disabled","error_description":"Device Flow must be explicitly enabled for this App"}`: "not enabled",
		`{"device_code":"x","user_code":"WDJB-MJHT","verification_uri":"https://evil.example/login/device"}`:         "unexpected verification page",
		`{"device_code":"x","user_code":"<script>","verification_uri":"http://HOST/login/device"}`:                   "invalid code",
		`not json`: "unexpected answer",
	}
	for answer, want := range cases {
		d := newDeviceServer(t, answer)
		var sleeps []time.Duration
		if _, err := d.flow(&sleeps).Start(context.Background()); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", answer, err, want)
		}
	}
	if _, err := (DeviceFlow{CodeURL: "https://github.com/login/device/code"}).Start(context.Background()); err == nil {
		t.Error("started without a client id")
	}
}

func TestVerificationURLNeedsHTTPS(t *testing.T) {
	f := DeviceFlow{CodeURL: "https://github.com/login/device/code"}
	if err := f.checkVerificationURL("http://github.com/login/device"); err == nil {
		t.Error("plain http verification page accepted from an https server")
	}
	if err := f.checkVerificationURL("https://github.com/login/device"); err != nil {
		t.Error(err)
	}
	if err := f.checkVerificationURL("https://user@github.com/login/device"); err == nil {
		t.Error("userinfo accepted")
	}
}
