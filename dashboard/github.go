package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// The GitHub client (D-088, docs/designs/gaos-pr-agent-flows.md §7).
//
// GAOS has no GitHub client of its own: the GitHub webhook belongs to Brain
// Town. But the pull request's diff, body and author are NOT in Brain Town —
// it never stores the source payload — and the review agent needs all three.
// So GAOS reads them from GitHub itself, with its own token.
//
// Three rules shape this file:
//
//   - The credential is a FILE, mode 600, named by an environment variable.
//   - The repository comes from the PRODUCT, never from the event: a forged
//     event cannot make GAOS read a repository the product does not own.
//   - Everything read here is untrusted text: the pull request's title and
//     body and the diff are attacker-authored.

// The GitHub API's own kinds of credential, refused here for the same reason
// the Brain Town reader refuses a bt_live_ token: one credential must never be
// presented as another.
const (
	ghTokenPrefix      = "ghp_"
	ghTokenFineGrained = "github_pat_"
)

// ghPR is the pull request as GAOS needs it.
type ghPR struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	Draft   bool   `json:"draft"`
	Merged  bool   `json:"merged"`
	State   string `json:"state"`
	HTMLURL string `json:"html_url"`
	User    struct {
		Login string `json:"login"`
	} `json:"user"`
	AuthorAssociation string `json:"author_association"`
	Head              struct {
		Ref  string `json:"ref"`
		SHA  string `json:"sha"`
		Repo struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"head"`
	Base struct {
		Ref  string `json:"ref"`
		Repo struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"base"`
}

