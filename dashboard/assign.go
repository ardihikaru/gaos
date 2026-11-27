package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Agent assignment per product (D-102, docs/designs/agent-seats-and-pr-agents.md §3).
//
// A product lists the agents assigned to it. Until an admin saves that list
// (AssignedSet), the product behaves as it did before assignment existed:
// every pull-request flow runs. Once saved, a pull-request flow role runs only
// when an agent of that role is assigned. Assignment never chooses which agent
// runs a card; the scheduler is unchanged.
//
// The word is "assigned", never "seated": a seat is the desk's display of an
// agent (store_mem.go, the v0.40.1 comment), a different thing.

// assignGatedRoles are the roles whose cards are planned from outside events
// and so are switched by assignment (§3.4). The fixer is gated only for review
// defects; the D-091 defect flow has its own consent, AutoFix.Enabled.
var assignGatedRoles = []string{"pr-reviewer", "qa", "unit-test", "fixer"}

// normalizeAssigned sorts and de-duplicates, dropping blanks. Empty is nil, so a
// round trip through the store compares equal.
func normalizeAssigned(ids []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// roleAssigned reports whether a configured product has an agent of the role.
// An id that names no agent counts as none.
func roleAssigned(m Master, role string) bool {
	for _, id := range m.Assigned {
		if a := agentByID(id); a != nil && a.Role == role {
			return true
		}
	}
	return false
}

// roleRuns reports whether a pull-request flow role runs for a product: always
// on an unconfigured product, and only when assigned on a configured one.
func roleRuns(m Master, role string) bool {
	return !m.AssignedSet || roleAssigned(m, role)
}

// SetAssigned saves or resets a product's assignments. The event is
// RecordUpdated, as every master-record edit is; it names the actor's alias.
func (s *MemStore) SetAssigned(ctx context.Context, productID string, ids []string, set bool, actor string) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.masterPtr(productID)
	if m == nil {
		return Result{}, ErrNotFound
	}
	ids = normalizeAssigned(ids)
	if !set && len(ids) > 0 {
		return Result{}, reject("a reset assigns nobody")
	}
	for _, id := range ids {
		if agentByID(id) == nil {
			return Result{}, reject("no agent " + id)
		}
	}
	first := !m.AssignedSet && set
	m.Assigned, m.AssignedSet = ids, set
	alias := actorAlias(actor)
	name := s.productName(productID)
	var text string
	switch {
	case !set:
		text = name + ": assignments reset by " + alias
	case len(ids) == 0:
		text = name + ": no agents assigned by " + alias
	default:
		text = name + ": agents assigned by " + alias + ": " + strings.Join(ids, " ")
	}
	if first {
		text += " (gating on; medium and worse findings now request changes)"
	}
	ev := s.emit(RecordUpdated, productID, text, KindInfo, EventPayload{Actor: alias, Rule: "assign"})
	c := m.Clone()
	toast := fmt.Sprintf("%s: %d agent(s) assigned — gating on.", name, len(ids))
	if !set {
		toast = name + ": gating off — every PR flow runs."
	}
	return Result{Master: &c, NewEvents: []Event{ev}, Toast: toast}, nil
}
