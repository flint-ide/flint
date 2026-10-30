package codex

import "encoding/json"

// Method names of the Codex app-server protocol (v2, codex-cli 0.153).
const (
	methodInitialize    = "initialize"
	methodInitialized   = "initialized"
	methodThreadStart   = "thread/start"
	methodThreadResume  = "thread/resume"
	methodTurnStart     = "turn/start"
	methodTurnInterrupt = "turn/interrupt"

	notifTurnStarted       = "turn/started"
	notifTurnCompleted     = "turn/completed"
	notifItemStarted       = "item/started"
	notifItemCompleted     = "item/completed"
	notifAgentMessageDelta = "item/agentMessage/delta"
	notifPlanDelta         = "item/plan/delta"
	notifReasoningSummary  = "item/reasoning/summaryTextDelta"
	notifReasoningPart     = "item/reasoning/summaryPartAdded"
	notifReasoningText     = "item/reasoning/textDelta"
	notifCommandOutput     = "item/commandExecution/outputDelta"
	notifPatchUpdated      = "item/fileChange/patchUpdated"
	notifPlanUpdated       = "turn/plan/updated"
	notifTokenUsage        = "thread/tokenUsage/updated"
	notifError             = "error"
	notifRequestResolved   = "serverRequest/resolved"

	reqCommandApproval = "item/commandExecution/requestApproval"
	reqFileApproval    = "item/fileChange/requestApproval"
	reqPermissions     = "item/permissions/requestApproval"
	reqUserInput       = "item/tool/requestUserInput"
	reqElicitation     = "mcpServer/elicitation/request"
	reqDynamicTool     = "item/tool/call"
)

// Approval decisions (CommandExecutionApprovalDecision / FileChangeApprovalDecision).
const (
	decisionAccept           = "accept"
	decisionAcceptForSession = "acceptForSession"
	decisionDecline          = "decline"
	decisionCancel           = "cancel"
)

// Default policies: the agent edits files inside the workspace and runs
// sandboxed commands without asking; it asks before escaping the sandbox.
const (
	DefaultApprovalPolicy = "on-request"
	DefaultSandbox        = "workspace-write"
)

// noisyNotifications are opted out of at initialize: Flint ignores them.
var noisyNotifications = []string{
	"hook/started", "hook/completed", "mcpServer/startupStatus/updated",
	"remoteControl/status/changed", "thread/status/changed", "turn/diff/updated",
	"account/rateLimits/updated", "skills/changed",
}

type clientInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Version string `json:"version"`
}

type initializeCapabilities struct {
	ExperimentalAPI           bool     `json:"experimentalApi"`
	RequestAttestation        bool     `json:"requestAttestation"`
	OptOutNotificationMethods []string `json:"optOutNotificationMethods,omitempty"`
}

type initializeParams struct {
	ClientInfo   clientInfo             `json:"clientInfo"`
	Capabilities initializeCapabilities `json:"capabilities"`
}

type threadStartParams struct {
	Cwd            string `json:"cwd"`
	Model          string `json:"model,omitempty"`
	ApprovalPolicy string `json:"approvalPolicy,omitempty"`
	Sandbox        string `json:"sandbox,omitempty"`
}

// threadResumeParams reopens a stored thread; the policy fields mirror
// thread/start because a resumed thread does not keep them.
type threadResumeParams struct {
	ThreadID       string `json:"threadId"`
	Cwd            string `json:"cwd"`
	Model          string `json:"model,omitempty"`
	ApprovalPolicy string `json:"approvalPolicy,omitempty"`
	Sandbox        string `json:"sandbox,omitempty"`
	// ExcludeTurns skips sending the stored history back: Flint keeps its own transcript.
	ExcludeTurns bool `json:"excludeTurns,omitempty"`
}

// threadStartResponse is also the thread/resume response.
type threadStartResponse struct {
	Thread struct {
		ID string `json:"id"`
	} `json:"thread"`
	Model string `json:"model"`
	Cwd   string `json:"cwd"`
}

