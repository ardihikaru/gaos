package main

// The team's pull-request template (D-100, docs/designs/gaos-external-tasks-and-pr-bundles.md §7.1,
// Appendix A). A pure parser: what the body says about its task-manager code, the pull requests it
// travels with, and what QA should test. Every value is untrusted author text.

import (
	"regexp"
	"strings"
)

// prTemplate is what a pull request's body says.
type prTemplate struct {
	Present     bool     // the body has the line "Sumber task:"
	FromTaskMgr bool     // "- [x] Task manager"
	OtherSource bool     // any of the other three source boxes is ticked
	TaskCode    string   // after "Kode (jika dari task manager):", ** and the placeholder removed
	Links       []string // owner/repo#n of the github.com pull-request URLs in the two link sections
	TestNotes   string   // "## Apa saja yang perlu dites oleh QA?" up to the next "## "
	QACase      string   // the "QA-case:" line (qajobs.go reads it for an e2e automation)
}

const (
	prTemplateMaxLinks = 20
	prTemplateMaxNotes = 4 << 10
)

var (
	prTaskCodePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
	prTickPattern     = regexp.MustCompile(`^[-*]\s*\[([ xX])\]\s*(.*)$`)
	prURLPattern      = regexp.MustCompile(`https://github\.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+/pull/[0-9]+`)
)

// prPlaceholder reports a value the template ships with, never an answer.
func prPlaceholder(v string) bool {
	v = strings.TrimSpace(strings.Trim(strings.TrimSpace(v), "*"))
	switch strings.ToLower(v) {
	case "", "(masukkan di sini)", "(masukkan url di sini)", "...", "…":
		return true
	}
	return false
}

// parsePRTemplate reads a body. A body that does not follow the template answers Present=false
// and still carries its links, notes and QA-case line when it has them.
func parsePRTemplate(body string) prTemplate {
	var t prTemplate
	section := ""
	seen := map[string]bool{}
	var notes []string
	for _, raw := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		low := strings.ToLower(line)
		switch {
		case strings.HasPrefix(line, ">"):
			continue // the template's own hints
		case strings.HasPrefix(low, "sumber task:"):
			t.Present, section = true, "source"
			continue
		case strings.HasPrefix(low, "kode (jika dari task manager):"):
			v := strings.TrimSpace(line[len("kode (jika dari task manager):"):])
			v = strings.TrimSpace(strings.Trim(v, "*"))
			if !prPlaceholder(v) && prTaskCodePattern.MatchString(v) {
				t.TaskCode = v
			}
			section = ""
			continue
		case strings.HasPrefix(low, "cross-repo checklist"), strings.HasPrefix(low, "dependency terhadap pr lain"):
			section = "links"
			continue
		case strings.HasPrefix(low, "qa-case:"):
			t.QACase = strings.TrimSpace(line[len("qa-case:"):])
			continue
		case strings.HasPrefix(line, "## "):
			section = ""
			if strings.HasPrefix(low, "## apa saja yang perlu dites oleh qa") {
				section = "notes"
			}
			continue
		case strings.HasPrefix(line, "# "):
			section = ""
			continue
		}
		switch section {
		case "source":
			if m := prTickPattern.FindStringSubmatch(line); m != nil && m[1] != " " {
				label := strings.ToLower(m[2])
				if strings.HasPrefix(label, "task manager") {
					t.FromTaskMgr = true
				} else {
					t.OtherSource = true
				}
			}
		case "links":
			for _, u := range prURLPattern.FindAllString(line, -1) {
				if o, r, n, ok := parseGitHubPRURL(u); ok {
					ref := o + "/" + r + "#" + itoa(n)
					if !seen[ref] && len(t.Links) < prTemplateMaxLinks {
						seen[ref] = true
						t.Links = append(t.Links, ref)
					}
				}
			}
		case "notes":
			item := strings.TrimSpace(strings.TrimLeft(line, "-*"))
			if !prPlaceholder(item) {
				notes = append(notes, line)
			}
		}
	}
	n := strings.TrimSpace(strings.Join(notes, "\n"))
	if len(n) > prTemplateMaxNotes {
		n = n[:prTemplateMaxNotes]
	}
	if screenBody(n) {
		n = "" // held: a secret never travels in a packet
	}
	t.TestNotes = n
	return t
}

// dropSelf removes the pull request's own ref from its links.
func (t prTemplate) linksWithout(self string) []string {
	var out []string
	for _, l := range t.Links {
		if !strings.EqualFold(l, self) {
			out = append(out, l)
		}
	}
	return out
}
