package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// The version pin and the boot smoke test. D-049's standing obligation:
//
//	"the pin is worthless without the boot smoke test §7.6.3 already promises,
//	 and that lands with T-057 rather than before it."
//
// Why it exists at all is §7.6.2's interface-stability row: a CLI's "flags,
// defaults and output formats move between releases with no deprecation
// window". An adapter running against a version it was never measured on does
// not fail cleanly -- it produces WRONG EVIDENCE, which is worse than an error,
// because the evidence is what the whole audit trail is made of.
//
// Everything here is pure. The one function that runs a binary is
// CLIAdapter.Smoke in adapter_cli.go; this file is what it compares against.

// Semver is the three numbers, and nothing else. No pre-release, no build
// metadata: the pin grammar in §7.6.1 does not use them, and a parser that
// accepted them would need an ordering rule nobody has written down.
type Semver struct{ Major, Minor, Patch int }

func (v Semver) String() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch) }

func (v Semver) Less(o Semver) bool {
	if v.Major != o.Major {
		return v.Major < o.Major
	}
	if v.Minor != o.Minor {
		return v.Minor < o.Minor
	}
	return v.Patch < o.Patch
}

// VersionPin is §7.6.1's `pin: ">=2.0.0 <3.0.0"`.
//
// Min inclusive, Max exclusive -- the two operators that string actually uses,
// and no others. A pin grammar that also accepted ~ and ^ would need a
// semantics document nobody wrote, and an operator guessing at which one this
// implements is an operator who has pinned something other than what they meant.
type VersionPin struct {
	Raw    string
	Min    Semver
	Max    Semver
	HasMin bool
	HasMax bool
}

func (p VersionPin) String() string { return p.Raw }

// Permits reports whether v satisfies the pin.
func (p VersionPin) Permits(v Semver) bool {
	if p.HasMin && v.Less(p.Min) {
		return false
	}
	if p.HasMax && !v.Less(p.Max) {
		return false
	}
	return true
}

var pinTerm = regexp.MustCompile(`^(>=|<)(\d+)\.(\d+)\.(\d+)$`)

// ParsePin reads ">=2.0.0 <3.0.0".
func ParsePin(s string) (VersionPin, error) {
	p := VersionPin{Raw: strings.TrimSpace(s)}
	if p.Raw == "" {
		return VersionPin{}, fmt.Errorf("empty version pin")
	}
	for _, term := range strings.Fields(p.Raw) {
		m := pinTerm.FindStringSubmatch(term)
		if m == nil {
			return VersionPin{}, fmt.Errorf("version pin %q: %q is not >=x.y.z or <x.y.z", s, term)
		}
		v := Semver{atoiOr0(m[2]), atoiOr0(m[3]), atoiOr0(m[4])}
		switch m[1] {
		case ">=":
			p.Min, p.HasMin = v, true
		case "<":
			p.Max, p.HasMax = v, true
		}
	}
	if !p.HasMin && !p.HasMax {
		return VersionPin{}, fmt.Errorf("version pin %q constrains nothing", s)
	}
	return p, nil
}

var versionTriple = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// ParseVersion reads the first dotted triple out of a tool's --version output.
//
// Measured against @anthropic-ai/claude-code@2.1.238 (D-049), whose output is
// "2.1.238 (Claude Code)".
//
// Deliberately lenient about what surrounds the triple and strict about the
// triple itself: §7.6.3 says a CLI's output format moves between releases
// without notice, so the banner around the number is the part most likely to
// change and the number is the part that must never be guessed at.
//
// NO TRIPLE AT ALL IS AN ERROR, NEVER A ZERO. A zero version compares as below
// any minimum, so it would look like drift and send the operator hunting a
// version problem that is really a changed banner -- and against a pin with no
// minimum a zero would silently PASS, which is the same bug pointed the other
// way.
func ParseVersion(out string) (Semver, error) {
	m := versionTriple.FindStringSubmatch(out)
	if m == nil {
		return Semver{}, fmt.Errorf("no x.y.z version in tool output %q", firstLine(out))
	}
	return Semver{atoiOr0(m[1]), atoiOr0(m[2]), atoiOr0(m[3])}, nil
}

// pinDriftRefusal is the one sentence an operator sees when the tool has moved.
// Written once so the reason is the same wherever it is hit, and it names all
// three of the tool, the version and the pin, because otherwise a drift and a
// missing binary read identically.
const pinDriftRefusal = "%s reports version %s; the pin %q does not permit it. Refusing to start. " +
	"§7.6.3 pins the CLI per release because a provider tool's flags, defaults and output formats " +
	"move between releases with no deprecation window, and an adapter running against a version it " +
	"was not measured on produces wrong evidence rather than an error."

// checkPin is the comparison half of the smoke test, split out so it can be
// tested without running anything.
func checkPin(tool string, pin VersionPin, out string) (Semver, error) {
	v, err := ParseVersion(out)
	if err != nil {
		return Semver{}, fmt.Errorf("%s: %w", tool, err)
	}
	if !pin.Permits(v) {
		return v, fmt.Errorf(pinDriftRefusal, tool, v, pin.Raw)
	}
	return v, nil
}

func atoiOr0(s string) int { n, _ := strconv.Atoi(s); return n }

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
