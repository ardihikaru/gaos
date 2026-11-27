package main

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The pull-request text of an auto-fix (D-091, design §8.3 "Text safety").
//
// Everything the agent wrote is untrusted. The summary is one short line, the
// before and after are stripped of backticks and angle brackets and rendered
// inside a fence longer than any backtick run they could still contain, and the
// body always says that the after was predicted by the agent and not run by
// GAOS. A product's own mandatory form is data in a template, never code here.

// The slots of a template.
const (
	slotDefect  = "{{defect}}"
	slotTitle   = "{{title}}"
	slotSummary = "{{summary}}"
	slotBefore  = "{{before}}"
	slotAfter   = "{{after}}"
	slotFiles   = "{{files}}"
	slotLimits  = "{{limits}}"
)

const (
	prSummaryMax   = 72
	prTitleMax     = 200
	prSampleMax    = 1 << 10
	prLineMax      = 400
	prSampleWrap   = 100
	prAgentNotice  = "_The \"after\" above was predicted by the agent. GAOS did not run it; a person must verify it._"
	prDefaultFixed = "apply the proposed fix"
)

// defaultPRTemplate is the neutral English body shipped in GAOS.
const defaultPRTemplate = `## Draft fix for {{defect}}

This draft pull request was prepared by GAOS from an AI agent's proposal. A person must review it before it is marked ready or merged.

**Defect:** {{title}}

**Summary:** {{summary}}

### Before

{{before}}

### After (reported by the agent, not run by GAOS)

{{after}}

### Files

{{files}}

### Limits

{{limits}}
`

// isBidiOrZeroWidth reports a Unicode bidirectional control or zero-width
// character (and the byte order mark, which is one in the middle of a text).
func isBidiOrZeroWidth(r rune) bool {
	switch {
	case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
		return true
	case r == 0x200B, r == 0x200C, r == 0x200D, r == 0x200E, r == 0x200F, r == 0x2060, r == 0xFEFF, r == 0x061C:
		return true
	}
	return false
}

// hasBidi reports whether a text holds any such character, or a NUL.
func hasBidi(s string) bool {
	for _, r := range s {
		if isBidiOrZeroWidth(r) || r == 0 {
			return true
		}
	}
	return false
}

// stripBidi removes them.
func stripBidi(s string) string {
	return strings.Map(func(r rune) rune {
		if isBidiOrZeroWidth(r) {
			return -1
		}
		return r
	}, s)
}

// prSummaryLine is the agent's summary as one safe line: control, bidi and line
// break characters removed, `@` rendered as `(at)`, at most 72 characters.
func prSummaryLine(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			sb.WriteRune(' ')
		case unicode.IsControl(r) || isBidiOrZeroWidth(r):
		default:
			sb.WriteRune(r)
		}
	}
	line := strings.Join(strings.Fields(strings.NewReplacer("`", "", "<", "", ">", "").Replace(sb.String())), " ")
	line = strings.ReplaceAll(line, "@", "(at)")
	line = strings.TrimSpace(capRunes(line, prSummaryMax))
	if line == "" {
		return prDefaultFixed
	}
	return line
}

// prInlineText is a short untrusted text for a line of the body.
func prInlineText(s string, max int) string {
	s = stripBidi(stripBR53(s))
	return strings.ReplaceAll(capRunes(s, max), "@", "(at)")
}

// wrapAt splits a text so no line is longer than n runes.
func wrapAt(s string, n int) string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		for utf8.RuneCountInString(line) > n {
			r := []rune(line)
			out = append(out, string(r[:n]))
			line = string(r[n:])
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// prFenced renders an untrusted sample inside a fence longer than any backtick
// run in it.
func prFenced(s string) string {
	s = wrapAt(prInlineText(s, prSampleMax), prSampleWrap)
	f := fenceFor(s)
	return f + "\n" + s + "\n" + f
}

// prBodyIn is what a body is built from.
type prBodyIn struct {
	Defect, Title, Summary, Before, After string
	Files                                 []string
	Limits                                string
	// Added are the test files a unit fix adds (D-105 §7.5), listed for the reviewer.
	Added []string
}

// templateHasSlots reports whether a template names the two slots every body needs.
func templateHasSlots(tpl string) bool {
	return strings.Contains(tpl, slotBefore) && strings.Contains(tpl, slotAfter)
}

// renderPRBody fills a template (the product's, or the default when empty).
// The slots are replaced in one pass, so text that looks like a slot is never
// expanded. The agent notice is always the last paragraph.
func renderPRBody(tpl string, in prBodyIn) string {
	if strings.TrimSpace(tpl) == "" {
		tpl = defaultPRTemplate
	}
	var files strings.Builder
	for _, f := range in.Files {
		fmt.Fprintf(&files, "- `%s`\n", strings.ReplaceAll(f, "`", ""))
	}
	body := strings.NewReplacer(
		slotDefect, prInlineText(in.Defect, 40),
		slotTitle, prInlineText(in.Title, prTitleMax),
		slotSummary, prSummaryLine(in.Summary),
		slotBefore, prFenced(in.Before),
		slotAfter, prFenced(in.After),
		slotFiles, strings.TrimRight(files.String(), "\n"),
		slotLimits, in.Limits,
	).Replace(tpl)
	if len(in.Added) > 0 {
		var added strings.Builder
		added.WriteString("\n\n**Test files added**\n\n")
		for _, f := range in.Added {
			fmt.Fprintf(&added, "- `%s`\n", strings.ReplaceAll(f, "`", ""))
		}
		body = strings.TrimRight(body, "\n") + strings.TrimRight(added.String(), "\n")
	}
	body = stripBidi(strings.TrimRight(body, "\n")) + "\n\n" + prAgentNotice + "\n"
	return wrapAt(body, prLineMax)
}

// prCommentBody is the one comment with the before and after.
func prCommentBody(in prBodyIn) string {
	return "**Before**\n\n" + prFenced(in.Before) + "\n\n**After**\n\n" + prFenced(in.After) + "\n\n" + prAgentNotice + "\n"
}
