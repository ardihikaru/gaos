package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// The Blueprint. D-058, requirements A-8…A-11, page product/blueprint.html.
//
// A product's record says WHAT it must do; the Blueprint adds HOW it is built
// — technical and coding policies, and the repositories whose structure and
// interfaces it must follow. It is standing knowledge, true for every task of
// the product, and it is delivered as files in the product's workspace
// (CLAUDE.md and .claude/rules/*.md), which Claude Code loads on every turn,
// resumed ones included.
//
// The draft (policies, knowledge sources) is part of the record document while
// a human edits it (D-009). PUBLISHING renders it, deterministically, into an
// immutable, content-addressed BlueprintVersion. Every attempt will pin the
// version it ran under (BR-39, M14.4).

// Policy kinds. Each renders to its own rules file; custom policies each get
// one of their own.
const (
	PolicyTechnical = "technical"
	PolicyCoding    = "coding"
	PolicySecurity  = "security"
	PolicyTesting   = "testing"
	PolicyCustom    = "custom"
)

// Policy is one section of a Blueprint. Body is Markdown.
type Policy struct {
	Key   string // stable id, ^[a-z0-9][a-z0-9-]{0,40}$
	Kind  string
	Title string
	Body  string
	Order int
}

// KnowledgeSource is a repository the product must follow. Recorded and
// listed; how its contents reach the model is OQ-28.
type KnowledgeSource struct {
	URL     string // https, no credentials
	Ref     string // a tag or a commit SHA — an attempt must be reproducible
	Purpose string // style | structure | interfaces | reference
	Note    string
}

// BlueprintFile is one rendered file of a published version.
type BlueprintFile struct {
	Path    string
	SHA256  string
	Bytes   int
	Content string
}

// BlueprintVersion is a publication. Immutable once stored.
type BlueprintVersion struct {
	N           int
	Digest      string
	PublishedAt int64
	Files       []BlueprintFile
}

// Blueprint is the draft plus every version published from it.
type Blueprint struct {
	Policies []Policy
	Sources  []KnowledgeSource
	Versions []BlueprintVersion
}

// Latest returns the newest published version, if any.
func (b Blueprint) Latest() (BlueprintVersion, bool) {
	if len(b.Versions) == 0 {
		return BlueprintVersion{}, false
	}
	return b.Versions[len(b.Versions)-1], true
}

// Current is Latest for templates, which cannot call a (value, bool) method:
// nil when nothing has been published.
func (b Blueprint) Current() *BlueprintVersion {
	if v, ok := b.Latest(); ok {
		return &v
	}
	return nil
}

func (b Blueprint) Clone() Blueprint {
	c := Blueprint{Policies: append([]Policy(nil), b.Policies...), Sources: append([]KnowledgeSource(nil), b.Sources...)}
	for _, v := range b.Versions {
		v.Files = append([]BlueprintFile(nil), v.Files...)
		c.Versions = append(c.Versions, v)
	}
	return c
}

// Caps mirror the worker's (ai-claude-cli workspace.MaxFileBytes etc.): a
// version the workspace would refuse must be refused here, at publish, where
// the operator can still fix the policy that caused it.
const (
	BlueprintMaxFileBytes  = 64 << 10
	BlueprintMaxTotalBytes = 256 << 10
	BlueprintMaxFiles      = 32
)

