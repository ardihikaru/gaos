package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// The workspace lease. T-051, and D-3:
//
//	"Each run holds an exclusive workspace lease with an expiry, and heartbeats
//	 -- two runs in one workspace corrupt each other silently."
//
// The failure that requirement is written against is not loud. Two agents
// editing one checkout do not crash; they produce a diff neither of them wrote,
// which is then reviewed, scored and possibly merged. So the lease is not a
// nicety on the way to the runner, it is the thing that makes a second runner
// safe to have at all.
//
// # Why a lockfile and not a row in the graph
//
// The record of a lease IS in the graph: Task.leaseUntil and
// Task.lastHeartbeatAt are declared in 04-execution.sql and are what the
// reconciler reads on boot. This file is the ENFORCEMENT, and it is separate
// for three reasons.
//
// The thing being protected is a directory. The only primitive that excludes
// atomically at directory granularity, between processes, on one host, is the
// filesystem -- a read-then-write on a row is a race unless it is one
// conditional statement, and MemStore cannot offer even that across processes.
//
// The conformance suite runs both stores. A lease that only worked against
// ArcadeDB would be a rule with a hole exactly the shape of the memory
// deployment, which is the same argument the egress pass makes for living on
// the store rather than on the adapter.
//
// And it has to outlive the orchestrator, which is the entire point of D-2. A
// file carrying its own expiry explains itself to a process that has never seen
// the database and was not running when the lease was taken.
//
// This does not put authoritative state in the orchestrator's memory, so BR-01
// is intact: the lockfile is state on the resource, not state in the process.
//
// # First writer in the package
//
// Nothing in dashboard/ wrote to a workspace before this. readWorkspace and
// ReadHandoff read; freezePacket copies into memory. That is worth knowing
// because it means every path here is new ground rather than a variation on a
// pattern elsewhere in the file tree.

// The four numbers, together, because the relationship between them is the
// design and reading any one alone tells you nothing.
//
//	beat every 30s  <  stale after 90s (3 missed)  <  lease expires after 5m
//
// A short TTL with frequent renewal rather than a long TTL: a dead supervisor's
// lease then clears in at most five minutes with nobody intervening, and
// defaultDispatchTimeout (20 minutes, adapter_cli.go) means a live supervisor
// renews around forty times over one run.
//
// heartbeatEventEvery is the odd one out and is deliberately NOT
// heartbeatInterval. Writing a Heartbeat EVENT every thirty seconds would put
// forty pills on a twenty-minute run and roughly seven hundred an hour on a
// six-desk floor, into a stream that is append-only (BR-04), snapshotted every
// 500 events, and rendered in full by newView. The beat is a field write on a
// mutable property the DDL already declares; the event is evidence of
// continuity at a resolution a replay can actually use. See D-053.
const (
	leaseTTL            = 300 // seconds; extended on every beat
	heartbeatInterval   = 30  // seconds; writes the beat and extends the lease
	heartbeatTimeout    = 90  // seconds; three missed beats and the beat is stale
	heartbeatEventEvery = 300 // seconds; how often a Heartbeat EVENT is written
)

// LeasePath is where the lockfile lives, relative to the workspace. It sits
// beside the handoff document under .gaos/ so everything GAOS puts in somebody
// else's repository is under one directory that can be gitignored in one line.
const LeasePath = ".gaos/run/LEASE.json"

// Lease is what the file holds. Enough for a process that has never seen the
// database to work out whose it is, whether it has expired, and what to signal
// if it has to be broken.
type Lease struct {
	TaskID     string `json:"taskId"`
	RunID      string `json:"runId"`
	Holder     string `json:"holder"` // which orchestrator took it
	PID        int    `json:"pid"`
	PGID       int    `json:"pgid"`
	StartedAt  int64  `json:"startedAt"`
	LeaseUntil int64  `json:"leaseUntil"`
}

// Expired reports whether the lease has run out as of now.
//
// Expired is NOT the same as free. See AcquireLease: an expired lease still
// refuses a new holder, because expiry says the supervisor stopped renewing and
// says nothing whatever about the child it was supervising.
func (l Lease) Expired(now int64) bool { return l.LeaseUntil <= now }

// ErrLeaseHeld is the sentinel. It is not an ErrRejected: a held lease is not a
// refused mutation an operator should see as a toast, it is a scheduling fact
// the caller is expected to handle.
var ErrLeaseHeld = errors.New("workspace lease held")

// LeaseHeldError carries the lease that stopped you, because "the workspace is
// busy" leaves the caller nothing to decide with -- the reconciler needs the
// pid and the expiry to work out whether to adopt, requeue or orphan.
type LeaseHeldError struct{ Lease Lease }

func (e *LeaseHeldError) Error() string {
	return fmt.Sprintf("workspace is leased to %s (run %s) until %d",
		e.Lease.TaskID, e.Lease.RunID, e.Lease.LeaseUntil)
}
func (e *LeaseHeldError) Is(target error) bool { return target == ErrLeaseHeld }

