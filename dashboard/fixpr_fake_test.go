package main

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
)

// fakeGHW is a fake GitHub for the auto-fix's write path: the repository's
// files at one head, git objects, refs, pull requests, labels and comments.
// Every request is recorded as "METHOD path".
type fakeGHW struct {
	mu         sync.Mutex
	files      map[string]string
	modes      map[string]string
	workflows  map[string]string
	refs       map[string]string
	commits    map[string]ghCommit
	calls      []string
	bodies     map[string][]byte
	prFail     int
	prDraftMsg bool
	prs        []string // heads of the pull requests
	labels     map[string]bool
	seq        int
	lastCommit string
	// D-099: the reviewed pull request a review fix reads.
	pull      map[string]any
	pullFiles []map[string]any
	protected map[string]bool
}

func newFakeGHW() *fakeGHW {
	return &fakeGHW{
		files: map[string]string{fxFile: fxText}, modes: map[string]string{}, workflows: map[string]string{},
		refs: map[string]string{"main": "head1"}, commits: map[string]ghCommit{"head1": {Message: "base", Tree: "tree:"}},
		bodies: map[string][]byte{}, labels: map[string]bool{},
	}
}

func shaOf(parts ...string) string {
	h := sha1.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

func (f *fakeGHW) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func (f *fakeGHW) body(key string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bodies[key]
}

func fakeJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeGHW) treeEntries(prefix string) []map[string]string {
	seen := map[string]bool{}
	var out []map[string]string
	var paths []string
	for p := range f.files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		rest := strings.TrimPrefix(p, prefix)
		name, more, _ := strings.Cut(rest, "/")
		if seen[name] {
			continue
		}
		seen[name] = true
		if more != "" {
			out = append(out, map[string]string{"path": name, "mode": "040000", "type": "tree", "sha": "tree:" + prefix + name + "/"})
			continue
		}
		mode := f.modes[p]
		if mode == "" {
			mode = "100644"
		}
		out = append(out, map[string]string{"path": name, "mode": mode, "type": "blob", "sha": "b-" + p})
	}
	return out
}

