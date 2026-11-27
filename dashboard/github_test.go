package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The GitHub client: the credential's kind, the reads, and the review it posts.

func TestGHConfigRefusesTheWrongKindOfToken(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		tok  string
		want string
	}{
		{"a brain town reader token", btConsumerTokenPrefix + strings.Repeat("a", 40), "Brain Town reader credential"},
		{"a brain town input token", "bt_live_" + strings.Repeat("a", 40), "Brain Town input credential"},
		{"a gaos token", apiTokenPrefix + strings.Repeat("a", 40), "GAOS credential"},
		{"an empty file", "", "holds no token"},
		{"a short token", "ghp_short", "at least 20 characters"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(dir, "token")
			if err := os.WriteFile(path, []byte(c.tok+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("GAOS_GITHUB_TOKEN_FILE", path)
			cfg := ghConfigFromEnv()
			if cfg.why == "" {
				t.Fatalf("the token must be refused, got %+v", cfg)
			}
			if !strings.Contains(cfg.why, c.want) {
				t.Errorf("the reason %q must contain %q", cfg.why, c.want)
			}
		})
	}
}

func TestGHConfigRefusesAReadableFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte("ghp_"+strings.Repeat("a", 40)), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GAOS_GITHUB_TOKEN_FILE", path)
	if why := ghConfigFromEnv().why; !strings.Contains(why, "readable by group or others") {
		t.Fatalf("a loose file must be refused, got %q", why)
	}
}

func TestGHConfigAcceptsAGitHubToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte("ghp_"+strings.Repeat("a", 40)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GAOS_GITHUB_TOKEN_FILE", path)
	cfg := ghConfigFromEnv()
	if cfg.why != "" {
		t.Fatalf("a GitHub token must be accepted, got %q", cfg.why)
	}
	if cfg.token == "" {
		t.Error("the token must be kept")
	}
}

func TestGHOwnerRepo(t *testing.T) {
	owner, repo, ok := ghOwnerRepo("acme-org/sample-service")
	if !ok || owner != "acme-org" || repo != "sample-service" {
		t.Errorf("ghOwnerRepo = %q, %q, %v", owner, repo, ok)
	}
	for _, bad := range []string{"", "no-slash", "a/b/c", "/b", "a/"} {
		if _, _, ok := ghOwnerRepo(bad); ok {
			t.Errorf("%q must not split into an owner and a repository", bad)
		}
	}
}

func TestGHClientReadsAndPosts(t *testing.T) {
	var posted string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ghp_token" {
			w.WriteHeader(401)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls/42") && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(ghPR{Number: 42, Title: "Fix export", Body: "what to test",
				User: struct {
					Login string `json:"login"`
				}{"ardi"}})
		case strings.HasSuffix(r.URL.Path, "/files") && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode([]ghFile{{Filename: "a.go", Status: "modified", Patch: "@@"}})
		case strings.HasSuffix(r.URL.Path, "/reviews") && r.Method == http.MethodPost:
			b := make([]byte, 4096)
			n, _ := r.Body.Read(b)
			posted = string(b[:n])
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"id":"1"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	c := &ghClient{token: "ghp_token", base: srv.URL, hc: srv.Client(), logf: func(string, ...any) {}}
	pr, err := c.pullRequest(context.Background(), "acme-org", "sample-service", 42)
	if err != nil {
		t.Fatalf("read the pull request: %v", err)
	}
	if pr.Number != 42 || pr.Title != "Fix export" || pr.User.Login != "ardi" {
		t.Errorf("the pull request was read wrong: %+v", pr)
	}
	files, err := c.pullFiles(context.Background(), "acme-org", "sample-service", 42)
	if err != nil || len(files) != 1 {
		t.Fatalf("read the files: %v %+v", err, files)
	}
	if !strings.Contains(renderDiff(files), "a.go") {
		t.Errorf("the diff must name the file: %q", renderDiff(files))
	}
	if err := c.postReview(context.Background(), "acme-org", "sample-service", 42, "two findings", ghEventComment); err != nil {
		t.Fatalf("post the review: %v", err)
	}
	if !strings.Contains(posted, `"event":"COMMENT"`) {
		t.Errorf("the review must be a comment, never an approval: %s", posted)
	}
}

func TestGHPostReviewReportsTheStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(422)
	}))
	defer srv.Close()
	c := &ghClient{token: "ghp_token", base: srv.URL, hc: srv.Client(), logf: func(string, ...any) {}}
	err := c.postReview(context.Background(), "acme-org", "sample-service", 42, "x", ghEventComment)
	if err == nil || !strings.Contains(err.Error(), "422") {
		t.Fatalf("a refused review must be reported, got %v", err)
	}
}

func ghTestClient(srv *httptest.Server) *ghClient {
	return &ghClient{token: "ghp_token", base: srv.URL, logf: func(string, ...any) {},
		hc: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func TestGHFilesArePaginatedAndCapped(t *testing.T) {
	pages := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages++
		page := make([]ghFile, 100)
		if r.URL.Query().Get("page") == "3" {
			page = page[:7]
		}
		_ = json.NewEncoder(w).Encode(page)
	}))
	defer srv.Close()
	files, err := ghTestClient(srv).pullFiles(context.Background(), "o", "r", 1)
	if err != nil || len(files) != 207 || pages != 3 {
		t.Fatalf("got %d files over %d pages, err %v; want 207 over 3", len(files), pages, err)
	}
}

func TestGHErrorsAreClassifiedAndNeverCarryTheBody(t *testing.T) {
	for status, want := range map[int]string{401: "token", 403: "token", 404: "token", 422: "refused", 500: "error", 302: "redirect"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if status == 302 {
				w.Header().Set("Location", "http://example.invalid/")
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte("SECRET-RESPONSE-BODY"))
		}))
		err := ghTestClient(srv).postReview(context.Background(), "o", "r", 1, "x", ghEventComment)
		srv.Close()
		if err == nil {
			t.Fatalf("status %d was not an error", status)
		}
		if got := ghErrorClass(err); got != want {
			t.Errorf("status %d is class %q, want %q", status, got, want)
		}
		if strings.Contains(err.Error(), "SECRET-RESPONSE-BODY") || strings.Contains(err.Error(), "ghp_token") {
			t.Errorf("the error leaks: %v", err)
		}
	}
}

func TestGHRateLimitIsRetriedTwiceAndNoMore(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(429)
	}))
	defer srv.Close()
	err := ghTestClient(srv).postReview(context.Background(), "o", "r", 1, "x", ghEventComment)
	if ghErrorClass(err) != "rate" || calls != 3 {
		t.Errorf("class %q after %d calls; want rate after 3", ghErrorClass(err), calls)
	}
	// A 500 is not retried.
	calls = 0
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls++; w.WriteHeader(500) }))
	defer srv2.Close()
	_ = ghTestClient(srv2).postReview(context.Background(), "o", "r", 1, "x", ghEventComment)
	if calls != 1 {
		t.Errorf("a 500 was tried %d times, want once", calls)
	}
}

func TestGHCommitAuthorsAreLastFirst(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"author":{"login":"first"}},{"author":null},{"author":{"login":"last"}}]`))
	}))
	defer srv.Close()
	got, err := ghTestClient(srv).pullCommitAuthors(context.Background(), "o", "r", 1)
	if err != nil || len(got) != 2 || got[0] != "last" || got[1] != "first" {
		t.Errorf("got %v, %v; want [last first]", got, err)
	}
}

func TestAGitHubTokenFileEndingInBlankLinesIsAccepted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	tok := "ghp_" + strings.Repeat("a", 36)
	if err := os.WriteFile(path, []byte("\n  "+tok+" \r\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GAOS_GITHUB_TOKEN_FILE", path)
	cfg := ghConfigFromEnv()
	if cfg.why != "" || cfg.token != tok {
		t.Fatalf("a token with surrounding whitespace was refused or kept it: why=%q, len=%d", cfg.why, len(cfg.token))
	}
	if strings.ContainsAny(cfg.token, "\r\n \t") {
		t.Error("the token still carries whitespace and would make the Authorization header invalid")
	}
}
