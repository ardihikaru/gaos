package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// MemStore's Blueprint mutations. The domain rules live here once; ArcadeStore
// runs them on its projection and persists the result, as for every other
// record mutation (store_arcade.go).

// SavePolicy adds a policy, or replaces the one with the same key. It changes
// the DRAFT only: a published version is never touched (TC-287).
func (s *MemStore) SavePolicy(ctx context.Context, id string, p Policy) (Result, error) {
	p.Key, p.Kind, p.Title = strings.TrimSpace(p.Key), strings.TrimSpace(p.Kind), strings.TrimSpace(p.Title)
	if err := validatePolicy(p); err != nil {
		return Result{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.masterPtr(id)
	if m == nil {
		return Result{}, ErrNotFound
	}
	verb := "added"
	replaced := false
	for i := range m.Blueprint.Policies {
		if m.Blueprint.Policies[i].Key == p.Key {
			m.Blueprint.Policies[i], replaced, verb = p, true, "updated"
		}
	}
	if !replaced {
		m.Blueprint.Policies = append(m.Blueprint.Policies, p)
	}
	e := s.emit(RecordUpdated, id, fmt.Sprintf("%s: %s policy %s %s", s.productName(id), p.Kind, p.Key, verb), KindKnow, EventPayload{})
	r := s.result("Policy "+verb+". It reaches the workspace when the Blueprint is published.", e)
	c := m.Clone()
	r.Master = &c
	return r, nil
}

// AddKnowledgeSource records a pinned repository the product must follow.
func (s *MemStore) AddKnowledgeSource(ctx context.Context, id string, k KnowledgeSource) (Result, error) {
	k.URL, k.Ref, k.Purpose = strings.TrimSpace(k.URL), strings.TrimSpace(k.Ref), strings.TrimSpace(k.Purpose)
	if err := validateSource(k); err != nil {
		return Result{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.masterPtr(id)
	if m == nil {
		return Result{}, ErrNotFound
	}
	for _, have := range m.Blueprint.Sources {
		if have.URL == k.URL && have.Ref == k.Ref {
			return Result{}, reject("That repository is already a knowledge source at that ref.")
		}
	}
	m.Blueprint.Sources = append(m.Blueprint.Sources, k)
	e := s.emit(RecordUpdated, id, fmt.Sprintf("%s: knowledge source %s @ %s", s.productName(id), k.URL, k.Ref), KindKnow, EventPayload{})
	r := s.result("Knowledge source recorded. It is listed in the workspace when the Blueprint is published.", e)
	c := m.Clone()
	r.Master = &c
	return r, nil
}

// PublishBlueprint renders the draft and, if it differs from the latest
// version, stores it as the next one and emits BlueprintPublished. An
// unchanged render publishes nothing — no version, no event (TC-288) —
// because two versions with one digest would make "which rules did this
// attempt run under" ambiguous.
func (s *MemStore) PublishBlueprint(ctx context.Context, id string) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.masterPtr(id)
	if m == nil {
		return Result{}, ErrNotFound
	}
	files, err := RenderBlueprint(s.productName(id), *m)
	if err != nil {
		return Result{}, err
	}
	digest := BlueprintDigest(files)
	if last, ok := m.Blueprint.Latest(); ok && last.Digest == digest {
		r := s.result(fmt.Sprintf("Nothing changed since version %d; nothing was published.", last.N))
		c := m.Clone()
		r.Master = &c
		return r, nil
	}
	v := BlueprintVersion{N: len(m.Blueprint.Versions) + 1, Digest: digest, PublishedAt: time.Now().Unix(), Files: files}
	m.Blueprint.Versions = append(m.Blueprint.Versions, v)
	e := s.emit(BlueprintPublished, id, fmt.Sprintf("%s: blueprint v%d published · %d files", s.productName(id), v.N, len(files)),
		KindKnow, EventPayload{Version: v.N, Digest: digest})
	r := s.result(fmt.Sprintf("Blueprint version %d published.", v.N), e)
	c := m.Clone()
	r.Master = &c
	return r, nil
}