// AcquireLease takes the workspace exclusively, or fails.
//
// O_CREATE|O_EXCL is the whole mechanism: the kernel decides who won, once, and
// the loser gets EEXIST rather than a slightly older read of a file that was
// about to change.
//
// # An expired lease is not taken here, and that is the important sentence
//
// It would be easy to let an expired lease be overwritten -- the expiry exists,
// the clock says it passed, the previous holder is presumably gone. But "the
// lease expired" means the SUPERVISOR stopped renewing it, and the supervisor
// is not the thing editing the repository. D-2 detaches the child precisely so
// it survives its supervisor, so an expired lease is fully consistent with a
// live process still writing files in that directory. Taking it would start the
// second run D-3 exists to prevent, and the corruption would be silent.
//
// So expiry is reported, never acted on. Breaking a lease is BreakLease, and
// the reconciler calls it only after it has made the previous run dead.
//
// Re-acquiring with the same RunID succeeds and refreshes the file. A runner
// retrying its own acquire after a partial write must not deadlock against
// itself.
func AcquireLease(workspace string, l Lease) error {
	if workspace == "" {
		return errors.New("acquire a lease: no workspace")
	}
	if l.RunID == "" {
		return errors.New("acquire a lease: no run id")
	}
	path := filepath.Join(workspace, LeasePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	body, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err == nil {
		_, werr := f.Write(body)
		cerr := f.Close()
		if werr != nil {
			// A half-written lease is worse than none: the next reader cannot
			// parse it, and an unparseable lease is treated as held forever.
			os.Remove(path)
			return werr
		}
		return cerr
	}
	if !os.IsExist(err) {
		return err
	}

	held, ok, rerr := ReadLease(workspace)
	if rerr != nil {
		// The file exists and cannot be read. Held is the only safe answer --
		// see ReadLease.
		return rerr
	}
	if !ok {
		// It vanished between the O_EXCL failing and the read. Somebody
		// released it; let the caller try again rather than racing here.
		return &LeaseHeldError{}
	}
	if held.RunID == l.RunID {
		return writeLeaseFile(path, body)
	}
	return &LeaseHeldError{Lease: held}
}

// RenewLease extends the expiry, and only for the run that holds it.
//
// The runID check is not defensive programming. It is what stops a supervisor
// that lost its lease -- broken by a reconciler after being declared dead --
// from quietly extending somebody else's, which would hand one workspace to two
// runs through the back door the exclusivity above was built to close.
func RenewLease(workspace, runID string, until int64) error {
	held, ok, err := ReadLease(workspace)
	if err != nil {
		return err
	}
	if !ok {
		return &LeaseHeldError{}
	}
	if held.RunID != runID {
		return &LeaseHeldError{Lease: held}
	}
	held.LeaseUntil = until
	body, err := json.MarshalIndent(held, "", "  ")
	if err != nil {
		return err
	}
	return writeLeaseFile(filepath.Join(workspace, LeasePath), body)
}

// ReleaseLease gives the workspace back, and only for the run that holds it.
// Releasing a lease that is already gone is not an error: a runner that crashed
// after the reconciler broke its lease should still be able to finish its
// cleanup without a spurious failure.
func ReleaseLease(workspace, runID string) error {
	held, ok, err := ReadLease(workspace)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if held.RunID != runID {
		return &LeaseHeldError{Lease: held}
	}
	return os.Remove(filepath.Join(workspace, LeasePath))
}

// BreakLease removes a lease whose holder the caller has established is dead.
//
// Named for what it is, and separate from AcquireLease on purpose. Every other
// function here is safe to call from anywhere; this one is only safe after
// somebody has proved -- not assumed -- that nothing is still writing to the
// directory. The only caller that may do that is boot reconciliation (D-4),
// and only on the branch that kills first.
//
// Giving the dangerous operation its own name is the same move as readOnly
// being a type rather than a flag: it does not prevent misuse, but it makes
// misuse visible in the diff.
func BreakLease(workspace string) error {
	err := os.Remove(filepath.Join(workspace, LeasePath))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// ReadLease returns the lease on a workspace, whether there is one, and any
// error reading it.
//
// A file that exists and does not parse comes back as an ERROR rather than as
// "no lease". That distinction is load-bearing: treating an unreadable lease as
// an absent one means a corrupt file hands the workspace to a second run, which
// is exactly the failure the whole file exists to prevent. A lease that cannot
// be read is a lease that must not be taken.
func ReadLease(workspace string) (Lease, bool, error) {
	if workspace == "" {
		return Lease{}, false, nil
	}
	body, err := os.ReadFile(filepath.Join(workspace, LeasePath))
	if os.IsNotExist(err) {
		return Lease{}, false, nil
	}
	if err != nil {
		return Lease{}, false, err
	}
	var l Lease
	if err := json.Unmarshal(body, &l); err != nil {
		return Lease{}, false, fmt.Errorf("lease at %s is unreadable, so the workspace is not free: %w",
			filepath.Join(workspace, LeasePath), err)
	}
	return l, true, nil
}

// leaseStateOf classifies a workspace for the scheduler, which wants a value
// rather than an error. A workspace whose lease cannot be read reports as held
// with an expiry far in the future, for the reason ReadLease gives: unreadable
// is not free.
func leaseStateOf(workspace string, now int64) (LeaseState, bool) {
	l, ok, err := ReadLease(workspace)
	if err != nil {
		return LeaseState{Workspace: workspace, TaskID: "unreadable", Until: now + leaseTTL}, true
	}
	if !ok {
		return LeaseState{}, false
	}
	return LeaseState{Workspace: workspace, TaskID: l.TaskID, Until: l.LeaseUntil}, true
}

// writeLeaseFile replaces the lockfile atomically.
//
// Write-then-rename rather than truncate-then-write, because a renewal
// interrupted halfway leaves a file that does not parse -- and ReadLease treats
// that as held forever, which would strand the workspace until somebody deleted
// it by hand. rename(2) within one directory is atomic, so a reader sees the
// old lease or the new one and never half of either.
func writeLeaseFile(path string, body []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
