package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// The provisioning half of the ai-manager client (M14.4). These are the only
// calls that use the admin key; chat never does (TC-284). Wire shapes:
// apps/ai-manager/docs/workspace-management.md §9.

// WorkspaceFile is one managed instruction file as the gateway receives it.
type WorkspaceFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// WorkspaceManifest is the worker's view of a provisioned workspace: names,
// sizes and hashes, never contents. Digest is the formula BlueprintDigest
// reproduces, so GAOS can compare without asking for contents.
type WorkspaceManifest struct {
	ID     string `json:"id"`
	Digest string `json:"manifest_digest"`
	Files  []struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"files"`
	Created bool `json:"created"`
}

// ErrNotProvisioned: the worker has no such provisioned workspace (404).
var ErrNotProvisioned = fmt.Errorf("ai-manager: the workspace is not provisioned (404)")

func (c *AIMClient) admin(ctx context.Context, method, path string, body any) (int, []byte, error) {
	if c.Creds.AdminKey == "" {
		return 0, nil, fmt.Errorf("ai-manager %s %s: no admin key (GAOS_AIM_ADMIN_KEY_FILE)", method, path)
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.Base, "/")+path, rd)
	if err != nil {
		return 0, nil, fmt.Errorf("ai-manager %s %s: %w", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Creds.AdminKey)
	resp, err := c.client().Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %s %s unreachable", ErrGatewayTransient, method, path)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, out, nil
}

// Provision creates the workspace on the worker and enables it for chat.
// Idempotent: an existing workspace answers 200 and changes nothing.
func (c *AIMClient) Provision(ctx context.Context, id string, files []WorkspaceFile) (WorkspaceManifest, error) {
	status, body, err := c.admin(ctx, http.MethodPost, "/admin/workspaces/provision",
		map[string]any{"id": id, "files": files, "active": true})
	if err != nil {
		return WorkspaceManifest{}, err
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return WorkspaceManifest{}, statusError(status, body)
	}
	var r struct {
		Workspace WorkspaceManifest `json:"workspace"`
	}
	if json.Unmarshal(body, &r) != nil {
		return WorkspaceManifest{}, fmt.Errorf("ai-manager provision: unexpected reply")
	}
	return r.Workspace, nil
}

// Files reads a provisioned workspace's manifest.
func (c *AIMClient) Files(ctx context.Context, id string) (WorkspaceManifest, error) {
	status, body, err := c.admin(ctx, http.MethodGet, "/admin/workspaces/"+url.PathEscape(id)+"/files", nil)
	if err != nil {
		return WorkspaceManifest{}, err
	}
	if status == http.StatusNotFound {
		return WorkspaceManifest{}, ErrNotProvisioned
	}
	if status != http.StatusOK {
		return WorkspaceManifest{}, statusError(status, body)
	}
	var m WorkspaceManifest
	return m, json.Unmarshal(body, &m)
}

// PutFiles replaces the managed files; with prune the folder holds exactly
// these. ifMatch makes the write conditional on the current digest (412).
func (c *AIMClient) PutFiles(ctx context.Context, id string, files []WorkspaceFile, prune bool, ifMatch string) (WorkspaceManifest, error) {
	status, body, err := c.admin(ctx, http.MethodPut, "/admin/workspaces/"+url.PathEscape(id)+"/files",
		map[string]any{"files": files, "prune": prune, "if_match": ifMatch})
	if err != nil {
		return WorkspaceManifest{}, err
	}
	if status != http.StatusOK {
		return WorkspaceManifest{}, statusError(status, body)
	}
	var m WorkspaceManifest
	return m, json.Unmarshal(body, &m)
}
