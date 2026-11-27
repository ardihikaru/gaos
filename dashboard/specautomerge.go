package main

import (
	"context"
	"net/http"
)

// GET /api/v1/products/{id}/spec-auto-merge (D-103 §4.1): the QA tool asks,
// right before it turns on GitHub auto-merge for an approved spec, whether the
// product still allows it. The answer is read at that moment: a switch turned
// off, or a band that dropped while the spec waited for approval, stops the
// merge. GAOS itself never merges.
func (s *server) apiSpecAutoMerge(w http.ResponseWriter, r *http.Request) {
	scope, cl, ok := s.apiGate(w, r, scopeQA)
	if !ok {
		return
	}
	id, ok := s.apiProduct(w, r, scope, cl)
	if !ok {
		return
	}
	m, err := s.store.Master(r.Context(), id)
	if err != nil {
		apiStoreErr(w, err)
		return
	}
	apiJSON(w, http.StatusOK, map[string]bool{"allowed": specAutoMergeAllowed(m)})
}

// storeAutoMerge is the runner's view of the same rule, for the create request.
func storeAutoMerge(st Store) func(context.Context, string) bool {
	return func(ctx context.Context, id string) bool {
		m, err := st.Master(ctx, id)
		return err == nil && specAutoMergeAllowed(m)
	}
}
