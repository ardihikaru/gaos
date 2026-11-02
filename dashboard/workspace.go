package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
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

	// Others is what the copy on each vendor other than Claude holds (M16):
	// one workspace id, provisioned on every vendor the product's policy
	// uses. Claude's copy stays in the three fields above, where every record
	// written before M16 has it.
	Others map[string]WorkspaceSync
}

// WorkspaceSync is what one vendor's copy of the workspace holds.
type WorkspaceSync struct {
	Version int
	Digest  string
	At      int64
}

// Holds reports what vendor's copy of the workspace holds.
func (w ProductWorkspace) Holds(vendor string) WorkspaceSync {
	if vendor == VendorClaude || vendor == "" {
		return WorkspaceSync{Version: w.SyncedVersion, Digest: w.SyncedDigest, At: w.SyncedAt}
	}
	return w.Others[vendor]
}

// setHolds records what vendor's copy now holds.
func (w *ProductWorkspace) setHolds(vendor string, h WorkspaceSync) {
	if vendor == VendorClaude || vendor == "" {
		w.SyncedVersion, w.SyncedDigest, w.SyncedAt = h.Version, h.Digest, h.At
		return
	}
	if w.Others == nil {
		w.Others = map[string]WorkspaceSync{}
	}
	w.Others[vendor] = h
}

// Clone deep-copies the per-vendor map.
func (w ProductWorkspace) Clone() ProductWorkspace {
	c := w
	if w.Others != nil {
		c.Others = make(map[string]WorkspaceSync, len(w.Others))
		for k, v := range w.Others {
			c.Others[k] = v
		}
	}
	return c
}

// WorkspaceGateway is what an adapter offers when model work runs in a
// remote, provisioned workspace. It is a capability, asserted by type, and
// never a branch on the invocation mode (BR-33): an adapter that has no
// workspace to keep in step simply does not implement it.
type WorkspaceGateway interface {
	WorkspaceFor(productID string) string
	// SyncWorkspace makes vendor's copy of the workspace hold exactly files,
	// provisioning it first if needed, and returns the digest it now reports.
	SyncWorkspace(ctx context.Context, vendor, workspaceID string, files []BlueprintFile) (digest string, provisioned bool, err error)
	// WorkspaceDigest is the digest vendor's copy reports now, read after a
	// turn to catch a model that rewrote its own instructions.
	WorkspaceDigest(ctx context.Context, vendor, workspaceID string) (string, error)
}

// Drift and refusal rules, beside egress's own (D-052 made DispatchRefused
// the one event for every refused dispatch).
const (
	RuleBlueprintDrift  = "blueprint-drift"
	RuleEgressBlueprint = "egress-blueprint"
	// RuleObservedDrift is drift seen AFTER a turn (T-156): the workspace no
	// longer holds the version the attempt pinned, or the context the worker
	// reports shows instructions GAOS did not write. It refuses the next
	// dispatch, the way blueprint-drift refuses this one.
	RuleObservedDrift = "observed-drift"
)

// WorkspaceDigest implements WorkspaceGateway for ai-manager.
func (a *AIMAdapter) WorkspaceDigest(ctx context.Context, vendor, id string) (string, error) {
	m, err := a.Client.Files(ctx, vendor, id)
	return m.Digest, err
}

