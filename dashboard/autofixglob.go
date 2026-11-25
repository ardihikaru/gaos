package main

// Path rules for the auto-fix (D-091, design §6.1, §8.2). Matching itself is
// matchGlob and matchAny in egress.go: `*` and `?` stay inside a segment and a
// whole segment `**` crosses any number of them, `**/x` included at the top.

// builtinForbiddenGlobs are never touched by an auto-fix, whatever the
// product allows (design §6.1).
var builtinForbiddenGlobs = []string{
	"**/*_gen.go", "**/*.graphqls", "**/gqlgen.yml", "**/migrations/**", "**/*.sql",
	"**/go.mod", "**/go.sum", ".github/**", "**/Dockerfile*", "**/auth/**", "**/*auth*",
	"**/billing/**", "**/tariff/**", "**/payment/**", "**/*.env*", "**/*.pem", "**/*.key",
}

// pathForbidden reports whether a path is built-in forbidden or matches the
// product's extra forbidden globs.
func pathForbidden(extra []string, path string) bool {
	for _, set := range [][]string{builtinForbiddenGlobs, extra} {
		for _, g := range set {
			if matchGlob(g, path) {
				return true
			}
		}
	}
	return false
}
