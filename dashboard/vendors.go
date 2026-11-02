package main

import (
	"context"
	"fmt"
	"html/template"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// The product's vendor policy (M16, D-067, BR-45).
//
// A product chooses which of ai-manager's session providers runs its tasks:
// Claude CLI only, the Antigravity pool only, or both, chosen by the task's
// role. It is a per-PRODUCT choice of vendor and not a per-task choice of
// execution mode -- every attempt still goes through the gateway (BRD §7.6.1,
// D-055). A task's vendor is resolved when its packet is frozen, and changes
// only when a new session opens: ai-manager keeps a session with the provider
// that created it, so a policy edit never moves a turn in flight (BR-45).

// The vendors are ai-manager's session providers, by their ai-manager names.
const (
	VendorClaude      = "claude"
	VendorAntigravity = "antigravity"
)

// The policy modes.
const (
	VendorModeClaude      = "claude"
	VendorModeAntigravity = "antigravity"
	VendorModeMixed       = "mixed"
)

var vendorNames = []string{VendorClaude, VendorAntigravity}

// VendorPolicy is the product's choice. The zero value is Claude only, which
// is what every product created before M16 has always run on.
type VendorPolicy struct {
	Mode string `json:"mode,omitempty"`
	// Roles maps a role to its vendor in mixed mode; a role it does not name
	// runs on Claude.
	Roles map[string]string `json:"roles,omitempty"`
	// AgyModel is the model Antigravity turns ask for; "" is ai-manager's
	// default for the pool.
	AgyModel string `json:"agyModel,omitempty"`
}

// Clone deep-copies the role map.
func (p VendorPolicy) Clone() VendorPolicy {
	c := p
	if p.Roles != nil {
		c.Roles = make(map[string]string, len(p.Roles))
		for k, v := range p.Roles {
			c.Roles[k] = v
		}
	}
	return c
}

// ModeOrDefault is the mode, with "" read as Claude only.
func (p VendorPolicy) ModeOrDefault() string {
	if p.Mode == "" {
		return VendorModeClaude
	}
	return p.Mode
}

// VendorFor is the vendor a task of role runs on.
func (p VendorPolicy) VendorFor(role string) string {
	switch p.ModeOrDefault() {
	case VendorModeAntigravity:
		return VendorAntigravity
	case VendorModeMixed:
		if v := p.Roles[role]; v != "" {
			return v
		}
	}
	return VendorClaude
}

// ModelFor is the model a turn on vendor asks for: "" lets the adapter (for
// Claude, GAOS_MODEL) or ai-manager (for Antigravity) choose.
func (p VendorPolicy) ModelFor(vendor string) string {
	if vendor == VendorAntigravity {
		return p.AgyModel
	}
	return ""
}

// Vendors lists every vendor the policy can dispatch to, in vendorNames order.
// Each of them needs the product's workspace to hold the published Blueprint.
func (p VendorPolicy) Vendors() []string {
	in := map[string]bool{}
	switch p.ModeOrDefault() {
	case VendorModeAntigravity:
		in[VendorAntigravity] = true
	case VendorModeMixed:
		for _, r := range roleOrder {
			in[p.VendorFor(r)] = true
		}
	default:
		in[VendorClaude] = true
	}
	var out []string
	for _, v := range vendorNames {
		if in[v] {
			out = append(out, v)
		}
	}
	return out
}

// validateVendorPolicy checks the shape. models is the Antigravity model list
// the gateway offers; nil means it could not be asked, and then any model name
// is accepted -- the gateway refuses an unknown one with a 400 at the turn.
func validateVendorPolicy(p VendorPolicy, models []string) error {
	switch p.Mode {
	case VendorModeClaude, VendorModeAntigravity, VendorModeMixed:
	default:
		return reject(fmt.Sprintf("The vendor mode must be claude, antigravity or mixed, not %q.", p.Mode))
	}
	for role, v := range p.Roles {
		if _, ok := roles[role]; !ok {
			return reject(fmt.Sprintf("There is no role %q to assign a vendor to.", role))
		}
		if !slices.Contains(vendorNames, v) {
			return reject(fmt.Sprintf("The %s role's vendor must be claude or antigravity, not %q.", role, v))
		}
	}
	if p.AgyModel != "" && models != nil && !slices.Contains(models, p.AgyModel) {
		return reject(fmt.Sprintf("Antigravity does not offer the model %q; it offers %s.", p.AgyModel, strings.Join(models, ", ")))
	}
	return nil
}

// describe is the one-line summary the timeline and the toast use.
func (p VendorPolicy) describe() string {
	switch p.ModeOrDefault() {
	case VendorModeAntigravity:
		return "Antigravity only" + p.modelSuffix()
	case VendorModeMixed:
		var on []string
		for _, r := range roleOrder {
			if p.VendorFor(r) == VendorAntigravity {
				on = append(on, r)
			}
		}
		if len(on) == 0 {
			return "mixed by role, every role on Claude"
		}
		return "mixed by role, " + strings.Join(on, ", ") + " on Antigravity" + p.modelSuffix()
	}
	return "Claude CLI only"
}

func (p VendorPolicy) modelSuffix() string {
	if p.AgyModel == "" {
		return ""
	}
	return " (" + p.AgyModel + ")"
}

// SaveVendorPolicy replaces the product's vendor policy. It is a record edit,
// so it is a RecordUpdated like a Blueprint policy edit; it never touches a
// task in flight (BR-45).
func (s *MemStore) SaveVendorPolicy(ctx context.Context, id string, p VendorPolicy) (Result, error) {
	p.Mode, p.AgyModel = strings.TrimSpace(p.Mode), strings.TrimSpace(p.AgyModel)
	if p.Mode != VendorModeMixed {
		p.Roles = nil // a role map outside mixed mode would be dead data
	}
	s.mu.RLock()
	cat, _ := s.adapter.(VendorCatalog)
	s.mu.RUnlock()
	var models []string
	if cat != nil {
		if infos, err := cat.VendorInfo(ctx); err == nil {
			models = []string{}
			for _, in := range infos {
				if in.Name == VendorAntigravity {
					models = in.Models
				}
			}
		}
	}
	if err := validateVendorPolicy(p, models); err != nil {
		return Result{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.masterPtr(id)
	if m == nil {
		return Result{}, ErrNotFound
	}
	m.Vendors = p.Clone()
	e := s.emit(RecordUpdated, id, fmt.Sprintf("%s: AI vendors set to %s", s.productName(id), p.describe()), KindKnow, EventPayload{})
	toast := "AI vendors saved: " + p.describe() + ". Each task's next new session uses them."
	if _, gw := s.adapter.(WorkspaceGateway); gw {
		if v, ok := m.Blueprint.Latest(); ok {
			for _, vendor := range p.Vendors() {
				if m.Workspace.Holds(vendor).Digest != v.Digest {
					toast += " Sync the Blueprint so " + vendorLabel(vendor) + " holds it."
					break
				}
			}
		}
	}
	r := s.result(toast, e)
	c := m.Clone()
	r.Master = &c
	return r, nil
}

func (s *ArcadeStore) SaveVendorPolicy(ctx context.Context, id string, p VendorPolicy) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.SaveVendorPolicy(ctx, id, p) })
}

