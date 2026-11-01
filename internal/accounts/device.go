package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Errors that end a device-flow sign-in.
var (
	ErrDenied  = errors.New("sign-in was denied in the browser")
	ErrExpired = errors.New("the sign-in code expired before it was entered: start again")
)

// Bounds of the device flow (security limits, not settings).
const (
	maxDeviceBody     = 64 << 10
	defaultInterval   = 5 * time.Second
	minInterval       = time.Second
	maxInterval       = 60 * time.Second
	slowDownStep      = 5 * time.Second
	defaultExpiresIn  = 15 * time.Minute
	maxExpiresIn      = 30 * time.Minute
	maxUserCodeLen    = 32
	maxDeviceCodeLen  = 1024
	deviceGrantType   = "urn:ietf:params:oauth:grant-type:device_code"
	deviceHTTPTimeout = 30 * time.Second
)

// DeviceFlow is RFC 8628 against one authorization server.
type DeviceFlow struct {
	CodeURL  string // device authorization endpoint
	TokenURL string // token endpoint
	ClientID string
	Scopes   []string
	// VerificationHost is the only host the verification URL may point to;
	// "" means the host of CodeURL.
	VerificationHost string
	UserAgent        string
	HTTP             *http.Client
	// Sleep waits between polls (tests replace it).
	Sleep func(ctx context.Context, d time.Duration) error
}

// DeviceCode is a started sign-in: the code the user types at VerificationURI.
type DeviceCode struct {
	UserCode        string
	VerificationURI string
	Interval        time.Duration
	Expires         time.Time
	deviceCode      string
}

var userCodeRE = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// deviceAnswer covers both endpoints' JSON (GitHub answers errors with 200).
type deviceAnswer struct {
	DeviceCode       string  `json:"device_code"`
	UserCode         string  `json:"user_code"`
	VerificationURI  string  `json:"verification_uri"`
	VerificationURL  string  `json:"verification_url"` // Google's name for it
	ExpiresIn        float64 `json:"expires_in"`
	Interval         float64 `json:"interval"`
	AccessToken      string  `json:"access_token"`
	RefreshToken     string  `json:"refresh_token"`
	TokenType        string  `json:"token_type"`
	Scope            string  `json:"scope"`
	Error            string  `json:"error"`
	ErrorDescription string  `json:"error_description"`
}

// Start asks for a device and user code.
func (f DeviceFlow) Start(ctx context.Context) (DeviceCode, error) {
	if f.ClientID == "" {
		return DeviceCode{}, errors.New("no OAuth client id is configured")
	}
	form := url.Values{"client_id": {f.ClientID}}
	if len(f.Scopes) > 0 {
		form.Set("scope", strings.Join(f.Scopes, " "))
	}
	a, err := f.post(ctx, f.CodeURL, form)
	if err != nil {
		return DeviceCode{}, err
	}
	if a.Error != "" {
		return DeviceCode{}, deviceError(a)
	}
	verify := a.VerificationURI
	if verify == "" {
		verify = a.VerificationURL
	}
	if err := f.checkVerificationURL(verify); err != nil {
		return DeviceCode{}, err
	}
	if a.DeviceCode == "" || len(a.DeviceCode) > maxDeviceCodeLen || len(a.UserCode) > maxUserCodeLen || !userCodeRE.MatchString(a.UserCode) {
		return DeviceCode{}, errors.New("the sign-in server sent an invalid code")
	}
	expires := seconds(a.ExpiresIn, defaultExpiresIn)
	return DeviceCode{
		UserCode:        a.UserCode,
		VerificationURI: verify,
		Interval:        clampInterval(seconds(a.Interval, defaultInterval)),
		Expires:         time.Now().Add(min(expires, maxExpiresIn)),
		deviceCode:      a.DeviceCode,
	}, nil
}

