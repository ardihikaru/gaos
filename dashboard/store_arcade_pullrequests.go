package main

import "context"

// The PullRequest vertex (schema/18-pull-requests.sql, D-092): written in the transaction of the
// change that touched it, read once at boot.

func (s *ArcadeStore) writePullRequest(ctx context.Context, tx *ArcadeTx, p PullRequest) error {
	return s.upsert(ctx, tx, "PullRequest", p.Key, map[string]any{
		"productId": p.ProductID, "repo": p.Repo, "number": p.Number, "kind": p.Kind, "state": p.State,
		"title": p.Title, "author": p.Author, "headRef": p.HeadRef, "baseRef": p.BaseRef,
		"taskId": p.TaskID, "qaJob": p.QAJob, "qaCase": p.QACase, "qaLink": p.QALink,
		"openedAt": p.OpenedAt, "checkedAt": p.CheckedAt,
	})
}

func (s *ArcadeStore) deletePullRequest(ctx context.Context, tx *ArcadeTx, key string) error {
	return s.run(ctx, tx, "DELETE FROM PullRequest WHERE id = :id AND tenantId = :t",
		map[string]any{"id": key, "t": s.tenant})
}

func (s *ArcadeStore) loadPullRequests(ctx context.Context) ([]PullRequest, error) {
	var rows []struct {
		ID        string `json:"id"`
		ProductID string `json:"productId"`
		Repo      string `json:"repo"`
		Number    int    `json:"number"`
		Kind      string `json:"kind"`
		State     string `json:"state"`
		Title     string `json:"title"`
		Author    string `json:"author"`
		HeadRef   string `json:"headRef"`
		BaseRef   string `json:"baseRef"`
		TaskID    string `json:"taskId"`
		QAJob     string `json:"qaJob"`
		QACase    string `json:"qaCase"`
		QALink    string `json:"qaLink"`
		OpenedAt  int64  `json:"openedAt"`
		CheckedAt int64  `json:"checkedAt"`
	}
	if err := s.arc.Query(ctx,
		`SELECT id, productId, repo, number, kind, state, title, author, headRef, baseRef, taskId,
		        qaJob, qaCase, qaLink, openedAt, checkedAt
		 FROM PullRequest WHERE tenantId = :t ORDER BY id`,
		map[string]any{"t": s.tenant}, &rows); err != nil {
		return nil, err
	}
	out := make([]PullRequest, 0, len(rows))
	for _, r := range rows {
		out = append(out, PullRequest{Key: r.ID, ProductID: r.ProductID, Repo: r.Repo, Number: r.Number,
			Kind: r.Kind, State: r.State, Title: r.Title, Author: r.Author, HeadRef: r.HeadRef,
			BaseRef: r.BaseRef, TaskID: r.TaskID, QAJob: r.QAJob, QACase: r.QACase, QALink: r.QALink,
			OpenedAt: r.OpenedAt, CheckedAt: r.CheckedAt})
	}
	return out, nil
}