var (
	policyKey = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,40}$`)
	rulesFile = regexp.MustCompile(`^\.claude/rules/[a-z0-9][a-z0-9._-]{0,80}\.md$`)
	purposes  = map[string]bool{"style": true, "structure": true, "interfaces": true, "reference": true}
	kinds     = map[string]bool{PolicyTechnical: true, PolicyCoding: true, PolicySecurity: true, PolicyTesting: true, PolicyCustom: true}
)

// validatePolicy is the shape check SavePolicy applies.
func validatePolicy(p Policy) error {
	switch {
	case !policyKey.MatchString(p.Key):
		return reject("A policy key is lowercase letters, digits and dashes, starting with a letter or digit.")
	case !kinds[p.Kind]:
		return reject("A policy kind is technical, coding, security, testing or custom.")
	case strings.TrimSpace(p.Title) == "":
		return reject("Give the policy a title.")
	case len(p.Body) > BlueprintMaxFileBytes:
		return reject(fmt.Sprintf("Policy %s is %d bytes; a rules file holds at most %d.", p.Key, len(p.Body), BlueprintMaxFileBytes))
	}
	return nil
}

// validateSource refuses what would make an attempt irreproducible or leak a
// credential into every turn: non-https URLs, URLs carrying a userinfo, no ref.
func validateSource(k KnowledgeSource) error {
	u, err := url.Parse(strings.TrimSpace(k.URL))
	switch {
	case err != nil || u.Scheme != "https" || u.Host == "":
		return reject("A knowledge source is an https URL.")
	case u.User != nil:
		return reject("A knowledge source URL may not carry credentials; it would reach every turn.")
	case strings.TrimSpace(k.Ref) == "":
		return reject("Pin the knowledge source to a tag or a commit SHA.")
	case k.Purpose != "" && !purposes[k.Purpose]:
		return reject("A knowledge source's purpose is style, structure, interfaces or reference.")
	}
	return nil
}

// policyFile is where a policy renders.
func policyFile(p Policy) string {
	switch p.Kind {
	case PolicyTechnical:
		return ".claude/rules/40-technical-policy.md"
	case PolicyCoding:
		return ".claude/rules/50-coding-policy.md"
	case PolicySecurity:
		return ".claude/rules/60-security-policy.md"
	case PolicyTesting:
		return ".claude/rules/61-testing-policy.md"
	}
	return ".claude/rules/65-" + p.Key + ".md"
}

// RenderBlueprint renders a product's Blueprint into its workspace files.
// Deterministic: the same record yields the same bytes in either store, so the
// digest identifies the content. The version number is NOT in the files —
// otherwise every publish would differ and "unchanged" could never be seen.
func RenderBlueprint(name string, m Master) ([]BlueprintFile, error) {
	files := map[string]*strings.Builder{}
	owner := map[string]string{} // file -> the policy that put it there
	put := func(path, text string) {
		if files[path] == nil {
			files[path] = &strings.Builder{}
		}
		files[path].WriteString(text)
	}

	put("CLAUDE.md", renderIndex(name, m))
	put(".claude/rules/00-gaos-contract.md", standingContract)
	if s := renderBRD(m.BRD); s != "" {
		put(".claude/rules/10-brd.md", s)
	}
	if s := renderStories(m.Stories); s != "" {
		put(".claude/rules/20-stories.md", s)
	}
	if s := renderDecisions(m.Decisions); s != "" {
		put(".claude/rules/30-decisions.md", s)
	}
	pols := append([]Policy(nil), m.Blueprint.Policies...)
	sort.SliceStable(pols, func(i, j int) bool {
		if pols[i].Order != pols[j].Order {
			return pols[i].Order < pols[j].Order
		}
		return pols[i].Key < pols[j].Key
	})
	for _, p := range pols {
		f := policyFile(p)
		if files[f] == nil {
			put(f, "# "+kindTitle(p.Kind)+"\n")
		}
		put(f, "\n## "+p.Title+"\n\n"+strings.TrimSpace(p.Body)+"\n")
		owner[f] = p.Key
	}
	if s := renderSources(m.Blueprint.Sources); s != "" {
		put(".claude/rules/70-knowledge-sources.md", s)
	}

	out := make([]BlueprintFile, 0, len(files))
	total := 0
	for path, b := range files {
		body := b.String()
		if path != "CLAUDE.md" && !rulesFile.MatchString(path) {
			return nil, reject(fmt.Sprintf("Policy %s would render to %s, which a workspace may not hold.", owner[path], path))
		}
		if len(body) > BlueprintMaxFileBytes {
			return nil, reject(fmt.Sprintf("%s renders to %d bytes (policy %s); a workspace file holds at most %d.",
				path, len(body), owner[path], BlueprintMaxFileBytes))
		}
		total += len(body)
		sum := sha256.Sum256([]byte(body))
		out = append(out, BlueprintFile{Path: path, SHA256: hex.EncodeToString(sum[:]), Bytes: len(body), Content: body})
	}
	if len(out) > BlueprintMaxFiles || total > BlueprintMaxTotalBytes {
		return nil, reject(fmt.Sprintf("The Blueprint renders to %d files and %d bytes; a workspace holds at most %d and %d.",
			len(out), total, BlueprintMaxFiles, BlueprintMaxTotalBytes))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// BlueprintDigest is the workspace manifest digest: hex SHA-256 over one line
// per file, "<path> <sha256>\n", in path order — byte-for-byte the formula of
// ai-claude-cli's workspace.ManifestDigest, so GAOS can compare what it
// published with what the worker reports without asking for contents.
func BlueprintDigest(files []BlueprintFile) string {
	sorted := append([]BlueprintFile(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	h := sha256.New()
	for _, f := range sorted {
		h.Write([]byte(f.Path + " " + f.SHA256 + "\n"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func kindTitle(kind string) string {
	switch kind {
	case PolicyCustom:
		return "Policy"
	}
	return strings.ToUpper(kind[:1]) + kind[1:] + " policy"
}