// userInput is one UserInput item; only the text variant is used.
type userInput struct {
	Type         string `json:"type"`
	Text         string `json:"text"`
	TextElements []any  `json:"text_elements"`
}

type turnStartParams struct {
	ThreadID string      `json:"threadId"`
	Input    []userInput `json:"input"`
}

type turnInfo struct {
	ID     string `json:"id"`
	Status string `json:"status"` // completed | interrupted | failed | inProgress
	Error  *struct {
		Message           string `json:"message"`
		AdditionalDetails string `json:"additionalDetails"`
	} `json:"error"`
}

type turnStartResponse struct {
	Turn turnInfo `json:"turn"`
}

type turnInterruptParams struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
}

// envelope holds the routing fields shared by thread-scoped notifications.
type envelope struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	ItemID   string `json:"itemId"`
}

type turnNotification struct {
	ThreadID string   `json:"threadId"`
	Turn     turnInfo `json:"turn"`
}

type itemNotification struct {
	ThreadID string     `json:"threadId"`
	TurnID   string     `json:"turnId"`
	Item     threadItem `json:"item"`
}

type deltaNotification struct {
	envelope
	Delta        string `json:"delta"`
	SummaryIndex int    `json:"summaryIndex"`
}

type patchUpdatedNotification struct {
	envelope
	Changes []fileUpdateChange `json:"changes"`
}

type planUpdatedNotification struct {
	envelope
	Plan []struct {
		Step   string `json:"step"`
		Status string `json:"status"` // pending | inProgress | completed
	} `json:"plan"`
}

type tokenBreakdown struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
}

type tokenUsageNotification struct {
	envelope
	TokenUsage struct {
		Total tokenBreakdown `json:"total"`
	} `json:"tokenUsage"`
}

type errorNotification struct {
	envelope
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
	WillRetry bool `json:"willRetry"`
}

type requestResolvedNotification struct {
	ThreadID  string          `json:"threadId"`
	RequestID json.RawMessage `json:"requestId"`
}

// threadItem is the union of ThreadItem variants; fields not used by a
// variant stay zero.
type threadItem struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	// agentMessage / plan
	Text string `json:"text"`
	// reasoning
	Summary []string `json:"summary"`
	Content []string `json:"content"`
	// commandExecution
	Command          string          `json:"command"`
	Cwd              string          `json:"cwd"`
	Status           string          `json:"status"`
	AggregatedOutput *string         `json:"aggregatedOutput"`
	ExitCode         *int            `json:"exitCode"`
	CommandActions   []commandAction `json:"commandActions"`
	// fileChange
	Changes []fileUpdateChange `json:"changes"`
	// mcpToolCall / dynamicToolCall
	Server    string          `json:"server"`
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
	Result    *struct {
		Content []json.RawMessage `json:"content"`
	} `json:"result"`
	ItemError *struct {
		Message string `json:"message"`
	} `json:"error"`
	ContentItems []json.RawMessage `json:"contentItems"`
	Success      *bool             `json:"success"`
	// webSearch
	Query string `json:"query"`
	// imageView
	Path string `json:"path"`
}

type commandAction struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

type fileUpdateChange struct {
	Path string `json:"path"`
	Kind struct {
		Type     string `json:"type"` // add | delete | update
		MovePath string `json:"move_path"`
	} `json:"kind"`
	Diff string `json:"diff"`
}

type commandApprovalParams struct {
	envelope
	Command string `json:"command"`
	Cwd     string `json:"cwd"`
	Reason  string `json:"reason"`
}

type fileApprovalParams struct {
	envelope
	Reason    string `json:"reason"`
	GrantRoot string `json:"grantRoot"`
}

type permissionsApprovalParams struct {
	envelope
	Reason      string                     `json:"reason"`
	Permissions map[string]json.RawMessage `json:"permissions"`
}

type decisionResponse struct {
	Decision string `json:"decision"`
}

type permissionsResponse struct {
	Permissions map[string]json.RawMessage `json:"permissions"`
	Scope       string                     `json:"scope"` // turn | session
}
