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
	if view == "archive" || view == "integrations" {
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
	bo.Sub = "What GAOS is connected to, and whether each connection works. Read-only; no secret is shown."
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
	bo.Integrations = p
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
