package main

import (
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// The Agents page's product filter and the assignment route (D-102 §3.3, §3.5).

// agentsHref is the Agents page's address: the product first, the page last.
func agentsHref(base, product string, page int) string {
	q := url.Values{}
	if product != "" {
		q.Set("product", product)
	}
	if page > 1 {
		q.Set("page", strconv.Itoa(page))
	}
	if len(q) == 0 {
		return base
	}
	// Encode sorts keys, and "page" > "product" would put the page first; the
	// order is written by hand so the address reads product, then page.
	out := base + "?"
	if product != "" {
		out += "product=" + url.QueryEscape(product)
		if page > 1 {
			out += "&"
		}
	}
	if page > 1 {
		out += "page=" + strconv.Itoa(page)
	}
	return out
}

// productNameOf is a product's display name from the product list.
func productNameOf(products []Product, id string) string {
	for _, p := range products {
		if p.ID == id {
			return p.Name
		}
	}
	return id
}

// assignedProducts are the products an agent is assigned to, sorted by name.
func assignedProducts(masters []Master, products []Product, agentID string) []AgentProduct {
	var out []AgentProduct
	for _, m := range masters {
		if m.AssignedSet && containsFold(m.Assigned, agentID) {
			out = append(out, AgentProduct{ID: m.ID, Name: productNameOf(products, m.ID)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// agentAssignView validates the product filter against the product list (an
// unknown id is no filter) and, for an admin with a product chosen, builds the
// editor: every agent, the gated roles first.
func agentAssignView(r *http.Request, masters []Master, products []Product, roster []Agent, product string) AgentAssign {
	a := AgentAssign{Opts: []AFOpt{{Value: "", Label: "Every product", On: true}}}
	var m *Master
	for i := range masters {
		if masters[i].ID == product {
			m = &masters[i]
		}
	}
	if m != nil {
		a.Product, a.ProductName, a.Configured = m.ID, productNameOf(products, m.ID), m.AssignedSet
		a.None = m.AssignedSet && len(m.Assigned) == 0
		a.Opts[0].On = false
	}
	for _, p := range products {
		a.Opts = append(a.Opts, AFOpt{Value: p.ID, Label: p.Name, On: p.ID == a.Product})
	}
	if ss, ok := sessionOf(r); ok && autoFixAdmin(ss.Email) {
		a.Admin = true
	}
	if m == nil || !a.Admin {
		return a
	}
	for _, repo := range m.Repos {
		k := m.RepoKinds[strings.ToLower(repo)]
		a.Repos = append(a.Repos, RepoKindPick{Repo: repo, Kind: k})
		if k != "" {
			a.KindsSet++
		}
	}
	a.StagingCreates, a.AutoMergeSpecs = m.StagingCreates, m.AutoMergeSpecs
	b := ReadinessBand(m.ReadinessScore())
	a.AutoMergeBand, a.Band = b == "working" || b == "complete", b
	for _, ag := range roster {
		a.Picks = append(a.Picks, AgentPick{ID: ag.ID, Name: ag.Name, Label: roles[ag.Role].Label,
			On: m.AssignedSet && containsFold(m.Assigned, ag.ID), Gated: containsFold(assignGatedRoles, ag.Role)})
	}
	sort.SliceStable(a.Picks, func(i, j int) bool { return a.Picks[i].Gated && !a.Picks[j].Gated })
	return a
}

// handleAssign is POST /agents/assign: an admin saves or resets the agents
// assigned to one product (D-102 §3.3). It answers the refreshed Agents view.
func (s *server) handleAssign(w http.ResponseWriter, r *http.Request) {
	ss, ok := sessionOf(r)
	if !ok || ss.Email == "" {
		writeBoardJSON(w, http.StatusUnauthorized, map[string]string{"error": "sign in to the back office"})
		return
	}
	if !autoFixAdmin(ss.Email) {
		writeBoardJSON(w, http.StatusForbidden, map[string]string{"error": "assignments are restricted to the accounts listed for auto-fix"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		writeBoardJSON(w, http.StatusBadRequest, map[string]string{"error": "the form could not be read"})
		return
	}
	id := r.PostFormValue("product")
	if r.PostFormValue("action") == "kinds" {
		// Repository kinds and the two Release B switches (D-103 §2.1): assignment is untouched.
		kinds := map[string]string{}
		for k, v := range r.PostForm {
			if repo, ok := strings.CutPrefix(k, "kind."); ok && len(v) > 0 {
				kinds[repo] = v[0]
			}
		}
		res, err := s.mut(r).SetRepoKinds(r.Context(), id, kinds,
			r.PostFormValue("stagingCreates") == "on", r.PostFormValue("autoMergeSpecs") == "on", ss.Email)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		v := s.applyResult(r, res)
		s.sheet(w, r, v, "agents", id, "brd")
		if v.BO != nil {
			v.BO.Assign.KindsOpen = true // the admin keeps sight of the kinds just saved
		}
		s.rend.RenderWithChrome(w, "bo-main-oob", v)
		return
	}
	set := true
	ids := r.PostForm["agent"]
	switch r.PostFormValue("action") {
	case "save", "":
	case "reset":
		set, ids = false, nil // a reset ignores every ticked box
	default:
		writeBoardJSON(w, http.StatusBadRequest, map[string]string{"error": "action must be save or reset"})
		return
	}
	res, err := s.mut(r).SetAssigned(r.Context(), id, ids, set, ss.Email)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	v := s.applyResult(r, res)
	s.sheet(w, r, v, "agents", id, "brd")
	if v.BO != nil {
		v.BO.Assign.Open = true // the admin keeps sight of what was just saved
	}
	s.rend.RenderWithChrome(w, "bo-main-oob", v)
}
