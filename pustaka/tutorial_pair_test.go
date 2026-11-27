package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestExternalTasksTutorialPair keeps the external tasks tutorial page and its
// markdown download in step, and keeps both free of anything private (D-099,
// docs/designs/gaos-external-tasks-and-pr-bundles.md §6).
func TestExternalTasksTutorialPair(t *testing.T) {
	files := []struct{ label, path string }{
		{"the tutorial page", "docs/integrations/external-tasks-api.html"},
		{"the markdown download", "docs/downloads/gaos-external-tasks-api.md"},
	}
	routes := []string{
		"POST /api/v1/products/{id}/tasks",
		"GET /api/v1/products/{id}/tasks/{taskId}",
		"GET /api/v1/products/{id}/tasks?externalId=",
		"POST /api/v1/products/{id}/tasks/{taskId}/cancel",
	}
	fields := []string{"kind", "externalId", "pr", "review", "caseCode", "dispatch"}
	hostRe := regexp.MustCompile(`https?://([A-Za-z0-9.-]+)`)
	productRe := regexp.MustCompile(`\bp-[0-9]+\b`)
	tokenRe := regexp.MustCompile(`gaos_[0-9A-Za-z]{32,}`)
	allowedHosts := map[string]bool{"gaos.example.org": true, "github.com": true}

	for _, f := range files {
		b, err := os.ReadFile(f.path)
		if err != nil {
			t.Fatalf("%s cannot be read: %v", f.label, err)
		}
		text := strings.ReplaceAll(string(b), "&amp;", "&")
		for _, r := range routes {
			if !strings.Contains(text, r) {
				t.Errorf("%s does not name the route %q", f.label, r)
			}
		}
		for _, fl := range fields {
			if !strings.Contains(text, "`"+fl+"`") && !strings.Contains(text, "<code>"+fl+"</code>") {
				t.Errorf("%s does not name the field %q", f.label, fl)
			}
		}
		for _, m := range hostRe.FindAllStringSubmatch(text, -1) {
			if !allowedHosts[strings.ToLower(m[1])] {
				t.Errorf("%s names a host other than the placeholders: %q", f.label, m[1])
			}
		}
		for _, id := range productRe.FindAllString(text, -1) {
			if id != "p-2" {
				t.Errorf("%s names a product id other than p-2: %q", f.label, id)
			}
		}
		if tokenRe.MatchString(text) {
			t.Errorf("%s carries a token literal", f.label)
		}
		if strings.Contains(text, "/run/secrets") {
			t.Errorf("%s names a container secret path", f.label)
		}
	}
}
