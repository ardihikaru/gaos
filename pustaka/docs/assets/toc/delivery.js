/* ToC part — group "Delivery".
   Loaded by assets/toc.js (parts list). Keep this file under 200 lines.
   Entries MUST keep the key order: id, file, title, desc, tags, sections. */
window.DOCS.nav.push({
  group: "Delivery",
  pages: [
    {
      id: "implementation-plan",
      file: "delivery/implementation-plan.html",
      title: "Implementation plan",
      desc: "Every BRD requirement mapped against the code that is actually committed, cut into fifteen modules and sixty-five sub-modules — one hundred and sixty-nine tasks, seventy-six done, seventeen standing on fixtures or partly done, seventy-one not started, five superseded by the move to ai-manager.",
      tags: ["implementation", "roadmap", "tasks", "gap-analysis", "modules", "build-order", "brd", "delivery", "backlog"],
      sections: [
        { anchor: "how-to-read-this", title: "How to read this",
          text: "Why the BRD's four statuses are the wrong vocabulary for a Monday morning, and what the three flags mean: done and pointable at a file, standing on fixtures or the simulator, not started, and superseded. Seventy-six tasks are done: twenty-one are M14 — the gateway documentation, the upstream provisioning API, the ai-manager adapter, the Blueprint, one workspace per product, turns with sessions and recovery, and the board — eleven are the operator surface and the record it persists, thirteen the scheduler, the detached runner with its lease and boot reconciliation, the attempt record with the context slice it was given, and the packet on disk with the handoff contract in it, five the schema, three the store seam that reads and writes it, one the event catalog block, and the rest the foundations, the readiness rubric, the planner's task graph and the pre-dispatch egress pass that landed with them." },
        { anchor: "scoreboard", title: "Scoreboard",
          text: "Fifteen modules and sixty-five sub-modules with their done, partial, not-started and superseded counts, and the first open task in each — the thing to pick up if you open that module today." },
        { anchor: "sequencing", title: "Sequencing",
          text: "The ten prescribed build-order steps from D-018 mapped to modules, why D-019 already inverted them once and the rest must not be reordered, and the three modules that sit outside the spine — the budget guard before the first API run, the egress pass that now runs before the first byte leaves, and the proofs after the event store." },
        { anchor: "register", title: "The register",
          text: "All one hundred and sixty-nine tasks in one filterable table: id, sub-module, task, the requirement or business rule it satisfies, and status. Filter by module, by domain, by status, or by any word in a task." },
        { anchor: "m0-foundations", title: "M0 — Foundations",
          text: "Schema and DDL, the store_arcade.go seam written against store.go unchanged and covered by one conformance suite both stores run, an event store that is the whole record rather than a window over its tail, the five catalog names still declared and never emitted, and the task state machine that refuses an undeclared transition at the boundary. Seventeen of eighteen tasks are done." },
        { anchor: "m1-product-master", title: "M1 — Product master & knowledge ingestion",
          text: "Domain A. The record structs exist and Inject() now writes the master subgraph under ArcadeStore, though the count it reports still comes from Master.Project() rather than from the write. Repository indexing is untouched; the back office is done and what it writes survives a restart — as the record document, not yet as vertices the planner can traverse." },
        { anchor: "m2-readiness", title: "M2 — Readiness gating",
          text: "Domain B. The twelve-signal hundred-point rubric now runs and the planner refuses below an effective 45 with the list of what is missing; what is left is the four autonomy bands and their gates, the drift penalty capped at 25, and scoring on a schedule." },
        { anchor: "m3-planning", title: "M3 — Planning",
          text: "Domain C. PlanWorkflow is pure but produces a fixed shape; the wizard is done and never asks for a gate policy. Atomicity as a rejection, and capability-then-risk-then-cost routing." },
        { anchor: "m4-execution", title: "M4 — Execution",
          text: "Domain D, the largest module. Thirteen of twenty done: the scheduler as a pure function, detached runners with leases, heartbeats and boot reconciliation, one TaskRun per attempt with the ContextSlice it was frozen against, and the packet materialised on disk with the handoff contract that made the first real model call possible. Still owed: the API adapter behind the same seam, the cost basis through every aggregation, and deleting sim.go." },
        { anchor: "m5-review", title: "M5 — Review, scoring & retry",
          text: "Domain E, which has no requirement IDs. The weighted score whose weights already exist over simulated inputs, the safety veto, the five score bands, the failure signature, the six guards of which one runs, and a reason code on every termination." },
        { anchor: "m6-gates", title: "M6 — Gates & approvals",
          text: "Domain F. Gate policy per workflow, a gated task unmergeable through the API, approvals as vertices — and F-6, the BRD's only Not built: the back-office cookie is checked and the password never is." },
        { anchor: "m7-memory", title: "M7 — Memory & learning",
          text: "Domain G. Knowledge written back on completion, ADRs with supersession, meetings as records rather than a scripted overlay, recall by tag and reuse count, and contradiction detection." },
        { anchor: "m8-replay", title: "M8 — Replay & audit",
          text: "Domain H. Five of six tasks done: replay is a fold from the nearest snapshot forward, the reducer is pure, total and order-stable, and the read-only handle replaced the replayAt flag BR-20 forbids. What is left is scope — three of the four do not exist, and the comparison between two runs needs TaskRun first." },
        { anchor: "m9-operator-surface", title: "M9 — Operator surface",
          text: "Domain I, the only module that is mostly done. The five views and the prohibition that the UI invents no state, plus what the surface still owes: the five blocking reasons, the SSE broker the emit() seam was cut for, and asset fingerprinting." },
        { anchor: "m10-tenancy", title: "M10 — Tenancy, capacity & cost control",
          text: "Domain J. Database-per-tenant and a gateway boundary against the hardcoded loop-eng string, quota profiles rather than billing tiers, per-type retention, and the three-zone budget guard with pacing and deferral." },
        { anchor: "m11-data-protection", title: "M11 — Data protection, egress & residency",
          text: "Domain L. The egress inventory as a release obligation, provider data-handling profiles, the pre-dispatch pass that must refuse rather than report, no-egress mode, deletion with a certificate, and the statement template." },
        { anchor: "m12-non-functional", title: "M12 — Non-functional proofs",
          text: "The six properties of §8.13 as tests. Determinism is proved from genesis and holds; the other five cannot be written until the module each depends on exists — auditability, recoverability, isolation, explicability and termination." },
        { anchor: "m13-hygiene", title: "M13 — Repository hygiene",
          text: "Complete since 2026-08-20. The dead citations of test/stacks_test.go now name check-stacks.sh where the assertions live, the upstream font and charting licences are vendored beside the assets and asserted, and both tunnelled stacks build from a tag in a detached worktree rather than from the working tree." },
        { anchor: "m14-gateway", title: "M14 — Gateway, Blueprint & Board",
          text: "Added 2026-11-02. Upstream workspace provisioning in ai-claude-cli and ai-manager, an ai-manager client and adapter, the Blueprint with versions and pinning, one workspace and one live turn per product, turns with request-id recovery, a kanban per product, and the retirement of the CLI execution layer and the floor." },
        { anchor: "whats-excluded", title: "What this page deliberately leaves out",
          text: "Domain K is business rulings rather than buildable work and lives in the FAQ and the decision log; J-7 is withdrawn by D-032; L-1 and L-10 are answered by D-034 as posture; and the risk register stays in BRD §11 so a risk is not mistaken for a commitment." }
      ]
    }
    /* pustaka:insert — `pustaka new` adds entries above this marker */
  ]
});
