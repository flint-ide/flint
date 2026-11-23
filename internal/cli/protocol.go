package cli

// Method is the one JSON-RPC method of the command-line socket.
const Method = "cli.run"

// Bounds of the wire, checked by the server (the client keeps to them too).
const (
	// MaxMessage bounds one message either way.
	MaxMessage = 1 << 20
	// MaxArgs bounds the words of a command line.
	MaxArgs = 64
	// MaxFiles bounds the --<flag>-file values a request carries.
	MaxFiles = 4
	// MaxPath bounds the working directory.
	MaxPath = 4096
)

// Request is one command line, sent by the flint client.
type Request struct {
	// Token is the contents of cli.token: the client could read Flint's
	// run directory (docs/ARCHITECTURE.md, "Command-line socket").
	Token string `json:"token"`
	// Args are the words after "flint", without the --<flag>-file ones.
	Args []string `json:"args"`
	// Cwd is the client's working directory (absolute).
	Cwd string `json:"cwd"`
	// Files are the texts of --<flag>-file options the client read, by
	// flag name ("task"). Never a path.
	Files map[string]string `json:"files,omitempty"`
}

// Response is what the client prints and its exit status.
type Response struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	Exit   int    `json:"exit"`
}
