/* ToC part — group "Integrations" (v0.45.0, D-085).
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
    /* pustaka:insert — `pustaka new` adds entries above this marker */
  ]
});
