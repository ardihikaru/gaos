package main

// Third-party platforms bound to one product (D-104, docs/designs/gaos-third-party-taskmgr.md
// §3). The only kind is "taskmgr". A product holds at most one binding of a kind, and an instance
// at most one ENABLED product per kind: the platform's people and tasks belong to one product.

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ThirdPartyLink is one binding.
type ThirdPartyLink struct {
	Kind        string `json:"kind"`
	Enabled     bool   `json:"enabled"`
	ProductCode string `json:"productCode"`
}

// ThirdPartyTaskMgr is the TaskMgr kind.
const ThirdPartyTaskMgr = "taskmgr"

// thirdPartyKinds is the closed set of kinds.
var thirdPartyKinds = map[string]bool{ThirdPartyTaskMgr: true}

// thirdPartyCodePattern is a platform's product code.
var thirdPartyCodePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,31}$`)

// ErrConflict is a write another record already holds (D-104: a second enabled binding).
var ErrConflict = errors.New("conflict")

// thirdPartyOf is the product's binding of a kind.
func thirdPartyOf(m Master, kind string) (ThirdPartyLink, bool) {
	for _, l := range m.ThirdParty {
		if l.Kind == kind {
			return l, true
		}
	}
	return ThirdPartyLink{}, false
}

// SetThirdParty validates and saves one binding and writes ThirdPartyConfigured.
func (s *MemStore) SetThirdParty(ctx context.Context, productID string, link ThirdPartyLink, actor string) (Result, error) {
	link.Kind = strings.TrimSpace(link.Kind)
	link.ProductCode = strings.TrimSpace(link.ProductCode)
	if !thirdPartyKinds[link.Kind] {
		return Result{}, reject("the kind is taskmgr")
	}
	if !thirdPartyCodePattern.MatchString(link.ProductCode) {
		return Result{}, reject("the product code is 1 to 32 of A-Z a-z 0-9 _ - and starts with a letter or a digit")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.masterPtr(productID)
	if m == nil {
		return Result{}, ErrNotFound
	}
	if link.Enabled {
		for i := range s.masters {
			o := &s.masters[i]
			if o.ID == productID {
				continue
			}
			if l, ok := thirdPartyOf(*o, link.Kind); ok && l.Enabled {
				return Result{}, fmt.Errorf("%w: product %s already holds the %s binding", ErrConflict, o.ID, link.Kind)
			}
		}
	}
	var out []ThirdPartyLink
	for _, l := range m.ThirdParty {
		if l.Kind != link.Kind {
			out = append(out, l)
		}
	}
	out = append(out, link)
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	m.ThirdParty = out
	alias := actorAlias(actor)
	p := EventPayload{Actor: alias, Rule: "third-party-config",
		Reason: fmt.Sprintf("kind=%s enabled=%t code=%s", link.Kind, link.Enabled, link.ProductCode)}
	ev := s.emit(ThirdPartyConfigured, productID, s.productName(productID)+": "+link.Kind+" binding saved by "+alias, KindInfo, p)
	c := m.Clone()
	toast := "TaskMgr binding saved."
	if !link.Enabled {
		toast = "TaskMgr binding disabled."
	}
	return Result{Master: &c, NewEvents: []Event{ev}, Toast: toast}, nil
}