// ghFile is one changed file.
type ghFile struct {
	Filename  string `json:"filename"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Patch     string `json:"patch"`
}

// ghReview is the review GAOS posts.
type ghReview struct {
	Body  string `json:"body"`
	Event string `json:"event"` // COMMENT, always: GAOS never approves
}

// ghClient reads a pull request and posts a review.
type ghClient struct {
	token string
	base  string
	hc    *http.Client
	logf  func(string, ...any)
}

// ghConfig is the client's configuration.
type ghConfig struct {
	token string
	base  string
	why   string // non-empty: the client is unusable
}

// ghConfigFromEnv reads the two settings.
func ghConfigFromEnv() ghConfig {
	c := ghConfig{base: "https://api.github.com"}
	path := strings.TrimSpace(os.Getenv("GAOS_GITHUB_TOKEN_FILE"))
	if path == "" {
		c.why = "GAOS_GITHUB_TOKEN_FILE is not set"
		return c
	}
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		c.why = "the GitHub token file cannot be read"
		return c
	}
	if st.Mode().Perm()&0o077 != 0 {
		c.why = "the GitHub token file is readable by group or others; it must be mode 0600"
		return c
	}
	b, err := os.ReadFile(path)
	if err != nil {
		c.why = "the GitHub token file cannot be read"
		return c
	}
	// Every kind of surrounding whitespace goes, not just one newline: a token file
	// that ends with a blank line made the Authorization header invalid (found live).
	tok := strings.TrimSpace(string(b))
	switch {
	case tok == "":
		c.why = "the GitHub token file holds no token"
		return c
	case len(tok) < 20:
		// The length is checked before the kind: a short token is refused
		// whatever it starts with.
		c.why = "the GitHub token file holds no token of at least 20 characters"
		return c
	case strings.HasPrefix(tok, btConsumerTokenPrefix):
		c.why = "the token is a Brain Town reader credential; a GitHub client must hold its own"
		return c
	case strings.HasPrefix(tok, "bt_live_"):
		c.why = "the token is a Brain Town input credential; a GitHub client must hold its own"
		return c
	case strings.HasPrefix(tok, apiTokenPrefix):
		c.why = "the token is a GAOS credential; a GitHub client must hold its own"
		return c
	case strings.HasPrefix(tok, ghTokenPrefix), strings.HasPrefix(tok, ghTokenFineGrained):
		// a GitHub token; the kind the client expects
	default:
		c.why = "the token is not a GitHub token"
		return c
	}
	c.token = tok
	return c
}

// newGHClient builds the client. A nil client means the setting is off.
func newGHClient(cfg ghConfig) *ghClient {
	if cfg.token == "" {
		return nil
	}
	return &ghClient{
		token: cfg.token, base: strings.TrimSuffix(cfg.base, "/"), logf: log.Printf,
		hc: &http.Client{Timeout: 20 * time.Second,
			Transport: &http.Transport{DisableCompression: true},
			// A redirect is refused, as the QA client refuses one: a credential
			// must never follow a Location header.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
}

// ghOwnerRepo splits a product's repository entry into its owner and name.
// The entry is an "owner/repo" pair (dashboard/model.go:388).
func ghOwnerRepo(entry string) (string, string, bool) {
	parts := strings.Split(strings.TrimSpace(entry), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// pullRequest reads one pull request.
func (c *ghClient) pullRequest(ctx context.Context, owner, repo string, number int) (ghPR, error) {
	var pr ghPR
	u := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", c.base, url.PathEscape(owner), url.PathEscape(repo), number)
	if err := c.get(ctx, u, &pr); err != nil {
		return pr, err
	}
	return pr, nil
}

// ghMaxPages bounds the file listing: a pull request of more than this many
// hundred files is reviewed on the first pages and says so.
const ghMaxPages = 30

// pullFiles reads the changed files of a pull request, page by page.
func (c *ghClient) pullFiles(ctx context.Context, owner, repo string, number int) ([]ghFile, error) {
	var all []ghFile
	for page := 1; page <= ghMaxPages; page++ {
		var files []ghFile
		u := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/files?per_page=100&page=%d", c.base, url.PathEscape(owner), url.PathEscape(repo), number, page)
		if err := c.get(ctx, u, &files); err != nil {
			return nil, err
		}
		all = append(all, files...)
		if len(files) < 100 {
			break
		}
	}
	return all, nil
}

// pullCommitAuthors reads the logins of a pull request's commits, last first.
// It is how a failed test finds the last committer's contact.
func (c *ghClient) pullCommitAuthors(ctx context.Context, owner, repo string, number int) ([]string, error) {
	var commits []struct {
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
	}
	u := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/commits?per_page=100", c.base, url.PathEscape(owner), url.PathEscape(repo), number)
	if err := c.get(ctx, u, &commits); err != nil {
		return nil, err
	}
	var out []string
	for i := len(commits) - 1; i >= 0; i-- {
		if l := commits[i].Author.Login; l != "" {
			out = append(out, l)
		}
	}
	return out, nil
}

// commitAuthor reads one commit's author login (a staging deploy has a commit
// and no pull request).
func (c *ghClient) commitAuthor(ctx context.Context, owner, repo, sha string) (string, error) {
	var cm struct {
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
	}
	u := fmt.Sprintf("%s/repos/%s/%s/commits/%s", c.base, url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(sha))
	if err := c.get(ctx, u, &cm); err != nil {
		return "", err
	}
	return cm.Author.Login, nil
}

// postReview posts a review comment. GAOS never approves: the event is
// always COMMENT, so the pull request's author still decides.
func (c *ghClient) postReview(ctx context.Context, owner, repo string, number int, body string) error {
	u := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/reviews", c.base, url.PathEscape(owner), url.PathEscape(repo), number)
	b, err := json.Marshal(ghReview{Body: body, Event: "COMMENT"})
	if err != nil {
		return err
	}
	_, err = c.do(ctx, http.MethodPost, u, b)
	return err
}

// ghMaxBody bounds what is read from GitHub: a diff can be large, and the
// dashboard's memory is not.
const ghMaxBody = 4 << 20

// ghError is a GitHub answer that was not a success. It carries the status and
// never the response body.
type ghError struct {
	Status int
	Retry  time.Duration
}

func (e *ghError) Error() string { return fmt.Sprintf("github answered %d", e.Status) }

// ghErrorClass names a failure for a log line and a card's reason: the class,
// never the body (§7).
func ghErrorClass(err error) string {
	var ge *ghError
	if !errors.As(err, &ge) {
		return "transport"
	}
	switch {
	case ge.Status == 401, ge.Status == 403, ge.Status == 404:
		return "token" // the token may not write to that repository
	case ge.Status == 422:
		return "refused" // GitHub refused the review (a closed pull request, say)
	case ge.Status == 429:
		return "rate"
	case ge.Status >= 300 && ge.Status < 400:
		return "redirect"
	}
	return "error"
}

// do sends one request. A rate-limit answer is retried at most twice, after the
// wait GitHub names (capped), and nothing else is retried.
func (c *ghClient) do(ctx context.Context, method, u string, body []byte) ([]byte, error) {
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		var rd io.Reader
		if body != nil {
			rd = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, u, rd)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.hc.Do(req)
		if err != nil {
			return nil, err
		}
		out, rerr := io.ReadAll(io.LimitReader(resp.Body, ghMaxBody))
		resp.Body.Close()
		if rerr != nil {
			return nil, rerr
		}
		if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
			return out, nil
		}
		ge := &ghError{Status: resp.StatusCode}
		limited := resp.StatusCode == 429 || (resp.StatusCode == 403 && resp.Header.Get("X-RateLimit-Remaining") == "0")
		if !limited || attempt == 2 {
			return nil, ge
		}
		wait := 2 * time.Second
		if n, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && n >= 0 {
			wait = time.Duration(n) * time.Second
		}
		if wait > 10*time.Second {
			wait = 10 * time.Second
		}
		last = ge
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	return nil, last
}

func (c *ghClient) get(ctx context.Context, u string, out any) error {
	b, err := c.do(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

// pathOf is a URL's path, without the host and without the query. It is safe
// to log: it names a repository and a number, never a credential.
func pathOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Path
}

// ghDiff is the diff as one text, capped.
const ghDiffMax = 256 << 10

// renderDiff joins the changed files into one text the agent can read.
func renderDiff(files []ghFile) string {
	var sb strings.Builder
	for _, f := range files {
		if sb.Len() > ghDiffMax {
			break
		}
		sb.WriteString("--- " + f.Filename + " (" + f.Status + ")\n")
		if f.Patch != "" {
			sb.WriteString(f.Patch)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}
