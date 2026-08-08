/* ToC part — group "Product".
   Loaded by assets/toc.js (parts list). Keep this file under 200 lines.
   Entries MUST keep the key order: id, file, title, desc, tags, sections. */
window.DOCS.nav.push({
  group: "Product",
  pages: [
    {
      id: "brd",
      file: "product/brd.html",
      title: "BRD — Business requirements",
      desc: "GAOS's business source of truth: the three design artifacts restated as business requirements across eleven domains, with 26 business rules, a risk register, a scope boundary, and every commercial parameter marked open rather than invented.",
      tags: ["brd", "business-requirements", "objectives", "stakeholders", "scope", "business-rules", "risks", "tenancy", "commercial"],
      sections: [
        { anchor: "executive-summary", title: "1. Executive summary",
          text: "Vision, business summary, background, and the one success measurement the design supplies: run the same objective twice a week apart and see whether the second run needs fewer retries. Numeric targets are explicitly open." },
        { anchor: "business-background", title: "2. Business background",
          text: "The designed-versus-built table from the README as the honest spine of the document, the five operational challenges, and the auditability gap GAOS aims at." },
        { anchor: "problem-statement", title: "3. Problem statement",
          text: "Six problems the design documents name explicitly: unaddressable prompt state, orchestrators that lose memory, briefs too thin to work from, record rot, unbounded retry loops, and unauditable model reasoning." },
        { anchor: "business-objectives", title: "4. Business objectives & success metrics",
          text: "Seven objectives mapped to the problem each answers, five of them deliberately binary acceptance tests rather than invented percentages, plus the three evaluation tests the design already specifies." },
        { anchor: "stakeholders", title: "5. Stakeholders",
          text: "Four internal roles the system actually distinguishes, three external roles that do not exist yet, and three third parties: Anthropic, ArcadeDB, and the edge-plus-VM hosting pair." },
        { anchor: "as-is-process", title: "6. Current business process (as-is)",
          text: "What v0.1.0 actually does end to end — a simulated floor, a login that checks nothing, an in-memory event store, and four paths served from one origin behind the tunnel." },
        { anchor: "to-be-process", title: "7. Future business process (to-be)",
          text: "The value chain from record to lesson, activities per stage, the happy path, the four conditions autonomy requires, and six exception flows each terminating with a reason code." },
        { anchor: "business-requirements", title: "8. Business requirements",
          text: "Eleven domains A–K: product master, readiness gating, planning, execution, review and retry, gates, memory, replay, the operator surface, tenancy and cost control, and commercial — the last entirely open." },
        { anchor: "business-rules", title: "9. Business rules",
          text: "BR-01 to BR-26 across core invariants, readiness and planning, scoring and gates, and data and replay. Each one is a statement a reviewer can score an implementation against." },
        { anchor: "scope", title: "10. Scope",
          text: "What is in scope, a boundary diagram showing what GAOS never owns — the tenant's repository, the merge, the model provider, the host — and six constraints including the ArcadeDB version coupling." },
        { anchor: "risks-and-assumptions", title: "11. Risks & assumptions",
          text: "Six unvalidated assumptions and a nine-entry risk register, led by an unbuilt scheduler, absent authentication, and a budget cap specified everywhere and implemented nowhere." },
        { anchor: "out-of-scope", title: "12. Out of scope",
          text: "Deliberate non-goals with reasons: high availability, cross-tenant analytics by default, snapshots as truth, generic edge types, database abstraction, owning the merge, and more UI." },
        { anchor: "references", title: "13. References",
          text: "Section-precise pointers into the three artifacts, the dashboard files that hold the seam and the closed event catalog, the deployment runbooks, and the architecture and infrastructure pages." },
        { anchor: "decision-log", title: "14. Decision log",
          text: "Pointer to the twenty-four logged decisions, maintained on their own page so that answering an open question has an obvious destination." },
        { anchor: "business-milestones", title: "15. Business milestones",
          text: "Five real dated milestones from git history, and eight next milestones marked open pending the owner's roadmap — starting with applying the DDL to a scratch database." },
        { anchor: "business-decision-matrix", title: "16. Business decision matrix",
          text: "Five genuinely open design choices with their options and trade-offs: commercial model, tenancy at scale, model provider, autonomy default, and what Mission Control is for." },
        { anchor: "appendix-glossary", title: "Appendix A — Glossary",
          text: "Product master record, readiness band, drift penalty, atomic task, context slice, failure signature, safety veto, workspace lease, fold, gate, tenant, product slot." },
        { anchor: "appendix-abbreviations", title: "Appendix B — Abbreviations",
          text: "ADR, BRD, DAG, DDL, HA, NFR, OQ, RID, SaaS, SLA and WG as used in this document." },
        { anchor: "appendix-document-notes", title: "Appendix C — Document notes",
          text: "Which reference template the structure came from, what was dropped and why, and the two structural departures: the decision log and the open questions are separate pages." },
        { anchor: "open-questions", title: "Consolidated open questions",
          text: "Pointer to the eighteen-question register: eight commercial, three product, four technical, three operational." }
      ]
    },
    {
      id: "open-questions",
      file: "product/open-questions.html",
      title: "Open questions",
      desc: "Eighteen decisions the BRD refuses to invent — commercial model, pricing, tenancy, authentication, budget caps — each with the options available, the trade-off each carries, and what it blocks.",
      tags: ["open-questions", "decisions", "commercial", "pricing", "tenancy", "authentication", "sla", "roadmap"],
      sections: [
        { anchor: "index", title: "The register",
          text: "All eighteen questions in one filterable table with theme, what each blocks, and status. Filter by pricing, tenant, auth, or any word in a question." },
        { anchor: "oq-commercial", title: "Commercial",
          text: "OQ-01 to OQ-07 and OQ-14: what GAOS is commercially, the target customer, the auditor persona, plan definitions, pricing basis, who pays the model provider, service level, and data residency." },
        { anchor: "oq-product", title: "Product & scope",
          text: "OQ-08 measurable success targets, OQ-12 band-driven autonomy versus gates on by default, and OQ-15 whether Mission Control is the product surface or an internal console." },
        { anchor: "oq-technical", title: "Technical policy",
          text: "OQ-09 one model provider or several, OQ-10 tenancy strategy at scale with all three costs, OQ-11 the authentication approach that blocks every commercial requirement, and OQ-17 the ArcadeDB target version." },
        { anchor: "oq-operations", title: "Operations",
          text: "OQ-13 who runs the three acceptance tests and how often, OQ-16 whether high availability is required before the first external tenant, and OQ-18 initial budget caps and what happens at the limit." }
      ]
    },
    {
      id: "decision-log",
      file: "product/decision-log.html",
      title: "Decision log",
      desc: "Twenty-five decisions GAOS has actually made, each traceable to an artifact section, a committed file, or git history — and the destination for every open question that gets answered.",
      tags: ["decisions", "adr", "rationale", "governance", "architecture", "deployment", "qa"],
      sections: [
        { anchor: "how-this-works", title: "How this log works",
          text: "A decision belongs here when it constrains future work and reversing it costs something. Every row carries evidence; a decision that cannot be pointed at has not been made." },
        { anchor: "thesis-decisions", title: "Thesis and architecture",
          text: "D-001 to D-005: the graph is the primitive, the orchestrator holds nothing, every change emits an event, events reference business ids, and six layers each with an explicit prohibition." },
        { anchor: "data-decisions", title: "Data model",
          text: "D-006 to D-012: one edge type per relationship, Task and TaskRun separate, USES_CONTEXT on the slice, hybrid schema, external payloads, tenancy as a database boundary, and per-type retention." },
        { anchor: "policy-decisions", title: "Control-loop policy",
          text: "D-013 to D-018: the readiness floor of 45, decay rather than persistence, capability-first routing, correctness weighted 0.45, safety as a veto, and the ten-step build order." },
        { anchor: "delivery-decisions", title: "Delivery and deployment",
          text: "D-019 to D-024: v0.1.0 ships the surface only and says so, the closed event catalog in Go, product slot 03 and its ports, TLS on the edge, the cookie rule behind the tunnel, and the vendored docs engine." },
        { anchor: "qa-decisions", title: "Quality assurance",
          text: "D-025: answers OQ-13. Who runs the acceptance tests and how often, given no CI exists and one person holds every internal stakeholder role — and what happens when one fails." }
      ]
    },
    {
      id: "qa-test-plan",
      file: "product/qa-test-plan.html",
      title: "QA test plan",
      desc: "Answers OQ-13 — who runs the acceptance tests and how often — and enumerates fifty-three positive and negative test cases for the deploy tooling that runs today, plus the three business acceptance tests nothing has ever executed.",
      tags: ["qa", "testing", "test-plan", "acceptance-tests", "deploy", "automation", "oq-13"],
      sections: [
        { anchor: "cadence", title: "Who runs these tests, and how often",
          text: "Resolves OQ-13: no CI exists, so the product owner is the runner for every automation, via rebuild-restart.sh or the gaos-deploy subagent. Table of automation, runner, cadence, and what happens on failure, logged as D-025." },
        { anchor: "register", title: "The register",
          text: "All fifty-three TC-nnn cases in one filterable table: automation, kind (positive/negative), scenario, and whether the automation is implemented today." },
        { anchor: "implemented", title: "Test scenarios — implemented automations",
          text: "check-stacks.sh's eight invariant categories and rebuild-restart.sh's full action surface — status, argument validation, pre-flight gates, dirty-tree handling, test gates, build/restart, and post-deploy verification — each with the exact ok/FAIL message it produces." },
        { anchor: "not-implemented", title: "Test scenarios — not yet implemented",
          text: "The unsatisfiable task, the fold test, and the second run a week apart, written as Given/When/Then forward specs with the blocking domain named, since none can run until the scheduler, runner and fold exist." }
      ]
    }
    /* pustaka:insert — `pustaka new` adds entries above this marker */
  ]
});
