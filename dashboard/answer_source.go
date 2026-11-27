package main

import (
	"bufio"
	"io"
	"net"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
)

// Where an answer came from (D-083, BR-57, docs/designs/answer-source.md).
//
// Every answer -- a task question answered in the back office, or an inbox
// item answered in the back office or by the machine client that asked it --
// records an AnswerSource on its existing event (QuestionAnswered,
// ExternalQuestionAnswered). Everything but Actor is derived on the server from
// the session or the bearer token; Actor is the session's email, or the handle
// a machine client reported.

const (
	SourceKindGAOS   = "gaos"
	SourceKindQATool = "qa-tool"

	ClientBackOffice = "back-office"
	ClientQATools    = "qa-tools"
)

// AnswerSource is who answered, through what.
type AnswerSource struct {
	Kind   string `json:"kind"`   // gaos | qa-tool | a registry client's kind
	Client string `json:"client"` // back-office | qa-tools | a registry client's name
	Origin string `json:"origin"` // a host name or the back office's label; "" when not configured
	Actor  string `json:"actor"`  // the session email, or the client's reported handle
}

// recorded is the one rule both answer paths use: a source with no kind is no
// source (an old event, a test, a caller that had none).
func (a *AnswerSource) recorded() bool { return a != nil && a.Kind != "" }

// sourceOrNil is what an answer event stores.
func sourceOrNil(a AnswerSource) *AnswerSource {
	if a.Kind == "" {
		return nil
	}
	return &a
}

// Phrase is the source in words: "in GAOS (gaos.example) by agent@x",
// "via QA tool (qa-tool.example) by ardi", "via hikaru-app (…) by …". "" when
// nothing was recorded; the caller prints its own sentence for that.
func (a *AnswerSource) Phrase() string {
	if !a.recorded() {
		return ""
	}
	var b strings.Builder
	switch {
	case a.Client == ClientBackOffice || a.Kind == SourceKindGAOS:
		b.WriteString("in GAOS")
	case a.Client == ClientQATools:
		b.WriteString("via QA tool")
	default:
		b.WriteString("via " + a.Client)
	}
	if a.Origin != "" {
		b.WriteString(" (" + a.Origin + ")")
	}
	if a.Actor != "" {
		b.WriteString(" by " + a.Actor)
	}
	return b.String()
}

// answeredSentence is the refusal: who got there first.
func answeredSentence(a *AnswerSource) string {
	if p := a.Phrase(); p != "" {
		return "That question was already answered " + p + "."
	}
	return "That question was already answered (source not recorded)."
}

// recentSentence is the "Answered recently" row.
func recentSentence(a *AnswerSource) string {
	if p := a.Phrase(); p != "" {
		return "answered " + p
	}
	return "answered · source not recorded"
}

// sheetSentence is the conversation sheet's, printed after "Answered <x> ·".
func sheetSentence(a *AnswerSource) string {
	if p := a.Phrase(); p != "" {
		return p
	}
	return "source not recorded"
}

// textSuffix is what an answer event's text gains: nothing without a source.
func textSuffix(a *AnswerSource) string {
	if p := a.Phrase(); p != "" {
		return " — " + p
	}
	return ""
}

// eventActor fills Event.actor: the email for the back office, client/handle
// for a machine -- the prefix marks an identity GAOS did not verify.
func eventActor(a *AnswerSource) string {
	if !a.recorded() || a.Actor == "" {
		return ""
	}
	if a.Kind == SourceKindGAOS {
		return a.Actor
	}
	return a.Client + "/" + a.Actor
}

// apiSource is the source as the machine API returns it.
type apiSource struct {
	Kind   string `json:"kind"`
	Client string `json:"client"`
	Origin string `json:"origin"`
	Actor  string `json:"actor"`
}

// apiSourceOf never hands a back-office account to a machine: the email is the
// sign-in identifier (BR-55), so a GAOS answer's actor reads "operator".
func apiSourceOf(a *AnswerSource) *apiSource {
	if !a.recorded() {
		return nil
	}
	out := &apiSource{Kind: a.Kind, Client: a.Client, Origin: a.Origin, Actor: a.Actor}
	if a.Kind == SourceKindGAOS {
		out.Actor = "operator"
	}
	return out
}

