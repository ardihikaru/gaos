package main

// The unattended start of an API or bundle card under GAOS_RUNNER=manual (D-099, amends BR-56;
// docs/designs/gaos-external-tasks-and-pr-bundles.md §5.5). It rides autoFixLoop's ticker and the
// one dispatch function, with a claim that re-checks everything under the lock.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
)

// MoveAPI is the unattended start of an autoDispatch card (D-099).
const MoveAPI = "api"

// autorunProducts is GAOS_API_AUTORUN_PRODUCTS, read on every pass: empty means off.
func autorunProducts() map[string]bool {
	out := map[string]bool{}
	for _, id := range strings.Split(os.Getenv("GAOS_API_AUTORUN_PRODUCTS"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			out[id] = true
		}
	}
	return out
}

func autorunDaily() int { return envPosInt("GAOS_API_AUTORUN_DAILY", 20) }

// apiContextReady says whether a card's packet context is in hand; a test-planner card's is
// built outside the lock (D-100, bundles.go). The default admits every card.
var apiContextReady = func(t *TaskRecord) bool { return true }

// apiClaimLocked is the claim of the unattended path. s.mu is held.
func (s *MemStore) apiClaimLocked(productID string, t *TaskRecord, stamp bool) error {
	switch {
	case runnerMode() != RunnerManual:
		return reject("the unattended start runs only under GAOS_RUNNER=manual")
	case !autorunProducts()[productID]:
		return reject(productID + " is not listed in GAOS_API_AUTORUN_PRODUCTS")
	case !t.AutoDispatch:
		return reject(t.ID + " was not asked to start unattended")
	case !labelsAllowDispatch(t.Labels):
		return reject(t.ID + " carries a label a person owns")
	case t.WorkflowID != prWorkflowID(productID):
		return reject(t.ID + " is not on the pull-request lane")
	case t.BlockReason == BlockEgress:
		return reject(t.ID + " was refused by the egress pass")
	case !apiContextReady(t):
		return reject(t.ID + "'s context is not built yet")
	}
	day := autoFixNow() / 86400
	n := 0
	for i := range s.tasks {
		o := &s.tasks[i]
		if o.DeskID == productID && o.ID != t.ID && o.AutoDispatch && o.AutoAt != 0 && o.AutoAt/86400 == day {
			n++
		}
	}
	if limit := autorunDaily(); n >= limit {
		return reject(fmt.Sprintf("unattended cap %d/%d until 00:00 UTC", n, limit))
	}
	if stamp {
		t.AutoAt = autoFixNow()
	}
	return nil
}

// APIAutoDispatch dispatches one READY autoDispatch card for the pass.
func (s *MemStore) APIAutoDispatch(ctx context.Context, productID, taskID string) (Result, error) {
	return s.dispatch(ctx, productID, taskID, MoveAPI, func(t *TaskRecord, stamp bool) error {
		return s.apiClaimLocked(productID, t, stamp)
	})
}

// APIAutoDispatch is persisted like the auto-fix's: without the store's write lock, then written.
func (s *ArcadeStore) APIAutoDispatch(ctx context.Context, productID, taskID string) (Result, error) {
	return s.dispatchPersisted(ctx, func() (Result, error) { return s.proj.APIAutoDispatch(ctx, productID, taskID) })
}

var (
	apiRefusalMu   sync.Mutex
	apiRefusalLast = map[string]string{}
)

// apiAutoPass starts at most one eligible card per listed product (BR-38). Under on and off it
// does nothing: the scheduler dispatches under on, and off spends nothing.
func apiAutoPass(ctx context.Context, st Store) int {
	if runnerMode() != RunnerManual {
		return 0
	}
	listed := autorunProducts()
	if len(listed) == 0 {
		return 0
	}
	f, err := st.Snapshot(ctx)
	if err != nil || f.Stats.Replay {
		return 0
	}
	n := 0
	for _, d := range f.Desks {
		if !listed[d.ID] {
			continue
		}
		cards, err := st.PRCards(ctx, d.ID)
		if err != nil {
			continue
		}
		sort.Slice(cards, func(i, j int) bool { return cards[i].ID < cards[j].ID })
		for _, c := range cards {
			if c.Status != StatusReady || !c.AutoDispatch || c.BlockReason == BlockEgress {
				continue
			}
			if _, err := st.APIAutoDispatch(ctx, d.ID, c.ID); err != nil {
				if errors.Is(err, ErrRejected) {
					apiRefusalMu.Lock()
					if apiRefusalLast[c.ID] != err.Error() {
						apiRefusalLast[c.ID] = err.Error()
						log.Printf("api: %s is ready but not started: %s", c.ID, capRunes(err.Error(), 200))
					}
					apiRefusalMu.Unlock()
				} else {
					log.Printf("api: an unattended start failed: %v", err)
				}
				continue
			}
			n++
			break
		}
	}
	return n
}
