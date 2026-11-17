package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"flint/internal/mcpserver"
	"flint/internal/mcptools"
)

// grantApp is a chat app with orchestration on, a trusted project, one chat
// in it, and a dialog that records its questions and answers as told.
type grantApp struct {
	*chatApp
	project string
	chat    string
	asked   atomic.Int32
	mu      sync.Mutex
	text    string
	yes     bool
}

func newGrantApp(t *testing.T) *grantApp {
	t.Helper()
	g := &grantApp{chatApp: newChatApp(t), project: t.TempDir(), yes: true}
	g.settings.Orchestration.Enabled = true
	g.state.TrustedFolders = []string{cleanFolder(g.project)}
	g.ask = func(_, message, _ string) bool {
		g.asked.Add(1)
		g.mu.Lock()
		defer g.mu.Unlock()
		g.text = message
		return g.yes
	}
	info, err := g.openChat(context.Background(), ChatRequest{Agent: "fake", Cwd: g.project})
	if err != nil {
		t.Fatal(err)
	}
	g.chat = info.ID
	return g
}

func (g *grantApp) answer(yes bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.yes = yes
}

// call is what the MCP server hands approveAction for an orchestration tool.
func (g *grantApp) call() (context.Context, mcpserver.Approval) {
	ctx := mcpserver.WithCaller(context.Background(), g.chat)
	return ctx, mcpserver.Approval{Root: g.project, Tool: mcptools.ToolListAgents, Granted: func(ctx context.Context) bool {
		return g.orchGranted(mcpserver.CallerFrom(ctx))
	}}
}

func TestGrantDialogIsShownOnceAndNamesWhatIsAllowed(t *testing.T) {
	g := newGrantApp(t)
	ctx, call := g.call()
	if err := g.approveAction(ctx, call); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"fake", g.project, "up to 4 agents", "depth 2", "appear in their chats"} {
		if !strings.Contains(g.text, want) {
			t.Errorf("dialog text lacks %q: %s", want, g.text)
		}
	}
	for i := 0; i < 3; i++ {
		if err := g.approveAction(ctx, call); err != nil {
			t.Fatal(err)
		}
	}
	if n := g.asked.Load(); n != 1 {
		t.Fatalf("the dialog was shown %d times", n)
	}
}

