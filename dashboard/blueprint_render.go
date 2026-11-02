package main

import (
	"fmt"
	"strings"
)

// The section renderers. Each returns "" for an empty section, so the file is
// left out rather than written empty — an empty rules file is noise the model
// reads on every turn.

const managedBanner = "> Managed by GAOS. Do not edit: this folder is rewritten from the product's\n" +
	"> Blueprint, and a change made here is detected as drift and refuses the next task.\n"

func renderIndex(name string, m Master) string {
	var b strings.Builder
	title := name
	if title == "" {
		title = m.ID
	}
	fmt.Fprintf(&b, "# %s\n\n%s\n", title, managedBanner)
	if m.Code != "" {
		fmt.Fprintf(&b, "Product code: `%s`.\n\n", m.Code)
	}
	b.WriteString("## What is in .claude/rules\n\n" +
		"- `00-gaos-contract.md` — how every reply must end. Read it first.\n" +
		"- `10-brd.md` — the problem, goals, scope and the domain rules your work is scored against.\n" +
		"- `20-stories.md` — the stories; each task's brief carries its own acceptance criteria in full.\n" +
		"- `30-decisions.md` — architecture decisions already made.\n" +
		"- `4x`–`6x` — technical, coding and other policies: how this product is built.\n" +
		"- `70-knowledge-sources.md` — repositories whose structure and interfaces to follow.\n")
	return b.String()
}

// standingContract is the part of the reply contract that is true for every
// task. Each brief repeats it with the task and run ids (gatewayContract), so
// it survives a resumed session's stale first-turn note either way.
const standingContract = `# The reply contract

Every task you are given here comes from GAOS, which reads your reply and
nothing else. End every reply with ONE fenced block tagged gaos-handoff,
holding the handoff front matter the task's brief shows you: task, run, role,
status (complete | blocked | partial) and one line per acceptance criterion.

- Use the task and run ids from the brief exactly. A block naming another run is
  rejected, so never repeat a block from an earlier attempt.
- Blocked is a legitimate answer, and a better one than a plausible guess.
- A reply without a readable block is recorded as failed, whatever the work was.
- Do not edit CLAUDE.md or anything in .claude/: GAOS owns them.
`

func renderBRD(b BRD) string {
	var s strings.Builder
	if strings.TrimSpace(b.Problem) != "" {
		fmt.Fprintf(&s, "## Problem\n\n%s\n\n", strings.TrimSpace(b.Problem))
	}
	list := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&s, "## %s\n\n", title)
		for _, it := range items {
			fmt.Fprintf(&s, "- %s\n", it)
		}
		s.WriteString("\n")
	}
	list("Goals", b.Goals)
	list("Scope", b.Scope)
	list("Out of scope", b.OutOfScope)
	list("Domain rules — your work is scored against these", b.Rules)
	list("Success metrics", b.Metrics)
	if s.Len() == 0 {
		return ""
	}
	return "# Business requirements\n\n" + s.String()
}

func renderStories(stories []Story) string {
	if len(stories) == 0 {
		return ""
	}
	var s strings.Builder
	s.WriteString("# Stories\n\nAn index. The task you are given names its story and carries its criteria in full.\n\n")
	for _, st := range stories {
		fmt.Fprintf(&s, "- **%s** (%s) — as a %s, I want %s, so that %s.\n", st.ID, st.Pri, st.Role, st.Want, st.So)
	}
	return s.String()
}

func renderDecisions(ds []Decision) string {
	if len(ds) == 0 {
		return ""
	}
	var s strings.Builder
	s.WriteString("# Decisions already made\n\nDo not reopen these in a task; raise a blocker if one is wrong.\n\n")
	for _, d := range ds {
		fmt.Fprintf(&s, "- **%s** — %s\n", d.ID, d.Title)
	}
	return s.String()
}

func renderSources(ks []KnowledgeSource) string {
	if len(ks) == 0 {
		return ""
	}
	var s strings.Builder
	s.WriteString("# Knowledge sources\n\nRepositories this product follows, each pinned. Follow `structure` and `style`\n" +
		"sources' layout and conventions, call into `interfaces` sources rather than\nre-implementing them, and consult `reference` sources.\n\n")
	for _, k := range ks {
		purpose := k.Purpose
		if purpose == "" {
			purpose = "reference"
		}
		fmt.Fprintf(&s, "- %s @ `%s` — %s", k.URL, k.Ref, purpose)
		if n := strings.TrimSpace(k.Note); n != "" {
			fmt.Fprintf(&s, ": %s", n)
		}
		s.WriteString("\n")
	}
	return s.String()
}