func (f *fakeGHW) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, _ := io.ReadAll(r.Body)
	key := r.Method + " " + r.URL.Path
	f.calls = append(f.calls, key)
	f.bodies[key] = raw
	const base = "/repos/acme-org/sample-service"
	p := r.URL.Path
	switch {
	case p == base && r.Method == "GET":
		fakeJSON(w, 200, map[string]string{"default_branch": "main"})
	case f.pull != nil && strings.HasPrefix(p, base+"/pulls/") && strings.HasSuffix(p, "/files") && r.Method == "GET":
		if r.URL.Query().Get("page") != "1" {
			fakeJSON(w, 200, []any{})
			return
		}
		fakeJSON(w, 200, f.pullFiles)
	case f.pull != nil && strings.HasPrefix(p, base+"/pulls/") && !strings.Contains(strings.TrimPrefix(p, base+"/pulls/"), "/") && r.Method == "GET":
		fakeJSON(w, 200, f.pull)
	case strings.HasPrefix(p, base+"/branches/") && r.Method == "GET":
		fakeJSON(w, 200, map[string]bool{"protected": f.protected[strings.TrimPrefix(p, base+"/branches/")]})
	case strings.HasPrefix(p, base+"/git/ref/heads/"):
		sha, ok := f.refs[strings.TrimPrefix(p, base+"/git/ref/heads/")]
		if !ok {
			w.WriteHeader(404)
			return
		}
		fakeJSON(w, 200, map[string]any{"object": map[string]string{"sha": sha}})
	case strings.HasPrefix(p, base+"/git/commits/") && r.Method == "GET":
		c, ok := f.commits[strings.TrimPrefix(p, base+"/git/commits/")]
		if !ok {
			w.WriteHeader(404)
			return
		}
		fakeJSON(w, 200, map[string]any{"message": c.Message, "tree": map[string]string{"sha": c.Tree}})
	case strings.HasPrefix(p, base+"/git/trees/") && r.Method == "GET":
		t := strings.TrimPrefix(p, base+"/git/trees/")
		if !strings.HasPrefix(t, "tree:") {
			w.WriteHeader(404)
			return
		}
		fakeJSON(w, 200, map[string]any{"tree": f.treeEntries(strings.TrimPrefix(t, "tree:"))})
	case p == base+"/contents/.github/workflows":
		if len(f.workflows) == 0 {
			w.WriteHeader(404)
			return
		}
		var list []map[string]string
		for path := range f.workflows {
			list = append(list, map[string]string{"name": path[strings.LastIndex(path, "/")+1:], "path": path, "type": "file"})
		}
		fakeJSON(w, 200, list)
	case strings.HasPrefix(p, base+"/contents/"):
		text, ok := f.workflows[strings.TrimPrefix(p, base+"/contents/")]
		if !ok {
			w.WriteHeader(404)
			return
		}
		fakeJSON(w, 200, map[string]any{"type": "file", "encoding": "base64", "sha": "wf", "size": len(text),
			"content": base64.StdEncoding.EncodeToString([]byte(text))})
	case p == base+"/git/blobs":
		var b struct{ Content string }
		_ = json.Unmarshal(raw, &b)
		fakeJSON(w, 201, map[string]string{"sha": "blob-" + shaOf(b.Content)})
	case p == base+"/git/trees":
		var b struct {
			Tree []json.RawMessage `json:"tree"`
		}
		_ = json.Unmarshal(raw, &b)
		parts := make([]string, len(b.Tree))
		for i, t := range b.Tree {
			parts[i] = string(t)
		}
		fakeJSON(w, 201, map[string]string{"sha": "newtree-" + shaOf(parts...)})
	case p == base+"/git/commits":
		var b struct {
			Message string `json:"message"`
			Tree    string `json:"tree"`
		}
		_ = json.Unmarshal(raw, &b)
		sha := "commit-" + shaOf(b.Message, b.Tree)
		f.commits[sha] = ghCommit{Message: b.Message, Tree: b.Tree}
		f.lastCommit = sha
		fakeJSON(w, 201, map[string]string{"sha": sha})
	case p == base+"/git/refs" && r.Method == "POST":
		var b struct{ Ref, SHA string }
		_ = json.Unmarshal(raw, &b)
		name := strings.TrimPrefix(b.Ref, "refs/heads/")
		if _, ok := f.refs[name]; ok {
			w.WriteHeader(422)
			return
		}
		f.refs[name] = b.SHA
		fakeJSON(w, 201, map[string]string{"ref": b.Ref})
	case strings.HasPrefix(p, base+"/git/refs/heads/") && r.Method == "DELETE":
		delete(f.refs, strings.TrimPrefix(p, base+"/git/refs/heads/"))
		w.WriteHeader(204)
	case p == base+"/pulls" && r.Method == "GET":
		var out []map[string]any
		head := r.URL.Query().Get("head")
		for i, h := range f.prs {
			if "acme-org:"+h == head {
				out = append(out, map[string]any{"number": 7 + i, "html_url": "https://github.example/acme-org/sample-service/pull/7"})
			}
		}
		fakeJSON(w, 200, out)
	case p == base+"/pulls" && r.Method == "POST":
		if f.prFail != 0 {
			w.WriteHeader(f.prFail)
			if f.prDraftMsg {
				_, _ = w.Write([]byte(`{"message":"Draft pull requests are not supported in this repository"}`))
			}
			return
		}
		var b struct{ Head string }
		_ = json.Unmarshal(raw, &b)
		f.prs = append(f.prs, b.Head)
		fakeJSON(w, 201, map[string]any{"number": 7, "html_url": "https://github.example/acme-org/sample-service/pull/7"})
	case strings.HasPrefix(p, base+"/labels/"):
		if !f.labels[strings.TrimPrefix(p, base+"/labels/")] {
			w.WriteHeader(404)
			return
		}
		fakeJSON(w, 200, map[string]string{})
	case p == base+"/labels":
		var b struct{ Name string }
		_ = json.Unmarshal(raw, &b)
		f.labels[b.Name] = true
		fakeJSON(w, 201, map[string]string{})
	case strings.HasSuffix(p, "/labels"), strings.HasSuffix(p, "/comments"):
		fakeJSON(w, 201, map[string]any{})
	default:
		w.WriteHeader(404)
	}
}

func newFakeWriter(t *testing.T, f *fakeGHW) *ghWriter {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return newGHWriter(ghConfig{token: "github_pat_" + strings.Repeat("a", 30), base: srv.URL})
}
