package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The version pin and the boot smoke test. D-049's standing obligation: "the
// pin is worthless without the boot smoke test §7.6.3 already promises."
//
// Everything here is pure, so none of it runs the real tool. The exec half is
// exercised in adapter_test.go against the fake CLI.

func fixtureVersion(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "version", name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return string(b)
}

// TestTheVersionParserReadsTheCapturedOutput is measured, not guessed: the
// fixture is what @anthropic-ai/claude-code@2.1.238 actually prints (D-049).
func TestTheVersionParserReadsTheCapturedOutput(t *testing.T) {
	out := fixtureVersion(t, "claude-2.1.238.txt")
	got, err := ParseVersion(out)
	if err != nil {
		t.Fatalf("ParseVersion(%q) failed: %v", strings.TrimSpace(out), err)
	}
	want := Semver{2, 1, 238}
	if got != want {
		t.Fatalf("ParseVersion(%q) = %v, want %v; this is the captured output of the version D-049 pins, and the parser reading it wrong is how a correct install refuses to boot", strings.TrimSpace(out), got, want)
	}
}

// TestAnUnparseableVersionIsARefusalNotAZero
func TestAnUnparseableVersionIsARefusalNotAZero(t *testing.T) {
	out := fixtureVersion(t, "garbage.txt")
	got, err := ParseVersion(out)
	if err == nil {
		t.Fatalf("ParseVersion(%q) returned %v with no error; a zero version compares as below the pin, which sends the operator hunting a drift that is really a changed banner — and against a pin with no minimum it would silently pass", strings.TrimSpace(out), got)
	}
}

// TestThePinPermitsTheMeasuredVersionAndRefusesTheNeighbours pins both edges,
// because a pin that got either one wrong would look right for every version
// except the two that matter.
func TestThePinPermitsTheMeasuredVersionAndRefusesTheNeighbours(t *testing.T) {
	pin, err := ParsePin(">=2.0.0 <3.0.0")
	if err != nil {
		t.Fatalf("the pin from BRD §7.6.1 did not parse: %v", err)
	}

	cases := []struct {
		v    Semver
		want bool
	}{
		{Semver{2, 1, 238}, true}, // the measured version
		{Semver{2, 0, 0}, true},   // >= is inclusive
		{Semver{1, 9, 9}, false},  // below the floor
		{Semver{3, 0, 0}, false},  // < is exclusive
		{Semver{2, 99, 99}, true}, // inside the major
		{Semver{10, 0, 0}, false}, // not a string comparison
	}
	for _, c := range cases {
		if got := pin.Permits(c.v); got != c.want {
			t.Errorf("pin %q permits %v = %v, want %v; >= is inclusive and < is exclusive, and a pin that got either edge wrong would look right for every version except the two that matter", pin.Raw, c.v, got, c.want)
		}
	}

	for _, bad := range []string{"", "2.0.0", "~2.0.0", "^2", ">2.0.0", "latest"} {
		if _, err := ParsePin(bad); err == nil {
			t.Errorf("ParsePin(%q) succeeded; the grammar is >=x.y.z and <x.y.z and nothing else, because an operator guessing which one ~ implements has pinned something other than what they meant", bad)
		}
	}
}

// TestTheBootSmokeTestRefusesOnDrift is §7.6.3's whole reason for existing.
func TestTheBootSmokeTestRefusesOnDrift(t *testing.T) {
	pin, err := ParsePin(">=2.0.0 <3.0.0")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := checkPin("claude", pin, fixtureVersion(t, "claude-2.1.238.txt")); err != nil {
		t.Fatalf("the pinned version was refused: %v; a smoke test that fails on the version it was written for blocks every deploy", err)
	}

	drift := fixtureVersion(t, "claude-1.9.0.txt")
	v, err := checkPin("claude", pin, drift)
	if err == nil {
		t.Fatalf("a tool reporting %v against pin %q started the dashboard; §7.6.3 pins the CLI because flags and output formats move with no deprecation window, and an adapter on an unmeasured version produces wrong evidence rather than an error", v, pin.Raw)
	}
	for _, want := range []string{"claude", "1.9.0", ">=2.0.0 <3.0.0"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal reads %q and does not name %q; it has to name the version, the pin and the tool, or the operator cannot tell a drift from a missing binary", err, want)
		}
	}
}

// TestAnUnconfiguredAdapterIsAValidState. Mode off is the local stack and the
// state every deployment is in today.
func TestAnUnconfiguredAdapterIsAValidState(t *testing.T) {
	a, err := selectAdapter(ModelConfig{Mode: ModeOff})
	if err != nil {
		t.Fatalf("mode off refused to build: %v; an unconfigured dependency is the local stack and a valid state, exactly as handleHealth has always treated an unconfigured database", err)
	}
	if a != nil {
		t.Fatalf("mode off built adapter %v, want nil", a)
	}

	// An empty mode is the same thing: every deployment that predates this
	// change sets no variable at all and must keep booting.
	if a, err := selectAdapter(ModelConfig{}); err != nil || a != nil {
		t.Fatalf("an empty ModelConfig gave (%v, %v), want (nil, nil); an upgrade that refused to boot until a new variable was set would take every running dashboard down", a, err)
	}

	// A configured mode with a broken pin must fail loudly rather than
	// silently running unpinned.
	if _, err := selectAdapter(ModelConfig{Mode: ModeCLI, Command: "claude", Pin: "whatever"}); err == nil {
		t.Fatal("an unparseable pin built an adapter anyway; an adapter with no working pin is the boot smoke test switched off without anyone deciding to")
	}

	// API mode is T-058 and refuses rather than downgrading to CLI: an
	// operator who asked for a metered adapter and got a subscription one has
	// had their cost model changed without being told.
	if _, err := selectAdapter(ModelConfig{Mode: ModeAPI}); err == nil {
		t.Fatal("mode api built an adapter; the API adapter is T-058 and is not written")
	}
}
