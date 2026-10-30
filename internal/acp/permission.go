package acp

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// Permission option kinds (PermissionOptionKind in the schema).
const (
	PermissionAllowOnce    = "allow_once"
	PermissionAllowAlways  = "allow_always"
	PermissionRejectOnce   = "reject_once"
	PermissionRejectAlways = "reject_always"
)

// Permission outcomes (RequestPermissionOutcome.outcome in the schema).
const (
	OutcomeSelected  = "selected"
	OutcomeCancelled = "cancelled"
)

// ToolCallRef is the tool call a permission request is about (a
// ToolCallUpdate in the schema). Only the commonly displayed fields are
// parsed; Raw holds the full object (content, locations, rawInput, ...).
type ToolCallRef struct {
	ToolCallID string          `json:"toolCallId"`
	Title      string          `json:"title,omitempty"`
	Kind       string          `json:"kind,omitempty"`
	Status     string          `json:"status,omitempty"`
	Raw        json.RawMessage `json:"-"`
}

// UnmarshalJSON keeps the raw object alongside the parsed fields.
func (t *ToolCallRef) UnmarshalJSON(data []byte) error {
	type plain ToolCallRef
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	p.Raw = slices.Clone(json.RawMessage(data))
	*t = ToolCallRef(p)
	return nil
}

// PermissionOption is one choice offered to the user.
type PermissionOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

// PermissionRequest is the params of `session/request_permission`.
type PermissionRequest struct {
	SessionID string             `json:"sessionId"`
	ToolCall  ToolCallRef        `json:"toolCall"`
	Options   []PermissionOption `json:"options"`
}

// PermissionOutcome is the user's decision; it marshals to the schema's
// RequestPermissionOutcome: {"outcome":"selected","optionId":...} or
// {"outcome":"cancelled"}.
type PermissionOutcome struct {
	Outcome  string `json:"outcome"`
	OptionID string `json:"optionId,omitempty"`
}

// Selected returns the outcome choosing optionID.
func Selected(optionID string) PermissionOutcome {
	return PermissionOutcome{Outcome: OutcomeSelected, OptionID: optionID}
}

// Cancelled returns the outcome for a cancelled prompt turn.
func Cancelled() PermissionOutcome {
	return PermissionOutcome{Outcome: OutcomeCancelled}
}

type permissionResponse struct {
	Outcome PermissionOutcome `json:"outcome"`
}

func (r PermissionRequest) validate() error {
	if r.SessionID == "" {
		return errors.New("sessionId is required")
	}
	if r.ToolCall.ToolCallID == "" {
		return errors.New("toolCall.toolCallId is required")
	}
	if len(r.Options) == 0 {
		return errors.New("options must not be empty")
	}
	return nil
}

// checkOutcome rejects outcomes that do not match the request, so a buggy
// policy cannot approve an option the agent never offered.
func (r PermissionRequest) checkOutcome(o PermissionOutcome) error {
	switch o.Outcome {
	case OutcomeCancelled:
		return nil
	case OutcomeSelected:
		for _, opt := range r.Options {
			if opt.OptionID == o.OptionID {
				return nil
			}
		}
		return fmt.Errorf("acp: permission option %q was not offered", o.OptionID)
	default:
		return fmt.Errorf("acp: invalid permission outcome %q", o.Outcome)
	}
}
