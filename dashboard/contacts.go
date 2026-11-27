package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
)

// The master contact registry (D-088, docs/designs/gaos-pr-agent-flows.md §8.4).
//
// A contact maps a GitHub account to a WhatsApp destination, so a finding can
// reach the person who can act on it.
//
// This is the ONE place personal data lives in GAOS, and the rules are
// therefore the strict ones:
//
//   - It never reaches an event. An event is read by the town and by the
//     boards; a phone number must not be in one.
//   - It never reaches Brain Town. The consumer stream carries the repository
//     and the pull request, never a person.
//   - It never reaches the Blueprint. The registry is not part of Master, so
//     it is never rendered into a workspace file and never reaches a model.
//   - It is never rendered whole. The back office shows the destination
//     masked; the Integrations view shows a count.

// Contact is one person's destination.
type Contact struct {
	// ProductID is the product the contact belongs to, or empty for every
	// product.
	ProductID string `json:"productId"`
	// GitHubLogin is the account the contact is found by.
	GitHubLogin string `json:"githubLogin"`
	// DisplayName is the person's name. It is shown to the operator, never to
	// a model.
	DisplayName string `json:"displayName"`
	// Channel is "whatsapp".
	Channel string `json:"channel"`
	// Destination is the group jid or the number. It is the sensitive field:
	// masked everywhere it is rendered.
	Destination string `json:"destination"`
	// NotifyOn is what reaches this person: review:medium, review:high,
	// e2e:failure, unit:failure. A review value includes the worse severities.
	// Empty means everything.
	NotifyOn []string `json:"notifyOn"`
}

// maskDestination is how a destination is shown to a human: enough to tell two
// contacts apart, not enough to use.
func maskDestination(d string) string {
	d = strings.TrimSpace(d)
	if d == "" {
		return ""
	}
	if i := strings.Index(d, "@"); i > 0 {
		return "…" + d[i:]
	}
	if len(d) <= 4 {
		return strings.Repeat("*", len(d))
	}
	return d[:2] + "…" + d[len(d)-2:]
}

// notifyKinds is the closed vocabulary of NotifyOn.
var notifyKinds = map[string]bool{"review:medium": true, "review:high": true, "e2e:failure": true, "unit:failure": true,
	// D-101, D-100: a person must review; a pull request names no task-manager code.
	KindReviewManual: true, KindReviewNeedsManual: true, "pr:" + alertNoTaskCode: true}

// The two manual-review notice kinds (D-101). They match a contact exactly, never by rank.
const (
	KindReviewManual      = "review:manual"
	KindReviewNeedsManual = "review:needs-manual"
)

// Alias is the name a log line and an event use for the contact: the display
// name as a slug, or the account. Never the destination.
func (c Contact) Alias() string {
	if n := sanitizeIDPart(c.DisplayName); strings.TrimSpace(c.DisplayName) != "" {
		return n
	}
	return sanitizeIDPart(c.GitHubLogin)
}

// contactID is the vertex id: product and account, lower-cased.
func contactID(c Contact) string { return contactKey(c) }

// ValidContact refuses a contact that would not work.
func ValidContact(c Contact) error {
	for _, k := range c.NotifyOn {
		if !notifyKinds[strings.TrimSpace(k)] {
			return fmt.Errorf("notifyOn %q is not one of review:medium, review:high, e2e:failure, unit:failure", k)
		}
	}
	if strings.TrimSpace(c.GitHubLogin) == "" {
		return fmt.Errorf("a contact needs a GitHub account")
	}
	if strings.TrimSpace(c.Destination) == "" {
		return fmt.Errorf("a contact needs a destination")
	}
	if c.Channel != "" && c.Channel != "whatsapp" {
		return fmt.Errorf("a contact's channel is whatsapp")
	}
	return nil
}

// contactKey is a contact's identity: the product and the account.
func contactKey(c Contact) string {
	return strings.ToLower(strings.TrimSpace(c.ProductID)) + "|" + strings.ToLower(strings.TrimSpace(c.GitHubLogin))
}

// ContactBook is the registry.
type ContactBook struct {
	contacts []Contact
}

// Add records a contact, replacing one with the same product and account.
func (b *ContactBook) Add(c Contact) error {
	if err := ValidContact(c); err != nil {
		return err
	}
	c.ProductID = strings.TrimSpace(c.ProductID)
	c.GitHubLogin = strings.TrimSpace(c.GitHubLogin)
	c.DisplayName = strings.TrimSpace(c.DisplayName)
	if c.Channel == "" {
		c.Channel = "whatsapp"
	}
	key := contactKey(c)
	for i := range b.contacts {
		if contactKey(b.contacts[i]) == key {
			b.contacts[i] = c
			return nil
		}
	}
	b.contacts = append(b.contacts, c)
	return nil
}

// Remove drops a contact.
func (b *ContactBook) Remove(productID, githubLogin string) bool {
	key := strings.ToLower(strings.TrimSpace(productID)) + "|" + strings.ToLower(strings.TrimSpace(githubLogin))
	for i := range b.contacts {
		if contactKey(b.contacts[i]) == key {
			b.contacts = append(b.contacts[:i], b.contacts[i+1:]...)
			return true
		}
	}
	return false
}

// List is the registry, sorted, with every destination masked.
func (b *ContactBook) List() []Contact {
	out := make([]Contact, 0, len(b.contacts))
	for _, c := range b.contacts {
		c.Destination = maskDestination(c.Destination)
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ProductID != out[j].ProductID {
			return out[i].ProductID < out[j].ProductID
		}
		return out[i].GitHubLogin < out[j].GitHubLogin
	})
	return out
}

