package main

import "context"

// The UnitWatch vertex (schema/21-unit-checks.sql, D-105; the posted result's
// properties are schema/22-unit-results.sql, D-106): written in the transaction of the
// change that touched it, read once at boot. Never folded from the event stream.

func (s *ArcadeStore) writeUnitWatch(ctx context.Context, tx *ArcadeTx, w UnitWatch) error {
	return s.upsert(ctx, tx, "UnitWatch", w.Key, map[string]any{
		"productId": w.ProductID, "repo": w.Repo, "pr": w.PR, "sha": w.SHA, "base": w.Base,
		"mergedAt": w.MergedAt, "firstSeen": w.FirstSeen, "nextAt": w.NextAt, "doneAt": w.DoneAt,
		"tries": w.Tries, "reads": w.Reads, "state": w.State, "outcome": nilIfEmpty(w.Outcome),
		"defectKey": w.DefectKey,
		"runUrl":    w.RunURL, "postedFrom": w.PostedFrom, "postedResult": nilIfEmpty(w.PostedResult),
		"conflictNoticeSent": w.ConflictNoticeSent,
	})
}

func (s *ArcadeStore) deleteUnitWatch(ctx context.Context, tx *ArcadeTx, key string) error {
	return s.run(ctx, tx, "DELETE FROM UnitWatch WHERE id = :id AND tenantId = :t",
		map[string]any{"id": key, "t": s.tenant})
}

func (s *ArcadeStore) loadUnitWatches(ctx context.Context) ([]UnitWatch, error) {
	var rows []struct {
		ID        string `json:"id"`
		ProductID string `json:"productId"`
		Repo      string `json:"repo"`
		PR        int    `json:"pr"`
		SHA       string `json:"sha"`
		Base      string `json:"base"`
		MergedAt  int64  `json:"mergedAt"`
		FirstSeen int64  `json:"firstSeen"`
		NextAt    int64  `json:"nextAt"`
		DoneAt    int64  `json:"doneAt"`
		Tries     int    `json:"tries"`
		Reads     int    `json:"reads"`
		State     string `json:"state"`
		Outcome   string `json:"outcome"`
		DefectKey string `json:"defectKey"`
		RunURL    string `json:"runUrl"`
		From      string `json:"postedFrom"`
		Posted    string `json:"postedResult"`
		Conflict  bool   `json:"conflictNoticeSent"`
	}
	if err := s.arc.Query(ctx,
		`SELECT id, productId, repo, pr, sha, base, mergedAt, firstSeen, nextAt, doneAt, tries, reads,
		        state, outcome, defectKey, runUrl, postedFrom, postedResult, conflictNoticeSent
		 FROM UnitWatch WHERE tenantId = :t ORDER BY id`,
		map[string]any{"t": s.tenant}, &rows); err != nil {
		return nil, err
	}
	out := make([]UnitWatch, 0, len(rows))
	for _, r := range rows {
		out = append(out, UnitWatch{Key: r.ID, ProductID: r.ProductID, Repo: r.Repo, PR: r.PR, SHA: r.SHA,
			Base: r.Base, MergedAt: r.MergedAt, FirstSeen: r.FirstSeen, NextAt: r.NextAt, DoneAt: r.DoneAt,
			Tries: r.Tries, Reads: r.Reads, State: r.State, Outcome: r.Outcome, DefectKey: r.DefectKey,
			RunURL: r.RunURL, PostedFrom: r.From, PostedResult: r.Posted, ConflictNoticeSent: r.Conflict})
	}
	return out, nil
}
