package main

import (
	"context"
	"encoding/json"
)

// ArcadeStore's QA catalog (qacatalog.go, D-072). Reads come from the
// projection; a write runs the rule on the projection and persists the Result
// -- the Master (its Qa document), the changed QaCase vertices and the one
// RecordUpdated event -- in the mutation's single transaction. A QA Result
// carries no Floor, so persist's desk loop does not run (qacatalog_store.go).

func (s *ArcadeStore) QaCases(ctx context.Context, id string, q QaQuery) (QaPage, error) {
	return s.proj.QaCases(ctx, id, q)
}
func (s *ArcadeStore) QaSummary(ctx context.Context, id string) (QaSummary, error) {
	return s.proj.QaSummary(ctx, id)
}

func (s *ArcadeStore) QaImport(ctx context.Context, id string, in QaImportIn) (QaImportOut, Result, error) {
	var out QaImportOut
	r, err := s.mutate(ctx, func() (Result, error) {
		o, r, err := s.proj.QaImport(ctx, id, in)
		out = o
		return r, err
	})
	return out, r, err
}

func (s *ArcadeStore) QaApplyResults(ctx context.Context, id string, in QaResultsIn) (QaResultsOut, Result, error) {
	var out QaResultsOut
	r, err := s.mutate(ctx, func() (Result, error) {
		o, r, err := s.proj.QaApplyResults(ctx, id, in)
		out = o
		return r, err
	})
	return out, r, err
}

func (s *ArcadeStore) QaRecordSync(ctx context.Context, id, dir string, side QaSyncSide) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.QaRecordSync(ctx, id, dir, side) })
}

func qaVertexID(productID, caseID string) string { return productID + ":" + caseID }

// writeQaCase upserts one case and links it to its product.
func (s *ArcadeStore) writeQaCase(ctx context.Context, tx *ArcadeTx, c QaCase) error {
	props := map[string]any{
		"productId": c.ProductID, "caseId": c.CaseID, "epic": c.Epic, "module": c.Module,
		"features": c.Features, "userStory": c.UserStory, "scenario": c.Scenario,
		"polarity": c.Polarity, "level": c.Level, "pri": c.Pri,
		"decision": c.Decision, "reason": c.Reason,
		"implemented": c.Implemented, "spec": c.Spec, "runAt": c.RunAt, "runRef": c.RunRef,
		"catRev": c.CatRev,
	}
	// result is constrained to pass|fail|skipped; an empty one is an absent
	// property, never an empty string the regexp would refuse.
	if c.Result != "" {
		props["result"] = c.Result
	}
	id := qaVertexID(c.ProductID, c.CaseID)
	if err := s.upsert(ctx, tx, "QaCase", id, props); err != nil {
		return err
	}
	return s.edge(ctx, tx, "HAS_QA_CASE", "Product", c.ProductID, "QaCase", id)
}

// loadQaCases reads every case of the tenant back.
func (s *ArcadeStore) loadQaCases(ctx context.Context) ([]QaCase, error) {
	var rows []struct {
		ProductID   string          `json:"productId"`
		CaseID      string          `json:"caseId"`
		Epic        string          `json:"epic"`
		Module      string          `json:"module"`
		Features    json.RawMessage `json:"features"`
		UserStory   string          `json:"userStory"`
		Scenario    string          `json:"scenario"`
		Polarity    string          `json:"polarity"`
		Level       string          `json:"level"`
		Pri         string          `json:"pri"`
		Decision    string          `json:"decision"`
		Reason      string          `json:"reason"`
		Implemented bool            `json:"implemented"`
		Spec        string          `json:"spec"`
		Result      string          `json:"result"`
		RunAt       int64           `json:"runAt"`
		RunRef      string          `json:"runRef"`
		CatRev      int             `json:"catRev"`
	}
	if err := s.arc.Query(ctx,
		`SELECT productId, caseId, epic, module, features, userStory, scenario, polarity, level, pri,
		        decision, reason, implemented, spec, result, runAt, runRef, catRev
		 FROM QaCase WHERE tenantId = :t ORDER BY productId, caseId`,
		map[string]any{"t": s.tenant}, &rows); err != nil {
		return nil, err
	}
	out := make([]QaCase, 0, len(rows))
	for _, r := range rows {
		c := QaCase{ProductID: r.ProductID, CaseID: r.CaseID, Epic: r.Epic, Module: r.Module,
			UserStory: r.UserStory, Scenario: r.Scenario, Polarity: r.Polarity, Level: r.Level, Pri: r.Pri,
			Decision: r.Decision, Reason: r.Reason, Implemented: r.Implemented, Spec: r.Spec,
			Result: r.Result, RunAt: r.RunAt, RunRef: r.RunRef, CatRev: r.CatRev}
		if err := decodeInto(r.Features, &c.Features); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}
