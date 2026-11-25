package main

import (
	"strings"
)

// The task labels (D-088, docs/designs/gaos-pr-agent-flows.md §3).
//
// A label says what kind of work a card is, so an operator can tell at a
// glance whether it is waiting on a person or on an agent. The vocabulary is
// CLOSED: a label that is not in this table is refused at write time, the
// same way the task-status vocabulary is refused by the DDL's regexp
// (dashboard/schema/04-execution.sql:28-30).
//
// A label is task data, not an event type. It rides on the task and is
// rendered on the card.

const (
	// LabelPRReviewAuto is the default for a pull-request review: an agent
	// reviews unless a human says otherwise.
	LabelPRReviewAuto = "pr-review:auto"
	// LabelPRReviewManual means a human reviews the pull request.
	LabelPRReviewManual = "pr-review:manual"
	// LabelPRReviewNeedsManual means the agent ran and a human must take over.
	LabelPRReviewNeedsManual = "pr-review:needs-manual"
	// LabelE2EAutomation is the e2e automation script builder.
	LabelE2EAutomation = "e2e:automation"
	// LabelE2ERun is the e2e automation test runner.
	LabelE2ERun = "e2e:run"
	// LabelUnitTest is the unit test builder and runner.
	LabelUnitTest = "unit:test"
)

// labelSet is the closed vocabulary.
var labelSet = map[string]bool{
	LabelPRReviewAuto:        true,
	LabelPRReviewManual:      true,
	LabelPRReviewNeedsManual: true,
	LabelE2EAutomation:       true,
	LabelE2ERun:              true,
	LabelUnitTest:            true,
}

// labelOrder is the vocabulary in the order the docs list it, so a rendered
// list is stable and reads the same way every time.
var labelOrder = []string{
	LabelPRReviewAuto,
	LabelPRReviewManual,
	LabelPRReviewNeedsManual,
	LabelE2EAutomation,
	LabelE2ERun,
	LabelUnitTest,
}

// ValidLabel reports whether a label is in the closed vocabulary.
func ValidLabel(label string) bool { return labelSet[label] }

// validateLabels refuses a label that is not in the vocabulary. It returns the
// first offender, so the caller can name it.
func validateLabels(labels []string) (string, bool) {
	for _, l := range labels {
		if !labelSet[l] {
			return l, false
		}
	}
	return "", true
}

// normalizeLabels trims, drops the empty and de-duplicates, keeping the
// vocabulary's order so two cards with the same labels read the same way.
func normalizeLabels(labels []string) []string {
	seen := map[string]bool{}
	for _, l := range labels {
		l = strings.TrimSpace(l)
		if l != "" && labelSet[l] {
			seen[l] = true
		}
	}
	out := make([]string, 0, len(seen))
	for _, l := range labelOrder {
		if seen[l] {
			out = append(out, l)
		}
	}
	return out
}

// labelsAllowDispatch reports whether the scheduler may hand a card with these
// labels to an agent. A card a person reviews (pr-review:manual or
// pr-review:needs-manual) and a card the QA-tool job runner drives
// (e2e:automation, e2e:run) are never dispatched (D-088).
func labelsAllowDispatch(labels []string) bool {
	for _, l := range labels {
		switch l {
		case LabelPRReviewManual, LabelPRReviewNeedsManual, LabelE2EAutomation, LabelE2ERun:
			return false
		}
	}
	return true
}