// Find returns the contact for a GitHub account, preferring the product's own
// contact over the one that applies to every product.
func (b *ContactBook) Find(productID, githubLogin string) (Contact, bool) {
	login := strings.ToLower(strings.TrimSpace(githubLogin))
	var fallback Contact
	haveFallback := false
	for _, c := range b.contacts {
		if !strings.EqualFold(c.GitHubLogin, login) {
			continue
		}
		if c.ProductID != "" && c.ProductID == productID {
			return c, true
		}
		if c.ProductID == "" && !haveFallback {
			fallback, haveFallback = c, true
		}
	}
	return fallback, haveFallback
}

// Count is how many contacts the registry holds. The Integrations view shows
// the count and never the contacts.
func (b *ContactBook) Count() int { return len(b.contacts) }

// Clone deep-copies the registry.
func (b *ContactBook) Clone() ContactBook {
	return ContactBook{contacts: append([]Contact(nil), b.contacts...)}
}

// notifyOnWants reports whether a contact wants this kind of notification. A
// kind is "review:<severity>", "e2e:failure" or "unit:failure"; a contact that
// lists review:medium also wants review:high and review:critical.
func notifyOnWants(c Contact, kind string) bool {
	if len(c.NotifyOn) == 0 {
		return true
	}
	sev := ""
	if strings.HasPrefix(kind, "review:") && kind != KindReviewManual && kind != KindReviewNeedsManual {
		sev = strings.TrimPrefix(kind, "review:")
	}
	for _, k := range c.NotifyOn {
		k = strings.TrimSpace(k)
		if strings.EqualFold(k, kind) {
			return true
		}
		if sev != "" && strings.HasPrefix(k, "review:") {
			if prSeverityRank(sev) >= prSeverityRank(strings.TrimPrefix(k, "review:")) {
				return true
			}
		}
	}
	return false
}

// resolveDestination is §8.4's resolution: the contact for the account when it
// exists and wants this kind, else the pinned group (an empty destination).
// The alias is for logs and events.
func resolveDestination(book ContactBook, productID, login, kind string) (destination, alias string) {
	if c, ok := book.Find(productID, login); ok && notifyOnWants(c, kind) {
		return c.Destination, c.Alias()
	}
	return "", "group"
}

// ContactOp is one change to the registry, carried on a Result so ArcadeStore
// writes it in the same transaction (D-088). There is no event: the registry
// is deliberately outside the stream.
type ContactOp struct {
	Contact Contact
	Delete  bool
}

// ListContacts is the registry, with real destinations. The notifier needs
// them; every renderer masks them.
func (s *MemStore) ListContacts(ctx context.Context) ([]Contact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := append([]Contact(nil), s.contacts.contacts...)
	sort.Slice(out, func(i, j int) bool { return contactKey(out[i]) < contactKey(out[j]) })
	return out, nil
}

// UpsertContact adds or replaces a contact.
func (s *MemStore) UpsertContact(ctx context.Context, c Contact, actor string) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.contacts.Add(c); err != nil {
		return Result{}, reject(err.Error())
	}
	stored, _ := s.contacts.findExact(c.ProductID, c.GitHubLogin)
	log.Printf("contact: upsert %s by %s", stored.Alias(), actorAlias(actor))
	return Result{Contacts: []ContactOp{{Contact: stored}}, Toast: "Contact saved."}, nil
}

// DeleteContact removes a contact.
func (s *MemStore) DeleteContact(ctx context.Context, productID, login, actor string) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.contacts.findExact(productID, login)
	if !ok || !s.contacts.Remove(productID, login) {
		return Result{}, ErrNotFound
	}
	log.Printf("contact: delete %s by %s", c.Alias(), actorAlias(actor))
	return Result{Contacts: []ContactOp{{Contact: c, Delete: true}}, Toast: "Contact removed."}, nil
}

// RecordNotification is the one event a notification leaves: the alias, the
// kind and the outcome, never the destination and never the body.
func (s *MemStore) RecordNotification(ctx context.Context, productID, alias, kind, outcome string) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.deskIdxByID(productID)
	if idx < 0 {
		return Result{}, ErrNotFound
	}
	p := deskPayload(s.desks[idx])
	p.Actor, p.Rule, p.Reason = alias, kind, outcome
	k := KindInfo
	if outcome != "accepted" {
		k = KindWarn
	}
	ev := s.emit(ContactNotified, productID, "notified "+alias+" ("+kind+"): "+outcome, k, p)
	return Result{NewEvents: []Event{ev}}, nil
}

// findExact is the contact for exactly this product and account, with no
// fallback to the every-product contact.
func (b *ContactBook) findExact(productID, login string) (Contact, bool) {
	key := strings.ToLower(strings.TrimSpace(productID)) + "|" + strings.ToLower(strings.TrimSpace(login))
	for _, c := range b.contacts {
		if contactKey(c) == key {
			return c, true
		}
	}
	return Contact{}, false
}

// restoreContacts loads the registry from the graph at boot.
func (s *MemStore) restoreContacts(cs []Contact) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range cs {
		_ = s.contacts.Add(c)
	}
}

// notifyOnJSON and notifyOnFrom are the property's encoding.
func notifyOnJSON(n []string) string {
	if len(n) == 0 {
		return ""
	}
	b, _ := json.Marshal(n)
	return string(b)
}
