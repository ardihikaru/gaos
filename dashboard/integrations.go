package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// The back office's Integrations view (D-085, docs/designs/open-source-docs.md
// §3.6): what GAOS is connected to and whether each connection works.
//
// It is read-only and it renders no secret. Every value below is a host, a
// mode, a count, a time or a yes/no built here from configuration GAOS already
// holds; none is a token, a token digest, a credential, a URL's userinfo, path
// or query, a run event's text, or an error ai-manager wrote. It never calls
// ai-manager: the provider list is read from the vendors cache without waiting.

// noStoreView marks the views whose pages must not be cached: the archive
// (archived conversations, D-082) and the integrations (topology, D-085). Both
// view routes call it, so the sheet route cannot be the way around it.
func noStoreView(w http.ResponseWriter, view string) {
	if view == "archive" || view == "integrations" || view == "autofix" || view == "prs" {
		w.Header().Set("Cache-Control", "no-store")
	}
}

var hostNamePattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)

// urlHost is scheme://host[:port] of an http or https URL, or "" -- never the
// userinfo, path or query. It refuses anything url.Parse could have misread: a
// password holding an unescaped '/' or '@' puts part of the userinfo where the
// host should be, and that host then fails the pattern.
func urlHost(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	h := u.Hostname()
	if h == "" || (net.ParseIP(h) == nil && !hostNamePattern.MatchString(h)) {
		return ""
	}
	if p := u.Port(); p != "" {
		for _, r := range p {
			if r < '0' || r > '9' {
				return ""
			}
		}
		return u.Scheme + "://" + net.JoinHostPort(h, p)
	}
	if strings.Contains(h, ":") { // an IPv6 literal
		return u.Scheme + "://[" + h + "]"
	}
	return u.Scheme + "://" + h
}

func agoAt(at int64, now time.Time) string {
	if at <= 0 {
		return ""
	}
	return relSpan(now.Sub(time.Unix(at, 0))) + " ago"
}

