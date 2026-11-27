package main

import (
	"context"
	"sort"
	"strings"
)

// Repository kinds (D-103 §2, docs/designs/release-b-e2e-kinds-merge-unit-tests.md).
//
// A product repository may be "be" (backend only: a run of every case runs API
// and GraphQL cases; a create writes an API spec) or "fullstack" (a run runs
// every case; a create writes a browser spec). No entry means no kind, which is
// today's behaviour: no label, nothing sent.

const (
	RepoKindBE        = "be"
	RepoKindFullstack = "fullstack"
)

// repoKindOK is a value a kind may take ("" deletes it).
func repoKindOK(v string) bool { return v == RepoKindBE || v == RepoKindFullstack }

// repoKindFor is the kind of owner/repo on a product, or "".
func repoKindFor(m Master, owner, repo string) string {
	return m.RepoKinds[strings.ToLower(owner+"/"+repo)]
}

// withRepoKind adds the deployed repository's kind label to a staging run spec
// (D-103 §2.2). pr.opened automation cards and API runs never carry one.
func withRepoKind(m Master, ev PREvent, specs []prCardSpec) []prCardSpec {
	if ev.Type != prTypeStaging {
		return specs
	}
	k := repoKindFor(m, ev.Org, ev.Repo)
	if k == "" {
		return specs
	}
	out := make([]prCardSpec, len(specs))
	for i, s := range specs {
		s.Labels = append(append([]string(nil), s.Labels...), labelFamilyE2EKind+":"+k)
		out[i] = s
	}
	return out
}

// SetRepoKinds replaces a product's kinds and sets its two Release B switches.
// A key must be one of the product's repositories and a value be|fullstack.
// Assignment is untouched.
func (s *MemStore) SetRepoKinds(ctx context.Context, productID string, kinds map[string]string, staging, autoMerge bool, actor string) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.masterPtr(productID)
	if m == nil {
		return Result{}, ErrNotFound
	}
	owned := map[string]bool{}
	for _, r := range m.Repos {
		owned[strings.ToLower(r)] = true
	}
	next := map[string]string{}
	for k, v := range kinds {
		k = strings.ToLower(strings.TrimSpace(k))
		switch {
		case v == "":
			continue
		case !owned[k]:
			return Result{}, reject("not a repository of this product: " + k)
		case !repoKindOK(v):
			return Result{}, reject("a repository kind is be or fullstack")
		}
		next[k] = v
	}
	if len(next) == 0 {
		next = nil
	}
	m.RepoKinds, m.StagingCreates, m.AutoMergeSpecs = next, staging, autoMerge
	alias := actorAlias(actor)
	ev := s.emit(RecordUpdated, productID, s.productName(productID)+": repository kinds by "+alias+": "+
		repoKindsText(next)+"; staging creates "+onOff(staging)+"; spec auto-merge "+onOff(autoMerge),
		KindInfo, EventPayload{Actor: alias, Rule: "repo-kinds"})
	c := m.Clone()
	return Result{Master: &c, NewEvents: []Event{ev}, Toast: "Repository kinds saved."}, nil
}

func repoKindsText(k map[string]string) string {
	if len(k) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(k))
	for r := range k {
		keys = append(keys, r)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, r := range keys {
		parts[i] = r + "=" + k[r]
	}
	return strings.Join(parts, " ")
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// specAutoMergeAllowed is the rule GAOS answers the QA tool with (D-103 §4.1):
// the product turned it on and its band is working or complete.
func specAutoMergeAllowed(m Master) bool {
	if !m.AutoMergeSpecs {
		return false
	}
	b := ReadinessBand(m.ReadinessScore())
	return b == "working" || b == "complete"
}