func (readOnly) SaveVendorPolicy(context.Context, string, VendorPolicy) (Result, error) {
	return Result{}, reject(replayRefusal)
}

// vendorForLocked resolves the vendor and model of a task on desk deskID with
// role. Caller holds s.mu.
func (s *MemStore) vendorForLocked(deskID, role string) (string, string) {
	var p VendorPolicy
	if m := s.masterPtr(deskID); m != nil {
		p = m.Vendors
	}
	v := p.VendorFor(role)
	return v, p.ModelFor(v)
}

// vendorLabel is the human name of a vendor.
func vendorLabel(v string) string {
	if v == VendorAntigravity {
		return "Antigravity"
	}
	return "Claude CLI"
}

// runVendor is the vendor a run went to. A gateway run recorded before M16
// carries no provider and went to Claude, which was then the only one.
func runVendor(r TaskRunRecord) string {
	if r.Provider == "" {
		return VendorClaude
	}
	return r.Provider
}

// ---------------------------------------------------------------- the catalog

// VendorInfo is one session provider as ai-manager reports it.
type VendorInfo struct {
	Name         string
	Available    bool
	DefaultModel string
	Models       []string
}

// VendorCatalog is what an adapter offers when it can say which vendors exist
// and which models they accept -- asserted by type, like WorkspaceGateway.
type VendorCatalog interface {
	VendorInfo(ctx context.Context) ([]VendorInfo, error)
}

// vendorCache keeps the gateway's provider list for a minute: the vendors pane
// and a policy save both read it, and neither should cost a round trip each.
type vendorCache struct {
	mu    sync.Mutex
	at    time.Time
	infos []VendorInfo
}

const vendorCacheTTL = 60 * time.Second

// VendorInfo implements VendorCatalog for ai-manager: its session providers,
// claude and antigravity, in that order.
func (a *AIMAdapter) VendorInfo(ctx context.Context) ([]VendorInfo, error) {
	a.vendors.mu.Lock()
	defer a.vendors.mu.Unlock()
	if a.vendors.infos != nil && time.Since(a.vendors.at) < vendorCacheTTL {
		return a.vendors.infos, nil
	}
	sc, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	all, err := a.Client.ProviderInfo(sc)
	if err != nil {
		return nil, err
	}
	out := make([]VendorInfo, 0, len(vendorNames))
	for _, name := range vendorNames {
		in := VendorInfo{Name: name}
		for _, p := range all {
			if p.Name == name {
				in = p
			}
		}
		sort.Strings(in.Models)
		out = append(out, in)
	}
	a.vendors.infos, a.vendors.at = out, time.Now()
	return out, nil
}

