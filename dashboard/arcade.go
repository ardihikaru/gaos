package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// The ArcadeDB leg -- reachability only, in this pass.
//
// The dashboard does not query the graph yet. What it does do is tell you,
// through /healthz, whether the database it WILL query is reachable from inside
// the container, on the network the compose file put it on, with the
// credentials the env file supplied. That is the check worth having first,
// because the three ways this goes wrong -- wrong external network name, wrong
// container alias, wrong password -- all produce a stack that starts cleanly
// and looks completely healthy.
//
// The endpoint is GET {url}/api/v1/ready. MEASURED against both live instances
// on 2026-08-07: it answers 204 No Content, NOT 200. A check written against
// 200 reports every healthy database as unreachable, and you go looking at the
// network for an hour.
//
// Instances on this host:
//
//	dev  http://arcadedb-master-dev:2480   network master-compose-ai-dev-net
//	prod http://arcadedb-master:2480       network master-compose-ai-net
//
// Unset GAOS_ARCADE_URL means "not configured", which is the local stack and a
// perfectly valid state -- see handleHealth for why that stays a 200.
type Arcade struct {
	URL      string
	DB       string
	User     string
	Password string
	client   *http.Client
}

func ArcadeFromEnv() *Arcade {
	return &Arcade{
		URL:      strings.TrimRight(os.Getenv("GAOS_ARCADE_URL"), "/"),
		DB:       envOr("GAOS_ARCADE_DB", "gaos"),
		User:     os.Getenv("GAOS_ARCADE_USER"),
		Password: os.Getenv("GAOS_ARCADE_PASSWORD"),
		client:   &http.Client{Timeout: 4 * time.Second},
	}
}

func (a *Arcade) Configured() bool { return a != nil && a.URL != "" }

// Describe is for the boot log. It never prints the password, and it never
// prints the user either -- a boot line is the easiest thing in the world to
// paste into an issue.
func (a *Arcade) Describe() string {
	if !a.Configured() {
		return "unconfigured"
	}
	return a.URL + "/" + a.DB
}

// Probe returns the database name on success, or "unreachable" with the reason.
func (a *Arcade) Probe(ctx context.Context) (string, error) {
	if !a.Configured() {
		return "unconfigured", nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL+"/api/v1/ready", nil)
	if err != nil {
		return "unreachable", err
	}
	if a.User != "" {
		req.SetBasicAuth(a.User, a.Password)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return "unreachable", err
	}
	defer resp.Body.Close()

	// 204 is the healthy answer. 200 is accepted too, so that a future ArcadeDB
	// that starts sending a body does not read as an outage.
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return "unreachable", fmt.Errorf("ready probe returned %d", resp.StatusCode)
	}
	return a.DB, nil
}
