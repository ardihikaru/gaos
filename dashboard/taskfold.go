package main

import "sort"

// The task fold. T-164, TC-308, I-9, BR-05.
//
// fold.go rebuilds the desks; this rebuilds the task graph's cards -- every
// task's status and rank -- from the event stream alone, so a product's board
// can be replayed rather than only read. It is a separate fold on purpose:
// FloorState is the wrong container for tasks (see the TaskPlanned case in
// fold.go), and a snapshot rung carrying every task would grow with the plan.
//
// Three events are enough, because since T-164 every status change writes a
// TaskMoved naming the task, whoever made it:
//
//	TaskPlanned    the plan: one PENDING record per PlannedTask, in plan order
//	TaskMoved      Payload.To is the status the task reached
//	TaskReordered  Payload.Version is the index, replayed through reorderColumn
//
// Every other event is skipped, which keeps the fold total over the closed
// catalog. A TaskMoved naming a task the fold never saw planned is skipped too:
// a stream that starts mid-plan has nothing to move.

// foldTasks rebuilds one product's tasks from events. Pure: it reads nothing
// but its arguments and does not modify them.
func foldTasks(events []Event, productID string) []TaskRecord {
	evs := append([]Event(nil), events...)
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].Seq < evs[j].Seq })

	var tasks []TaskRecord
	at := map[string]int{}
	for _, e := range evs {
		if e.DeskID != productID {
			continue
		}
		p := e.Payload
		switch e.Type {
		case TaskPlanned:
			for i, pt := range p.Tasks {
				at[pt.ID] = len(tasks)
				tasks = append(tasks, TaskRecord{
					ID: pt.ID, WorkflowID: p.WorkflowID, DeskID: productID,
					Title: pt.Title, Role: pt.Role, Story: pt.Story, Model: pt.Model,
					Status: StatusPending, Deps: append([]string(nil), pt.Deps...),
					Priority: pt.Priority, Ordinal: i, Gate: pt.Gate,
				})
			}
		case TaskMoved:
			if k, ok := at[p.Task]; ok && p.To != "" {
				tasks[k].Status = p.To
			}
		case TaskReordered:
			if _, ok := at[p.Task]; ok {
				reorderColumn(tasks, productID, p.Task, p.Version)
			}
		}
	}
	return tasks
}