// Poll waits until the user entered the code (a token), denied it, or it
// expired, honouring the server's interval and slow_down answers.
func (f DeviceFlow) Poll(ctx context.Context, code DeviceCode) (Token, error) {
	ctx, cancel := context.WithDeadline(ctx, code.Expires)
	defer cancel()
	interval := clampInterval(code.Interval)
	sleep := f.Sleep
	if sleep == nil {
		sleep = sleepCtx
	}
	form := url.Values{"client_id": {f.ClientID}, "device_code": {code.deviceCode}, "grant_type": {deviceGrantType}}
	for {
		if err := sleep(ctx, interval); err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return Token{}, ErrExpired
			}
			return Token{}, err
		}
		a, err := f.post(ctx, f.TokenURL, form)
		if err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return Token{}, ErrExpired
			}
			return Token{}, err
		}
		switch a.Error {
		case "":
			if err := CheckToken(a.AccessToken); err != nil {
				return Token{}, errors.New("the sign-in server sent an invalid token")
			}
			if a.RefreshToken != "" && CheckToken(a.RefreshToken) != nil {
				return Token{}, errors.New("the sign-in server sent an invalid refresh token")
			}
			t := Token{Access: a.AccessToken, Refresh: a.RefreshToken, Scopes: splitScopes(a.Scope)}
			if a.ExpiresIn > 0 {
				t.Expiry = time.Now().Add(seconds(a.ExpiresIn, 0))
			}
			return t, nil
		case "authorization_pending":
		case "slow_down":
			// RFC 8628 §3.5: add 5 seconds; GitHub also says the new interval.
			interval = clampInterval(max(interval+slowDownStep, seconds(a.Interval, 0)))
		case "expired_token":
			return Token{}, ErrExpired
		case "access_denied":
			return Token{}, ErrDenied
		default:
			return Token{}, deviceError(a)
		}
	}
}

func (f DeviceFlow) post(ctx context.Context, endpoint string, form url.Values) (deviceAnswer, error) {
	ctx, cancel := context.WithTimeout(ctx, deviceHTTPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return deviceAnswer{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if f.UserAgent != "" {
		req.Header.Set("User-Agent", f.UserAgent)
	}
	hc := f.HTTP
	if hc == nil {
		hc = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return deviceAnswer{}, fmt.Errorf("could not reach %s: %w", req.URL.Host, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDeviceBody))
	if err != nil {
		return deviceAnswer{}, err
	}
	var a deviceAnswer
	if jerr := json.Unmarshal(body, &a); jerr != nil || (resp.StatusCode != http.StatusOK && a.Error == "") {
		if resp.StatusCode == http.StatusNotFound {
			return deviceAnswer{}, fmt.Errorf("%s does not know this OAuth client id (404): check the client id and that Device Flow is enabled for the app", req.URL.Host)
		}
		return deviceAnswer{}, fmt.Errorf("unexpected answer from %s (HTTP %d)", req.URL.Host, resp.StatusCode)
	}
	return a, nil
}

// checkVerificationURL accepts only an https page on the expected host, so a
// tampered answer can't make Flint open some other site.
func (f DeviceFlow) checkVerificationURL(raw string) error {
	want := f.VerificationHost
	code, err := url.Parse(f.CodeURL)
	if err != nil {
		return err
	}
	if want == "" {
		want = code.Host
	}
	u, err := url.Parse(raw)
	// A plain-http verification page is only accepted from a plain-http
	// server (a local fake in development).
	schemeOK := u != nil && (u.Scheme == "https" || (u.Scheme == "http" && code.Scheme == "http"))
	if err != nil || !schemeOK || u.Host != want || u.User != nil || len(raw) > maxURLLen {
		return fmt.Errorf("the sign-in server sent an unexpected verification page (%.100q)", raw)
	}
	return nil
}

func deviceError(a deviceAnswer) error {
	switch a.Error {
	case "device_flow_disabled":
		return errors.New("Device Flow is not enabled for this OAuth App: enable it in the app's settings on GitHub")
	case "incorrect_client_credentials", "invalid_client", "unauthorized_client":
		return errors.New("the OAuth client id was not accepted: check Settings ▸ Accounts")
	}
	msg := oneLine(a.ErrorDescription, 200)
	if msg == "" {
		msg = oneLine(a.Error, 100)
	}
	return fmt.Errorf("sign-in failed: %s", msg)
}

func seconds(v float64, def time.Duration) time.Duration {
	if v <= 0 || v > 86400 {
		return def
	}
	return time.Duration(v * float64(time.Second))
}

func clampInterval(d time.Duration) time.Duration {
	if d <= 0 {
		return defaultInterval
	}
	return max(minInterval, min(maxInterval, d))
}

func splitScopes(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' })
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ParseScopes splits a scope header ("repo, read:org").
func ParseScopes(header string) []string { return splitScopes(header) }

// Since formats a sign-in time for Account.SignedInAt.
func Since(t time.Time) string { return t.UTC().Format(time.RFC3339) }