// buildIntegrations fills the Integrations view.
func (s *server) buildIntegrations(ctx context.Context, bo *BOView) {
	bo.Title = "Integrations"
	bo.Sub = "What GAOS is connected to, and whether each connection works. No secret is shown; the contact list below is the one place it can be edited."
	now := time.Now()
	p := &IntegrationsPage{}

	products, _ := s.store.Products(ctx)
	name := func(id string) string {
		for _, pr := range products {
			if pr.ID == id {
				return pr.Name
			}
		}
		return id
	}

	// ---- ai-manager ----
	p.AIM.Adapter = s.describeAdapter()
	p.AIM.Runner = runnerMode()
	p.AIM.BootCheck = "not run (no gateway)"
	if a, ok := s.adapter.(*AIMAdapter); ok && a != nil {
		p.AIM.Configured = true
		if a.Client != nil {
			p.AIM.Host = urlHost(a.Client.Base)
		}
		if !s.bootChecked.IsZero() {
			p.AIM.BootCheck = "passed at boot, " + relSpan(now.Sub(s.bootChecked)) + " ago"
		} else {
			p.AIM.BootCheck = "passed at boot"
		}
		infos, at, busy := a.CachedVendorInfo()
		switch {
		case busy:
			p.AIM.ProviderNote = "a provider check is in flight"
		case infos == nil:
			p.AIM.ProviderNote = "not asked since boot — a product's AI vendors tab asks"
		default:
			for _, in := range infos {
				st := "not available"
				if in.Available {
					st = "available"
				}
				p.AIM.Providers = append(p.AIM.Providers, in.Name+" ("+st+")")
			}
			p.AIM.ProviderNote = "as of " + relSpan(now.Sub(at)) + " ago"
		}
	}

	// ---- the event scan: last turn, last delivery, given up ----
	// Store.Events carries no payloads, so this reads only what GAOS itself
	// wrote: the type, the product, the time, and a delivery's own sentence
	// ("QA tool answer: attempt 2 retry"). gave-up is terminal for an item --
	// the worker never tries it again -- so counting those events counts the
	// items whose decision never reached the QA tools.
	if evs, err := s.store.Events(ctx); err == nil {
		for i := len(evs) - 1; i >= 0; i-- {
			e := evs[i]
			switch e.Type {
			case RunStarted, RunExited:
				if p.AIM.LastTurn == "" {
					p.AIM.LastTurn = strings.Join(nonEmpty(string(e.Type), name(e.DeskID), agoAt(e.At, now)), " · ")
				}
			case ExternalAnswerDelivery:
				if strings.HasSuffix(e.Text, " "+DeliveryGaveUp) {
					p.QA.GaveUp++
					if e.At >= now.Add(-24*time.Hour).Unix() {
						p.QA.GaveUpRecent++
					}
				}
				if p.QA.LastDelivery == "" {
					p.QA.LastDelivery = strings.Join(nonEmpty(agoAt(e.At, now), e.Text), " · ")
				}
			}
		}
	}

	// ---- QA tools ----
	p.QA.OutboundWhy = "GAOS_QA_URL is not set"
	if o := s.qaOut; o != nil {
		if o.why == "" && o.base != nil {
			p.QA.Outbound = true
			p.QA.OutboundHost = o.base.Scheme + "://" + o.base.Host
			p.QA.OutboundWhy = ""
		} else if o.why != "" {
			p.QA.OutboundWhy = o.why
		}
	}
	if pend, err := s.store.ExternalPending(ctx); err == nil {
		p.QA.Pending = len(pend)
		// The newest attempt among the waiting items, with GAOS's own reason
		// (an HTTP status, or "could not be reached" and an address; never a
		// URL path or a body -- qaOutbound.do strips both).
		var last *ExternalItem
		for i := range pend {
			if pend[i].Delivery.Attempts > 0 && (last == nil || pend[i].Delivery.LastAt > last.Delivery.LastAt) {
				last = &pend[i]
			}
		}
		p.QA.Failing = last != nil && last.Delivery.LastError != ""
		if last != nil {
			p.QA.PendingNote = strings.Join(nonEmpty(fmt.Sprintf("item %s", last.ExternalID),
				fmt.Sprintf("attempt %d", last.Delivery.Attempts), agoAt(last.Delivery.LastAt, now), last.Delivery.LastError), " · ")
		}
	}

	// ---- the machine API and its clients ----
	p.API.State = "disabled (" + whyNoToken + ")"
	if c := s.api; c != nil {
		p.API.State = c.Describe()
		p.API.Admin, p.API.QA, p.API.Outbound = c.hasAdmin, c.hasQA, c.hasOut
		p.API.QAProducts = sortedKeys(c.qaProducts)
		if qc := c.qaClient(); qc != nil {
			p.QA.Origin = qc.Origin
		}
		for _, cl := range c.clients {
			scope := "inbox"
			if cl.scope == scopeQA {
				scope = "qa"
			}
			p.Clients = append(p.Clients, ClientRow{Name: cl.Name, Kind: cl.Kind, Origin: cl.Origin,
				Scope: scope, Products: sortedKeys(cl.products)})
		}
	}
	if s.brain != nil {
		p.BrainTown = s.brain.Status()
	} else {
		p.BrainTown = s.brainOff
	}
	if s.btConsumer != nil {
		p.BrainTownConsumer = s.btConsumer.Status()
	} else {
		p.BrainTownConsumer = s.btConsumerOff
	}
	p.PR = s.prFlowRow(ctx, products, now)
	integChips(p)
	bo.Integrations = p
}

// The chip tones (D-098). Green is working, red is not, amber is working with
// a failure GAOS has seen or a part that is broken.
const (
	toneOK   = "ok"
	toneWarn = "warn"
	toneBad  = "bad"
)

