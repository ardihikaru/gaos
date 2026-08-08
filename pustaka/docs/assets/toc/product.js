/* ToC part — group "Product".
   Loaded by assets/toc.js (parts list). Keep this file under 200 lines.
   Entries MUST keep the key order: id, file, title, desc, tags, sections. */
window.DOCS.nav.push({
  group: "Product",
  pages: [
    {
      id: "faq",
      file: "product/faq.html",
      title: "FAQ",
      desc: "What GAOS costs, who it is for, what is open and what is sold, who pays for the AI models, and where your code goes when an agent runs — the commercial answers without the BRD around them.",
      tags: ["faq", "commercial", "pricing", "licensing", "self-hosting", "support", "data-handling", "open-source"],
      sections: [
        { anchor: "the-basics", title: "Is it free, and can I run it myself?",
          text: "Apache-2.0, free to clone and run, self-hosted only, and no paid component is ever required. The licence covers the code; the name is reserved." },
        { anchor: "who-its-for", title: "Who GAOS is for",
          text: "Researchers who want reproducibility, small companies that want gated agent work, and individual experts in IT and outside it — plus what a non-technical expert can and cannot do alone." },
        { anchor: "commercial", title: "What is commercial, then?",
          text: "Private submodules, installation, and support and custom work — and the rule that no safety control, guard, budget cap or audit record is ever sold." },
        { anchor: "pricing", title: "What the services cost",
          text: "The proposed rate card: three install packages, three support tiers of which one is free, a submodule licence with an academic reduction, and a day rate — plus why single-maintainer projects price this way." },
        { anchor: "models-and-cost", title: "Who pays for the AI models",
          text: "You do, always, on your own account. CLI mode or API mode and how to choose — flat subscription with estimated costs, or metered with exact ones — and the three-zone budget guard that defers work instead of failing it." },
        { anchor: "data", title: "Where your code goes",
          text: "Residency is your machines; egress is source code selected by traversal and widened on every retry. The inventory, the provider profile, the pre-dispatch redaction, and deletion under an append-only stream." },
        { anchor: "support", title: "What support commits to",
          text: "Response times, never uptime — and why a bug fix is never behind a subscription." },
        { anchor: "project", title: "The project itself",
          text: "What actually works today, how to contribute under the DCO, and what protects you if a single-maintainer project stalls." }
      ]
    },
    {
      id: "brd",
      file: "product/brd.html",
      title: "BRD — Business requirements",
      desc: "GAOS's business source of truth: the three design artifacts restated as business requirements across twelve domains, with 35 business rules, a twelve-entry risk register, a scope boundary, the dual CLI/API execution model, the budget guard, and a commercial model that is now decided rather than deferred.",
      tags: ["brd", "business-requirements", "objectives", "stakeholders", "scope", "business-rules", "risks", "tenancy", "commercial", "open-core", "pricing", "data-protection"],
      sections: [
        { anchor: "executive-summary", title: "1. Executive summary",
          text: "Vision, business summary, background, and the one success measurement the design supplies: run the same objective twice a week apart and see whether the second run needs fewer retries. Numeric targets are explicitly open." },
        { anchor: "business-background", title: "2. Business background",
          text: "The designed-versus-built table from the README as the honest spine of the document, the five operational challenges, and the auditability gap GAOS aims at." },
        { anchor: "problem-statement", title: "3. Problem statement",
          text: "Six problems the design documents name explicitly: unaddressable prompt state, orchestrators that lose memory, briefs too thin to work from, record rot, unbounded retry loops, and unauditable model reasoning." },
        { anchor: "business-objectives", title: "4. Business objectives & success metrics",
          text: "Eight objectives mapped to the problem each answers, seven of them deliberately binary acceptance tests rather than invented percentages, plus the three evaluation tests the design already specifies." },
        { anchor: "stakeholders", title: "5. Stakeholders",
          text: "Four internal roles the system distinguishes, five external ones led by the self-hosting operator, the difference between an operator and a tenant, and three third parties: the model provider, ArcadeDB, and the edge-plus-VM hosting pair." },
        { anchor: "as-is-process", title: "6. Current business process (as-is)",
          text: "What v0.1.0 actually does end to end — a simulated floor, a login that checks nothing, an in-memory event store, and four paths served from one origin behind the tunnel." },
        { anchor: "to-be-process", title: "7. Future business process (to-be)",
          text: "The value chain from record to lesson, activities per stage, the happy path, the five conditions autonomy requires, seven exception flows each terminating with a reason code, and §7.6 — how a task reaches a model through either a CLI or an API, with the flowchart, the master configuration and the handoff-document contract." },
        { anchor: "business-requirements", title: "8. Business requirements",
          text: "Twelve domains A–L: product master, readiness gating, planning, execution, review and retry, gates, memory, replay, the operator surface, tenancy and cost control including the three-zone budget guard, commercial — now decided, with the proposed rate card — and data protection, egress and residency." },
        { anchor: "business-rules", title: "9. Business rules",
          text: "BR-01 to BR-35 across core invariants, readiness and planning, scoring and gates, data and replay, and the commercial, execution and data-handling rules — the open-core boundary, what may never be sold, the execution mode that may be recorded but never branched on, and why deferral is not failure." },
        { anchor: "scope", title: "10. Scope",
          text: "What is in scope, a boundary diagram showing what GAOS never owns — the tenant's repository, the merge, the model provider, the host — and ten constraints including the ArcadeDB version coupling, the open-core seam, Apache-2.0 as a one-way door, and the two adapters that must stay indistinguishable." },
        { anchor: "risks-and-assumptions", title: "11. Risks & assumptions",
          text: "Nine unvalidated assumptions and a twelve-entry risk register, led by an unbuilt scheduler, a budget cap specified everywhere and implemented nowhere, automatic source-code egress, and driving a provider CLI that never promised to be driven." },
        { anchor: "out-of-scope", title: "12. Out of scope",
          text: "Deliberate non-goals with reasons: high availability, cross-tenant analytics by default, snapshots as truth, generic edge types, database abstraction, owning the merge, more UI — and since D-026, hosting GAOS for anyone, billing, reselling model capacity, and the external-auditor package." },
        { anchor: "references", title: "13. References",
          text: "Section-precise pointers into the three artifacts, the dashboard files that hold the seam and the closed event catalog, the deployment runbooks, the architecture and infrastructure pages, and the FAQ." },
        { anchor: "decision-log", title: "14. Decision log",
          text: "Pointer to the thirty-four logged decisions, maintained on their own page so that answering an open question has an obvious destination." },
        { anchor: "business-milestones", title: "15. Business milestones",
          text: "Eight real dated milestones from git history, ending with the repository becoming licensed, and the next twelve — led by landing the new event names and task state together, then the first adapter and the budget guard." },
        { anchor: "business-decision-matrix", title: "16. Business decision matrix",
          text: "Six design choices with their options and trade-offs: the decided commercial model with its rejected alternatives intact, tenancy at scale, model provider, autonomy default, what Mission Control is for, and which capabilities a private submodule should hold." },
        { anchor: "appendix-glossary", title: "Appendix A — Glossary",
          text: "Product master record, readiness band, drift penalty, atomic task, context slice, failure signature, safety veto, workspace lease, fold, gate, tenant, product slot, operator, open core, task packet, handoff document, cost basis, execution mode, pacing, deferral, egress." },
        { anchor: "appendix-abbreviations", title: "Appendix B — Abbreviations",
          text: "ADR, BRD, BYOK, CLI, DAG, DDL, HA, NFR, OQ, RID, SaaS, SLA and WG as used in this document." },
        { anchor: "appendix-document-notes", title: "Appendix C — Document notes",
          text: "Which reference template the structure came from, what was dropped and why, and the two structural departures: the decision log and the open questions are separate pages." },
        { anchor: "open-questions", title: "Consolidated open questions",
          text: "Pointer to the twenty-three-question register: eleven commercial, three product, six technical, three operational, nine of them answered." }
      ]
    },
    {
      id: "open-questions",
      file: "product/open-questions.html",
      title: "Open questions",
      desc: "Twenty-three decisions the BRD refuses to invent — eleven now answered, including the whole commercial block, the Apache-2.0 licence and the budget guard — each with the options available, the trade-off each carries, and what it blocks.",
      tags: ["open-questions", "decisions", "commercial", "pricing", "tenancy", "authentication", "licensing", "sla", "roadmap"],
      sections: [
        { anchor: "index", title: "The register",
          text: "All twenty-three questions in one filterable table with theme, what each blocks, and status. Filter by pricing, licence, auth, or any word in a question." },
        { anchor: "oq-commercial", title: "Commercial",
          text: "OQ-01 to OQ-07 and OQ-14, all answered on 2026-08-08 — open core, the target customer, the auditor persona, quota profiles, pricing basis, who pays the provider, support, and data handling — plus OQ-20, answered as Apache-2.0, and the two still open: OQ-19 submodule scope and OQ-21 the rate card." },
        { anchor: "oq-product", title: "Product & scope",
          text: "OQ-08 measurable success targets, OQ-12 band-driven autonomy versus gates on by default, and OQ-15 whether Mission Control is the product surface or an internal console." },
        { anchor: "oq-technical", title: "Technical policy",
          text: "OQ-09 one model provider or several, OQ-10 tenancy strategy at scale, OQ-11 authentication now narrowed to what must exist before anything binds beyond localhost, OQ-17 the ArcadeDB target version, OQ-22 cost estimation when a CLI reports nothing — now the input the budget guard enforces against — and OQ-23 which CLIs are supported." },
        { anchor: "oq-operations", title: "Operations",
          text: "OQ-13 who runs the three acceptance tests and how often, OQ-16 high availability now that GAOS hosts nothing, and OQ-18 answered — the budget cap becomes three zones with pacing and deferral." }
      ]
    },
    {
      id: "decision-log",
      file: "product/decision-log.html",
      title: "Decision log",
      desc: "Thirty-seven decisions GAOS has actually made, each traceable to an artifact section, a committed file, a product-owner ruling, or git history — and the destination for every open question that gets answered.",
      tags: ["decisions", "adr", "rationale", "governance", "architecture", "deployment", "qa", "commercial", "open-core", "data-protection"],
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
          text: "D-025: answers OQ-13. Who runs the acceptance tests and how often, given no CI exists and one person holds every internal stakeholder role — and what happens when one fails." },
        { anchor: "commercial-decisions", title: "Commercial model",
          text: "D-026 to D-031: open core with private submodules and paid services, the target customer, the auditor deferred, service-and-licence pricing, the operator always pays the model provider, and support that commits to response times rather than uptime." },
        { anchor: "execution-decisions", title: "Execution and data handling",
          text: "D-032 to D-034: quota profiles instead of billing tiers with a measured-or-estimated cost basis, models invoked through provider CLIs with a mandatory handoff document, and residency answered as an egress inventory rather than a location." },
        { anchor: "licence-decisions", title: "Licence",
          text: "D-035: Apache-2.0 with a DCO sign-off and a reserved name — why permissive beats copyleft for this buyer, why a CLA would be paperwork for an unused power, and the fact that it cannot be undone." },
        { anchor: "execution-mode-decisions", title: "Execution mode and spend control",
          text: "D-036 extends D-033 to two interchangeable adapters behind one master switch, and D-037 turns the budget cap into three zones with pacing and deferral so work waits for budget instead of failing for lack of it." }
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
