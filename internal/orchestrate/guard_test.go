package orchestrate

import (
	"errors"
	"testing"

	as "flint/internal/agentsession"
)

func TestCheckSendable(t *testing.T) {
	permission := as.Event{SessionID: "a", Kind: as.KindPermission, Permission: &as.PermissionRequest{ID: "p"}}
	for _, tc := range []struct {
		name  string
		setup func(*Registry)
		want  error
	}{
		{"idle chat", func(*Registry) {}, nil},
		{"finished chat", func(r *Registry) {
			r.Prompted("a")
			r.Observe(turnEnd("a", as.StopEndTurn))
		}, nil},
		{"open draft", func(r *Registry) { r.SetDraft("a", true) }, ErrDraftOpen},
		{"draft closed again", func(r *Registry) { r.SetDraft("a", true); r.SetDraft("a", false) }, nil},
		{"busy", func(r *Registry) { r.Prompted("a") }, ErrBusy},
		{"waiting for approval", func(r *Registry) { r.Prompted("a"); r.Observe(permission) }, ErrWaitingApproval},
		// Waiting is what the user must act on, so it is reported even when
		// a draft is open too.
		{"waiting and draft", func(r *Registry) { r.Prompted("a"); r.Observe(permission); r.SetDraft("a", true) }, ErrWaitingApproval},
		{"busy and draft", func(r *Registry) { r.Prompted("a"); r.SetDraft("a", true) }, ErrDraftOpen},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newWith(t, Node{ID: "a"})
			tc.setup(r)
			if err := r.CheckSendable("a"); !errors.Is(err, tc.want) {
				t.Fatalf("CheckSendable = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestCheckSendableUnknownChat(t *testing.T) {
	if err := New().CheckSendable("nope"); !errors.Is(err, ErrUnknown) {
		t.Fatalf("err = %v, want ErrUnknown", err)
	}
}

func TestSendErrorsAreDistinct(t *testing.T) {
	errs := []error{ErrDraftOpen, ErrWaitingApproval, ErrBusy, ErrUnknown}
	for i, a := range errs {
		for j, b := range errs {
			if i != j && errors.Is(a, b) {
				t.Errorf("%v must not match %v", a, b)
			}
		}
	}
}
