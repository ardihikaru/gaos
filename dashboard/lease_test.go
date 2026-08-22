package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T-051. D-3 asks for an exclusive workspace lease with an expiry, and states
// the failure it is written against: "two runs in one workspace corrupt each
// other silently."

// leaseFixture returns a temporary workspace and a lease over it.
func leaseFixture(t *testing.T, runID string, until int64) (string, Lease) {
	t.Helper()
	return t.TempDir(), Lease{
		TaskID: "wf-9-t-1", RunID: runID, Holder: "orch-1",
		PID: 4242, PGID: 4242, StartedAt: 1000, LeaseUntil: until,
	}
}

// TestASecondLeaseOnALiveWorkspaceIsRefused is the positive/negative pair at
// the centre of the file: one holder at a time, and the refusal says who has it.
func TestASecondLeaseOnALiveWorkspaceIsRefused(t *testing.T) {
	ws, first := leaseFixture(t, "run-1", 2000)
	if err := AcquireLease(ws, first); err != nil {
		t.Fatalf("the first acquire on an empty workspace failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, LeasePath)); err != nil {
		t.Fatalf("acquire reported success and wrote no lockfile: %v", err)
	}

	second := first
	second.RunID, second.TaskID = "run-2", "wf-9-t-2"
	err := AcquireLease(ws, second)
	if err == nil {
		t.Fatal("a second run took a workspace that was already leased; D-3 exists because " +
			"two runs in one workspace corrupt each other silently, and neither would crash")
	}
	if !errors.Is(err, ErrLeaseHeld) {
		t.Errorf("the refusal is %v, which is not an ErrLeaseHeld; a caller cannot tell a busy "+
			"workspace from a broken disk", err)
	}
	var held *LeaseHeldError
	if !errors.As(err, &held) || held.Lease.RunID != "run-1" {
		t.Errorf("the refusal is %+v and does not name the holder; the reconciler needs the pid "+
			"and the expiry to decide between adopting, requeuing and orphaning", err)
	}

	// And the loser did not overwrite anything on its way out.
	back, ok, rerr := ReadLease(ws)
	if rerr != nil || !ok {
		t.Fatalf("the lease is gone after a refused acquire: ok=%v err=%v", ok, rerr)
	}
	if back.RunID != "run-1" || back.PID != 4242 {
		t.Errorf("the lease now reads %+v; the refused acquire wrote over the holder it was "+
			"refused by", back)
	}
}

// TestAnExpiredLeaseIsStillNotTakenByAcquire is the row most likely to be
// "fixed" later, so it is asserted rather than left to the comment.
//
// It looks like an oversight: the lease has an expiry, the expiry passed, so
// surely the next runner may have it. But an expiry means the SUPERVISOR
// stopped renewing, and D-2 detaches the child precisely so it outlives its
// supervisor -- so an expired lease is entirely consistent with a live process
// still writing to that directory. Taking it starts the second run.
func TestAnExpiredLeaseIsStillNotTakenByAcquire(t *testing.T) {
	ws, first := leaseFixture(t, "run-1", 1000)
	if err := AcquireLease(ws, first); err != nil {
		t.Fatal(err)
	}
	if !first.Expired(1001) {
		t.Fatal("a lease whose expiry has passed does not report as expired")
	}

	second := first
	second.RunID = "run-2"
	if err := AcquireLease(ws, second); !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("an expired lease was taken by a new run (err=%v); the expiry says the "+
			"supervisor stopped renewing and says nothing about the child it was supervising, "+
			"which D-2 detached so it would outlive exactly this", err)
	}

	// BreakLease is how an expired lease is actually cleared, and it is a
	// separate name because it is only safe once somebody has made the previous
	// run dead.
	if err := BreakLease(ws); err != nil {
		t.Fatalf("BreakLease: %v", err)
	}
	if err := AcquireLease(ws, second); err != nil {
		t.Errorf("the workspace is still not takeable after the lease was broken: %v", err)
	}
}

// TestReAcquiringYourOwnLeaseSucceeds. A runner retrying its own acquire after
// a partial write must not deadlock against itself.
func TestReAcquiringYourOwnLeaseSucceeds(t *testing.T) {
	ws, l := leaseFixture(t, "run-1", 2000)
	if err := AcquireLease(ws, l); err != nil {
		t.Fatal(err)
	}
	l.LeaseUntil = 3000
	if err := AcquireLease(ws, l); err != nil {
		t.Fatalf("a run could not re-acquire its own lease: %v; a retry after a partial write "+
			"would deadlock against itself", err)
	}
	back, _, _ := ReadLease(ws)
	if back.LeaseUntil != 3000 {
		t.Errorf("re-acquiring left the expiry at %d, want the refreshed 3000", back.LeaseUntil)
	}
}

