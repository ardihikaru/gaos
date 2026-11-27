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

// The card's lead time (D-081). The server formats the text and the title;
// an open card's chip holds a <time data-elapsed-since> that live.js re-ticks,
// so it keeps counting while nothing moves. textContent only.
function timingChip(product, card) {
  const t = card.timing;
  if (!t) return null;
  const b = document.createElement("button");
  b.type = "button";
  b.className = "tm-chip mono";
  b.id = "tm-" + card.id;
  b.title = t.title || "";
  b.setAttribute("aria-label", "Timing of " + card.id + ": " + (t.title || ""));
  b.dataset.timingOpen = "ui/timing/" + encodeURIComponent(product) + "/" + encodeURIComponent(card.id);
  if (t.known && t.complete && !t.finished && t.createdAt > 0) {
    const tm = document.createElement("time");
    tm.dataset.elapsedSince = new Date(t.createdAt * 1000).toISOString();
    tm.textContent = t.leadText;
    b.append(tm);
  } else {
    b.textContent = t.leadText || "—";
  }
  b.addEventListener("keydown", (ev) => {
    if (["Enter", " ", "F2", "Delete", "Backspace"].includes(ev.key)) ev.stopPropagation();
  });
  return b;
}

// The card's conversation (D-082): how many attempts it has; opens the
// conversation sheet. Shown only while the conversation is active -- an
// archived one is read from the Archive page. textContent only.
function conversationChip(product, card) {
  const c = card.conversation;
  if (!c || c.archived || c.pruned || !c.attempts) return null;
  const b = document.createElement("button");
  b.type = "button";
  b.className = "cv-chip mono";
  b.id = "cv-" + card.id;
  b.textContent = c.attempts + (c.attempts === 1 ? " attempt" : " attempts");
  b.setAttribute("aria-label", "Conversation of " + card.id + ": " + b.textContent);
  b.dataset.sheetOpen = "ui/conversation/" + encodeURIComponent(product) + "/" + encodeURIComponent(card.id);
  b.addEventListener("keydown", (ev) => {
    if (["Enter", " ", "F2", "Delete", "Backspace"].includes(ev.key)) ev.stopPropagation();
  });
  return b;
}

// The design gate's decoration (D-084): why a blocked card is held, and on a
// design-blocked review the button that sends its design back -- or the line
// that says the cap is reached. textContent only.
function gateFace(product, card) {
  const out = [];
  if (card.reason) {
    const r = document.createElement("div");
    r.className = "mono card-reason";
    r.textContent = "blocked: " + card.reason;
    out.push(r);
  }
  const sb = card.sendBack;
  if (sb && sb.open) {
    const b = document.createElement("button");
    b.type = "button";
    b.className = "btn card-sendback";
    b.id = "sb-" + card.id;
    b.textContent = "Revise design";
    b.setAttribute("aria-label", "Send the design " + sb.design + " back to its author");
    b.dataset.sheetOpen = "ui/send-back/" + encodeURIComponent(product) + "/" + encodeURIComponent(card.id);
    b.addEventListener("keydown", (ev) => {
      if (["Enter", " ", "F2", "Delete", "Backspace"].includes(ev.key)) ev.stopPropagation();
    });
    out.push(b);
  } else if (sb) {
    const l = document.createElement("div");
    l.className = "mono card-reason";
    l.textContent = "revision limit reached (" + sb.sent + " of " + sb.cap + ")";
    out.push(l);
  }
  return out;
}

// severityChip is the reviewer's severity (D-102): the level as text, the
// colour only repeating it. "none" has no chip.
function severityChip(card) {
  if (!card.severity || card.severity === "none") return null;
  const el = document.createElement("span");
  el.className = "card-label mono sev-" + card.severity;
  el.title = "Review severity: " + card.severity;
  el.textContent = card.severity;
  return el;
}

function cardFace(product, card, ctx) {
  const face = ctx.defaultRender(card);
  const sev = severityChip(card);
  if (sev) (face.querySelector(".sfb-card-meta") || face.querySelector(".sfb-card-content") || face).append(sev);
  const badge = vendorBadge(card);
  if (badge) (face.querySelector(".sfb-card-meta") || face.querySelector(".sfb-card-content") || face).append(badge);
  const qb = qaBadge(card);
  if (qb) (face.querySelector(".sfb-card-content") || face).append(qb);
  const cc = conversationChip(product, card);
  if (cc) (face.querySelector(".sfb-card-content") || face).append(cc);
  const tc = timingChip(product, card);
  if (tc) (face.querySelector(".sfb-card-content") || face).append(tc);
  if (card.story) {
    const st = document.createElement("span");
    st.className = "card-label mono";
    st.textContent = card.story;
    (face.querySelector(".sfb-card-meta") || face.querySelector(".sfb-card-content") || face).append(st);
  }
  for (const el of gateFace(product, card)) (face.querySelector(".sfb-card-content") || face).append(el);
  return card.status === AWAITING ? [needsBadge(product, card), face] : face;
}

