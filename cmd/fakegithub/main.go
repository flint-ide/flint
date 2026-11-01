// Command fakegithub serves internal/github/githubtest's fake GitHub on a
// loopback port, for trying the GitHub features of a development server build
// without GitHub (DEVELOPMENT.md):
//
//	go run ./cmd/fakegithub -addr 127.0.0.1:34230
//	FLINT_DEV_GITHUB=http://127.0.0.1:34230 FLINT_CONFIG_DIR=$(mktemp -d) WAILS_SERVER_PORT=34220 go run -tags server . <project>
//
// The project needs a remote on the fake: git remote add fake http://127.0.0.1:34230/acme/widgets.git.
// Signing in: Settings ▸ Accounts ▸ Sign in with GitHub, then open the page it
// shows (the fake approves the code when the page is opened).
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"flint/internal/github/githubtest"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:34230", "loopback address of the fake GitHub")
	logAddr := flag.String("log-addr", "127.0.0.1:0", "loopback address of the fake log host (redirected Actions logs)")
	flag.Parse()
	for _, a := range []string{*addr, *logAddr} {
		host, _, err := net.SplitHostPort(a)
		if ip := net.ParseIP(host); err != nil || ip == nil || !ip.IsLoopback() {
			log.Fatalf("fakegithub: %q is not a loopback address", a)
		}
	}
	api, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	logs, err := net.Listen("tcp", *logAddr)
	if err != nil {
		log.Fatal(err)
	}
	f := githubtest.New()
	f.Web, f.LogBase = "http://"+api.Addr().String(), "http://"+logs.Addr().String()
	go serve(logs, f.LogHandler())
	fmt.Printf("fake GitHub at %s (logs at %s), token %s\nrepository: %s.git\n", f.Web, f.LogBase, f.Token, f.RepoURL())
	log.Fatal(serve(api, f.Handler()))
}

func serve(l net.Listener, h http.Handler) error {
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	return srv.Serve(l)
}
