/* ToC part — group "Maintaining these docs".
   This site runs on Pustaka, an HTML-first docs engine vendored into the GAOS
   repository. These pages are the engine's own authoring documentation, kept
   because anyone adding a page here needs them. They document the DOCS, not
   GAOS. Menu nodes use the key order id, title, children; page leaves keep
   id, file, title, desc, tags, sections. */
window.DOCS.nav.push({
  group: "Maintaining these docs",
  pages: [
    {
      id: "getting-started", file: "guide/getting-started.html", title: "Getting started",
      desc: "Folder layout, adding your first page, registering it in the table of contents, running the engine.",
      tags: ["setup", "tutorial", "quickstart", "authoring"],
      sections: [
        { anchor: "layout", title: "Project layout", text: "The site is HTML files plus a registry, with no build step." },
        { anchor: "first-page", title: "Add your first page", text: "Create a page from the template and author plain HTML." },
        { anchor: "register", title: "Register it in the ToC", text: "Register the page so navigation and search discover it." },
        { anchor: "run", title: "Run it", text: "Open statically or serve through the Go engine." }
      ]
    },
    {
      id: "guide-authoring-menu", title: "Authoring", children: [
        {
          id: "authoring", file: "guide/authoring.html", title: "Writing pages",
          desc: "The component library: metadata, admonitions, tabs, code blocks, tables, and filtering.",
          tags: ["components", "metadata", "admonition", "tabs", "code", "filter", "reference"],
          sections: [
            { anchor: "structure", title: "Page structure", text: "Authored content lives in main.doc; the runtime injects the shell." },
            { anchor: "admonitions", title: "Admonitions", text: "Note, tip and warning callouts use simple classes." },
            { anchor: "tabs-code", title: "Tabs & code blocks", text: "Tabs and copyable code blocks are runtime wired." },
            { anchor: "page-filter", title: "Filtering page content", text: "data-filter makes page content filterable." }
          ]
        },
        {
          id: "markdown-to-html", file: "guide/markdown-to-html.html", title: "Markdown to HTML",
          desc: "Convert Markdown into a clean page, then validate and register it.",
          tags: ["markdown", "html", "pandoc", "conversion", "migration", "tutorial"],
          sections: [
            { anchor: "convert", title: "Convert the source", text: "Use Pandoc to produce an HTML fragment from GitHub-Flavored Markdown." },
            { anchor: "fit-the-template", title: "Fit the template", text: "Place the fragment inside main.doc and restore the page contract." },
            { anchor: "repair-semantics", title: "Repair semantics", text: "Add heading ids, translate extensions, and sanitize untrusted output." },
            { anchor: "register-and-check", title: "Register and check", text: "Register the page and use the checker as the acceptance gate." },
            { anchor: "ai-recipe", title: "AI conversion recipe", text: "A copyable prompt gives an AI deterministic conversion inputs and outputs." }
          ]
        },
        {
          id: "page-metadata", file: "guide/page-metadata.html", title: "Page metadata",
          desc: "Add optional tags, hashtags, publication dates, update dates, and version applicability to a page header.",
          tags: ["metadata", "tags", "hashtags", "published", "updated", "semver", "version"],
          sections: [
            { anchor: "contract", title: "The metadata contract", text: "One declarative element renders the complete header metadata panel." },
            { anchor: "version-ranges", title: "Version ranges", text: "A required lower SemVer bound and optional upper bound describe applicability." },
            { anchor: "validation", title: "Validation", text: "The checker rejects malformed dates, tokens, duplicates, and reversed ranges." },
            { anchor: "ai-prompt", title: "AI prompt", text: "A copyable prompt produces metadata without inventing values." }
          ]
        },
        {
          id: "nested-sidebar", file: "guide/nested-sidebar.html", title: "Nested sidebar",
          desc: "Implement recursive sidebar sub-menus with active paths, persistence, keyboard controls, and ARIA.",
          tags: ["sidebar", "navigation", "submenu", "recursive", "accessibility", "toc"],
          sections: [
            { anchor: "schema", title: "Recursive schema", text: "Menu nodes contain children while page leaves keep the established contract." },
            { anchor: "runtime-behavior", title: "Runtime behavior", text: "Active ancestors open automatically and other branches persist." },
            { anchor: "migration", title: "Migrate a flat group", text: "Wrap existing leaves without changing their page records." },
            { anchor: "ai-recipe", title: "AI authoring recipe", text: "Rules for safely editing a recursive ToC." }
          ]
        },
        {
          id: "ai-tutorial", file: "guide/ai-tutorial.html", title: "AI authoring tutorial",
          desc: "Build the prompt, attach the right context, and run the check-loop that validates AI-generated pages.",
          tags: ["ai", "prompt", "generation", "llm", "automation", "tutorial"],
          sections: [
            { anchor: "four-inputs", title: "The four inputs of a good prompt", text: "Role, standard, registry state and task brief." },
            { anchor: "prompt-template", title: "A prompt template you can copy", text: "A complete page-authoring task template." },
            { anchor: "the-loop", title: "The correction loop", text: "Generate, check, correct and repeat." },
            { anchor: "failure-modes", title: "Failure modes and their fixes", text: "Common structural and lifecycle errors." },
            { anchor: "ci-gate", title: "Make the gate automatic", text: "Run the validator in continuous integration." }
          ]
        }
      ]
    }
    {
      id: "qa-sync-api", file: "guide/qa-sync-api.html", title: "The QA sync API",
      desc: "The public contract for the QA tools: the bearer-token REST API GAOS serves, the two endpoints GAOS calls, and which side owns which field of a test case.",
      tags: ["api", "rest", "qa", "integration", "bearer", "curl", "catalog"],
      sections: [
        { anchor: "base-and-auth", title: "Base URL and authentication", text: "The base URL, the bearer token, the qa and admin scopes, product scoping and the fail-closed rules." },
        { anchor: "limits-and-errors", title: "Limits, errors and status codes", text: "200 items and 1 MiB a request, strict JSON, one write in flight per token, the error shape and what each status means." },
        { anchor: "calls", title: "The calls GAOS serves", text: "One curl example per call: products, summary, the cases feed, create-only cases and the card a new epic adds, results and the run-record rules, the admin catalog and the product seed." },
        { anchor: "ownership", title: "Who owns which field", text: "The planning half is GAOS's and the automation half is the QA tools'; inside the automation half the newest run wins." },
        { anchor: "gaos-calls", title: "What GAOS calls on the QA tools", text: "GET /api/v1/gaos/results with its page shape and POST /api/v1/gaos/catalog, behind the two sync buttons." }
      ]
    },
    {
      id: "bo-sign-in", file: "guide/bo-sign-in.html", title: "Back-office sign-in",
      desc: "How the GAOS back office authenticates an operator: the hashed accounts file, the signed session cookie, the throttle, and what needs a session.",
      tags: ["auth", "login", "session", "back-office", "operations", "password"],
      sections: [
        { anchor: "what-it-is", title: "What it is", text: "An account is a line of a file, a session is a signed twelve-hour cookie, and there is no open mode." },
        { anchor: "accounts", title: "Accounts", text: "The users file format, the hash-password command, and why the file and the key are secrets." },
        { anchor: "sessions", title: "Sessions and revocation", text: "The cookie's shape, the key and the deployment label, and the only ways a session ends." },
        { anchor: "throttle", title: "Throttle", text: "Five failures lock one account for fifteen minutes; why there is no per-address lock." },
        { anchor: "routes", title: "What needs a session", text: "Public, bearer and back-office routes, and the cross-origin rule." },
        { anchor: "limits", title: "What this does not do", text: "No roles, no tenant scoping, no actor on events, no reset page." }
      ]
    },
    /* pustaka:insert — `pustaka new` adds entries above this marker */
  ]
});