function themeName() {
  return document.documentElement.getAttribute("data-theme") === "light" ? "sismaya-light" : "sismaya-dark";
}

function say(host, text) {
  const out = host.parentElement && host.parentElement.querySelector(".board-status");
  if (out) out.textContent = text;
}

// What the client board remembers across a re-render of the sheet (D-095). A
// send-back, a label or an answer replaces the sheet and with it the host
// element, so sisflow mounts again; kept by product, not by element, the new
// mount opens at the depth and the scroll position the old one had.
const depths = new Map(); // product id -> { column id: cards wanted }
const scrolls = new Map(); // product id -> [scrollLeft, scrollTop]
const STEP = 30, MAX_SHOWN = 300;

function pairs(obj) {
  return Object.keys(obj).map((k) => k + ":" + obj[k]).join(",");
}

// A card that arrived in a column on a live refresh: one bar, gone by fading.
function fadeFresh(host) {
  setTimeout(() => {
    for (const el of host.querySelectorAll(".sfb-card.is-fresh")) {
      el.classList.add("is-fading");
      setTimeout(() => el.classList.remove("is-fresh", "is-fading"), 200);
    }
  }, 1500);
}

export function mount(host) {
  if (!host || mounted.has(host)) return;
  const product = host.dataset.product;
  const base = new URL("products/" + encodeURIComponent(product), document.baseURI).pathname;
  const form = document.getElementById("boardFilters");
  const pane = document.getElementById("boardPane");
  if (!depths.has(product)) depths.set(product, {});
  const shown = depths.get(product);
  let board = null;
  let doc = null; // the document on screen
  const publish = () => { host.dataset.shown = pairs(shown); };
  publish();

  // The board's document is one page of the product's cards: the filter the
  // bar holds, each column at its depth (GET …/board?paged=1). The view and
  // the window travel in the same two headers every request from this tab
  // carries (static/live.js); the search text is in neither URL nor body.
  const fetchDoc = async () => {
    const headers = {};
    if (window.gaosBoard) {
      headers["X-Board-State"] = window.gaosBoard.state();
      const q = window.gaosBoard.search();
      if (q) headers["X-Board-Search"] = q;
    }
    const res = await fetch(base + "/board?paged=1", { headers });
    if (!res.ok) throw Object.assign(new Error("the board did not load"), { status: res.status });
    return res.json();
  };
  // hinted: this reload answers a live refresh, so a card that is new to its
  // column is marked. A filter change or "Show more" marks nothing.
  const dress = (next, hinted) => {
    const before = new Map(((doc && doc.cards) || []).map((c) => [c.id, c.column]));
    for (const c of next.cards || []) {
      const css = [];
      if (c.status === AWAITING) css.push("is-needs-you");
      if (hinted && doc && before.get(c.id) !== c.column) css.push("is-fresh");
      if (css.length) c.css = css.join(" ");
    }
    return next;
  };

  const create = () => {
    board && board.destroy();
    // sisflow writes to {base}/cards/… and reads {base}/board; GAOS's routes
    // are …/board and …/board/cards/…, so the writes take …/board as their base
    // and the read is made here.
    const rest = createRestProvider({ base: base + "/board", retry: { max: 0 }, debounceMs: 0 });
    const provider = {
      ...rest,
      async load() {
        if (!doc) doc = dress(await fetchDoc(), false);
        return doc;
      },
      // The library's own paging stays off: it cannot be told a column is
      // finished and stalls after a page that adds nothing. "Show more" is the
      // count line's button and a deeper reload (below).
      async loadPage() {
        return { cards: [], exhausted: true };
      },
      push(entry) {
        say(host, "");
        rest.push(entry);
        rest.flush().then(() => reload(false));
      },
    };
    board = createBoard(host, {
      provider, theme: themeName(), cards: [], columns: [], rows: [],
      // There are no lanes: sisflow draws one for cards with no row, and its
      // header (hidden in app.css) would otherwise say "(none)".
      catalogs: { en: { "lane.none": "" } },
      // A column never holds more than MAX_SHOWN cards, so the library's
      // virtual list (on above 30 by default) is never needed -- and it must
      // stay off: when a reload leaves a column with fewer cards than before,
      // an observer of the old, virtualised list fires once more and renders
      // a card that is gone.
      virtual: { threshold: MAX_SHOWN },
      cardTemplate: (card, ctx) => cardFace(host.dataset.product, card, ctx),
    });
    board.on("board:provider-error", ({ error, status }) =>
      say(host, status === 409 ? "Refused: the task state machine does not allow that move." : (status || 0) + ": " + error.message));
  };

  // One reload for everything that changes what the board shows: a hint, a
  // save, a filter, "Show more". It hands the new document to the board's own
  // store, which redraws once and keeps the scroller -- where this used to
  // destroy the board and build another. board._store is the vendored
  // library's, at the version in the tree; without it, the old way.
  let again = null;
  const reload = async (hinted) => {
    if (!host.isConnected) return;
    // A drag in progress wins: reloading under the pointer would drop the card mid-air.
    if (host.querySelector(".sfb-dragging") || host.firstElementChild?.classList.contains("sfb-dragging")) {
      clearTimeout(again);
      again = setTimeout(() => reload(hinted), 500);
      return;
    }
    publish();
    let next;
    try {
      next = dress(await fetchDoc(), hinted);
    } catch (err) {
      say(host, "The board did not refresh" + (err.status ? " (" + err.status + ")" : "") + "; it shows what it had.");
      return;
    }
    doc = next;
    say(host, "");
    // The store redraws the board from the new document, and a redraw puts
    // its scroller back at the start: where the operator was is put back, now
    // and once more after the frame in which the library lays the cards out.
    const body = host.querySelector(".sfb-body");
    const was = body ? [body.scrollLeft, body.scrollTop] : scrolls.get(product);
    if (board && board._store && typeof board._store.load === "function") board._store.load(doc);
    else create();
    if (was) {
      const keep = () => { const b = host.querySelector(".sfb-body"); if (b) [b.scrollLeft, b.scrollTop] = was; };
      keep();
      requestAnimationFrame(keep);
    }
    if (hinted) fadeFresh(host);
  };

  create();
  mounted.set(host, true);

  if (form) {
    // The form's own request is answered with the count line only while this
    // board is on screen; its arrival is the cue to load the cards.
    const surface = document.createElement("input");
    surface.type = "hidden";
    surface.name = "surface";
    surface.value = "sisflow";
    form.append(surface);
    form.addEventListener("htmx:afterRequest", (ev) => {
      if (ev.detail.elt !== form) return;
      const headers = (ev.detail.requestConfig && ev.detail.requestConfig.headers) || {};
      reload(!!headers["X-GAOS-Refresh"]);
    });
  }
  if (pane) {
    pane.classList.add("sisflow-on");
    // "Show more": one column deeper, then the count line and the cards.
    pane.addEventListener("click", (ev) => {
      const btn = ev.target.closest && ev.target.closest("[data-board-more]");
      if (!btn) return;
      const col = btn.dataset.boardMore;
      shown[col] = Math.min((shown[col] || STEP) + STEP, MAX_SHOWN);
      publish();
      if (form && window.htmx) htmx.trigger(form, "refresh");
      else reload(false);
    });
  }

  // Needs you sits right of Working, often scrolled into view; nothing the
  // operator did not ask for may throw them back to Backlog.
  host.addEventListener("scroll", (ev) => {
    const b = ev.target;
    if (b && b.classList && b.classList.contains("sfb-body")) scrolls.set(product, [b.scrollLeft, b.scrollTop]);
  }, true);
  // A new mount on the same product (the sheet was re-rendered) goes back to
  // where the old one was. The cards arrive after the document is fetched, so
  // the position is tried until the scroller is long enough to take it.
  const at = scrolls.get(product);
  if (at && (at[0] || at[1])) {
    let tries = 0;
    const keep = () => {
      if (!host.isConnected) return;
      const b = host.querySelector(".sfb-body");
      if (b) [b.scrollLeft, b.scrollTop] = at;
      if ((!b || b.scrollLeft !== at[0] || b.scrollTop !== at[1]) && ++tries < 25) setTimeout(keep, 100);
    };
    setTimeout(keep, 0);
  }

  // style, not the hidden attribute: both elements carry a display of their
  // own, which an author style gives precedence over [hidden].
  host.style.display = "";
  const ssr = host.parentElement && host.parentElement.querySelector(".board-ssr");
  if (ssr) {
    ssr.style.display = "none";
    // The server board has done its job -- it was on screen until this one
    // was ready -- and a hidden copy of every card is a few thousand elements
    // the page does not need. The next render of the sheet brings it back.
    ssr.replaceChildren();
  }
}
