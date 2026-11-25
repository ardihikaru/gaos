package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// The auto-fix side of the GitHub client (D-091, design §7.1, §8.3, §10).
//
// This file holds the READ-ONLY methods the defect context needs: the default
// branch's head, a code search and a file at a pinned commit. They use the read
// client's do(), so they inherit its rules: the token is a file, a redirect is
// refused and never followed, and an answer's body never reaches an error. The
// write methods (blobs, trees, refs, pull requests) land with the fix phase;
// for now the same token loader serves both.

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
func (c *ghClient) defaultHead(ctx context.Context, owner, repo string) (string, error) {
	var r struct {
		DefaultBranch string `json:"default_branch"`
	}
	base := fmt.Sprintf("%s/repos/%s/%s", c.base, url.PathEscape(owner), url.PathEscape(repo))
	if err := c.get(ctx, base, &r); err != nil {
		return "", err
	}
	if r.DefaultBranch == "" {
		return "", errors.New("the repository names no default branch")
	}
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := c.get(ctx, base+"/git/ref/heads/"+escPath(r.DefaultBranch), &ref); err != nil {
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