// SyncWorkspace implements WorkspaceGateway for ai-manager. Provision when the
// worker has never heard of the workspace; otherwise replace its files
// (prune, guarded by if_match) unless it already holds this digest.
func (a *AIMAdapter) SyncWorkspace(ctx context.Context, vendor, id string, files []BlueprintFile) (string, bool, error) {
	want := BlueprintDigest(files)
	wire := make([]WorkspaceFile, len(files))
	for i, f := range files {
		wire[i] = WorkspaceFile{Path: f.Path, Content: f.Content}
	}
	cur, err := a.Client.Files(ctx, vendor, id)
	provisioned := false
	switch {
	case errors.Is(err, ErrNotProvisioned):
		cur, err = a.Client.Provision(ctx, vendor, id, wire)
		provisioned = true
	case errors.Is(err, ErrPoolDisagrees):
		// Some accounts lack the workspace or hold other files. Provisioning
		// is idempotent -- it creates the missing copies and leaves the rest
		// -- and an unconditional pruning write then makes them all equal:
		// there is no one current digest to make the write conditional on.
		if _, err = a.Client.Provision(ctx, vendor, id, wire); err == nil {
			cur, err = a.Client.PutFiles(ctx, vendor, id, wire, true, "")
		}
	case err == nil && cur.Digest != want:
		cur, err = a.Client.PutFiles(ctx, vendor, id, wire, true, cur.Digest)
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
	var vendors []string
	if m != nil {
		vendors = m.Vendors.Vendors()
	}
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

	// Every vendor the policy dispatches to holds its own copy (M16). One
	// that fails does not undo one that succeeded: what each holds is a fact
	// about that vendor, and the gate below asks each of them.
	wsID := gw.WorkspaceFor(id)
	type synced struct {
		vendor, digest string
		provisioned    bool
	}
	var done []synced
	var failed []string
	var firstErr error
	for _, vendor := range vendors {
		digest, provisioned, err := gw.SyncWorkspace(ctx, vendor, wsID, v.Files)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			failed = append(failed, fmt.Sprintf("%s: %v", vendorLabel(vendor), err))
			continue
		}
		done = append(done, synced{vendor, digest, provisioned})
	}
	if len(done) == 0 {
		return Result{}, firstErr
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	m = s.masterPtr(id)
	if m == nil {
		return Result{}, ErrNotFound
	}
	now := time.Now().Unix()
	var ev []Event
	only := len(vendors) == 1 && vendors[0] == VendorClaude // the message every product had before M16
	for _, d := range done {
		on := ""
		if !only {
			on = " on " + vendorLabel(d.vendor)
		}
		if m.Workspace.ID == "" || d.provisioned {
			m.Workspace.ID, m.Workspace.ProvisionedAt = wsID, now
			ev = append(ev, s.emit(WorkspaceProvisioned, id, fmt.Sprintf("%s: workspace %s provisioned%s", s.productName(id), wsID, on), KindInfo, EventPayload{}))
		}
		m.Workspace.setHolds(d.vendor, WorkspaceSync{Version: v.N, Digest: d.digest, At: now})
		ev = append(ev, s.emit(BlueprintSynced, id, fmt.Sprintf("%s: blueprint v%d is in %s%s", s.productName(id), v.N, wsID, on), KindKnow,
			EventPayload{Version: v.N, Digest: d.digest}))
	}
	if _, drift := s.blueprintGate(id); drift == nil {
		ev = append(ev, s.releaseDriftLocked(id)...)
	}
	toast := fmt.Sprintf("Blueprint version %d is in the workspace %s.", v.N, wsID)
	if !only {
		var on []string
		for _, d := range done {
			on = append(on, vendorLabel(d.vendor))
		}
		toast = fmt.Sprintf("Blueprint version %d is in the workspace %s on %s.", v.N, wsID, strings.Join(on, " and "))
	}
	if len(failed) > 0 {
		toast += " Not synced — " + strings.Join(failed, "; ")
	}
	r := s.result(toast, ev...)
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
	// Every vendor the policy uses, because any of the product's tasks may be
	// the next one dispatched to it (M16).
	for _, vendor := range m.Vendors.Vendors() {
		h := m.Workspace.Holds(vendor)
		if h.Digest == v.Digest {
			continue
		}
		held := "no Blueprint"
		if h.Version > 0 {
			held = fmt.Sprintf("version %d", h.Version)
		}
		where := "the workspace"
		if vendor != VendorClaude {
			where = "the workspace on " + vendorLabel(vendor)
		}
		return 0, &EgressVerdict{Rule: RuleBlueprintDrift, Reason: fmt.Sprintf(
			"%s holds %s and Blueprint version %d is published; sync it before dispatching (BR-39)", where, held, v.N)}
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
	// One TaskMoved per task released (T-164), then the desk-level TaskResumed.
	var ev []Event
	for j := range s.tasks {
		if s.tasks[j].DeskID == deskID && s.tasks[j].Status == StatusBlocked {
			if m, ok := s.moveTaskLocked(s.tasks[j].ID, StatusReady, MoveDriftRelease); ok {
				ev = append(ev, m)
			}
		}
	}
	dk := &s.desks[i]
	dk.BlockReason = ""
	_ = s.setState(dk, StateIdle)
	return append(ev, s.emit(TaskResumed, deskID, fmt.Sprintf("%d task(s) released: the workspace now holds the published Blueprint", len(ev)),
		KindInfo, EventPayload{Rule: RuleRequeued})) // the fold makes the desk idle, as above
}

// ---------------------------------------------------------------- after the turn

// ObservedContext is what the worker reports its workspace offered Claude Code
// for the turn: ai-manager's metadata.context.workspace, names and sizes only.
// ClaudeMD paths are the worker's own, absolute; Kind is project (the
// workspace's CLAUDE.md), local (CLAUDE.local.md), dot-claude
// (.claude/CLAUDE.md) or parent (a CLAUDE.md in a folder above). Rules are
// .claude/rules entries relative to that folder, without ".md".
type ObservedContext struct {
	ClaudeMD []ObservedFile `json:"claude_md"`
	Rules    []string       `json:"rules"`
	Agents   []string       `json:"agents"`
	Skills   []string       `json:"skills"`
	Commands []string       `json:"commands"`
}

// ObservedFile is one instruction file the worker found.
type ObservedFile struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	Kind  string `json:"kind"`
}

// observedDrift compares what the worker reports with the version the attempt
// pinned (BR-39, blueprint.html "After the turn"). A Blueprint writes exactly
// one CLAUDE.md, the workspace's own, and its rules; anything else that
// instructs the model -- a CLAUDE.md of any other kind, a rule GAOS did not
// render, an agent, a skill, a command -- is an instruction GAOS did not write,
// and a rule it did render that is missing is drift too. Pure; "" is no drift,
// and so is a reply that reported no context, which leaves nothing to judge.
func observedDrift(v BlueprintVersion, o *ObservedContext) string {
	if o == nil {
		return ""
	}
	want := map[string]bool{}
	var index *BlueprintFile
	for i, f := range v.Files {
		if f.Path == "CLAUDE.md" {
			index = &v.Files[i]
			continue
		}
		want[strings.TrimSuffix(strings.TrimPrefix(f.Path, ".claude/rules/"), ".md")] = true
	}
	sawIndex := false
	for _, f := range o.ClaudeMD {
		if f.Kind != "project" {
			return fmt.Sprintf("the worker loaded %s (%s), an instruction file the Blueprint did not write", f.Path, f.Kind)
		}
		sawIndex = true
		if index != nil && f.Bytes != int64(index.Bytes) {
			return fmt.Sprintf("the workspace's CLAUDE.md is %d bytes and Blueprint version %d wrote %d", f.Bytes, v.N, index.Bytes)
		}
	}
	if index != nil && !sawIndex {
		return fmt.Sprintf("the worker found no CLAUDE.md, and Blueprint version %d wrote one", v.N)
	}
	seen := map[string]bool{}
	for _, r := range o.Rules {
		if !want[r] {
			return fmt.Sprintf("the worker loaded the rule %s, which Blueprint version %d did not write", r, v.N)
		}
		seen[r] = true
	}
	var missing []string
	for r := range want {
		if !seen[r] {
			missing = append(missing, r)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Sprintf("the worker did not load the rule(s) %s that Blueprint version %d wrote", strings.Join(missing, ", "), v.N)
	}
	for kind, names := range map[string][]string{"agent": o.Agents, "skill": o.Skills, "command": o.Commands} {
		if len(names) > 0 {
			return fmt.Sprintf("the worker offered the %s %s, which the Blueprint did not write", kind, names[0])
		}
	}
	return ""
}

// checkAfterTurn is BR-39's second half (T-156). Once a run has finished, the
// workspace is asked for its digest again -- the only way to see a model that
// rewrote its own CLAUDE.md during the turn -- and the context the worker
// reported is compared with the pinned version. Either kind of drift leaves the
// work's own outcome alone (it is judged like any other attempt) and refuses
// the NEXT dispatch: the workspace record stops matching the published digest,
// the desk is blocked with a BR-39 reason a sync releases, and a
// DispatchRefused with rule observed-drift says why. Detected, not prevented
// (R-14). The network call happens outside the store's lock.
func (s *MemStore) checkAfterTurn(ctx context.Context, deskID, runID string, att Attempt) {
	s.mu.RLock()
	gw, isGateway := s.adapter.(WorkspaceGateway)
	pin, vendor := 0, VendorClaude
	for _, r := range s.runs {
		if r.ID == runID {
			pin, vendor = r.BlueprintVersion, runVendor(r)
		}
	}
	var v BlueprintVersion
	wsID := ""
	if m := s.masterPtr(deskID); m != nil && pin > 0 {
		for _, bv := range m.Blueprint.Versions {
			if bv.N == pin {
				v = bv
			}
		}
		wsID = m.Workspace.ID
	}
	s.mu.RUnlock()
	if !isGateway || v.N == 0 {
		return
	}

	why, digest := "", ""
	if wsID != "" {
		// A worker that cannot be asked is not evidence of drift; the next
		// sync or the next turn asks again.
		if d, err := gw.WorkspaceDigest(ctx, vendor, wsID); err == nil && d != v.Digest {
			on := ""
			if vendor != VendorClaude {
				on = " on " + vendorLabel(vendor)
			}
			why, digest = fmt.Sprintf("the workspace %s%s reports digest %s after run %s, which ran under Blueprint version %d", wsID, on, d, runID, v.N), d
		}
	}
	if why == "" {
		why = observedDrift(v, att.Observed)
	}
	if why == "" {
		return
	}
	reason := why + "; the next dispatch is refused until the Blueprint is synced (BR-39)"

	s.mu.Lock()
	if m := s.masterPtr(deskID); m != nil {
		h := m.Workspace.Holds(vendor)
		h.Digest = digest // no longer the published one, so blueprintGate refuses
		m.Workspace.setHolds(vendor, h)
	}
	var e Event
	if i := s.deskIdxByID(deskID); i >= 0 {
		dk := &s.desks[i]
		if dk.State == StateBlocked || s.setState(dk, StateBlocked) == nil {
			dk.BlockReason = reason
		}
		s.log(dk, "blueprint: "+reason, "r")
	}
	e = s.emit(DispatchRefused, deskID, reason, KindFail, EventPayload{Reason: reason, Rule: RuleObservedDrift, Version: v.N, Digest: digest})
	s.mu.Unlock()
	s.publish(e)
}
