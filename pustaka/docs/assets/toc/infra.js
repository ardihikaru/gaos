/* ToC part — group "Infrastructure".
   Loaded by assets/toc.js (parts list). Keep this file under 200 lines;
   when it grows past that, split the group or move pages to a new part.
   Entries MUST keep the key order: id, file, title, desc, tags, sections. */
window.DOCS.nav.push({
  group: "Infrastructure",
  pages: [
    {
      id: "infra-overview",
      file: "infra/overview.html",
      title: "How the infrastructure runs",
      desc: "Two machines, one public IP: a thin edge that terminates TLS and a private VM that runs every container.",
      tags: ["infrastructure", "topology", "wireguard", "nginx", "traefik", "tls", "edge"],
      sections: [
        { anchor: "two-machines", title: "Two machines, one public IP",
          text: "A 1 vCPU public edge terminates TLS and forwards plain HTTP over WireGuard to a private VM that has no public address and runs every container." },
        { anchor: "request-path", title: "The path of one request",
          text: "Browser to nginx over HTTPS, nginx to the tunnel as plain HTTP with forwarded headers, then the product's own Traefik routes by path to a container." },
        { anchor: "system-architecture", title: "The system, in one picture",
          text: "Three layers and a membership table: which containers may join a shared database network and which cannot reach one at all." },
        { anchor: "who-owns-tls", title: "Who owns TLS",
          text: "Certificates are issued by certbot on the edge over HTTP-01 for two exact hostnames. The design called for Cloudflare DNS-01 wildcards, but these domains are not on Cloudflare and exact names need no DNS credentials." },
        { anchor: "tls-setup", title: "How TLS was set up, step by step",
          text: "DNS first, then certbot, then the :80 block, then issue, then :443, then the renewal hook. nginx needs a certificate to open :443 and the certificate needs nginx to answer :80." },
        { anchor: "verify-live", title: "Verifying it end to end",
          text: "Health, certificate SANs, and the one check nothing else makes for you: whether the real client address survived the hop." },
        { anchor: "real-client-ip", title: "The real client IP",
          text: "The edge cannot be a transparent proxy, so the client address travels in X-Forwarded-For and every proxy behind it must be told which peers to trust." },
        { anchor: "rules", title: "Rules that keep it standing",
          text: "Nothing runs on the edge, nothing binds a public interface on the private VM, and no stack terminates TLS of its own." }
      ]
    },
    {
      id: "infra-environments",
      file: "infra/environments.html",
      title: "Development and production",
      desc: "Two GAOS stacks side by side on one private VM: what differs, what must never differ, and how to bring either up.",
      tags: ["deployment", "environments", "dev", "production", "compose", "arcadedb", "verification"],
      sections: [
        { anchor: "same-shape", title: "The same shape, twice",
          text: "Four services behind one Traefik on a single origin: the landing page at the root, Mission Control under /app, these docs under /docs, and the health surface under /api. Prod adds a fifth, Redis, the traffic store behind the Traffic page." },
        { anchor: "what-differs", title: "What differs, exactly",
          text: "Hostname, published port, compose project, internal subnet, shared infra network, ArcadeDB instance, certificate lineage and env files. A difference outside that table is a bug." },
        { anchor: "what-must-not", title: "What must not differ",
          text: "The Secure cookie flag stays on in both tiers because both sit behind real TLS at the edge, the database name is the same in two instances, and the proxy trust list is one shared file." },
        { anchor: "bring-up", title: "Bringing a tier up",
          text: "Run the stack checks, build, then start. Development first. There is no bootstrap step yet because no schema is applied in this release." },
        { anchor: "access", title: "Where to reach it, and where the credentials are",
          text: "Public URLs per tier and four gitignored env files split per service, so the docs container never holds the database password." },
        { anchor: "model-gateway", title: "The model gateway, per tier",
          text: "GAOS_MODEL_MODE=aimanager, the ai-manager URL, two secret files per tier, and why the tenant in the workspace id keeps the tiers apart." },
        { anchor: "verify", title: "Verifying over the tunnel",
          text: "Check nothing is bound beyond the tunnel, curl the health endpoint from the edge with a Host header, and confirm the base href survived the prefix strip." }
      ]
    },
    {
      id: "infra-neighbours",
      file: "infra/neighbours.html",
      title: "Living beside other products",
      desc: "One private VM hosts several unrelated products. The port register, the network boundaries, and what may be shared.",
      tags: ["multi-tenant", "isolation", "ports", "networks", "shared", "conventions"],
      sections: [
        { anchor: "one-vm-many", title: "One VM, many products",
          text: "Each product owns a stack, a Traefik and one tunnel port. No product terminates TLS, and no product publishes on a public interface." },
        { anchor: "port-register", title: "The port register",
          text: "Ports encode environment and product, so a number can be derived rather than remembered. GAOS holds slot 02." },
        { anchor: "dns", title: "One namespace per product",
          text: "Each product gets a wildcard namespace and one line in the edge map. Wildcards match a single level, so every namespace needs its own record and certificate SAN." },
        { anchor: "boundaries", title: "What is shared and what is not",
          text: "Databases are shared infrastructure joined as external networks; only the services that need a database join one, so a product cannot reach its neighbours' data." },
        { anchor: "ai-manager", title: "A neighbour GAOS calls: ai-manager",
          text: "The one dependency among the neighbours: turns, polling and provisioning over HTTPS, its two credentials, and ai-claude-cli kept private." },
        { anchor: "adding", title: "Adding a product",
          text: "Claim the next port, add a DNS record and a certificate SAN, add one line to the edge map, then everything else is the stack's own business." }
      ]
    }
    /* pustaka:insert — `pustaka new` adds entries above this marker */
  ]
});