// source turns the redacted API form back into an AnswerSource, for the
// sentences the API sends.
func (s *apiSource) source() *AnswerSource {
	if s == nil {
		return nil
	}
	return &AnswerSource{Kind: s.Kind, Client: s.Client, Origin: s.Origin, Actor: s.Actor}
}

// ---------- origins and the client registry ----------

// originPattern is a host name, optionally with a port.
var originPattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?(:[0-9]{1,5})?$`)

// qaOrigin resolves the QA tool's origin: GAOS_QA_ORIGIN when it is a host
// name, else the host of GAOS_QA_URL when that is a name rather than an IP
// literal, else "".
func qaOrigin(env, qaURL string, logf func(string, ...any)) string {
	if env = strings.TrimSpace(env); env != "" {
		if originPattern.MatchString(env) {
			return env
		}
		logf("api: GAOS_QA_ORIGIN is not a host name (ignored)")
	}
	u, err := url.Parse(strings.TrimSpace(qaURL))
	if err != nil {
		return ""
	}
	h := u.Hostname()
	if h == "" || net.ParseIP(h) != nil || !originPattern.MatchString(h) {
		return ""
	}
	return h
}

var clientNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)

var (
	reservedClientNames = map[string]bool{ClientQATools: true, ClientBackOffice: true, "admin": true,
		"gaos": true, "qa-tool": true, "operator": true}
	reservedClientKinds = map[string]bool{SourceKindGAOS: true, SourceKindQATool: true}
)

// parseClients reads the clients file (GAOS_API_CLIENTS_FILE): one client per
// line, "name kind origin token-file products", # comments. A bad line is
// skipped and logged by number and reason, never by token. Collisions with the
// built-in tokens are addClients' job.
func parseClients(r io.Reader, readToken func(path string) string, logf func(string, ...any)) []*apiClient {
	var out []*apiClient
	seen := map[string]bool{}
	sc := bufio.NewScanner(r)
	n := 0
	for sc.Scan() {
		n++
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		skip := func(why string) { logf("api: clients file line %d: %s (skipped)", n, why) }
		if len(f) != 5 && len(f) != 6 {
			skip("a line is name kind origin token-file products [caps]")
			continue
		}
		name, kind, origin, path, prods := f[0], f[1], f[2], f[3], f[4]
		caps := map[string]bool{CapInbox: true}
		if len(f) == 6 {
			caps = map[string]bool{}
			bad := ""
			for _, c := range strings.Split(f[5], ",") {
				if c = strings.TrimSpace(c); c == "" {
					continue
				} else if !validCaps[c] {
					bad = c
					break
				}
				caps[c] = true
			}
			if bad != "" || len(caps) == 0 {
				skip("unknown capability " + bad)
				continue
			}
		}
		switch {
		case !clientNamePattern.MatchString(name):
			skip("the name is 2 to 32 of a-z 0-9 - and starts with a letter")
			continue
		case reservedClientNames[name]:
			skip("the name " + name + " is reserved")
			continue
		case seen[name]:
			skip("the name " + name + " is used twice")
			continue
		case !clientNamePattern.MatchString(kind):
			skip("the kind is 2 to 32 of a-z 0-9 - and starts with a letter")
			continue
		case reservedClientKinds[kind]:
			skip("the kind " + kind + " is reserved")
			continue
		}
		if origin == "-" {
			origin = ""
		} else if !originPattern.MatchString(origin) {
			skip("the origin is not a host name")
			continue
		}
		tok := readToken(path)
		if tok == "" {
			skip("the token file is unreadable, empty, malformed or readable by others")
			continue
		}
		products := map[string]bool{}
		if prods != "-" {
			for _, id := range strings.Split(prods, ",") {
				if id = strings.TrimSpace(id); id != "" {
					products[id] = true
				}
			}
		}
		seen[name] = true
		out = append(out, &apiClient{Name: name, Kind: kind, Origin: origin, sum: sumOf(tok),
			products: products, busy: new(atomic.Bool), scope: scopeClient, caps: caps})
	}
	return out
}
