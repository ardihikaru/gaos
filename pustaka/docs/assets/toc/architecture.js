/* ToC part — group "Architecture".
   Loaded by assets/toc.js (parts list). Keep this file under 200 lines.
   Entries MUST keep the key order: id, file, title, desc, tags, sections. */
window.DOCS.nav.push({
  group: "Architecture",
  pages: [
    {
      id: "arch-system",
      file: "architecture/system.html",
      title: "The system",
      desc: "Six layers, five control loops, and the thesis that decides every other question: the graph is the primitive, not the prompt.",
      tags: ["architecture", "orchestration", "agents", "events", "replay", "scoring"],
      sections: [
        { anchor: "two-invariants", title: "Two invariants everything else follows from",
          text: "State lives in the graph, so killing the orchestrator loses nothing. Every change emits an event, because an event that was never written is a state change that never happened." },
        { anchor: "layers", title: "Six layers, and what each may never do",
          text: "UI, gateway, orchestration, execution, model gateway and ArcadeDB, each with an explicit prohibition drawn from a way the layering has been broken before." },
        { anchor: "three-layers", title: "Three layers outside the six",
          text: "Since D-055 the model runs in ai-claude-cli behind ai-manager; GAOS shapes each turn with the product, the task and the Blueprint." },
        { anchor: "boot-recovery", title: "What happens when the orchestrator restarts",
          text: "Every turn is recorded with its request id before it is sent; at boot each open turn is polled by that id and adopted if it finished, or orphaned as request-lost, held until its deadline and requeued. The lease-and-heartbeat reconciler it replaced was deleted in v0.29.0." },
        { anchor: "setup-to-autonomy", title: "From an empty record to autonomous work",
          text: "Seven stages from product setup to lesson extraction. Autonomy begins when the scheduler can find a READY task without a human naming it." },
        { anchor: "task-split", title: "Splitting work, and choosing who does it",
          text: "A task is atomic when it has one concern, one workspace and at least one acceptance criterion. The routing table maps risk to role and model tier." },
        { anchor: "scoring", title: "Scoring, and when to stop trying",
          text: "Five weighted dimensions reduce to one number, five verdict bands, and a safety floor that stops work regardless of the total. Guardrails, not scoring, are what end a loop." },
        { anchor: "readiness", title: "Knowledge maturity, scored out of 100",
          text: "A machine-checkable rubric in four bands, plus a drift penalty so a record nobody has read in a year stops being treated as a specification." },
        { anchor: "replay", title: "Replay",
          text: "State at a moment is a fold over events from the nearest snapshot. The reducer must be pure, total and order-stable, and replay reads through a read-only handle rather than a flag." },
        { anchor: "build-order", title: "Build order",
          text: "Ten sequenced deliverables with Mission Control at number nine, and an explicit warning against starting with the visual layer." },
        { anchor: "where-this-lives", title: "Where this lives in the repository",
          text: "Which artifact document and which Go file corresponds to each part of the design." }
      ]
    },
    {
      id: "arch-graph",
      file: "architecture/graph.html",
      title: "The graph",
      desc: "25 vertex types, 3 document types and 32 edge types in ArcadeDB, with the conventions every type obeys and the decisions that are expensive to reverse.",
      tags: ["database", "schema", "graph", "arcadedb", "events", "indexes"],
      sections: [
        { anchor: "principles", title: "Six modelling principles",
          text: "Schema-full where machines read, business ids rather than RIDs in events, one edge type per relationship, an append-only event type, external heavy payloads, and tenancy as a database boundary." },
        { anchor: "supertypes", title: "Two supertypes",
          text: "Every vertex extends GaosVertex and every edge extends GaosEdge, so identity and audit properties are defined exactly once." },
        { anchor: "vertices", title: "The vertex types",
          text: "Product master, code graph, execution, capacity, and judgment and memory. Task and TaskRun are separate so per-retry evidence survives." },
        { anchor: "documents", title: "Three document types, not vertices",
          text: "Event, Snapshot and Sample. Event carries a per-tenant sequence that is monotonic and gapless, which is why there is a single writer per tenant." },
        { anchor: "edges", title: "The edges, and two direction rules",
          text: "DEPENDS_ON points from the dependent to its prerequisite, and USES_CONTEXT hangs off ContextSlice rather than Task so each attempt keeps its own evidence." },
        { anchor: "indexes", title: "The indexes that matter",
          text: "The scheduler index runs every tick; the rest cover identity, orphan detection, dispatch, replay, retry policy and recall." },
        { anchor: "invariants", title: "Invariants, each with a detection query",
          text: "Eight properties checked continuously rather than assumed, from lease exclusivity to a gapless event sequence." },
        { anchor: "the-ready-scan", title: "The one query to understand first",
          text: "Finding tasks whose prerequisites are all done is where the scheduler starts, and it is why the edge direction and that one index are load-bearing." },
        { anchor: "retention", title: "Storage and retention",
          text: "Ninety days of full-fidelity events, downsampled samples, logs by reference, and everything else kept indefinitely because the audit trail is the product." },
        { anchor: "connection", title: "Connecting to it here",
          text: "Two instances reached by container DNS on a shared network, and the readiness probe that answers 204 rather than 200." },
        { anchor: "gateway-additions", title: "What the gateway adds",
          text: "Policy, KnowledgeSource and BlueprintVersion, the Workspace reused one per product, PINNED_TO and HOSTED_IN, six new events, and the floor's types left dormant rather than deleted." },
        { anchor: "not-yet", title: "What is not built yet",
          text: "Both tiers carry the schema and the dashboard reads and writes it; every attempt is a TaskRun turn through ai-manager with its frozen ContextSlice. What is not built is what judges the result: no review of a run's output, no guard that ends a retry loop." }
      ]
    },
    {
      id: "arch-gateway",
      file: "architecture/gateway.html",
      title: "The gateway",
      desc: "How a task leaves GAOS and comes back through ai-manager and ai-claude-cli: three layers, one turn end to end, the error mapping, one session per task, recovery by request id, and the upstream provisioning API.",
      tags: ["architecture", "gateway", "ai-manager", "ai-claude-cli", "sessions", "workspaces", "recovery"],
      sections: [
        { anchor: "three-layers", title: "Three layers, one job each",
          text: "ai-claude-cli works the task with context, ai-manager shapes it with a workspace, GAOS shapes it with the product, the task and the Blueprint." },
        { anchor: "turn", title: "One turn, end to end",
          text: "Assemble, screen, freeze, submit asynchronously with a deterministic request id, poll, extract the handoff." },
        { anchor: "error-mapping", title: "What each answer means",
          text: "410 is session-lost, a session warning is turn-unsaved, 409 is busy rather than failed, a 404 on a submitted request is an orphan." },
        { anchor: "sessions", title: "Sessions: one per task",
          text: "Retries resume, escalation, loss and rotation open a new session; the session is transport and GAOS keeps the record." },
        { anchor: "recovery", title: "When GAOS restarts mid-turn",
          text: "Poll every open request id; adopt a finished one, keep polling a running one, and hold the lease past the deadline for a lost one." },
        { anchor: "configuration", title: "Configuration",
          text: "GAOS_AIM_URL, the auth files, poll interval, turn deadline and session age replace the GAOS_CLI settings." },
        { anchor: "limits", title: "Limits that shape the design",
          text: "256 KB requests, a 600 second turn cap, two worker processes, one turn per session, 30-day sessions, in-memory async records." },
        { anchor: "upstream", title: "The upstream change: provisioning a workspace",
          text: "A managed root, an instruction-file allowlist, all-or-nothing writes and hot reload in ai-claude-cli; a provision route in ai-manager." }
      ]
    }
    /* pustaka:insert — `pustaka new` adds entries above this marker */
  ]
});
