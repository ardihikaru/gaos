package main

import (
	"strings"
	"testing"
)

// The back-office bar's groups (docs/designs/top-bar-menu.md §4.1, §6.1). A
// group is a popovertarget button in the menu, and its panel holds the
// existing links, moved byte for byte; the trigger of the current view's group
// carries is-current.

func boTrigger(id string) string { return `popovertarget="` + id + `"` }

func boIsCurrent(body, id string) bool {
	return strings.Contains(body, `class="bo-grp is-current" popovertarget="`+id+`"`)
}

// boPanel is the panel's markup: from its opening tag to the first </div>.
func boPanel(body, id string) string {
	i := strings.Index(body, `id="`+id+`" popover="auto"`)
	if i < 0 {
		return ""
	}
	j := strings.Index(body[i:], `</div>`)
	if j < 0 {
		return ""
	}
	return body[i : i+j]
}

func boMenuGet(t *testing.T, srv *server, path string) string {
	t.Helper()
	rec := do(t, srv, "GET", path, nil, boCookies()...)
	if rec.Code != 200 {
		t.Fatalf("GET %s answered %d", path, rec.Code)
	}
	return rec.Body.String()
}

func boCheckGroup(t *testing.T, body, path, id string, wantCurrent bool) {
	t.Helper()
	if n := strings.Count(body, boTrigger(id)); n != 1 {
		t.Errorf("the bar has %d triggers for %s, want 1", n, id)
	}
	if boPanel(body, id) == "" {
		t.Errorf("the bar has no %s panel", id)
	}
	if got := boIsCurrent(body, id); got != wantCurrent {
		t.Errorf("on %s the %s trigger is current=%v, want %v", path, id, got, wantCurrent)
	}
}

// TC-904
func TestBoMenuGroupsGlobal(t *testing.T) {
	srv, _ := newTestServer(t)
	groups := []string{"boGrpLibrary", "boGrpAutomation", "boGrpSystem"}

	path := "/ui/bo/stories"
	body := boMenuGet(t, srv, path)
	for _, id := range groups {
		boCheckGroup(t, body, path, id, id == "boGrpLibrary")
	}
	if !strings.Contains(boPanel(body, "boGrpLibrary"), `<a aria-current="page"   href="stories"`) {
		t.Errorf("the Library panel does not hold the current Stories link")
	}
	for _, lit := range []string{
		`href="inbox" hx-get="ui/bo/inbox"`, `href="./" hx-get="ui/bo/products"`,
		`href="stories" hx-get="ui/bo/stories"`, `href="diagrams" hx-get="ui/bo/diagrams"`,
		`href="knowledge" hx-get="ui/bo/knowledge"`, `href="agents" hx-get="ui/bo/agents"`,
		`href="archive" hx-get="ui/bo/archive"`, `href="integrations" hx-get="ui/bo/integrations"`,
		`href="traffic" hx-get="ui/bo/traffic"`, `href="autofix" hx-get="ui/bo/autofix"`,
		`href="prs" hx-get="ui/bo/prs"`,
	} {
		if n := strings.Count(body, lit); n != 1 {
			t.Errorf("the bar carries %q %d times, want once", lit, n)
		}
	}

	path = "/ui/bo/inbox"
	body = boMenuGet(t, srv, path)
	for _, id := range groups {
		boCheckGroup(t, body, path, id, false)
	}

	// The negative: the groups render only inside {{if .BO}}. A signed-out
	// request is refused (BR-55), and whatever it answers carries no group.
	if body := do(t, srv, "GET", "/stories", nil).Body.String(); strings.Contains(body, "boGrp") {
		t.Errorf("a signed-out page carries the bar's groups")
	}
}

// TC-905
func TestBoMenuGroupsProduct(t *testing.T) {
	srv, _ := newTestServer(t)

	path := "/products/p-1?tab=blueprint"
	body := boMenuGet(t, srv, path)
	boCheckGroup(t, body, path, "boGrpSetup", true)
	p := boPanel(body, "boGrpSetup")
	if !(strings.Contains(p, `aria-current="page"`) && strings.Contains(p, `hx-get="products/p-1?tab=blueprint"`)) {
		t.Errorf("the Setup panel does not hold the current Blueprint link")
	}
	if !strings.Contains(p, `hx-get="products/p-1?tab=vendors"`) {
		t.Errorf("the Setup panel does not hold the AI vendors link")
	}
	for _, tab := range []string{"brd", "stories", "diagrams", "knowledge", "board", "qa", "timeline"} {
		lit := `hx-get="products/p-1?tab=` + tab + `"`
		if n := strings.Count(body, lit); n != 1 {
			t.Errorf("the bar carries %q %d times, want once", lit, n)
		}
		if strings.Contains(p, lit) {
			t.Errorf("the flat tab %q is inside the Setup panel", tab)
		}
	}
	if strings.Contains(body, boTrigger("boGrpLibrary")) {
		t.Errorf("a product's bar carries the global groups")
	}

	path = "/products/p-1?tab=board"
	body = boMenuGet(t, srv, path)
	boCheckGroup(t, body, path, "boGrpSetup", false)
}
