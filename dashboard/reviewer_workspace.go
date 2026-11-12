package main

import (
	"context"
	"fmt"
	"time"
)

// The reviewer's own workspace. M17.5, D-071.
//
// A review task's turn used to run in the product's own workspace — the folder
// the author's turn writes into, and the key the author's transcript is stored
// under. The packet was already minimal (the design and the rubric, and
// nothing else) and the session was already fresh, but the folder was shared,
// so the reviewer could reach the author's files.
//
// The product now has a second workspace: its reviewer workspace. It holds the
// published Blueprint byte-identical — its own copy of the standing files, its
// own root — and it is reset before every review turn, so it holds the standing
// files and nothing else: no author file, and no earlier review's leftover.
//
// The reviewer's own session lives there too. A session the gateway opened in a
// workspace stays in it, so a review's retry resumes in the reviewer's folder
// and never in the author's.

// maxWorkspaceIDLen is the gateway's cap on a workspace id. The id matches
// ^[a-z0-9][a-z0-9._-]{0,62}$, so it is at most 63 characters.
const maxWorkspaceIDLen = 63

// ReviewWorkspaceSuffix is the tail of a reviewer's workspace id.
const ReviewWorkspaceSuffix = "-review"

// ReviewWorkspaceFor derives the reviewer's workspace id for a product.
//
// It is never the shared-workspace override: every product's reviewer keeps its
// own root and its own initial context, which is the whole point of the id
// (D-071). The id obeys the gateway's grammar, and one that would overflow is
// truncated and given a digest of the full id, so it stays unique and legal.
func (a *AIMAdapter) ReviewWorkspaceFor(productID string) string {
	return workspaceID(a.Tenant, productID, ReviewWorkspaceSuffix)
}

// reviewerWorkspaceID is the reviewer's workspace for a product, or "" when the
// adapter has no workspace to keep in step — the CLI adapter, which has none.
func (s *MemStore) reviewerWorkspaceID(deskID string) string {
	gw, ok := s.adapter.(WorkspaceGateway)
	if !ok {
		return ""
	}
	return gw.ReviewWorkspaceFor(deskID)
}

// prepareReviewWorkspace makes the reviewer's workspace hold exactly the
// published Blueprint and nothing else.
//
// The reset is also the reviewer's drift check (BR-39): the folder holds the
// standing files the published version wrote, so a review never runs under
// rules the record says it never saw. And because the write prunes, the folder
// carries no author file — the author's file is in the other one — and no
// earlier review's leftover.
//
// A failure is a refusal, not a turn: the dispatch is refused before anything
// is spent. The network calls happen outside the store's lock.
func (s *MemStore) prepareReviewWorkspace(ctx context.Context, deskID string) *EgressVerdict {
	gw, ok := s.adapter.(WorkspaceGateway)
	if !ok {
		return nil
	}
	s.mu.RLock()
	m := s.masterPtr(deskID)
	var files []BlueprintFile
	var vendors []string
	if m != nil {
		if v, has := m.Blueprint.Latest(); has {
			files = v.Files
		}
		vendors = m.Vendors.Vendors()
	}
	s.mu.RUnlock()

	if len(files) == 0 {
		return &EgressVerdict{Rule: RuleBlueprintDrift, Reason: fmt.Sprintf(
			"the product %s has no published Blueprint, so its reviewer has no standing files to run under (BR-39)", deskID)}
	}

	// The sync is a dispatch path: the files reach the model on every review
	// turn, so the egress screen runs on them (BR-32) — the same screen the
	// product's own workspace gets.
	if verdict := s.screenBlueprintFiles(deskID, files); verdict != nil {
		// The reason carries (BR-39), because that is the marker the drift
		// release looks for: a desk blocked by this must be released when the
		// Blueprint is synced, not left blocked forever.
		return &EgressVerdict{Rule: verdict.Rule, Reason: fmt.Sprintf(
			"the published Blueprint was not written to the reviewer workspace: %s (BR-39)", verdict.Reason)}
	}

	wsID := gw.ReviewWorkspaceFor(deskID)
	done, _, err := s.syncWorkspaceTo(ctx, wsID, files, vendors)
	if err != nil {
		return &EgressVerdict{Rule: RuleBlueprintDrift, Reason: fmt.Sprintf(
			"the reviewer workspace %s could not be prepared: %v (BR-39)", wsID, err)}
	}

	// The record says what the reviewer's copy holds, so the drift check after
	// the turn knows which folder it is looking at.
	s.mu.Lock()
	defer s.mu.Unlock()
	if m := s.masterPtr(deskID); m != nil {
		m.Workspace.Review.ID = wsID
		m.Workspace.Review.ProvisionedAt = time.Now().Unix()
		for _, d := range done {
			m.Workspace.Review.setHolds(d.vendor, WorkspaceSync{Version: latestVersion(m), Digest: d.digest, At: time.Now().Unix()})
		}
	}
	return nil
}

// latestVersion is the published version's number, or 0.
func latestVersion(m *Master) int {
	if v, ok := m.Blueprint.Latest(); ok {
		return v.N
	}
	return 0
}

// runWorkspace is the workspace a run used, "" for a run recorded before the
// reviewer workspace existed.
func (s *MemStore) runWorkspace(runID string) string {
	for _, r := range s.runs {
		if r.ID == runID {
			return r.Workspace
		}
	}
	return ""
}

// isReviewWorkspace reports whether a workspace id is a product's reviewer
// workspace rather than the product's own.
func (s *MemStore) isReviewWorkspace(deskID, wsID string) bool {
	review := s.reviewerWorkspaceID(deskID)
	return review != "" && review == wsID
}