// integChips derives every status chip from the page's own fields. Pure: no
// server, no clock, no I/O. The rules and the reasons for them are in
// docs/designs/integrations-status-chips.md §3.2; every Label is one of the
// closed set chipLabels lists.
func integChips(p *IntegrationsPage) {
	chip := func(label, tone string) StatusChip { return StatusChip{Label: label, Tone: tone} }

	if p.AIM.Configured {
		p.AIMChip = chip("Connected", toneOK)
	} else {
		p.AIMChip = chip("Off", toneBad)
	}

	bt := func(r BrainTownRow) StatusChip {
		switch {
		case r.State == "enabled" && r.Failing:
			return chip("Degraded", toneWarn)
		case r.State == "enabled":
			return chip("Enabled", toneOK)
		case strings.HasPrefix(r.State, "unusable"):
			return chip("Unusable", toneBad)
		}
		return chip("Disabled", toneBad)
	}
	p.BrainTownChip = bt(p.BrainTown)
	p.BrainTownConsumerChip = bt(p.BrainTownConsumer)

	switch {
	case !p.QA.Outbound:
		p.QAChip = chip("Outbound off", toneBad)
	case p.QA.GaveUpRecent > 0 || p.QA.Failing:
		p.QAChip = chip("Degraded", toneWarn)
	default:
		p.QAChip = chip("Connected", toneOK)
	}

	switch {
	case strings.HasPrefix(p.API.State, "enabled") && (!p.API.QA || len(p.API.QAProducts) == 0):
		p.APIChip = chip("Degraded", toneWarn)
	case strings.HasPrefix(p.API.State, "enabled"):
		p.APIChip = chip("Enabled", toneOK)
	default:
		p.APIChip = chip("Disabled", toneBad)
	}

	if p.PR.GitHub == "on" {
		p.PR.GitHubChip = chip("On", toneOK)
	} else {
		p.PR.GitHubChip = chip("Off", toneBad)
	}
	switch {
	case p.PR.StagingState == "enabled":
		p.PR.StagingChip = chip("Enabled", toneOK)
	case strings.HasPrefix(p.PR.StagingState, "unusable"):
		p.PR.StagingChip = chip("Unusable", toneBad)
	default:
		p.PR.StagingChip = chip("Disabled", toneBad)
	}
	switch {
	case p.PR.WA.State == "enabled":
		p.PR.WAChip = chip("Enabled", toneOK)
	case strings.HasPrefix(p.PR.WA.State, "enabled,"): // unencrypted, or linked to no product
		p.PR.WAChip = chip("Degraded", toneWarn)
	default:
		p.PR.WAChip = chip("Disabled", toneBad)
	}

	// The roll-up. The review needs the GitHub client, and pull-request
	// events reach GAOS only through the Brain Town reader (main.go's Drain).
	// A sub-part that is only not configured is a choice, not a fault.
	broken := func(c StatusChip) bool { return c.Label == "Unusable" || c.Label == "Degraded" }
	switch {
	case p.PR.GitHubChip.Tone != toneOK:
		p.PRChip = chip("Off", toneBad)
	case p.BrainTownConsumerChip.Tone != toneOK:
		p.PRChip = chip("Partly connected", toneWarn)
	case broken(p.PR.StagingChip) || broken(p.PR.WAChip):
		p.PRChip = chip("Partly connected", toneWarn)
	default:
		p.PRChip = chip("Connected", toneOK)
	}
}

// chipLabels is the closed set of chip labels; a test holds the page to it.
var chipLabels = map[string]bool{"Connected": true, "Off": true, "Enabled": true, "Unusable": true,
	"Disabled": true, "Outbound off": true, "Degraded": true, "On": true, "Partly connected": true}

// prFlowRow is the pull-request flow's rows (D-088). A destination is masked
// here, so no template can render one whole.
func (s *server) prFlowRow(ctx context.Context, products []Product, now time.Time) PRFlowRow {
	r := PRFlowRow{GitHub: s.ghState, WA: s.wa.Status(), WriteToken: tokenKindLabel(s.writeTokenKind)}
	if r.GitHub == "" {
		r.GitHub = "off"
	}
	switch {
	case s.staging.enabled():
		r.StagingState = "enabled"
	case s.staging != nil && s.staging.why != "":
		r.StagingState = "unusable: " + s.staging.why
	default:
		r.StagingState = "disabled"
	}
	for _, p := range products {
		if at, ok := s.staging.LastCallback(p.ID); ok {
			r.StagingLast = append(r.StagingLast, StagingSeen{Product: p.Name, At: relSpan(now.Sub(at)) + " ago"})
		}
	}
	if cs, err := s.store.ListContacts(ctx); err == nil {
		for _, c := range cs {
			r.Contacts = append(r.Contacts, ContactRow{ProductID: c.ProductID, Login: c.GitHubLogin, Name: c.DisplayName,
				Destination: maskDestination(c.Destination), NotifyOn: strings.Join(c.NotifyOn, ", ")})
		}
	}
	return r
}

func nonEmpty(xs ...string) []string {
	out := xs[:0:0]
	for _, x := range xs {
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
