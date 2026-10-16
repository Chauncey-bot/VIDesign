package domain

import "fmt"

// CanTransition centralizes project workflow rules so API handlers and workers
// cannot accidentally skip a human or Agent checkpoint.
func CanTransition(from, to ProjectStatus) bool {
	if from == to {
		return true
	}
	switch from {
	case ProjectDraft:
		return to == ProjectCollecting
	case ProjectCollecting:
		return to == ProjectBriefReady || to == ProjectCollecting
	case ProjectBriefReady:
		return to == ProjectGenerating
	case ProjectGenerating:
		return to == ProjectAwaitFeedback
	case ProjectAwaitFeedback:
		return to == ProjectRevising || to == ProjectApproved
	case ProjectRevising:
		return to == ProjectAwaitFeedback
	case ProjectApproved:
		return to == ProjectExporting
	case ProjectExporting:
		return to == ProjectCompleted
	default:
		return false
	}
}

func Transition(from, to ProjectStatus) error {
	if !CanTransition(from, to) {
		return fmt.Errorf("invalid project status transition: %s -> %s", from, to)
	}
	return nil
}
