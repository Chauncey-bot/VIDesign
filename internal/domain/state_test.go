package domain

import "testing"

func TestCanTransitionAwaitingFeedbackBackToCollection(t *testing.T) {
	if err := Transition(ProjectAwaitFeedback, ProjectCollecting); err != nil {
		t.Fatalf("awaiting_feedback should re-enter collection: %v", err)
	}
}

func TestCanTransitionCollectionToBriefReady(t *testing.T) {
	if err := Transition(ProjectCollecting, ProjectBriefReady); err != nil {
		t.Fatalf("collecting_requirements should become brief_ready: %v", err)
	}
}
