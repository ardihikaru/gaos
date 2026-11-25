package main

import "testing"

func TestAutoFixGlobSemantics(t *testing.T) {
	cases := []struct {
		pat, path string
		want      bool
	}{
		{"pkg/*.go", "pkg/a.go", true},
		{"pkg/*.go", "pkg/sub/a.go", false},
		{"pkg/**", "pkg/a/b/c.go", true},
		{"pkg/**", "pkg", true},
		{"**/*_test.go", "a/b/x_test.go", true},
		{"**/*_test.go", "x_test.go", true},
		{"**/*.sql", "db/m/1.sql", true},
		{"a/?.go", "a/b.go", true},
		{"a/?.go", "a/bc.go", false},
		{"a/**/z.go", "a/z.go", true},
		{"a/**/z.go", "a/b/c/z.go", true},
		{"a/**/z.go", "b/z.go", false},
		{"*", "a/b", false},
		{"src/*/handler*.go", "src/user/handler_x.go", true},
		{"src/*/handler*.go", "src/handler_x.go", false},
	}
	for _, c := range cases {
		if got := matchGlob(c.pat, c.path); got != c.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", c.pat, c.path, got, c.want)
		}
	}
}

func TestPathForbiddenCoversTheBuiltinsAndTopLevelFiles(t *testing.T) {
	for _, p := range []string{"go.mod", "svc/go.sum", ".github/workflows/ci.yml", "svc/auth/token.go",
		"svc/user_auth.go", "db/migrations/1.go", "Dockerfile", "a/b/.env.local", "x/key.pem", "schema.sql"} {
		if !pathForbidden(nil, p) {
			t.Errorf("%q must be forbidden", p)
		}
	}
	if pathForbidden(nil, "pkg/x/validate.go") {
		t.Error("an ordinary source file must not be forbidden")
	}
	if !pathForbidden([]string{"pkg/x/**"}, "pkg/x/validate.go") {
		t.Error("a product's own forbidden glob must apply")
	}
}
