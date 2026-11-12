package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The master configuration. T-059, requirement D-12, BRD §7.6.1.
//
// "Which mode runs is a deployment decision held in one place, not a per-call
// choice scattered through the code. One file, read at boot, and every routing
// decision resolves against it."
//
// Three properties of that file are requirements rather than conveniences, and
// two of them are enforced here:
//
//   - ONE SWITCH, DEPLOYMENT-WIDE. A per-task mode override is deliberately not
//     offered, "because it would make two attempts at the same task
//     incomparable and quietly break the fourth replay scope in H-4". There is
//     no per-task field anywhere in this file, and that is the enforcement.
//   - THE EGRESS BLOCK IS NOT PER-MODE. EgressPolicy hangs off the config, not
//     off an adapter, so L-4 "runs identically whether a subprocess or an HTTP
//     request is about to carry the slice out".
//
// WHY ENVIRONMENT VARIABLES RATHER THAN config/model.yaml. §7.6.1's requirement
// is "one place, read at boot", not "one file"; the repository is stdlib-only
// by house rule (see the header of main.go) and the standard library has no
// YAML parser. The variable names below map one-to-one onto the document's keys
// so the mapping is obvious, and the deviation is written down here rather than
// discovered. If a YAML dependency is ever accepted, this file is the only one
// that changes.

// ModelConfig is §7.6.1's file, as a value.
type ModelConfig struct {
	// Mode is aimanager | api | off. "off" is the honest default -- an
	// unconfigured adapter, the local stack -- and what every deployment gets
	// on upgrade. "cli" was retired in M14.7 and is refused with the reason.
	Mode string

	Model    string // passed to the gateway as the model; ai-manager's documented defaults disagree
	Provider string
	Local    bool // true when the model runs on the operator's own host (L-6)

	Egress EgressPolicy

	// AIM is the gateway's settings, read only when Mode is ModeAIM.
	AIM AIMConfig
}

// AIMConfig configures the ai-manager gateway (architecture/gateway.html
// §configuration). Secrets are FILE paths, never values, so they cannot leak
// through a process listing or a crash dump.
type AIMConfig struct {
	URL           string        // GAOS_AIM_URL
	ChatAuthFile  string        // GAOS_AIM_CHAT_AUTH_FILE: {"login_id","secret_key"}
	AdminKeyFile  string        // GAOS_AIM_ADMIN_KEY_FILE: provisioning only (M14.4)
	Tenant        string        // GAOS_AIM_TENANT: names workspaces gaos-<tenant>-<product>
	PollEvery     time.Duration // GAOS_AIM_POLL_MS
	TurnDeadline  time.Duration // GAOS_AIM_TURN_DEADLINE (seconds)
	SessionMaxAge time.Duration // GAOS_AIM_SESSION_MAX_AGE (days)
}

const (
	ModeOff = "off"
	ModeCLI = "cli" // retired in M14.7; selectAdapter refuses it with the reason
	ModeAPI = "api"
	ModeAIM = "aimanager" // D-055: one turn per attempt through ai-manager
)

// The documented defaults, from §7.6.1's own example block.
const (
	defaultMaxBytes  = 256 << 10
	defaultPartBytes = 64 << 10
)

// ModelConfigFromEnv reads the configuration once, at boot.
func ModelConfigFromEnv() ModelConfig {
	c := ModelConfig{
		Mode:     strings.ToLower(envOr("GAOS_MODEL_MODE", ModeOff)),
		Model:    envOr("GAOS_MODEL", ""),
		Provider: envOr("GAOS_PROVIDER", "anthropic"),
		Local:    envBool("GAOS_MODEL_LOCAL", false),

		Egress: EgressPolicy{
			// nil Redact arms every detector. See the field comment in
			// egress.go: the safe default for a forgotten control is on.
			Redact:       envList("GAOS_EGRESS_REDACT", nil),
			AllowPaths:   envList("GAOS_EGRESS_ALLOW_PATHS", []string{"src/**", "tests/**"}),
			DenyPaths:    envList("GAOS_EGRESS_DENY_PATHS", nil),
			LocalOnly:    envBool("GAOS_EGRESS_LOCAL_ONLY", false),
			MaxBytes:     envInt("GAOS_EGRESS_MAX_BYTES", defaultMaxBytes),
			MaxPartBytes: envInt("GAOS_EGRESS_MAX_PART_BYTES", defaultPartBytes),
		},

		AIM: AIMConfig{
			URL:           envOr("GAOS_AIM_URL", ""),
			ChatAuthFile:  envOr("GAOS_AIM_CHAT_AUTH_FILE", ""),
			AdminKeyFile:  envOr("GAOS_AIM_ADMIN_KEY_FILE", ""),
			Tenant:        envOr("GAOS_AIM_TENANT", envOr("GAOS_ARCADE_TENANT", "local")),
			PollEvery:     time.Duration(envInt("GAOS_AIM_POLL_MS", 2000)) * time.Millisecond,
			TurnDeadline:  time.Duration(envInt("GAOS_AIM_TURN_DEADLINE", 660)) * time.Second,
			SessionMaxAge: time.Duration(envInt("GAOS_AIM_SESSION_MAX_AGE", 25)) * 24 * time.Hour,
		},
	}
	return c
}

