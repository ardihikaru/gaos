package main

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"time"
)

// The QA tab and its two sync buttons (D-072, D-075, design §4.10).
//
// The routes sit behind the back office's verified session (auth.go, BR-55):
// only a signed-in operator can press them. They are also bounded -- one
// configured origin, only products listed in GAOS_API_QA_PRODUCTS, one sync at
// a time and one start per 30 s. What a sync can do is what an inbound results post can do:
// change automation facts, never a decision and never a task (BR-54).

// ---------- the view model ----------

// QAPane is everything the QA tab prints. Every string that came from the QA
// tools (Note, Rejected, a case's spec or runRef) is plain text; the template
// prints it through html/template's auto-escaping and never as template.HTML.
type QAPane struct {
	Has           bool // the product has a catalog
	Totals        QaTotals
	CovStyle      template.CSS
	Epics         []QAEpicRow
	Regressed     []string
	NeedsDecision []string
	NeedsMore     int
	Sync          QASyncView
	Pager         Pager // the epic table's pages (D-097)
}

type QAEpicRow struct {
	Code, Title, Service                                 string
	Total, Automated, Planned, Blocked, ManualOnly, Wont int
	Pass, Fail                                           int
	Pct                                                  int
	PctStyle                                             template.CSS
	Regressed                                            bool
}

// QASyncView is the two buttons and the two last-sync lines.
type QASyncView struct {
	Enabled bool
	Why     string // why it is not enabled, as visible text
	Pull    QASyncLine
	Push    QASyncLine
	PullURL string
	PushURL string
}

type QASyncLine struct {
	Never     bool
	OK        bool
	When      string // "4m ago"
	WhenISO   string
	WhenAbs   string
	Note      string
	Items     int
	Rejected  []string
	RejectedN int
}

func qaLine(side QaSyncSide) QASyncLine {
	if side.At == 0 {
		return QASyncLine{Never: true}
	}
	at := time.Unix(side.At, 0).UTC()
	return QASyncLine{OK: side.OK, When: relSpan(time.Since(at)) + " ago", WhenISO: at.Format(time.RFC3339),
		WhenAbs: at.Format("2006-01-02 15:04 UTC"), Note: side.Note, Items: side.Items,
		Rejected: side.Rejected, RejectedN: side.RejectedN}
}

// qaSyncGate says whether the buttons work for this product, and if not, why.
func (s *server) qaSyncGate(productID string) (ok bool, why string) {
	switch {
	case s.qaOut == nil || s.qaOut.why != "":
		w := "the QA tools are not connected"
		if s.qaOut != nil && s.qaOut.why != "" {
			w += " (" + s.qaOut.why + ")"
		}
		return false, w
	case s.api == nil || !s.api.qaProducts[productID]:
		return false, "this product is not listed in GAOS_API_QA_PRODUCTS, so it may not exchange data with the QA tools"
	}
	return true, ""
}

func (s *server) buildQAPane(ctx context.Context, m Master) *QAPane {
	pane := &QAPane{Sync: QASyncView{
		PullURL: "products/" + m.ID + "/qa/sync/pull", PushURL: "products/" + m.ID + "/qa/sync/push",
		Pull: qaLine(m.Qa.Pull), Push: qaLine(m.Qa.Push),
	}}
	pane.Sync.Enabled, pane.Sync.Why = s.qaSyncGate(m.ID)
	if f, err := s.store.Snapshot(ctx); err == nil && f.Stats.Replay && pane.Sync.Enabled {
		pane.Sync.Enabled, pane.Sync.Why = false, "the floor is in replay; return to live first"
	}
	sum, err := s.store.QaSummary(ctx, m.ID)
	if err != nil {
		return pane
	}
	pane.Has = sum.Totals.Cases > 0
	pane.Totals = sum.Totals
	pane.CovStyle = pctStyle("width", float64(sum.Totals.CoveragePct))
	pane.Regressed, pane.NeedsDecision, pane.NeedsMore = sum.Regressed, sum.NeedsDecision, sum.NeedsMore
	for _, e := range sum.Epics {
		pane.Epics = append(pane.Epics, QAEpicRow{Code: e.Code, Title: e.Title, Service: e.Service,
			Total: e.Total, Automated: e.Automated, Planned: e.Planned, Blocked: e.Blocked, ManualOnly: e.ManualOnly,
			Wont: e.Wont, Pass: e.Pass, Fail: e.Fail, Pct: e.CoveragePct,
			PctStyle: pctStyle("width", float64(e.CoveragePct)), Regressed: e.Regressed})
	}
	return pane
}

// ---------- the two routes ----------

// qaRefuse answers a refused click with a toast and the status the design
// names (409), the same way fail does for a rejection.
func (s *server) qaRefuse(w http.ResponseWriter, r *http.Request, retry time.Duration, why string) {
	v, _ := s.newView(r)
	v.Toast = why
	if retry > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
	}
	w.WriteHeader(http.StatusConflict)
	s.rend.RenderWithChrome(w, "empty", v)
}

func (s *server) handleQaPull(w http.ResponseWriter, r *http.Request) { s.qaSync(w, r, "pull") }
func (s *server) handleQaPush(w http.ResponseWriter, r *http.Request) { s.qaSync(w, r, "push") }

func (s *server) qaSync(w http.ResponseWriter, r *http.Request, dir string) {
	if !s.requireBO(w, r) {
		return
	}
	ctx := r.Context()
	id := r.PathValue("id")
	var prod *Product
	if ps, err := s.store.Products(ctx); err == nil {
		for i := range ps {
			if ps[i].ID == id {
				prod = &ps[i]
			}
		}
	}
	if prod == nil {
		s.fail(w, r, ErrNotFound)
		return
	}
	if ok, why := s.qaSyncGate(id); !ok {
		s.qaRefuse(w, r, 0, "Cannot sync: "+why+".")
		return
	}
	if f, err := s.store.Snapshot(ctx); err == nil && f.Stats.Replay {
		s.qaRefuse(w, r, 0, replayRefusal)
		return
	}
	release, code, reason, wait := s.qaOut.begin()
	if release == nil {
		_ = code
		s.qaRefuse(w, r, wait, "Cannot sync: "+reason+".")
		return
	}
	defer release()

	st := s.mut(r)
	m, err := s.store.Master(ctx, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var side QaSyncSide
	if dir == "pull" {
		side = s.qaOut.qaPull(ctx, st, id, m.Qa.Pull)
	} else {
		side = s.qaOut.qaPush(ctx, st, *prod, m.Qa.Push)
	}
	res, err := st.QaRecordSync(ctx, id, dir, side)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	verdict := "finished"
	if !side.OK {
		verdict = "failed"
	}
	res.Toast = fmt.Sprintf("QA %s %s: %s.", dir, verdict, side.Note)
	s.renderBOAfter(w, r, res, id, "qa")
}
