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

	// The severity family (D-102): what the PR reviewer found. The reviewer
	// sets it; an operator may correct it. It never changes who works a card.
	LabelSeverityNone     = "severity:none"
	LabelSeverityLow      = "severity:low"
	LabelSeverityMedium   = "severity:medium"
	LabelSeverityHigh     = "severity:high"
	LabelSeverityCritical = "severity:critical"

	// The source family (D-102 §22): where a card came from when it did not come
	// from a Brain Town event or the staging callback, and so is not gated by
	// assignment. Planner-only, like the defect family: an operator filters on it
	// and never sets it.
	LabelSourceAPI    = "source:api"
	LabelSourceBundle = "source:bundle"

	// The repository-kind family (D-103 §2.2): the kind of the repository an e2e
	// card is for, sent to the QA tool. Planner-only.
	LabelE2EKindBE        = "e2e-kind:be"
	LabelE2EKindFullstack = "e2e-kind:fullstack"

	// The defect family (D-091). Only the planner sets it; the scheduler never
	// dispatches it in any mode (the auto-fix loop does, with its caps).
	LabelDefectTriage  = "defect:triage"
	LabelDefectEasy    = "defect:easy"
	LabelDefectMedium  = "defect:medium"
	LabelDefectComplex = "defect:complex"
)

// labelFamilyDefect is the planner-only family.
const labelFamilyDefect = "defect"

// labelFamilySource is the planner-only card-source family (D-102 §22).
const labelFamilySource = "source"

// labelFamilyE2EKind is the planner-only repository-kind family (D-103 §2.2).
const labelFamilyE2EKind = "e2e-kind"

// labelFamilySeverity is the reviewer's severity (D-102).
const labelFamilySeverity = "severity"

// severityOf is a card's severity level from its labels, or "".
func severityOf(labels []string) string {
	for _, l := range labels {
		if strings.HasPrefix(l, labelFamilySeverity+":") {
			return strings.TrimPrefix(l, labelFamilySeverity+":")
		}
	}
	return ""
}

// withoutSeverity is the labels minus the severity one, for the chips.
func withoutSeverity(labels []string) []string {
	var out []string
	for _, l := range labels {
		if !strings.HasPrefix(l, labelFamilySeverity+":") {
			out = append(out, l)
		}
	}
	return out
}

// labelSet is the closed vocabulary.
var labelSet = map[string]bool{
	LabelPRReviewAuto:        true,
	LabelPRReviewManual:      true,
	LabelPRReviewNeedsManual: true,
	LabelE2EAutomation:       true,
	LabelE2ERun:              true,
	LabelUnitTest:            true,
	LabelSeverityNone:        true,
	LabelSeverityLow:         true,
	LabelSeverityMedium:      true,
	LabelSeverityHigh:        true,
	LabelSeverityCritical:    true,
	LabelSourceAPI:           true,
	LabelSourceBundle:        true,
	LabelE2EKindBE:           true,
	LabelE2EKindFullstack:    true,
	LabelDefectTriage:        true,
	LabelDefectEasy:          true,
	LabelDefectMedium:        true,
	LabelDefectComplex:       true,
}

// labelOrder is the vocabulary in the order the docs list it, so a rendered
// list is stable and reads the same way every time.
var labelOrder = []string{
	LabelPRReviewAuto,
	LabelPRReviewManual,
	LabelPRReviewNeedsManual,
	LabelSeverityNone,
	LabelSeverityLow,
	LabelSeverityMedium,
	LabelSeverityHigh,
	LabelSeverityCritical,
	LabelE2EAutomation,
	LabelE2ERun,
	LabelUnitTest,
	LabelDefectTriage,
	LabelDefectEasy,
	LabelDefectMedium,
	LabelDefectComplex,
	LabelE2EKindBE,
	LabelE2EKindFullstack,
	// Last, so a card's job label stays first in an API answer (D-102 §22).
	LabelSourceAPI,
	LabelSourceBundle,
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
// (e2e:automation, e2e:run) are never dispatched (D-088); nor is any defect:* card (D-091).
func labelsAllowDispatch(labels []string) bool {
	for _, l := range labels {
		switch l {
		case LabelPRReviewManual, LabelPRReviewNeedsManual, LabelE2EAutomation, LabelE2ERun,
			LabelDefectTriage, LabelDefectEasy, LabelDefectMedium, LabelDefectComplex:
			return false
		}
	}
	return true
}
