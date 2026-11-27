/* ToC part — group "Integrations" (v0.45.0, D-085; the Brain Town feed v0.46.0, D-086).
   Every external system GAOS talks to: the picture, how any client and the
   QA tools connect (with tutorials), and the ai-manager calls in full.
   Page leaves keep the key order id, file, title, desc, tags, sections. */
window.DOCS.nav.push({
  group: "Integrations",
  pages: [
    {
      id: "integrations-overview", file: "integrations/overview.html", title: "How integration works",
      desc: "Every external system GAOS talks to, which side starts each call, how each is authenticated, and the two round trips that matter: a question answered and a test result pushed.",
      tags: ["integration", "api", "webhook", "ai-manager", "qa", "architecture", "tokens"],
      sections: [
        { anchor: "picture", title: "The picture", text: "Clients and the QA tools, the machine API, GAOS, ai-manager and the worker in the product's workspace." },
        { anchor: "connections", title: "Every connection", text: "Six connections: direction, paths, auth, credential file and who starts each." },
        { anchor: "tokens", title: "Three GAOS tokens, never equal", text: "Admin, QA and outbound tokens; equal tokens disable the API." },
        { anchor: "question-round-trip", title: "A question round trip", text: "Ask, answer in the back office, the webhook with its retries, or poll." },
        { anchor: "results-push", title: "A results push", text: "CI results pushed by the QA tools, or pulled by the operator's button." },
        { anchor: "a-turn", title: "A model turn", text: "Submit, poll, extract the handoff; workspace provisioning first." }
      ]
    },
    {
      id: "webhooks-and-clients", file: "integrations/webhooks-and-clients.html", title: "Webhooks and external clients",
      desc: "How any external platform integrates with GAOS: register a client, ask and answer through the inbox API, receive the answer webhook, verify and troubleshoot, with tutorials for the QA tools, ai-manager and a new client.",
      tags: ["webhook", "client", "registry", "inbox", "tutorial", "token", "mobile", "curl"],
      sections: [
        { anchor: "register", title: "Register a client", text: "The clients file line, the token file, products, origin, reserved names and every refusal." },
        { anchor: "inbox-api", title: "Ask, poll and answer", text: "Three curl calls against the inbox API." },
        { anchor: "webhook", title: "The answer webhook", text: "URL, headers, body, success, timeout, retries, idempotency and the URL rule." },
        { anchor: "verify", title: "Verify it", text: "The Integrations view, the boot log, health and a round trip." },
        { anchor: "troubleshooting", title: "Troubleshooting", text: "401, 503, 403, 404, id-taken, no webhook, retries, a missing client." },
        { anchor: "tutorial", title: "Tutorials", text: "Step by step: the QA tools and their three tokens, ai-manager's credentials and switches, a new mobile client." }
      ]
    },
    {
      id: "ai-manager-api", file: "integrations/ai-manager-api.html", title: "ai-manager REST reference",
      desc: "Every ai-manager endpoint GAOS calls: method, path, auth, headers, bodies, the status codes GAOS distinguishes and what it does on each, timeouts and polling, and the GAOS function that calls it.",
      tags: ["ai-manager", "rest", "api", "gateway", "reference", "chat", "workspace", "session"],
      sections: [
        { anchor: "configuration", title: "Configuration", text: "GAOS_MODEL_MODE, GAOS_AIM_URL, the chat login and admin key files, tenant, polling, deadline, session age, GAOS_RUNNER." },
        { anchor: "common", title: "Common behaviour", text: "The two helpers, headers, the 30 s timeout and the status map." },
        { anchor: "chat", title: "POST /v1/chat", text: "Submit a turn: body, request id, accepted and refused." },
        { anchor: "result", title: "GET /v1/requests/{id}/result", text: "Poll a turn every 2 s up to 660 s; the reply fields GAOS reads." },
        { anchor: "answer", title: "POST /v1/questions/{id}/answer", text: "Answer a question and resume; fallback to a plain chat." },
        { anchor: "providers", title: "GET /v1/providers", text: "The boot check and the vendors pane." },
        { anchor: "provision", title: "POST /admin/workspaces/provision", text: "Create a product's workspace." },
        { anchor: "files-get", title: "GET /admin/workspaces/{id}/files", text: "Read a workspace's manifest; the drift check." },
        { anchor: "files-put", title: "PUT /admin/workspaces/{id}/files", text: "Replace a workspace's files with the published Blueprint." },
        { anchor: "session-delete", title: "DELETE /admin/sessions/{id}", text: "Delete a gateway session on prune." },
        { anchor: "not-called", title: "Calls GAOS never makes", text: "What GAOS does not use." },
        { anchor: "gaps", title: "Known gaps against ai-manager's docs", text: "412 not retried, 423 read as a workspace, unread fields, the deadline." }
      ]
    },
    {
      id: "pr-agents", file: "integrations/pr-agents.html", title: "Pull-request agents",
      desc: "GAOS reacts to pull-request activity: which cards a pull request, a merge and a staging deploy open, the labels, the review it posts, the staging callback Jenkins calls, and the WhatsApp notice and contact registry.",
      tags: ["integration", "github", "pull request", "review", "staging", "jenkins", "whatsapp", "qa tool"],
      sections: [
        { anchor: "routing", title: "What opens what", text: "A pull request, a merge and a staging deploy; the cap, the draft and fork filters." },
        { anchor: "assignment", title: "Which agents a product has", text: "Assigned agents switch the flows on per product; see Agents, severity and review fixes." },
        { anchor: "labels", title: "The labels", text: "A closed vocabulary, one per family; manual and QA-tool cards are never dispatched." },
        { anchor: "review", title: "The review", text: "The gaos-pr-review block; GAOS screens and posts it as a comment." },
        { anchor: "settings", title: "The settings", text: "The GitHub token, the staging credentials, the WhatsApp URL, login and group, the cap." },
        { anchor: "review-fix", title: "A low finding fixed by an agent", text: "A draft into the reviewed pull request's own branch." },
        { anchor: "staging", title: "The staging callback", text: "HTTP Basic, one credential per product, and every answer." },
        { anchor: "qa", title: "The QA tool does the e2e work", text: "A job GAOS starts and polls; the QA-case line; the two routes live in the QA tool." },
        { anchor: "whatsapp", title: "WhatsApp and the contacts", text: "One line and the card id; the pinned group; the contact registry." },
        { anchor: "bundles", title: "Bundles from the PR template", text: "The task-manager code and links group pull requests; one test plan when all merged; the missing-code alert." },
        { anchor: "failure", title: "When something is down", text: "What each failure does to the card." }
      ]
    },
    {
      id: "agents", file: "integrations/agents.html", title: "Agents, severity and review fixes",
      desc: "Which agents work on a product and what that switches on: assignment per product, the reviewer's severity on the board, requested changes on GitHub, low findings fixed into the reviewed branch, the card-source labels, the runbook, and the second release.",
      tags: ["agents", "assignment", "severity", "review", "request changes", "review fix", "labels", "release b"],
      sections: [
        { anchor: "overview", title: "What it gives you", text: "The filter, the gate, the severity, requested changes, review fixes, the source labels." },
        { anchor: "assignment", title: "Assignment", text: "Three states of a product, the roles it switches, who may change it." },
        { anchor: "severity", title: "Severity on the board", text: "The severity:* label: a chip, a column, a filter with Medium or worse." },
        { anchor: "review-event", title: "Comment or request changes", text: "Request changes for medium and worse on a configured product; never approve; the dismissal line." },
        { anchor: "review-fix", title: "Low findings fixed by an agent", text: "A draft into the reviewed branch: when, the settings, the checks, what happens next." },
        { anchor: "source-labels", title: "Where a card came from", text: "source:api and source:bundle mark the cards assignment does not gate." },
        { anchor: "runbook", title: "Set it up for a product", text: "Assign the reviewer, unit-test, QA and fixer agents; the branch patterns." },
        { anchor: "release-b", title: "The second release", text: "E2E kinds by repository, create then run on staging, the guarded merge, unit tests through the QA tool." },
        { anchor: "failure", title: "When something is off", text: "No review card, 422 unassigned, a comment instead of changes, no fix planned." }
      ]
    },
    {
      id: "pull-requests", file: "integrations/pull-requests.html", title: "Pull requests",
      desc: "The back-office page that lists every open pull request tied to a task: where each kind comes from, what a row shows, how it is kept fresh, the settings and what it never does.",
      tags: ["integration", "pull request", "github", "auto-fix", "review", "qa tool", "back office"],
      sections: [
        { anchor: "kinds", title: "Where each kind comes from", text: "Auto-fix, review and QA-tool pull requests, and the task of each." },
        { anchor: "page", title: "The page", text: "Rows, tabs, filters, the drawer and how the page is kept fresh." },
        { anchor: "settings", title: "The settings", text: "automationRepos and allowedBases, and that a post replaces the configuration." },
        { anchor: "retarget", title: "Change the base branch", text: "The Retarget action: a new fix task and a new draft pull request against another allowed base." },
        { anchor: "never", title: "What it never does", text: "Merge, ready, close, build a link from a supplied URL." }
      ]
    },
    {
      id: "autofix", file: "integrations/autofix.html", title: "Defect auto-fix",
      desc: "How GAOS triages a defect the QA tool reports and opens a draft pull request for an easy one: the nine steps, what easy means, the settings, the QA tool's route, the caps, the Auto-fix view and what GAOS never does.",
      tags: ["integration", "auto-fix", "defect", "triage", "github", "pull request", "qa tool"],
      sections: [
        { anchor: "steps", title: "What auto-fix does", text: "The nine steps from a reported defect to a draft pull request." },
        { anchor: "easy", title: "What easy means", text: "Validation, formatting and minimum-value fixes; medium and complex; the re-check." },
        { anchor: "settings", title: "The settings", text: "The admin list, the write token, the classic switch and the per-product configuration." },
        { anchor: "route", title: "The QA tool's route", text: "POST qa/defects: fields, limits and what a report does." },
        { anchor: "caps", title: "The caps", text: "Three files, 40 and 120 changed lines, daily dispatches, draft only." },
        { anchor: "view", title: "The Auto-fix view", text: "Tiles, table, filters and the detail drawer." },
        { anchor: "never", title: "What GAOS never does", text: "Ready, merge, run tests, bypass the switch." },
        { anchor: "failure", title: "When something fails", text: "Each block reason and what it does to the card." }
      ]
    },
    {
      id: "brain-town-consumer", file: "integrations/brain-town-consumer.html", title: "Brain Town reader",
      desc: "GAOS can pull the town's pull-request events: the six settings, the scope it declares, what the reader sees that the public stream hides, and how a lost event is replayed rather than dropped.",
      tags: ["integration", "brain town", "github", "pull request", "inbound", "agent"],
      sections: [
        { anchor: "why", title: "Why a reader and not the public stream", text: "The public stream masks a hidden product's real name and every title." },
        { anchor: "settings", title: "The six settings", text: "BRAIN_TOWN_CONSUMER_ENABLED, _URL, _TOKEN_FILE, _CURSOR_FILE, _TYPES and _KINDS." },
        { anchor: "scope", title: "The scope GAOS declares", text: "Owners, repositories, kinds and event types; filtered again by the products." },
        { anchor: "events", title: "What the reader sees", text: "The public frame's shape with an unmasked payload, plus productKey and orgLogin." },
        { anchor: "delivery", title: "Reading, losing and replaying", text: "A full channel replays; the cursor advances only after the hand-over." },
        { anchor: "enable", title: "Turn it on", text: "Register the scope, create the two files, redeploy with the switch." }
      ]
    },
    {
      id: "brain-town", file: "integrations/brain-town.html", title: "Brain Town feed",
      desc: "GAOS can push each committed task and product change to a public receiver, off by default: the four settings, what is sent, what a hidden product leaks, delivery and retries.",
      tags: ["integration", "brain town", "webhook", "outbound", "privacy"],
      sections: [
        { anchor: "settings", title: "The four settings", text: "BRAIN_TOWN_ENABLED, _URL, _TOKEN_FILE and _PUBLIC_PRODUCTS." },
        { anchor: "events", title: "What is sent", text: "The eight event types, their data, and the one not sent." },
        { anchor: "privacy", title: "What a hidden product leaks", text: "Nothing that names it: alias name, task id as title." },
        { anchor: "delivery", title: "Delivery", text: "Ordered, non-blocking, retried with the same id, one log line per failure." },
        { anchor: "enable", title: "Turn it on", text: "The token file, the redeploy line and the Integrations view." }
      ]
    },
    {
      id: "external-tasks-api", file: "integrations/external-tasks-api.html", title: "External tasks API",
      desc: "How an outside platform plans and follows GAOS work without signing in: a PR review, unit tests, an e2e automation or an e2e run, polled and, for a run, cancelled; with a markdown download.",
      tags: ["api", "tasks", "client", "pull request", "review", "unit test", "automation", "tutorial", "curl"],
      sections: [
        { anchor: "purpose", title: "What it is for", text: "The four kinds and who works each." },
        { anchor: "client", title: "Ask the operator for a client", text: "A client with the tasks capability and its bearer token." },
        { anchor: "create", title: "Create a PR review", text: "POST /api/v1/products/{id}/tasks, the body rules, 201 and adoption." },
        { anchor: "poll", title: "Poll it", text: "GET by task id or by externalId; reason held, blocked, failed." },
        { anchor: "kinds", title: "The other kinds", text: "Unit tests, an e2e automation, an e2e run and its cancel." },
        { anchor: "dispatch", title: "What dispatch answers", text: "scheduler, auto, operator, manual-review, qa-tool per runner mode." },
        { anchor: "errors", title: "Errors", text: "Every status and code." },
        { anchor: "limits", title: "Limits", text: "One write in flight, the open and daily caps, one run per product, autorun." },
        { anchor: "troubleshooting", title: "Troubleshooting", text: "401, 403, a card left in Ready, held, a replay." }
      ]
    },
    /* pustaka:insert — `pustaka new` adds entries above this marker */
  ]
});
