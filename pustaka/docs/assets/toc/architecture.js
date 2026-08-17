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
        { anchor: "boot-recovery", title: "What happens when the orchestrator restarts",
          text: "Tasks claiming to be running are reconciled against a workspace lease and a heartbeat, then adopted, requeued, or failed as orphaned." },
        { anchor: "setup-to-autonomy", title: "From an empty record to autonomous work",
          text: "Six stages from product setup to lesson extraction. Autonomy begins when the scheduler can find a READY task without a human naming it." },
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
        { anchor: "not-yet", title: "What is not built yet",
          text: "The DDL applies clean to a scratch ArcadeDB 26.7.2 and produced two amendments to the model, but neither dev nor prod carries the schema. The dashboard still reads fixtures behind an interface, and the event catalog is already encoded as typed constants so the vocabulary cannot drift." }
      ]
    }
    /* pustaka:insert — `pustaka new` adds entries above this marker */
  ]
});
