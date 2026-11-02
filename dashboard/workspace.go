package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// The product's workspace. M14.4, D-058..D-060, BR-38, BR-39, BR-42.
//
// One product has one ai-manager workspace, named by id and never by path
// (BR-42): gaos-<tenant>-<product>, derived once and never renamed, because
// ai-claude-cli keys stored transcripts by it. GAOS provisions it through the
// upstream API (D-059), writes the product's published Blueprint into it, and
// remembers which version and digest the workspace now holds. A dispatch whose
// product has a published Blueprint the workspace does not hold is refused as
// drift (BR-39) — the model would otherwise run under rules the record says
// it never saw.

// ProductWorkspace is GAOS's record of the product's workspace.
type ProductWorkspace struct {
	ID            string // gaos-<tenant>-<product>
	ProvisionedAt int64
	SyncedVersion int    // the BlueprintVersion.N the workspace holds; 0 = none
	SyncedDigest  string // that version's digest, as the worker reported it
	SyncedAt      int64
}

// WorkspaceGateway is what an adapter offers when model work runs in a
// remote, provisioned workspace. It is a capability, asserted by type, and
// never a branch on the invocation mode (BR-33): an adapter that has no
// workspace to keep in step simply does not implement it.
type WorkspaceGateway interface {
	WorkspaceFor(productID string) string
	// SyncWorkspace makes the workspace hold exactly files, provisioning it
	// first if needed, and returns the digest the worker now reports.
	SyncWorkspace(ctx context.Context, workspaceID string, files []BlueprintFile) (digest string, provisioned bool, err error)
}

// Drift and refusal rules, beside egress's own (D-052 made DispatchRefused
// the one event for every refused dispatch).
const (
	RuleBlueprintDrift  = "blueprint-drift"
	RuleEgressBlueprint = "egress-blueprint"
)

// SyncWorkspace implements WorkspaceGateway for ai-manager. Provision when the
// worker has never heard of the workspace; otherwise replace its files
// (prune, guarded by if_match) unless it already holds this digest.
func (a *AIMAdapter) SyncWorkspace(ctx context.Context, id string, files []BlueprintFile) (string, bool, error) {
	want := BlueprintDigest(files)
	wire := make([]WorkspaceFile, len(files))
	for i, f := range files {
		wire[i] = WorkspaceFile{Path: f.Path, Content: f.Content}
	}
	cur, err := a.Client.Files(ctx, id)
	provisioned := false
	switch {
	case errors.Is(err, ErrNotProvisioned):
		cur, err = a.Client.Provision(ctx, id, wire)
		provisioned = true
	case err == nil && cur.Digest != want:
		cur, err = a.Client.PutFiles(ctx, id, wire, true, cur.Digest)
	}
	if err != nil {
		return "", provisioned, err
	}
	if cur.Digest != want {
		return cur.Digest, provisioned, fmt.Errorf("the workspace %s reports digest %s after the sync, and the published version is %s", id, cur.Digest, want)
	}
	return cur.Digest, provisioned, nil
}

