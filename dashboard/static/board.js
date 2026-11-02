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
        for (const c of doc.cards || []) if (c.row === "plan") c.row = null;
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
    });
    board.on("board:provider-error", ({ error, status }) =>
      say(host, status === 409 ? "Refused: the task state machine does not allow that move." : (status || 0) + ": " + error.message));
  };

  create();
  mounted.set(host, true);
  // style, not the hidden attribute: both elements carry a display of their
  // own, which an author style gives precedence over [hidden].
  host.style.display = "";
  const ssr = host.parentElement && host.parentElement.querySelector(".board-ssr");
  if (ssr) ssr.style.display = "none";
}
