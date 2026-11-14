// The sisflow board on a product's Board tab (T-165, I-10, D-063).
//
// Progressive: the server-rendered board in the same pane is what a page
// without JavaScript, or without ES modules, shows, and it stays in the page
// until this has mounted. The client renders nothing the server did not say:
// it loads GET products/{id}/board and sends every drag as PUT
// products/{id}/board/cards/{taskId}/move through sisflow's own REST provider. A move is a request
// (BR-43) -- a refusal answers 409, the provider reports it, and the board
// rolls the card back. Retrying a refusal would only be refused again, so the
// provider does not retry. After every save the board is reloaded, because an
// accepted move can still land somewhere else than where it was dropped (a
// dispatch puts the card in Working, and the runner moves it on).
import { createBoard, createRestProvider } from "./vendor/sisflow/board/index.js";

const mounted = new WeakMap();
const AWAITING = "AWAITING_INPUT";

// "Needs you" (M15): a waiting card carries a badge that opens the answer
// sheet. The badge is a button, so sisflow's drag ignores it; app.js handles
// the click by delegation, exactly as it does for the server board's badge.
function needsBadge(product, card) {
  const b = document.createElement("button");
  b.type = "button";
  b.className = "needs-badge";
  b.id = "nb-" + card.id;
  b.textContent = "Needs you";
  b.setAttribute("aria-label", "Needs you — answer the agent on " + card.id);
  b.dataset.answerOpen = "ui/answer/" + encodeURIComponent(product) + "/" + encodeURIComponent(card.id) + "?from=board";
  // Enter on a focused card opens sisflow's editor and Delete asks to delete
  // it; on the badge those keys belong to the badge.
  b.addEventListener("keydown", (ev) => {
    if (["Enter", " ", "F2", "Delete", "Backspace"].includes(ev.key)) ev.stopPropagation();
  });
  return b;
}

// The vendor the card's next turn goes to (M16), in the card's meta row: a
// quiet mono tag, lighter than anything else on the card.
const VENDORS = { claude: ["CL", "Claude CLI"], antigravity: ["AG", "Antigravity"] };
function vendorBadge(card) {
  const v = VENDORS[card.vendor];
  if (!v) return null;
  const b = document.createElement("span");
  b.className = "vnd-badge";
  b.textContent = v[0];
  b.title = v[1];
  return b;
}

// The epic card's QA roll-up (display only; the server fills card.qa at read
// time). Built with textContent: nothing from the payload is parsed as HTML.
function qaBadge(card) {
  const q = card.qa;
  if (!q) return null;
  const pct = Math.max(0, Math.min(100, Number(q.pct) || 0));
  const b = document.createElement("span");
  b.className = "qa-badge";
  b.title = q.epic + ": " + q.automated + " of " + q.cases + " cases automated" + (q.fail > 0 ? ", " + q.fail + " failing" : "");
  const t = document.createElement("span");
  t.className = "qa-badge-t mono";
  t.textContent = q.automated + "/" + q.cases + " automated";
  if (q.fail > 0) {
    const f = document.createElement("em");
    f.className = "qa-badge-f";
    f.textContent = q.fail + " failing";
    t.append(" ", f);
  }
  const bar = document.createElement("span");
  bar.className = "cov-bar qa-badge-bar";
  const i = document.createElement("i");
  i.style.width = pct + "%";
  bar.append(i);
  b.append(t, bar);
  return b;
}

function cardFace(product, card, ctx) {
  const face = ctx.defaultRender(card);
  const badge = vendorBadge(card);
  if (badge) (face.querySelector(".sfb-card-meta") || face.querySelector(".sfb-card-content") || face).append(badge);
  const qb = qaBadge(card);
  if (qb) (face.querySelector(".sfb-card-content") || face).append(qb);
  return card.status === AWAITING ? [needsBadge(product, card), face] : face;
}

function themeName() {
  return document.documentElement.getAttribute("data-theme") === "light" ? "sismaya-light" : "sismaya-dark";
}

function say(host, text) {
  const out = host.parentElement && host.parentElement.querySelector(".board-status");
  if (out) out.textContent = text;
}

export function mount(host) {
  if (!host || mounted.has(host)) return;
  const base = new URL("products/" + encodeURIComponent(host.dataset.product), document.baseURI).pathname;
  let board = null;

  const create = () => {
    board && board.destroy();
    // sisflow writes to {base}/cards/… and reads {base}/board; GAOS's routes
    // are …/board and …/board/cards/…, so the writes take …/board as their base
    // and the read is made here.
    const rest = createRestProvider({ base: base + "/board", retry: { max: 0 }, debounceMs: 0 });
    const provider = {
      ...rest,
      // GAOS names the lane of tasks outside any story "plan"; sisflow always
      // draws a lane for cards with no row. They are the same lane, so the
      // plan's cards travel as row-less and that lane is labelled Plan, rather
      // than the board showing an empty extra lane above it.
      async load() {
        const res = await fetch(base + "/board");
        if (!res.ok) throw Object.assign(new Error("the board did not load"), { status: res.status });
        const doc = await res.json();
        doc.rows = (doc.rows || []).filter((r) => r.id !== "plan");
        for (const c of doc.cards || []) {
          if (c.row === "plan") c.row = null;
          if (c.status === AWAITING) c.css = "is-needs-you";
        }
        return doc;
      },
      push(entry) {
        say(host, "");
        rest.push(entry);
        rest.flush().then(() => setTimeout(create, 250));
      },
    };
    board = createBoard(host, {
      provider, theme: themeName(), cards: [], columns: [], rows: [],
      catalogs: { en: { "lane.none": "Plan" } },
      cardTemplate: (card, ctx) => cardFace(host.dataset.product, card, ctx),
    });
    board.on("board:provider-error", ({ error, status }) =>
      say(host, status === 409 ? "Refused: the task state machine does not allow that move." : (status || 0) + ": " + error.message));
  };

  create();
  mounted.set(host, true);
  // live.js asks for a reload when a task hint names this product. A drag in
  // progress wins: reloading under the pointer would drop the card mid-air.
  host.addEventListener("gaos:board-refresh", () => {
    if (!host.isConnected || host.querySelector(".sfb-dragging") || host.firstElementChild?.classList.contains("sfb-dragging")) return;
    // Needs you sits right of Working, often scrolled into view; a refresh the
    // operator did not ask for must not throw them back to Backlog.
    const body = host.querySelector(".sfb-body");
    const at = body ? [body.scrollLeft, body.scrollTop] : null;
    create();
    if (at) {
      const keep = () => { const b = host.querySelector(".sfb-body"); if (b) [b.scrollLeft, b.scrollTop] = at; };
      board.on("board:change", function once() { keep(); board.off?.("board:change", once); });
      setTimeout(keep, 300);
    }
  });
  // style, not the hidden attribute: both elements carry a display of their
  // own, which an author style gives precedence over [hidden].
  host.style.display = "";
  const ssr = host.parentElement && host.parentElement.querySelector(".board-ssr");
  if (ssr) ssr.style.display = "none";
}
