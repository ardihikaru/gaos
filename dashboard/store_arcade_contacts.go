package main

import (
	"context"
	"encoding/json"
)

// The contact registry's graph half (D-088). A Contact vertex per account,
// keyed by product and login. It is written in the transaction of the change
// and read once at boot; nothing about it is ever in an event.

func (s *ArcadeStore) writeContact(ctx context.Context, tx *ArcadeTx, op ContactOp) error {
	id := contactID(op.Contact)
	if op.Delete {
		return s.run(ctx, tx, "DELETE FROM Contact WHERE id = :id AND tenantId = :t",
			map[string]any{"id": id, "t": s.tenant})
	}
	c := op.Contact
	return s.upsert(ctx, tx, "Contact", id, map[string]any{
		"productId": c.ProductID, "githubLogin": c.GitHubLogin, "displayName": c.DisplayName,
		"channel": c.Channel, "destination": c.Destination, "notifyOn": notifyOnJSON(c.NotifyOn),
	})
}

func (s *ArcadeStore) loadContacts(ctx context.Context) ([]Contact, error) {
	var rows []struct {
		ProductID   string `json:"productId"`
		GitHubLogin string `json:"githubLogin"`
		DisplayName string `json:"displayName"`
		Channel     string `json:"channel"`
		Destination string `json:"destination"`
		NotifyOn    string `json:"notifyOn"`
	}
	if err := s.arc.Query(ctx,
		`SELECT productId, githubLogin, displayName, channel, destination, notifyOn
		 FROM Contact WHERE tenantId = :t ORDER BY productId, githubLogin`,
		map[string]any{"t": s.tenant}, &rows); err != nil {
		return nil, err
	}
	out := make([]Contact, 0, len(rows))
	for _, r := range rows {
		c := Contact{ProductID: r.ProductID, GitHubLogin: r.GitHubLogin, DisplayName: r.DisplayName,
			Channel: r.Channel, Destination: r.Destination}
		if r.NotifyOn != "" {
			_ = json.Unmarshal([]byte(r.NotifyOn), &c.NotifyOn)
		}
		out = append(out, c)
	}
	return out, nil
}