// selectAdapter builds the adapter this deployment runs, or nil.
//
// It returns an error rather than falling back, for the same reason
// selectStore does: a dashboard asked for a model gateway that quietly ran
// without one would accept a dispatch and produce evidence nobody can trust.
//
// nil, nil is a VALID answer -- mode: off is the local stack and the state
// every existing deployment is in today. handleHealth has always treated an
// unconfigured dependency as healthy, and this is the same judgement.
func selectAdapter(c ModelConfig) (Adapter, error) {
	switch c.Mode {
	case "", ModeOff:
		return nil, nil

	case ModeCLI:
		// Retired in M14.7 (D-055, D-060): GAOS no longer runs a model tool
		// itself. Refused loudly rather than mapped to the gateway, because an
		// operator who asked for one and got the other has had where their
		// code goes changed without being told.
		return nil, fmt.Errorf("GAOS_MODEL_MODE=cli was retired in v0.29.0 (D-055): GAOS no longer spawns a model tool; use GAOS_MODEL_MODE=aimanager")

	case ModeAIM:
		return aimAdapterFor(c)

	case ModeAPI:
		// T-058. D-036 makes API mode a first-class supported configuration
		// rather than a hypothetical exit, so this refuses loudly instead of
		// silently downgrading to CLI -- an operator who asked for API mode and
		// got CLI mode has had their cost model changed without being told.
		return nil, fmt.Errorf("GAOS_MODEL_MODE=api: the API adapter is T-058 and is not built")

	default:
		return nil, fmt.Errorf("GAOS_MODEL_MODE=%q: want aimanager, api or off", c.Mode)
	}
}

// ---------- tiny env helpers ----------

func envList(k string, def []string) []string {
	v := strings.TrimSpace(envOr(k, ""))
	if v == "" {
		return def
	}
	var out []string
	for _, p := range strings.Split(v, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func envBool(k string, def bool) bool {
	v := strings.TrimSpace(envOr(k, ""))
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func envInt(k string, def int) int {
	v := strings.TrimSpace(envOr(k, ""))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return def
	}
	return n
}

// aimAdapterFor builds the gateway adapter, refusing to start half-configured:
// a gateway mode with no URL or no login is a deployment error, not a run to
// fail later.
func aimAdapterFor(c ModelConfig) (Adapter, error) {
	if strings.TrimSpace(c.AIM.URL) == "" {
		return nil, fmt.Errorf("GAOS_MODEL_MODE=aimanager but GAOS_AIM_URL is empty")
	}
	if c.AIM.ChatAuthFile == "" {
		return nil, fmt.Errorf("GAOS_MODEL_MODE=aimanager but GAOS_AIM_CHAT_AUTH_FILE is empty")
	}
	creds, err := LoadAIMCredentials(c.AIM.ChatAuthFile, c.AIM.AdminKeyFile)
	if err != nil {
		return nil, err
	}
	return &AIMAdapter{
		Client:   &AIMClient{Base: c.AIM.URL, Creds: creds},
		Model:    c.Model,
		Provider: c.Provider,
		Local:    c.Local,
		Tenant:   c.AIM.Tenant,
		PollEvery: c.AIM.PollEvery, Deadline: c.AIM.TurnDeadline, MaxSessionAge: c.AIM.SessionMaxAge,
	}, nil
}
