package main

import (
	"context"
	"encoding/json"
)

// The defect records' graph half (D-091, schema/17-defects.sql). A Defect and a
// FixPR vertex are written in the transaction of the change that touched them
// and read once at boot. Neither is folded from the event stream.

func listJSON(v []string) string {
	if len(v) == 0 {
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func listFrom(s string) []string {
	var out []string
	if s != "" {
		_ = json.Unmarshal([]byte(s), &out)
	}
	return out
}

// nilIfEmpty stores a constrained property that has no value as absent.
func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *ArcadeStore) writeDefect(ctx context.Context, tx *ArcadeTx, d Defect) error {
	ctxJSON := ""
	if d.Context.Built {
		b, err := json.Marshal(d.Context)
		if err != nil {
			return err
		}
		ctxJSON = string(b)
	}
	return s.upsert(ctx, tx, "Defect", d.Key, map[string]any{
		"productId": d.ProductID, "defectId": d.DefectID, "caseId": d.CaseID, "kind": d.Kind,
		"level": d.Level, "automationType": d.AutomationType, "backend": d.Backend,
		"targetRepos": listJSON(d.TargetRepos), "operations": listJSON(d.Operations),
		"spec": d.Spec, "title": d.Title, "excerpt": d.Excerpt, "excerptWithheld": d.ExcerptWithheld,
		"firstSeen": d.FirstSeen, "lastSeen": d.LastSeen, "runRef": d.RunRef, "gen": d.Gen,
		"regression": d.Regression, "difficulty": nilIfEmpty(d.Difficulty), "triageClass": d.Class,
		"repo": d.Repo, "files": listJSON(d.Files), "reason": d.Reason, "state": d.State,
		"supersededBy": d.SupersededBy, "triageTask": d.TriageTask, "fixTask": d.FixTask,
		"fixPR": d.FixPR, "context": ctxJSON,
	})
}

func (s *ArcadeStore) loadDefects(ctx context.Context) ([]Defect, error) {
	var rows []struct {
		ID              string `json:"id"`
		ProductID       string `json:"productId"`
		DefectID        string `json:"defectId"`
		CaseID          string `json:"caseId"`
		Kind            string `json:"kind"`
		Level           string `json:"level"`
		AutomationType  string `json:"automationType"`
		Backend         string `json:"backend"`
		TargetRepos     string `json:"targetRepos"`
		Operations      string `json:"operations"`
		Spec            string `json:"spec"`
		Title           string `json:"title"`
		Excerpt         string `json:"excerpt"`
		ExcerptWithheld bool   `json:"excerptWithheld"`
		FirstSeen       int64  `json:"firstSeen"`
		LastSeen        int64  `json:"lastSeen"`
		RunRef          string `json:"runRef"`
		Gen             int    `json:"gen"`
		Regression      bool   `json:"regression"`
		Difficulty      string `json:"difficulty"`
		Class           string `json:"triageClass"`
		Repo            string `json:"repo"`
		Files           string `json:"files"`
		Reason          string `json:"reason"`
		State           string `json:"state"`
		SupersededBy    string `json:"supersededBy"`
		TriageTask      string `json:"triageTask"`
		FixTask         string `json:"fixTask"`
		FixPR           string `json:"fixPR"`
		Context         string `json:"context"`
	}
	if err := s.arc.Query(ctx,
		`SELECT id, productId, defectId, caseId, kind, level, automationType, backend, targetRepos,
		        operations, spec, title, excerpt, excerptWithheld, firstSeen, lastSeen, runRef, gen,
		        regression, difficulty, triageClass, repo, files, reason, state, supersededBy,
		        triageTask, fixTask, fixPR, context
		 FROM Defect WHERE tenantId = :t ORDER BY id`,
		map[string]any{"t": s.tenant}, &rows); err != nil {
		return nil, err
	}
	out := make([]Defect, 0, len(rows))
	for _, r := range rows {
		d := Defect{
			Key: r.ID, ProductID: r.ProductID, DefectID: r.DefectID, CaseID: r.CaseID, Kind: r.Kind,
			Level: r.Level, AutomationType: r.AutomationType, Backend: r.Backend,
			TargetRepos: listFrom(r.TargetRepos), Operations: listFrom(r.Operations), Spec: r.Spec,
			Title: r.Title, Excerpt: r.Excerpt, ExcerptWithheld: r.ExcerptWithheld,
			FirstSeen: r.FirstSeen, LastSeen: r.LastSeen, RunRef: r.RunRef, Gen: r.Gen,
			Regression: r.Regression, Difficulty: r.Difficulty, Class: r.Class, Repo: r.Repo,
			Files: listFrom(r.Files), Reason: r.Reason, State: r.State, SupersededBy: r.SupersededBy,
			TriageTask: r.TriageTask, FixTask: r.FixTask, FixPR: r.FixPR,
		}
		if r.Context != "" {
			_ = json.Unmarshal([]byte(r.Context), &d.Context)
		}
		out = append(out, d)
	}
	return out, nil
}

func (s *ArcadeStore) writeFixPR(ctx context.Context, tx *ArcadeTx, f FixPR) error {
	return s.upsert(ctx, tx, "FixPR", f.ID, map[string]any{
		"productId": f.ProductID, "defectKey": f.DefectKey, "repo": f.Repo, "number": f.Number,
		"url": f.URL, "state": f.State, "openedAt": f.CreatedAt, "changedAt": f.UpdatedAt,
		"taskId": f.TaskID, "difficulty": f.Difficulty, "beforeText": f.Before, "afterText": f.After,
		"branch": f.Branch, "baseSHA": f.BaseSHA, "commitSHA": f.CommitSHA, "waSent": f.WaSent,
		"reply": f.Reply,
	})
}

func (s *ArcadeStore) loadFixPRs(ctx context.Context) ([]FixPR, error) {
	var rows []struct {
		ID         string `json:"id"`
		ProductID  string `json:"productId"`
		DefectKey  string `json:"defectKey"`
		Repo       string `json:"repo"`
		Number     int    `json:"number"`
		URL        string `json:"url"`
		State      string `json:"state"`
		CreatedAt  int64  `json:"openedAt"`
		UpdatedAt  int64  `json:"changedAt"`
		TaskID     string `json:"taskId"`
		Difficulty string `json:"difficulty"`
		Before     string `json:"beforeText"`
		After      string `json:"afterText"`
		Branch     string `json:"branch"`
		BaseSHA    string `json:"baseSHA"`
		CommitSHA  string `json:"commitSHA"`
		WaSent     bool   `json:"waSent"`
		Reply      string `json:"reply"`
	}
	if err := s.arc.Query(ctx,
		`SELECT id, productId, defectKey, repo, number, url, state, openedAt, changedAt, taskId,
		        difficulty, beforeText, afterText, branch, baseSHA, commitSHA, waSent, reply
		 FROM FixPR WHERE tenantId = :t ORDER BY id`,
		map[string]any{"t": s.tenant}, &rows); err != nil {
		return nil, err
	}
	out := make([]FixPR, 0, len(rows))
	for _, r := range rows {
		out = append(out, FixPR{ID: r.ID, ProductID: r.ProductID, DefectKey: r.DefectKey, Repo: r.Repo,
			Number: r.Number, URL: r.URL, State: r.State, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
			TaskID: r.TaskID, Difficulty: r.Difficulty, Before: r.Before, After: r.After, Branch: r.Branch,
			BaseSHA: r.BaseSHA, CommitSHA: r.CommitSHA, WaSent: r.WaSent, Reply: r.Reply})
	}
	return out, nil
}
