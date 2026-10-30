package httpfile

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Every test talks to an httptest.Server on 127.0.0.1; nothing leaves the machine.

func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		fmt.Fprint(w, `{"method":"`+r.Method+`","list":[1,2]}`)
	})
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "%s %s|%s|%s|%s", r.Method, r.Header.Get("X-Token"), r.Host, r.UserAgent(), body)
	})
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/json", http.StatusFound)
	})
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	})
	mux.HandleFunc("/big", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write(bytes.Repeat([]byte("é"), DisplayLimit)) // 2 bytes each: twice the limit
	})
	mux.HandleFunc("/set-cookie", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "s1", Path: "/"})
	})
	mux.HandleFunc("/cookie", func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("session")
		if err != nil {
			fmt.Fprint(w, "none")
			return
		}
		fmt.Fprint(w, c.Value)
	})
	mux.HandleFunc("/gzip", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		zw := gzip.NewWriter(w)
		_, _ = zw.Write([]byte(`{"zipped":true}`))
		_ = zw.Close()
	})
	mux.HandleFunc("/png", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func get(url string) Prepared { return Prepared{Method: "GET", URL: url, Key: "GET " + url} }

func send(t *testing.T, c *Client, p Prepared, opts Options) *Response {
	t.Helper()
	resp, err := c.Send(context.Background(), "id-"+t.Name(), "/p/api.http", p, opts)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestSendPrettyPrintsJSON(t *testing.T) {
	srv := testServer(t)
	resp := send(t, NewClient(), get(srv.URL+"/json"), Options{})
	if resp.Status != 200 || resp.StatusText != "200 OK" || resp.Kind != KindJSON || resp.ContentType != "application/json" {
		t.Errorf("got %+v", resp)
	}
	if want := "{\n  \"method\": \"GET\",\n  \"list\": [\n    1,\n    2\n  ]\n}"; resp.Body != want {
		t.Errorf("body = %q", resp.Body)
	}
	if resp.Size != int64(len(`{"method":"GET","list":[1,2]}`)) || resp.TimeMs < 0 {
		t.Errorf("size %d, time %v", resp.Size, resp.TimeMs)
	}
	var sawType bool
	for _, h := range resp.Headers {
		sawType = sawType || h.Name == "Content-Type"
	}
	if !sawType {
		t.Errorf("headers = %+v", resp.Headers)
	}
}

func TestSendHeadersBodyAndHost(t *testing.T) {
	srv := testServer(t)
	p := Prepared{Method: "POST", URL: srv.URL + "/echo", Body: []byte("hello"),
		Headers: []Header{{Name: "X-Token", Value: "t1"}, {Name: "Host", Value: "api.local"}}}
	resp := send(t, NewClient(), p, Options{})
	if resp.Body != "POST t1|api.local|"+userAgent+"|hello" || resp.Kind != KindText {
		t.Errorf("body = %q kind %s", resp.Body, resp.Kind)
	}
}

func TestSendRedirects(t *testing.T) {
	srv := testServer(t)
	c := NewClient()
	followed := send(t, c, get(srv.URL+"/redirect"), Options{FollowRedirects: true})
	if followed.Status != 200 || followed.FinalURL != srv.URL+"/json" {
		t.Errorf("followed: %d %s", followed.Status, followed.FinalURL)
	}
	stopped := send(t, c, get(srv.URL+"/redirect"), Options{FollowRedirects: false})
	if stopped.Status != http.StatusFound || stopped.FinalURL != srv.URL+"/redirect" {
		t.Errorf("not followed: %d %s", stopped.Status, stopped.FinalURL)
	}
	directive := get(srv.URL + "/redirect")
	directive.NoRedirect = true
	if r := send(t, c, directive, Options{FollowRedirects: true}); r.Status != http.StatusFound {
		t.Errorf("@no-redirect: %d", r.Status)
	}
}

func TestSendTimeout(t *testing.T) {
	srv := testServer(t)
	start := time.Now()
	_, err := NewClient().Send(context.Background(), "slow", "/p/api.http", get(srv.URL+"/slow"), Options{Timeout: 50 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "timed out after 50ms") {
		t.Errorf("err = %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Error("timeout did not stop the request")
	}
	p := get(srv.URL + "/slow")
	p.Timeout = 30 * time.Millisecond // @timeout wins over the caller's
	if _, err := NewClient().Send(context.Background(), "slow", "/p/api.http", p, Options{Timeout: time.Minute}); err == nil || !strings.Contains(err.Error(), "30ms") {
		t.Errorf("@timeout: err = %v", err)
	}
}

func TestSendCancel(t *testing.T) {
	srv := testServer(t)
	c := NewClient()
	done := make(chan error, 1)
	go func() {
		_, err := c.Send(context.Background(), "r1", "/p/api.http", get(srv.URL+"/slow"), Options{})
		done <- err
	}()
	// Cancel as soon as the request is registered.
	deadline := time.Now().Add(5 * time.Second)
	for {
		c.mu.Lock()
		_, running := c.running["r1"]
		c.mu.Unlock()
		if running || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	c.Cancel("r1")
	c.Cancel("unknown") // ignored
	select {
	case err := <-done:
		if err == nil || err.Error() != "request cancelled" {
			t.Errorf("err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancel did not stop the request")
	}
	if _, err := c.Send(context.Background(), "r1", "/p/api.http", get(srv.URL+"/json"), Options{}); err != nil {
		t.Errorf("the id is free again after cancel: %v", err)
	}
}

func TestSendTruncatesDisplayButKeepsBody(t *testing.T) {
	srv := testServer(t)
	c := NewClient()
	resp := send(t, c, get(srv.URL+"/big"), Options{})
	if !resp.Truncated || resp.Size != 2*DisplayLimit || len(resp.Body) > DisplayLimit || len(resp.Body) < DisplayLimit-4 {
		t.Errorf("truncated %v, size %d, shown %d", resp.Truncated, resp.Size, len(resp.Body))
	}
	if !strings.HasSuffix(resp.Body, "é") {
		t.Error("cut inside a UTF-8 sequence")
	}
	raw, ok := c.Body(resp.ID)
	if !ok || len(raw) != 2*DisplayLimit {
		t.Errorf("saved body %d bytes, ok %v", len(raw), ok)
	}
}

func TestCookieJarPerFile(t *testing.T) {
	srv := testServer(t)
	c := NewClient()
	if _, err := c.Send(context.Background(), "a", "/p/a.http", get(srv.URL+"/set-cookie"), Options{}); err != nil {
		t.Fatal(err)
	}
	same, _ := c.Send(context.Background(), "b", "/p/a.http", get(srv.URL+"/cookie"), Options{})
	other, _ := c.Send(context.Background(), "c", "/p/b.http", get(srv.URL+"/cookie"), Options{})
	if same.Body != "s1" || other.Body != "none" {
		t.Errorf("same file %q, other file %q", same.Body, other.Body)
	}
	noJar := get(srv.URL + "/cookie")
	noJar.NoCookieJar = true
	if r, _ := c.Send(context.Background(), "d", "/p/a.http", noJar, Options{}); r.Body != "none" {
		t.Errorf("@no-cookie-jar sent %q", r.Body)
	}
	c.ClearCookies("/p/a.http")
	if r, _ := c.Send(context.Background(), "e", "/p/a.http", get(srv.URL+"/cookie"), Options{}); r.Body != "none" {
		t.Errorf("after clearing: %q", r.Body)
	}
}

func TestSendDecodesAndDetects(t *testing.T) {
	srv := testServer(t)
	c := NewClient()
	p := get(srv.URL + "/gzip")
	p.Headers = []Header{{Name: "Accept-Encoding", Value: "gzip"}} // the transport then leaves it compressed
	if r := send(t, c, p, Options{}); r.Kind != KindJSON || r.Body != "{\n  \"zipped\": true\n}" {
		t.Errorf("gzip: %s %q", r.Kind, r.Body)
	}
	if r := send(t, c, get(srv.URL+"/png"), Options{}); r.Kind != KindImage || r.Body != "" || r.ContentType != "image/png" {
		t.Errorf("png: %s %q %s", r.Kind, r.Body, r.ContentType)
	}
}

func TestSendErrors(t *testing.T) {
	srv := testServer(t)
	tlsSrv := httptest.NewUnstartedServer(http.NotFoundHandler()) // self-signed, local
	tlsSrv.Config.ErrorLog = log.New(io.Discard, "", 0)           // the failed handshake is expected
	tlsSrv.StartTLS()
	t.Cleanup(tlsSrv.Close)
	c := NewClient()
	if _, err := c.Send(context.Background(), "x", "/p/api.http", get(tlsSrv.URL), Options{}); err == nil || !strings.Contains(err.Error(), "unknown authority") {
		t.Errorf("TLS: %v", err)
	}
	https := "https://" + strings.TrimPrefix(srv.URL, "http://")
	if _, err := c.Send(context.Background(), "x", "/p/api.http", get(https), Options{}); err == nil || !strings.Contains(err.Error(), "did not answer with TLS") {
		t.Errorf("https to http: %v", err)
	}
	if _, err := c.Send(context.Background(), "x", "/p/api.http", Prepared{Method: "GET", URL: "http://[::1"}, Options{}); err == nil {
		t.Error("invalid URL should fail")
	}
}

func TestHistoryIsBounded(t *testing.T) {
	srv := testServer(t)
	c := NewClient()
	var last *Response
	for i := range historyPerRequest + 3 {
		resp, err := c.Send(context.Background(), fmt.Sprint("h", i), "/p/api.http", get(srv.URL+"/json"), Options{})
		if err != nil {
			t.Fatal(err)
		}
		last = resp
	}
	send(t, c, get(srv.URL+"/echo"), Options{})
	hist := c.History("/p/api.http")
	if len(hist) != historyPerRequest+1 {
		t.Fatalf("kept %d responses", len(hist))
	}
	if hist[0].Key != "GET "+srv.URL+"/echo" || hist[0].Body != "" || hist[0].Headers != nil {
		t.Errorf("newest first, without bodies: %+v", hist[0])
	}
	if _, ok := c.Response("h0"); ok {
		t.Error("the oldest response should be gone")
	}
	if r, ok := c.Response(last.ID); !ok || r.Body == "" {
		t.Error("recent responses keep their body")
	}
	if curl, ok := c.CurlOf(last.ID); !ok || !strings.HasPrefix(curl, "curl") {
		t.Errorf("curl = %q", curl)
	}
	if len(c.History("/p/other.http")) != 0 {
		t.Error("history is per file")
	}
}

func TestHistoryByteBudgetKeepsNewest(t *testing.T) {
	big := entry{resp: &Response{ID: "big", File: "f", Key: "a"}, raw: make([]byte, historyBytes)}
	h := history{}.add(entry{resp: &Response{ID: "old", File: "f", Key: "b"}, raw: []byte("x")}).add(big)
	if len(h.entries) != 1 || h.entries[0].resp.ID != "big" {
		t.Errorf("kept %d entries", len(h.entries))
	}
}
