package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// The auto-fix side of the GitHub client (D-091, design §7.1, §8.3, §10).
//
// This file holds the READ-ONLY methods the defect context needs: the default
// branch's head, a code search and a file at a pinned commit. They use the read
// client's do(), so they inherit its rules: the token is a file, a redirect is
// refused and never followed, and an answer's body never reaches an error. The
// write methods are below, on a client of their own (ghWriter).

// escPath escapes each segment of a path separately, so a slash stays a path
// separator and every other byte is percent-encoded.
func escPath(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

// defaultHead reads the head SHA of the repository's default branch. The branch
// name is GitHub's, never the caller's.
func (c *ghClient) defaultHead(ctx context.Context, owner, repo, branch string) (string, error) {
	var r struct {
		DefaultBranch string `json:"default_branch"`
	}
	base := fmt.Sprintf("%s/repos/%s/%s", c.base, url.PathEscape(owner), url.PathEscape(repo))
	if err := c.get(ctx, base, &r); err != nil {
		return "", err
	}
	if branch == "" {
		branch = r.DefaultBranch
	}
	if branch == "" {
		return "", errors.New("the repository names no default branch")
	}
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := c.get(ctx, base+"/git/ref/heads/"+escPath(branch), &ref); err != nil {
		return "", err
	}
	if ref.Object.SHA == "" {
		return "", errors.New("the branch names no commit")
	}
	return ref.Object.SHA, nil
}

// searchCode searches one repository's code for a term, in GitHub's rank order.
func (c *ghClient) searchCode(ctx context.Context, owner, repo, term string) ([]ghCodeHit, error) {
	q := url.Values{"q": {term + " repo:" + owner + "/" + repo}, "per_page": {"8"}}
	var r struct {
		Items []struct {
			Path string `json:"path"`
		} `json:"items"`
	}
	if err := c.get(ctx, c.base+"/search/code?"+q.Encode(), &r); err != nil {
		return nil, err
	}
	out := make([]ghCodeHit, 0, len(r.Items))
	for _, it := range r.Items {
		out = append(out, ghCodeHit{Path: it.Path})
	}
	return out, nil
}

// fileAt reads a file at a commit. found is false for a path that does not
// exist there, is not a plain file or is larger than the packet allows.
func (c *ghClient) fileAt(ctx context.Context, owner, repo, filePath, sha string) (ghBlob, bool, error) {
	u := fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s", c.base, url.PathEscape(owner), url.PathEscape(repo),
		escPath(filePath), url.QueryEscape(sha))
	var r struct {
		Type     string `json:"type"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
		SHA      string `json:"sha"`
		Size     int    `json:"size"`
	}
	if err := c.get(ctx, u, &r); err != nil {
		var ge *ghError
		if errors.As(err, &ge) && ge.Status == 404 {
			return ghBlob{}, false, nil
		}
		return ghBlob{}, false, err
	}
	if r.Type != "file" || r.Encoding != "base64" || r.Size > defectMaxFileBytes {
		return ghBlob{}, false, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.NewReplacer("\n", "", "\r", "").Replace(r.Content))
	if err != nil {
		return ghBlob{}, false, nil
	}
	return ghBlob{Path: filePath, SHA: r.SHA, Text: string(raw)}, true, nil
}

// ---------------------------------------------------------------- the write client

// The auto-fix's write token (D-091, design §10): a separate file, a separate
// client, and a short list of methods. It never enters a workspace or a packet,
// and it is never logged.

// The kinds of write token, as AutoFixConfigured and the view name them.
const (
	tokenKindFine    = "fine-grained"
	tokenKindClassic = "classic (broad)"
)

// ghTokenKind names a token's kind by its prefix; "" for anything else.
func ghTokenKind(tok string) string {
	switch {
	case strings.HasPrefix(tok, ghTokenFineGrained):
		return tokenKindFine
	case strings.HasPrefix(tok, ghTokenPrefix):
		return tokenKindClassic
	}
	return ""
}

// ghWriteConfigFromEnv reads GAOS_GITHUB_WRITE_TOKEN_FILE. A classic ghp_ token
// is refused unless GAOS_AUTOFIX_ALLOW_CLASSIC=1 (a visible operator choice).
func ghWriteConfigFromEnv() ghConfig {
	c := ghConfigFromVar("GAOS_GITHUB_WRITE_TOKEN_FILE")
	if c.token != "" && ghTokenKind(c.token) == tokenKindClassic &&
		strings.TrimSpace(os.Getenv("GAOS_AUTOFIX_ALLOW_CLASSIC")) != "1" {
		c.token = ""
		c.why = "a classic ghp_ token is refused unless GAOS_AUTOFIX_ALLOW_CLASSIC=1; use a fine-grained token"
	}
	return c
}

// fixRefPrefix is the only namespace the writer may create or delete a ref in.
const fixRefPrefix = "gaos/fix-"

// errDraftUnsupported says GitHub refused a draft pull request. There is no
// non-draft fallback.
var errDraftUnsupported = errors.New("github refused a draft pull request")

// errRefExists says the branch is already there; it is never moved.
var errRefExists = errors.New("the branch already exists")

// ghWriter is the write-scoped client. Its methods are the whole list: read the
// head and a ref, read a tree entry and a commit, read the workflow files,
// create a blob, a tree, a commit and a ref, delete a gaos/fix- ref, create a
// draft pull request, add labels and a comment. It can mark nothing ready,
// merge nothing and update no ref.
type ghWriter struct{ c *ghClient }

func newGHWriter(cfg ghConfig) *ghWriter {
	c := newGHClient(cfg)
	if c == nil {
		return nil
	}
	return &ghWriter{c: c}
}

func (w *ghWriter) repoURL(owner, repo string) string {
	return fmt.Sprintf("%s/repos/%s/%s", w.c.base, url.PathEscape(owner), url.PathEscape(repo))
}

func (w *ghWriter) post(ctx context.Context, u string, in, out any) error {
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	raw, err := w.c.do(ctx, http.MethodPost, u, b)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// head reads the base branch's name and head commit: the product's configured
// base branch, else the repository's default branch.
func (w *ghWriter) head(ctx context.Context, owner, repo, override string) (branch, sha string, err error) {
	var r struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := w.c.get(ctx, w.repoURL(owner, repo), &r); err != nil {
		return "", "", err
	}
	if override != "" {
		r.DefaultBranch = override
	}
	if r.DefaultBranch == "" {
		return "", "", errors.New("the repository names no default branch")
	}
	sha, found, err := w.refSHA(ctx, owner, repo, r.DefaultBranch)
	if err != nil {
		return "", "", err
	}
	if !found {
		return "", "", errors.New("the default branch names no commit")
	}
	return r.DefaultBranch, sha, nil
}

// refSHA reads a branch's head; found is false for a branch that is not there.
func (w *ghWriter) refSHA(ctx context.Context, owner, repo, branch string) (string, bool, error) {
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := w.c.get(ctx, w.repoURL(owner, repo)+"/git/ref/heads/"+escPath(branch), &ref); err != nil {
		var ge *ghError
		if errors.As(err, &ge) && ge.Status == 404 {
			return "", false, nil
		}
		return "", false, err
	}
	return ref.Object.SHA, ref.Object.SHA != "", nil
}

// ghCommit is a commit's message and tree.
type ghCommit struct {
	Message string
	Tree    string
}

func (w *ghWriter) commit(ctx context.Context, owner, repo, sha string) (ghCommit, error) {
	var r struct {
		Message string `json:"message"`
		Tree    struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := w.c.get(ctx, w.repoURL(owner, repo)+"/git/commits/"+url.PathEscape(sha), &r); err != nil {
		return ghCommit{}, err
	}
	return ghCommit{Message: r.Message, Tree: r.Tree.SHA}, nil
}

// ghTreeEntry is one path at a commit.
type ghTreeEntry struct {
	Exists bool
	Mode   string
	Type   string
	SHA    string
}

// treeEntry looks a path up in the tree of a commit, one level at a time.
func (w *ghWriter) treeEntry(ctx context.Context, owner, repo, commitSHA, filePath string) (ghTreeEntry, error) {
	cm, err := w.commit(ctx, owner, repo, commitSHA)
	if err != nil {
		return ghTreeEntry{}, err
	}
	segs := strings.Split(filePath, "/")
	tree := cm.Tree
	for i, seg := range segs {
		var r struct {
			Tree []struct {
				Path string `json:"path"`
				Mode string `json:"mode"`
				Type string `json:"type"`
				SHA  string `json:"sha"`
			} `json:"tree"`
		}
		if err := w.c.get(ctx, w.repoURL(owner, repo)+"/git/trees/"+url.PathEscape(tree), &r); err != nil {
			return ghTreeEntry{}, err
		}
		found := false
		for _, e := range r.Tree {
			if e.Path != seg {
				continue
			}
			found = true
			if i == len(segs)-1 {
				return ghTreeEntry{Exists: true, Mode: e.Mode, Type: e.Type, SHA: e.SHA}, nil
			}
			if e.Type != "tree" {
				return ghTreeEntry{Exists: true, Mode: e.Mode, Type: e.Type, SHA: e.SHA}, nil
			}
			tree = e.SHA
			break
		}
		if !found {
			return ghTreeEntry{}, nil
		}
	}
	return ghTreeEntry{}, nil
}

// workflows reads the workflow files at a commit. unreadable names a file too
// large or odd to scan, which the caller treats as a risk.
func (w *ghWriter) workflows(ctx context.Context, owner, repo, sha string) (files map[string]string, unreadable []string, err error) {
	var list []struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Type string `json:"type"`
	}
	u := fmt.Sprintf("%s/contents/.github/workflows?ref=%s", w.repoURL(owner, repo), url.QueryEscape(sha))
	if err := w.c.get(ctx, u, &list); err != nil {
		var ge *ghError
		if errors.As(err, &ge) && ge.Status == 404 {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	files = map[string]string{}
	for _, e := range list {
		if e.Type != "file" || !(strings.HasSuffix(e.Name, ".yml") || strings.HasSuffix(e.Name, ".yaml")) {
			continue
		}
		b, found, err := w.c.fileAt(ctx, owner, repo, e.Path, sha)
		if err != nil {
			return nil, nil, err
		}
		if !found {
			unreadable = append(unreadable, e.Path)
			continue
		}
		files[e.Path] = b.Text
	}
	return files, unreadable, nil
}

func (w *ghWriter) createBlob(ctx context.Context, owner, repo, content string) (string, error) {
	var r struct {
		SHA string `json:"sha"`
	}
	err := w.post(ctx, w.repoURL(owner, repo)+"/git/blobs", map[string]string{"content": content, "encoding": "utf-8"}, &r)
	return r.SHA, err
}

// ghTreeItem is one entry of a new tree. Mode is 100644 or the existing 100755.
type ghTreeItem struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

func (w *ghWriter) createTree(ctx context.Context, owner, repo, baseTree string, items []ghTreeItem) (string, error) {
	for _, it := range items {
		if it.Mode != "100644" && it.Mode != "100755" {
			return "", errors.New("a tree entry may only be a regular file")
		}
	}
	var r struct {
		SHA string `json:"sha"`
	}
	err := w.post(ctx, w.repoURL(owner, repo)+"/git/trees", map[string]any{"base_tree": baseTree, "tree": items}, &r)
	return r.SHA, err
}

func (w *ghWriter) createCommit(ctx context.Context, owner, repo, message, tree, parent string) (string, error) {
	var r struct {
		SHA string `json:"sha"`
	}
	err := w.post(ctx, w.repoURL(owner, repo)+"/git/commits",
		map[string]any{"message": message, "tree": tree, "parents": []string{parent}}, &r)
	return r.SHA, err
}

// createRef creates refs/heads/<branch>, which must start with gaos/fix-. A ref
// that exists is errRefExists; nothing is ever moved or overwritten.
func (w *ghWriter) createRef(ctx context.Context, owner, repo, branch, sha string) error {
	if !strings.HasPrefix(branch, fixRefPrefix) {
		return errors.New("a branch GAOS creates starts with " + fixRefPrefix)
	}
	err := w.post(ctx, w.repoURL(owner, repo)+"/git/refs", map[string]string{"ref": "refs/heads/" + branch, "sha": sha}, nil)
	var ge *ghError
	if errors.As(err, &ge) && ge.Status == 422 {
		return errRefExists
	}
	return err
}

// deleteRef deletes a gaos/fix- branch. It is the only delete the client has.
func (w *ghWriter) deleteRef(ctx context.Context, owner, repo, branch string) error {
	if !strings.HasPrefix(branch, fixRefPrefix) || strings.Contains(branch, "..") {
		return errors.New("only a " + fixRefPrefix + " branch is deleted")
	}
	_, err := w.c.do(ctx, http.MethodDelete, w.repoURL(owner, repo)+"/git/refs/heads/"+escPath(branch), nil)
	return err
}

// findPR looks for a pull request from a branch, in any state, so a crash
// between the ref and the record does not open a second one.
func (w *ghWriter) findPR(ctx context.Context, owner, repo, branch string) (number int, htmlURL string, found bool, err error) {
	var prs []struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
	}
	q := url.Values{"head": {owner + ":" + branch}, "state": {"all"}}
	if err := w.c.get(ctx, w.repoURL(owner, repo)+"/pulls?"+q.Encode(), &prs); err != nil {
		return 0, "", false, err
	}
	if len(prs) == 0 {
		return 0, "", false, nil
	}
	return prs[0].Number, prs[0].HTMLURL, true, nil
}

// createPR opens a DRAFT pull request. A refusal that names drafts is
// errDraftUnsupported; there is no fallback.
func (w *ghWriter) createPR(ctx context.Context, owner, repo, title, head, base, body string) (int, string, error) {
	var r struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
	}
	err := w.post(ctx, w.repoURL(owner, repo)+"/pulls",
		map[string]any{"title": title, "head": head, "base": base, "body": body, "draft": true}, &r)
	var ge *ghError
	if errors.As(err, &ge) && ge.Draft {
		return 0, "", errDraftUnsupported
	}
	if err != nil {
		return 0, "", err
	}
	return r.Number, r.HTMLURL, nil
}

// addLabels creates the labels that are missing, then puts them on the pull request.
func (w *ghWriter) addLabels(ctx context.Context, owner, repo string, number int, labels []string) error {
	for _, l := range labels {
		var probe struct{}
		err := w.c.get(ctx, w.repoURL(owner, repo)+"/labels/"+url.PathEscape(l), &probe)
		var ge *ghError
		if errors.As(err, &ge) && ge.Status == 404 {
			err = w.post(ctx, w.repoURL(owner, repo)+"/labels", map[string]string{"name": l, "color": "ededed"}, nil)
			if errors.As(err, &ge) && ge.Status == 422 {
				err = nil // created meanwhile
			}
		}
		if err != nil {
			return err
		}
	}
	return w.post(ctx, fmt.Sprintf("%s/issues/%d/labels", w.repoURL(owner, repo), number), map[string]any{"labels": labels}, nil)
}

func (w *ghWriter) addComment(ctx context.Context, owner, repo string, number int, body string) error {
	return w.post(ctx, fmt.Sprintf("%s/issues/%d/comments", w.repoURL(owner, repo), number), map[string]string{"body": body}, nil)
}