// SyncBlueprint writes the product's latest published Blueprint into its
// workspace. The files are screened first — they reach the model on every
// turn, so the sync is a dispatch path and BR-32 applies (T-154). Network
// calls happen outside the store's lock.
func (s *MemStore) SyncBlueprint(ctx context.Context, id string) (Result, error) {
	s.mu.RLock()
	gw, isGateway := s.adapter.(WorkspaceGateway)
	m := s.masterPtr(id)
	var v BlueprintVersion
	var has bool
	if m != nil {
		v, has = m.Blueprint.Latest()
	}
	pol, tgt := s.egress, s.target
	s.mu.RUnlock()
	switch {
	case m == nil:
		return Result{}, ErrNotFound
	case !isGateway:
		return Result{}, reject("No gateway is configured (GAOS_MODEL_MODE=aimanager), so there is no workspace to sync to.")
	case !has:
		return Result{}, reject("Publish the Blueprint first; only a published version is written to the workspace.")
	}

	parts := make([]PacketPart, len(v.Files))
	for i, f := range v.Files {
		parts[i] = PacketPart{Path: f.Path, Kind: KindRules, Body: f.Content}
	}
	if _, verdict, err := Screen(pol, tgt, Packet{TaskID: "blueprint", DeskID: id, Parts: parts}); err != nil {
		reason := fmt.Sprintf("Blueprint v%d was not written to the workspace: %s", v.N, verdict.Reason)
		s.mu.Lock()
		e := s.emit(DispatchRefused, id, reason, KindFail, EventPayload{Reason: reason, Rule: RuleEgressBlueprint, Version: v.N, Digest: v.Digest})
		r := s.result(reason, e)
		s.mu.Unlock()
		// A refusal is a state change the timeline must show (L-5), so it is a
		// Result with its event and no error -- as DispatchTask answers one.
		// An error would make ArcadeStore drop the event it already emitted.
		return r, nil
	}

	wsID := gw.WorkspaceFor(id)
	digest, provisioned, err := gw.SyncWorkspace(ctx, wsID, v.Files)
	if err != nil {
		return Result{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	m = s.masterPtr(id)
	if m == nil {
		return Result{}, ErrNotFound
	}
	now := time.Now().Unix()
	var ev []Event
	if m.Workspace.ID == "" || provisioned {
		m.Workspace.ID, m.Workspace.ProvisionedAt = wsID, now
		ev = append(ev, s.emit(WorkspaceProvisioned, id, fmt.Sprintf("%s: workspace %s provisioned", s.productName(id), wsID), KindInfo, EventPayload{}))
	}
	m.Workspace.SyncedVersion, m.Workspace.SyncedDigest, m.Workspace.SyncedAt = v.N, digest, now
	ev = append(ev, s.emit(BlueprintSynced, id, fmt.Sprintf("%s: blueprint v%d is in %s", s.productName(id), v.N, wsID), KindKnow,
		EventPayload{Version: v.N, Digest: digest}))
	ev = append(ev, s.releaseDriftLocked(id)...)
	r := s.result(fmt.Sprintf("Blueprint version %d is in the workspace %s.", v.N, wsID), ev...)
	c := m.Clone()
	r.Master = &c
	return r, nil
}

// blueprintGate is BR-39 at dispatch: through a workspace gateway, a product
// with a published Blueprint dispatches only when its workspace holds the
// latest version. It returns the version the attempt pins (0 when there is
// nothing to pin) and, on drift, the refusal verdict. Caller holds s.mu.
func (s *MemStore) blueprintGate(deskID string) (int, *EgressVerdict) {
	if _, ok := s.adapter.(WorkspaceGateway); !ok {
		return 0, nil
	}
	m := s.masterPtr(deskID)
	if m == nil {
		return 0, nil
	}
	v, ok := m.Blueprint.Latest()
	if !ok {
		return 0, nil
	}
	if m.Workspace.SyncedDigest != v.Digest {
		held := "no Blueprint"
		if m.Workspace.SyncedVersion > 0 {
			held = fmt.Sprintf("version %d", m.Workspace.SyncedVersion)
		}
		return 0, &EgressVerdict{Rule: RuleBlueprintDrift, Reason: fmt.Sprintf(
			"the workspace holds %s and Blueprint version %d is published; sync it before dispatching (BR-39)", held, v.N)}
	}
	return v.N, nil
}

// releaseDriftLocked puts back to READY the tasks a drift refusal blocked, now
// that the workspace holds the published version. Only drift: a task an egress
// refusal blocked stays blocked, because syncing the Blueprint fixes nothing
// about its packet. Caller holds s.mu.
func (s *MemStore) releaseDriftLocked(deskID string) []Event {
	i := s.deskIdxByID(deskID)
	if i < 0 || !strings.Contains(s.desks[i].BlockReason, "(BR-39)") {
		return nil
	}
	n := 0
	for j := range s.tasks {
		if s.tasks[j].DeskID == deskID && s.tasks[j].Status == StatusBlocked {
			if moveTask(&s.tasks[j], StatusReady) == nil {
				n++
			}
		}
	}
	dk := &s.desks[i]
	dk.BlockReason = ""
	_ = s.setState(dk, StateIdle)
	return []Event{s.emit(TaskResumed, deskID, fmt.Sprintf("%d task(s) released: the workspace now holds the published Blueprint", n),
		KindInfo, EventPayload{Rule: RuleRequeued})} // the fold makes the desk idle, as above
}
