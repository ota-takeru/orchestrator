package statemachine

import "testing"

func TestInvalidTransitionRejected(t *testing.T) {
	if Task.CanTransition("implementing", "merged") {
		t.Fatal("implementing -> merged must be rejected")
	}
	if err := Task.ValidateTransition("implementing", "merged"); err == nil {
		t.Fatal("expected invalid task transition error")
	}
}

func TestAllowedTaskTransitions(t *testing.T) {
	tests := [][2]string{
		{"proposed", "ready"},
		{"ready", "implementing"},
		{"implementing", "verifying"},
		{"verifying", "reviewing"},
		{"reviewing", "ready_for_human_review"},
		{"ready_for_human_review", "approved_for_merge"},
		{"approved_for_merge", "queued_for_merge"},
		{"queued_for_merge", "rebasing"},
		{"rebasing", "reverifying"},
		{"reverifying", "merged"},
	}
	for _, tt := range tests {
		if !Task.CanTransition(tt[0], tt[1]) {
			t.Fatalf("expected %s -> %s to be allowed", tt[0], tt[1])
		}
	}
}

func TestRunTimestampShape(t *testing.T) {
	if err := Run.ValidateTimestampShape("pending", false, false); err != nil {
		t.Fatalf("pending timestamp shape rejected: %v", err)
	}
	if err := Run.ValidateTimestampShape("pending", true, false); err == nil {
		t.Fatal("pending run with started_at should be rejected")
	}
	if err := Run.ValidateTimestampShape("running", true, false); err != nil {
		t.Fatalf("running timestamp shape rejected: %v", err)
	}
	if err := Run.ValidateTimestampShape("succeeded", true, true); err != nil {
		t.Fatalf("terminal timestamp shape rejected: %v", err)
	}
}

func TestCommandEventIsNotResumedFromBlocked(t *testing.T) {
	if CommandEvent.CanTransition("blocked", "running") {
		t.Fatal("blocked command event must not resume")
	}
	if !CommandEvent.CanTransition("blocked", "cancelled") {
		t.Fatal("blocked command event should allow cancellation")
	}
}