// runProvider is the vendor a run records (M16): the packet's, through a
// gateway that has vendors; "" for any other adapter, which sends nowhere.
func runProvider(ad Adapter, p Packet) string {
	if _, ok := ad.(TurnAdapter); !ok {
		return ""
	}
	if p.Vendor == "" {
		return VendorClaude
	}
	return p.Vendor
}

// ---------------------------------------------------------------- the pane

// VendorsPane is the "AI vendors" tab of a product (M16).
type VendorsPane struct {
	Mode     string // claude | antigravity | mixed
	AgyModel string
	Roles    []VendorRoleRow
	// Vendors is one row per vendor: is it there, and what does its copy of
	// the workspace hold.
	Vendors []VendorStatus
	// AgyModels is what the Antigravity pool accepts, and AgyDefault the
	// model it uses when none is named. CatalogErr is why neither is known.
	AgyModels  []string
	AgyDefault string
	CatalogErr string
	// Gateway: a workspace gateway is configured, so workspace status means
	// something.
	Gateway bool
	// Published is the latest Blueprint version (0 = none).
	Published int
}

// VendorRoleRow is one role in mixed mode.
type VendorRoleRow struct {
	Role, Label, Short string
	Style              template.CSS
	Vendor             string
}

// VendorStatus is one vendor's line.
type VendorStatus struct {
	Name, Label string
	Known       bool // the gateway was asked
	Available   bool
	InUse       bool // the policy dispatches to it
	Holds       int  // the Blueprint version its workspace copy holds
	Current     bool // ... and it is the published one
}

// buildVendorsPane assembles the tab from the master and, when the adapter
// has one, the gateway's provider catalog.
func buildVendorsPane(ctx context.Context, m Master, ad Adapter) *VendorsPane {
	p := m.Vendors
	vp := &VendorsPane{Mode: p.ModeOrDefault(), AgyModel: p.AgyModel}
	for _, r := range roleOrder {
		role := roles[r]
		v := VendorClaude
		if p.ModeOrDefault() == VendorModeMixed {
			v = p.VendorFor(r)
		} else if r != "architect" && r != "security" {
			// The starting point offered when the operator switches to mixed:
			// design and review on Claude, the rest on Antigravity.
			v = VendorAntigravity
		}
		vp.Roles = append(vp.Roles, VendorRoleRow{Role: r, Label: role.Label, Short: role.Short,
			Style: cssVar("background", role.Color), Vendor: v})
	}
	_, vp.Gateway = ad.(WorkspaceGateway)
	latest, hasLatest := m.Blueprint.Latest()
	if hasLatest {
		vp.Published = latest.N
	}
	var infos []VendorInfo
	if cat, ok := ad.(VendorCatalog); ok {
		var err error
		if infos, err = cat.VendorInfo(ctx); err != nil {
			vp.CatalogErr = "ai-manager could not be asked which vendors it has: " + err.Error()
		}
	} else {
		vp.CatalogErr = "No gateway is configured (GAOS_MODEL_MODE=aimanager), so no vendor can be asked."
	}
	inUse := map[string]bool{}
	for _, v := range p.Vendors() {
		inUse[v] = true
	}
	for _, name := range vendorNames {
		st := VendorStatus{Name: name, Label: vendorLabel(name), InUse: inUse[name]}
		for _, in := range infos {
			if in.Name == name {
				st.Known, st.Available = true, in.Available
				if name == VendorAntigravity {
					vp.AgyModels, vp.AgyDefault = in.Models, in.DefaultModel
				}
			}
		}
		h := m.Workspace.Holds(name)
		st.Holds = h.Version
		st.Current = hasLatest && h.Digest == latest.Digest
		vp.Vendors = append(vp.Vendors, st)
	}
	return vp
}

// vendorPolicyFromForm reads the pane's form: mode, one role.<role> per role,
// and agyModel.
func vendorPolicyFromForm(form map[string][]string) VendorPolicy {
	get := func(k string) string {
		if v := form[k]; len(v) > 0 {
			return strings.TrimSpace(v[0])
		}
		return ""
	}
	p := VendorPolicy{Mode: get("mode"), AgyModel: get("agyModel")}
	if p.Mode == VendorModeMixed {
		p.Roles = map[string]string{}
		for k := range form {
			if role, ok := strings.CutPrefix(k, "role."); ok {
				p.Roles[role] = get(k)
			}
		}
	}
	return p
}