// TestAHeartbeatExtendsTheLeaseAndOnlyForTheRunThatHoldsIt is the renewal half
// of D-3, and its negative is the one that matters: a supervisor whose lease
// was broken must not be able to extend the lease that replaced it.
func TestAHeartbeatExtendsTheLeaseAndOnlyForTheRunThatHoldsIt(t *testing.T) {
	ws, l := leaseFixture(t, "run-1", 2000)
	if err := AcquireLease(ws, l); err != nil {
		t.Fatal(err)
	}
	if err := RenewLease(ws, "run-1", 2300); err != nil {
		t.Fatalf("the holder could not renew its own lease: %v", err)
	}
	back, _, _ := ReadLease(ws)
	if back.LeaseUntil != 2300 {
		t.Errorf("the expiry is %d after a renewal, want 2300; a beat that does not extend the "+
			"lease is a beat that lets the workspace expire under a live run", back.LeaseUntil)
	}
	if back.RunID != "run-1" || back.PID != 4242 {
		t.Errorf("renewal rewrote the lease as %+v; only the expiry moves", back)
	}

	err := RenewLease(ws, "run-2", 9999)
	if !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("a run that does not hold the lease extended it (err=%v); a supervisor declared "+
			"dead and broken off could keep somebody else's workspace alive, which hands one "+
			"directory to two runs through the back door", err)
	}
	back, _, _ = ReadLease(ws)
	if back.LeaseUntil != 2300 {
		t.Errorf("the refused renewal moved the expiry to %d anyway", back.LeaseUntil)
	}
}

// TestReleaseGivesTheWorkspaceBackAndOnlyToItsHolder.
func TestReleaseGivesTheWorkspaceBackAndOnlyToItsHolder(t *testing.T) {
	ws, l := leaseFixture(t, "run-1", 2000)
	if err := AcquireLease(ws, l); err != nil {
		t.Fatal(err)
	}
	if err := ReleaseLease(ws, "run-2"); !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("a run released a lease it did not hold (err=%v)", err)
	}
	if err := ReleaseLease(ws, "run-1"); err != nil {
		t.Fatalf("the holder could not release its own lease: %v", err)
	}
	if _, ok, _ := ReadLease(ws); ok {
		t.Error("the lockfile survived a release; the next run would be refused by a lease " +
			"whose holder has finished")
	}
	// Releasing what is already gone is not an error: a runner that crashed
	// after a reconciler broke its lease still has cleanup to finish.
	if err := ReleaseLease(ws, "run-1"); err != nil {
		t.Errorf("releasing an absent lease reported %v, want nil", err)
	}
}

// TestAnUnreadableLeaseIsTreatedAsHeldRatherThanAsAbsent is the corrupt-file
// case, and the direction of the answer is the whole test.
//
// Treating a lease that does not parse as "no lease" would hand the workspace
// to a second run on the strength of a truncated write -- which is the failure
// D-3 exists to prevent, reached through the mechanism meant to prevent it.
func TestAnUnreadableLeaseIsTreatedAsHeldRatherThanAsAbsent(t *testing.T) {
	ws, _ := leaseFixture(t, "run-1", 2000)
	path := filepath.Join(ws, LeasePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"taskId": "wf-9-t-1", "run`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, ok, err := ReadLease(ws); err == nil || ok {
		t.Fatalf("a truncated lease read as ok=%v err=%v; an unreadable lease must not read as "+
			"an absent one, or a half-written file hands the workspace to a second run", ok, err)
	}
	if _, _, err := ReadLease(ws); !strings.Contains(err.Error(), "not free") {
		t.Errorf("the error reads %q and does not say the workspace is still taken", err)
	}
	if err := AcquireLease(ws, Lease{RunID: "run-2", TaskID: "wf-9-t-2"}); err == nil {
		t.Fatal("a workspace with an unreadable lease was acquired")
	}

	// And the scheduler, which wants a value rather than an error, must reach
	// the same conclusion -- otherwise the safe answer stops at the boundary.
	st, held := leaseStateOf(ws, 5000)
	if !held {
		t.Fatal("leaseStateOf called an unreadable lease free; the scheduler would dispatch " +
			"into a workspace nothing could prove was empty")
	}
	if st.Until <= 5000 {
		t.Errorf("the unreadable lease reports as expiring at %d, which is already past at "+
			"now=5000, so the scheduler would step straight over it", st.Until)
	}
}

// TestTheLeaseFileSurvivesTheProcessThatWroteIt is the property that makes the
// file the right primitive at all: a value read back from disk by something
// that never held the original, which is what an adopting orchestrator is.
func TestTheLeaseFileSurvivesTheProcessThatWroteIt(t *testing.T) {
	ws, l := leaseFixture(t, "run-7", 4500)
	if err := AcquireLease(ws, l); err != nil {
		t.Fatal(err)
	}
	back, ok, err := ReadLease(ws)
	if err != nil || !ok {
		t.Fatalf("ReadLease: ok=%v err=%v", ok, err)
	}
	if back != l {
		t.Errorf("the lease round-tripped as %+v, wrote %+v; a process that has never seen the "+
			"database has only this file to work out whose workspace it is", back, l)
	}

	// The ladder the four constants describe, asserted rather than left to the
	// comment: beat, then stale, then expiry, in that order.
	if !(heartbeatInterval < heartbeatTimeout && heartbeatTimeout < leaseTTL) {
		t.Errorf("the intervals are beat=%d stale=%d ttl=%d; a beat slower than the staleness "+
			"window declares every live run dead, and a ttl inside it expires the lease before "+
			"the beat is even missed", heartbeatInterval, heartbeatTimeout, leaseTTL)
	}
	if heartbeatEventEvery <= heartbeatInterval {
		t.Errorf("the Heartbeat event cadence is %ds against a %ds beat; D-053 separates them so "+
			"a twenty-minute run writes five pills rather than forty into a stream that is "+
			"append-only and rendered in full", heartbeatEventEvery, heartbeatInterval)
	}
}
