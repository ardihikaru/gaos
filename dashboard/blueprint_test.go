package main

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
)

// prodSmokeDigest is evidence rather than a fixture: on 2026-11-02 the
// production ai-claude-cli provisioned gaos-smoke with exactly these two files
// and answered manifest_digest b7326dfe…. If BlueprintDigest ever stops
// reproducing it, GAOS and the worker disagree about what a workspace holds,
// and drift detection (BR-39) would report every workspace as drifted.
const prodSmokeDigest = "b7326dfed3afe21ae277a34be46ef18f4e0365ca5609df9325ebc50e574b019b"

func blueprintMaster() Master {
	return Master{ID: "p-9", Code: "P9",
		BRD: BRD{Problem: "Orders are lost between systems.", Goals: []string{"No lost order"},
			Rules: []string{"An order id is never reused"}, OutOfScope: []string{"Refunds"}},
		Stories: []Story{{ID: "US-1", Pri: "must", Role: "clerk", Want: "to see every order", So: "none is lost"}},
		Blueprint: Blueprint{
			Policies: []Policy{
				{Key: "go-style", Kind: PolicyCoding, Title: "Go style", Body: "gofmt; errors wrapped with %w", Order: 2},
				{Key: "layers", Kind: PolicyTechnical, Title: "Layering", Body: "handlers call services, never the store", Order: 1},
			},
			Sources: []KnowledgeSource{{URL: "https://github.com/acme/go-template", Ref: "v1.4.0", Purpose: "structure"}},
		},
	}
}

func TestTheBlueprintRendersTheSameBytesEveryTime(t *testing.T) {
	a, err := RenderBlueprint("Orders", blueprintMaster())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := RenderBlueprint("Orders", blueprintMaster())
	if BlueprintDigest(a) != BlueprintDigest(b) || len(a) != len(b) {
		t.Fatalf("two renders of one Blueprint differ: %s vs %s", BlueprintDigest(a), BlueprintDigest(b))
	}
	got := map[string]string{}
	for _, f := range a {
		got[f.Path] = f.Content
	}
	for _, want := range []string{"CLAUDE.md", ".claude/rules/00-gaos-contract.md", ".claude/rules/10-brd.md",
		".claude/rules/40-technical-policy.md", ".claude/rules/50-coding-policy.md", ".claude/rules/70-knowledge-sources.md"} {
		if got[want] == "" {
			t.Errorf("the render has no %s; files are %v", want, keys(got))
		}
	}
	smoke := []BlueprintFile{
		{Path: "CLAUDE.md", SHA256: "5fb14c5d2e4cf8c4c53a66d87ed8eed471925ffcb4e76b1af692158bc9dac7f4"},
		{Path: ".claude/rules/10-brd.md", SHA256: "6b79867175a1f0160044a8adb8b1b4f7a7e6f3ef76efeeaa633191f1f6c83d7d"},
	}
	if d := BlueprintDigest(smoke); d != prodSmokeDigest {
		t.Fatalf("BlueprintDigest = %s, the production worker computed %s for the same files", d, prodSmokeDigest)
	}
}

func TestABlueprintOverTheCapsRefusesThePublishAndNamesThePolicy(t *testing.T) {
	m := blueprintMaster()
	m.Blueprint.Policies = append(m.Blueprint.Policies, Policy{Key: "huge", Kind: PolicyCustom, Title: "Huge",
		Body: strings.Repeat("x", BlueprintMaxFileBytes)})
	_, err := RenderBlueprint("Orders", m)
	if !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "policy huge") {
		t.Fatalf("an oversized policy rendered with err %v, want a refusal naming policy huge", err)
	}
	if err := validatePolicy(Policy{Key: "../escape", Kind: PolicyCustom, Title: "x"}); !errors.Is(err, ErrRejected) {
		t.Fatalf("a key that would render outside .claude/rules was accepted: %v", err)
	}
}

