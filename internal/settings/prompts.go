package settings

// Prompts configures the prompt library (internal/prompts): the "/" picker
// in the agent chat. The personal prompts live in the config directory's
// prompts folder; that location is fixed, so it is not a setting.
type Prompts struct {
	// Builtins offers the prompts shipped with Flint (/review, /test, …).
	Builtins bool `json:"builtins"`
}

func defaultPrompts() Prompts { return Prompts{Builtins: true} }