func TestDeclinedGrantIsRememberedForTheChat(t *testing.T) {
	g := newGrantApp(t)
	g.answer(false)
	ctx, call := g.call()
	for i := 0; i < 3; i++ {
		if err := g.approveAction(ctx, call); !errors.Is(err, mcpserver.ErrNotApproved) {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if n := g.asked.Load(); n != 1 {
		t.Fatalf("a declined chat was asked %d times", n)
	}
	// Even a yes the user would now give is not asked for: the chat is done nagging.
	g.answer(true)
	if err := g.approveAction(ctx, call); err == nil || g.asked.Load() != 1 {
		t.Fatalf("declined chat later: %v, asked %d", err, g.asked.Load())
	}
	// Another chat is asked on its own.
	other, _ := g.openChat(context.Background(), ChatRequest{Agent: "fake", Cwd: g.project})
	ctx2 := mcpserver.WithCaller(context.Background(), other.ID)
	if err := g.approveAction(ctx2, call); err != nil || g.asked.Load() != 2 {
		t.Fatalf("other chat: %v, asked %d", err, g.asked.Load())
	}
}

func TestConcurrentFirstCallsShareOneDialog(t *testing.T) {
	g := newGrantApp(t)
	release := make(chan struct{})
	g.ask = func(string, string, string) bool {
		g.asked.Add(1)
		<-release
		return true
	}
	ctx, call := g.call()
	errs := make(chan error, 5)
	for i := 0; i < 5; i++ {
		go func() { errs <- g.approveAction(ctx, call) }()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	for i := 0; i < 5; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if n := g.asked.Load(); n != 1 {
		t.Fatalf("%d dialogs for one chat", n)
	}
}

func TestNoGrantWithoutACallerTrustOrSetting(t *testing.T) {
	g := newGrantApp(t)
	ctx, call := g.call()

	if err := g.approveAction(context.Background(), call); err == nil {
		t.Error("a call without a caller got a grant")
	}
	if err := g.approveAction(mcpserver.WithCaller(context.Background(), "unknown"), call); err == nil {
		t.Error("a chat Flint does not know got a grant")
	}
	elsewhere := call
	elsewhere.Root = t.TempDir() // not trusted: Restricted Mode
	if err := g.approveAction(ctx, elsewhere); err == nil || !strings.Contains(err.Error(), "Restricted") {
		t.Errorf("untrusted folder: %v", err)
	}
	g.settings.Orchestration.Enabled = false
	if err := g.approveAction(ctx, call); err == nil {
		t.Error("a grant was given while orchestration is off")
	}
	if g.asked.Load() != 0 {
		t.Errorf("the user was asked %d times about calls that were refused up front", g.asked.Load())
	}
}

func TestGrantEndsWithTheChatTrustAndTheSetting(t *testing.T) {
	g := newGrantApp(t)
	ctx, call := g.call()
	if err := g.approveAction(ctx, call); err != nil {
		t.Fatal(err)
	}
	if !g.orchGranted(g.chat) {
		t.Fatal("no grant after a yes")
	}

	g.settings.Orchestration.Enabled = false
	if g.orchGranted(g.chat) {
		t.Error("grant outlives the setting")
	}
	g.settings.Orchestration.Enabled = true
	g.state.TrustedFolders = nil
	if g.orchGranted(g.chat) {
		t.Error("grant outlives the folder's trust")
	}
	g.state.TrustedFolders = []string{cleanFolder(g.project)}
	if !g.orchGranted(g.chat) {
		t.Fatal("grant lost without a reason")
	}
	if err := (&AgentChatAPI{app: g.App}).Stop(g.chat); err != nil {
		t.Fatal(err)
	}
	if g.orchGranted(g.chat) {
		t.Error("grant outlives its chat")
	}
}

func TestChildrenInheritTheGrant(t *testing.T) {
	g := newGrantApp(t)
	ctx, call := g.call()
	if err := g.approveAction(ctx, call); err != nil {
		t.Fatal(err)
	}
	child, err := g.openChat(context.Background(), ChatRequest{Agent: "fake", Cwd: g.project, Parent: g.chat})
	if err != nil {
		t.Fatal(err)
	}
	stranger, _ := g.openChat(context.Background(), ChatRequest{Agent: "fake", Cwd: g.project})
	if !g.orchGranted(child.ID) {
		t.Error("a child does not inherit the grant")
	}
	if g.orchGranted(stranger.ID) {
		t.Error("a chat the grant holder did not start got the grant")
	}
	grant, _ := g.orchGrants.get(child.ID)
	if grant.root != g.project || grant.maxDepth != 2 {
		t.Errorf("inherited grant = %+v", grant)
	}
}

// Under a grant the agent's own permission request is allowed for the
// orchestration tools only.
func TestGateAllowsOnlyOrchestrationTools(t *testing.T) {
	g := newGrantApp(t)
	ctx, call := g.call()
	if err := g.approveAction(ctx, call); err != nil {
		t.Fatal(err)
	}
	for _, tool := range mcptools.OrchestrationNames {
		if !g.orchGate(g.chat, tool) {
			t.Errorf("%s is not allowed under the grant", tool)
		}
	}
	for _, tool := range append([]string{"", "run_tests", "run_configuration", "send_http_request", "run_sql_query", "get_diagnostics", "Bash", "start_agent2"}, mcptools.ActionNames...) {
		if g.orchGate(g.chat, tool) {
			t.Errorf("%q rides on the grant", tool)
		}
	}
	if g.orchGate("other-chat", mcptools.ToolStartAgent) {
		t.Error("a chat without a grant was allowed")
	}
}
