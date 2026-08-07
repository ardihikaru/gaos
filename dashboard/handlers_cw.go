package main

import (
	"net/http"
	"strings"
)

// The create-workflow wizard: objective, scope, plan.
//
// THE DRAFT LIVES IN THE FORM, NOT IN A SESSION. Every step posts the whole
// draft back as hidden inputs and gets the next step's markup in return. That
// is fewer moving parts than a server-side session map, it survives a restart
// and a redeploy, two operators can run the wizard at once without a keyspace,
// and it is the shape HTMX is built around. The cost is a slightly larger form
// body, which for a handful of strings is nothing.

func (s *server) handleCWOpen(w http.ResponseWriter, r *http.Request) {
	v, _ := s.newView(r)
	d := WorkflowDraft{Step: 1, ProductID: r.URL.Query().Get("product"), Priority: "high", Gate: true}
	cw, err := s.buildCW(r, d)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	v.CW = cw
	s.rend.Render(w, http.StatusOK, "cw-modal", v)
}

// handleCWStep advances (or retreats) one step. The {n} in the path is the step
// being REQUESTED, so Back and Continue are the same handler with a different
// number, and neither has to know which direction it is going.
func (s *server) handleCWStep(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	want := atoiDefault(r.PathValue("n"), 1)
	d := draftFromForm(r)

	// Validation happens on the way FORWARD only. Going back to fix something
	// must never be blocked by the thing you went back to fix.
	if want > d.Step {
		if d.ProductID == "" {
			s.toast(w, r, "Pick the product this work belongs to.")
			return
		}
		if want > 1 && strings.TrimSpace(d.Goal) == "" {
			s.toast(w, r, "Write the objective in one sentence.")
			return
		}
	}
	if want < 1 {
		want = 1
	}
	if want > 3 {
		want = 3
	}
	d.Step = want

	v, _ := s.newView(r)
	cw, err := s.buildCW(r, d)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	v.CW = cw
	s.rend.Render(w, http.StatusOK, "cw-modal", v)
}

func (s *server) handleWorkflowCreate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	d := draftFromForm(r)
	plan, err := s.store.PlanWorkflow(r.Context(), d)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	res, err := s.store.CreateWorkflow(r.Context(), d, plan)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	v := s.applyResult(r, res)
	// The modal closes itself: the response replaces it with an empty shell and
	// swaps the new floor in out of band.
	s.rend.RenderWithChrome(w, "cw-closed", v)
}

// draftFromForm reads the round-tripped draft. Story selection arrives as
// repeated `stories` values, which is what a checkbox group posts.
func draftFromForm(r *http.Request) WorkflowDraft {
	d := WorkflowDraft{
		Step:      atoiDefault(r.FormValue("step"), 1),
		ProductID: r.FormValue("product"),
		Goal:      strings.TrimSpace(r.FormValue("goal")),
		Priority:  def(r.FormValue("priority"), "high"),
		Gate:      r.FormValue("gate") != "no",
	}
	if r.Form != nil {
		d.Stories = r.Form["stories"]
	}
	return d
}

func (s *server) buildCW(r *http.Request, d WorkflowDraft) (*CWView, error) {
	masters, _ := s.store.Masters(r.Context())
	products, _ := s.store.Products(r.Context())
	name := func(id string) string {
		for _, p := range products {
			if p.ID == id {
				return p.Name
			}
		}
		return id
	}

	cw := &CWView{Draft: d, Steps: cwSteps(d.Step), Goal: d.Goal}
	for _, m := range masters {
		cw.Picks = append(cw.Picks, PickView{
			ID: m.ID, Name: name(m.ID), Stories: len(m.Stories),
			Coverage: m.Coverage(), Selected: m.ID == d.ProductID,
		})
	}

	if d.ProductID != "" {
		m, err := s.store.Master(r.Context(), d.ProductID)
		if err != nil {
			return nil, err
		}
		cw.Master = &m
		picked := map[string]bool{}
		for _, id := range d.Stories {
			picked[id] = true
		}
		for _, st := range m.Stories {
			cw.Stories = append(cw.Stories, StoryPick{Story: st, Selected: picked[st.ID]})
		}
		if cw.Goal == "" {
			for _, p := range products {
				if p.ID == d.ProductID {
					cw.Goal = p.Goal
				}
			}
		}
	}

	switch d.Step {
	case 1:
		cw.Hint = "The product record supplies the context the planner reads."
		cw.NextLabel = "Continue"
	case 2:
		cw.Hint = "Each selected story becomes an Issue the tasks are planned against."
		cw.NextLabel = "Build the plan"
	default:
		plan, err := s.store.PlanWorkflow(r.Context(), d)
		if err != nil {
			return nil, err
		}
		cw.Plan = plan
		for _, t := range plan {
			if t.Gate {
				cw.PlanGates++
			}
			if t.Dep != "" {
				cw.PlanDeps++
			}
		}
		cw.NextLabel = "Create workflow"
		cw.Hint = itoa(len(plan)) + " tasks · " + itoa(cw.PlanGates) + " approval gate"
	}
	return cw, nil
}

// toast answers a rejected step without moving the wizard: the modal stays
// where it is, and the operator gets told why.
func (s *server) toast(w http.ResponseWriter, r *http.Request, msg string) {
	v, _ := s.newView(r)
	v.Toast = msg
	w.WriteHeader(http.StatusUnprocessableEntity)
	s.rend.RenderWithChrome(w, "empty", v)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