func TestAPublishedVersionIsImmutable(t *testing.T) {
	s := NewMemStore(1)
	ctx := context.Background()
	if _, err := s.SavePolicy(ctx, "p-1", Policy{Key: "go-style", Kind: PolicyCoding, Title: "Go style", Body: "v1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishBlueprint(ctx, "p-1"); err != nil {
		t.Fatal(err)
	}
	m, _ := s.Master(ctx, "p-1")
	v1 := m.Blueprint.Versions[0]
	s.SavePolicy(ctx, "p-1", Policy{Key: "go-style", Kind: PolicyCoding, Title: "Go style", Body: "v2"})
	m, _ = s.Master(ctx, "p-1")
	if got := m.Blueprint.Versions[0]; got.Digest != v1.Digest || !strings.Contains(fileOf(got, ".claude/rules/50-coding-policy.md"), "v1") {
		t.Fatalf("editing a policy changed published version 1 (%s -> %s)", v1.Digest, got.Digest)
	}
	if m.Blueprint.Policies[0].Body != "v2" || len(m.Blueprint.Policies) != 1 {
		t.Fatalf("the draft is %+v, want the one policy, replaced by key", m.Blueprint.Policies)
	}
}

func TestPublishingAnUnchangedBlueprintDoesNothing(t *testing.T) {
	s := NewMemStore(1)
	ctx := context.Background()
	s.SavePolicy(ctx, "p-1", Policy{Key: "go-style", Kind: PolicyCoding, Title: "Go style", Body: "gofmt"})
	if _, err := s.PublishBlueprint(ctx, "p-1"); err != nil {
		t.Fatal(err)
	}
	before := len(s.events)
	r, err := s.PublishBlueprint(ctx, "p-1")
	m, _ := s.Master(ctx, "p-1")
	if err != nil || len(r.NewEvents) != 0 || len(s.events) != before || len(m.Blueprint.Versions) != 1 {
		t.Fatalf("an unchanged publish gave %d events and %d versions (%v); want none and still one", len(r.NewEvents), len(m.Blueprint.Versions), err)
	}
}

func TestAKnowledgeSourceIsRenderedPinned(t *testing.T) {
	files, _ := RenderBlueprint("Orders", blueprintMaster())
	var ks string
	for _, f := range files {
		if f.Path == ".claude/rules/70-knowledge-sources.md" {
			ks = f.Content
		}
	}
	if !strings.Contains(ks, "https://github.com/acme/go-template @ `v1.4.0` — structure") {
		t.Fatalf("knowledge sources rendered as %q, want the URL with its pinned ref and purpose", ks)
	}
}

func TestAKnowledgeSourceThatIsNotHTTPSOrCarriesCredentialsIsRefused(t *testing.T) {
	s := NewMemStore(1)
	for _, k := range []KnowledgeSource{
		{URL: "http://github.com/acme/t", Ref: "v1"},
		{URL: "https://user:token@github.com/acme/t", Ref: "v1"},
		{URL: "https://github.com/acme/t", Ref: " "},
	} {
		if _, err := s.AddKnowledgeSource(context.Background(), "p-1", k); !errors.Is(err, ErrRejected) {
			t.Errorf("%+v was accepted (%v); it would make an attempt irreproducible or leak a credential", k, err)
		}
	}
}

func TestTheBlueprintSurvivesARestart(t *testing.T) {
	arc := arcadeTestTarget(t)
	ctx := context.Background()
	tenant := "conf-blueprint-restart"
	first := newArcadeTestStore(t, arc, tenant)
	first.SavePolicy(ctx, "p-1", Policy{Key: "go-style", Kind: PolicyCoding, Title: "Go style", Body: "gofmt"})
	first.AddKnowledgeSource(ctx, "p-1", KnowledgeSource{URL: "https://github.com/acme/t", Ref: "v1", Purpose: "style"})
	if _, err := first.PublishBlueprint(ctx, "p-1"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	want, _ := first.Master(ctx, "p-1")
	second, err := NewArcadeStoreFor(ctx, arc, 1, tenant)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, _ := second.Master(ctx, "p-1")
	if len(got.Blueprint.Versions) != 1 || got.Blueprint.Versions[0].Digest != want.Blueprint.Versions[0].Digest ||
		len(got.Blueprint.Versions[0].Files) != len(want.Blueprint.Versions[0].Files) {
		t.Fatalf("the reopened store has versions %+v, want the one published (%s)", got.Blueprint.Versions, want.Blueprint.Versions[0].Digest)
	}
	if len(got.Blueprint.Policies) != 1 || len(got.Blueprint.Sources) != 1 {
		t.Fatalf("the draft came back as %+v", got.Blueprint)
	}
}

func fileOf(v BlueprintVersion, path string) string {
	for _, f := range v.Files {
		if f.Path == path {
			return f.Content
		}
	}
	return ""
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// The tab is the only way an operator reaches any of this, and a template that
// fails renders a 500 every test above would miss: the first version of the
// pane called a (value, bool) method, which html/template cannot.
func TestTheBlueprintTabRendersAndPublishes(t *testing.T) {
	s, st := newTestServer(t)
	if w := do(t, s, "GET", "/products/p-1?tab=blueprint", nil, boCookies()...); w.Code != 200 ||
		!strings.Contains(w.Body.String(), "Publish blueprint") {
		t.Fatalf("GET the Blueprint tab = %d; want 200 with the publish button", w.Code)
	}
	form := url.Values{"key": {"go-style"}, "kind": {"coding"}, "title": {"Go style"}, "body": {"gofmt"}, "order": {"1"}}
	if w := do(t, s, "POST", "/products/p-1/blueprint/policies", form, boCookies()...); w.Code != 200 {
		t.Fatalf("POST a policy = %d: %s", w.Code, w.Body)
	}
	w := do(t, s, "POST", "/products/p-1/blueprint/publish", nil, boCookies()...)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "v1 · ") {
		t.Fatalf("publish through the tab = %d; want the pane to show version 1", w.Code)
	}
	if m, _ := st.Master(context.Background(), "p-1"); len(m.Blueprint.Versions) != 1 {
		t.Fatalf("the store holds %d versions after publishing through the tab, want 1", len(m.Blueprint.Versions))
	}
	if w := do(t, s, "POST", "/products/p-1/blueprint/publish", nil); w.Code != 401 {
		t.Fatalf("publishing without a back-office session = %d, want 401", w.Code)
	}
}
