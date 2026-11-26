/* GAOS live updates (M15, D-065).
 *
 * GET events/stream is server-sent events carrying HINTS -- {seq, type, kind,
 * product, task} -- never HTML. A hint only says which fragment is stale; this
 * file re-fetches that fragment through the ordinary routes with htmx.ajax,
 * so there is exactly one rendering path and it is the server's.
 *
 *   question -> the topbar chip (GET ui/needs), and the inbox if it is open
 *   task     -> the board, if it is open on the hinted product
 *
 * The stream needs the back-office session. JavaScript cannot read that cookie
 * (HttpOnly), so "signed in" is read from the page: the back-office sheet is
 * rendered only for a session. Signed out, no stream is opened; signing in
 * swaps the sheet in, and that swap opens it.
 */
(function () {
  "use strict";

  var es = null;
  var retryTimer = null;
  var offTimer = null;
  var backoff = 3000;           // matches the server's retry: 3000
  var OFFLINE_AFTER = 4000;     // a blip is not news; four quiet seconds are
  var pending = {};             // debounce timers by fragment

  function signedIn() {
    var bo = document.getElementById("boSheet");
    return !!(bo && !bo.hidden);
  }

  // The dot lives in the back-office sheet's head, which every mutation
  // re-renders; the state is kept here and put back after each swap.
  var isOffline = false;
  function offline(on) {
    isOffline = on;
    var dot = document.getElementById("liveOff");
    if (dot) dot.hidden = !on;
  }

  // X-GAOS-Refresh marks a request nobody clicked for (D-094). The server
  // never moves the address for one: a refresh that lands after the operator
  // has gone elsewhere must not push them a history entry.
  function get(url, then) {
    if (!window.htmx) return;
    var p = htmx.ajax("GET", url, { target: "#sink", swap: "innerHTML", headers: { "X-GAOS-Refresh": "1" } });
    if (then && p && p.then) p.then(then, function () {});
  }

  // A burst of hints (an answer is a QuestionAnswered and a TaskMoved) is one
  // refresh, not three.
  function soon(key, fn) {
    clearTimeout(pending[key]);
    pending[key] = setTimeout(fn, 200);
  }

  // Re-rendering the back-office sheet replaces it, which would throw away a
  // half-typed form, the scroll position and the focus. So a pane refreshes
  // only when nothing in it is being edited and no dialog is up; otherwise the
  // refresh waits until the operator lets go.
  function busy() {
    if (document.querySelector(".modal-back:not([hidden])")) return true;
    var a = document.activeElement;
    return !!(a && a.closest && a.closest("#boSheet") && /^(INPUT|TEXTAREA|SELECT)$/.test(a.tagName));
  }

  function keepPlace(url) {
    var body = document.querySelector("#boSheet .sheet-body");
    var top = body ? body.scrollTop : 0;
    var focusId = document.activeElement && document.activeElement.id;
    get(url, function () {
      var b = document.querySelector("#boSheet .sheet-body");
      if (b) b.scrollTop = top;
      var f = focusId && document.getElementById(focusId);
      if (f && document.activeElement === document.body) f.focus({ preventScroll: true });
    });
  }

  function main() { return document.getElementById("boMain"); }

  // What is on screen: the view, the product, the tab and the inbox filter. A
  // refresh is decided when a hint arrives and sent a moment later; it belongs
  // to the screen it was decided for, and is dropped if that screen is gone.
  function where() {
    var m = main();
    return m ? [m.dataset.view || "", m.dataset.product || "", m.dataset.tab || "", m.dataset.filter || ""].join("|") : "";
  }

  var deferred = null; // {url, at}: a refresh held back while the operator is busy
  function refreshPane(url, at) {
    if (at !== where()) { deferred = null; return; }
    if (busy()) { deferred = { url: url, at: at }; return; }
    deferred = null;
    keepPlace(url);
  }
  function runDeferred() {
    if (!deferred || busy()) return;
    var d = deferred;
    deferred = null;
    refreshPane(d.url, d.at);
  }
  // A deferred refresh runs once the dialog closes or the field is left.
  document.addEventListener("focusout", function () { setTimeout(runDeferred, 0); });

  function onQuestion() {
    soon("chip", function () { get("ui/needs"); });
    var m = main();
    if (m && m.dataset.view === "inbox") {
      var f = m.dataset.filter, at = where();
      soon("inbox", function () { refreshPane("ui/bo/inbox" + (f ? "?product=" + encodeURIComponent(f) : ""), at); });
    }
  }

  /* ---------- the Board (D-095) ----------
     The Board tab keeps its view in a form (#boardFilters) and its window in
     the DOM: how deep each column is open (data-shown) and what each holds
     (data-rev). Three things here keep the server informed and the screen calm:

       - every request made from the tab carries that state in two headers;
       - a hint asks the form for a refresh, and the answer is only the
         columns that differ from what is on screen;
       - a column that is replaced keeps its scroll, and a card that arrived
         in it is marked for a moment.

     The search text travels in a header and never in a URL: the search box
     has no name, so nothing can serialise it, and a URL is what gets logged. */
  var BOARD_KEYS = ["view", "role", "story", "label", "wf", "waiting", "empty", "sort", "dir"];
  function boardForm() { return document.getElementById("boardFilters"); }
  function onBoard() {
    var m = main();
    return !!(m && m.dataset.tab === "board" && boardForm());
  }
  function sisflowOn() {
    var host = document.getElementById("board-host");
    return !!(host && host.style.display !== "none");
  }
  // The view and the window, as one URL-encoded string.
  function boardState() {
    var form = boardForm();
    if (!form) return "";
    var p = new URLSearchParams(), fd = new FormData(form);
    BOARD_KEYS.forEach(function (k) { var v = fd.get(k); if (v !== null) p.set(k, v); });
    var table = document.getElementById("boardTable");
    if (table) {
      p.set("rows", table.dataset.rows || "");
    } else if (sisflowOn()) {
      p.set("shown", document.getElementById("board-host").dataset.shown || "");
    } else {
      var shown = [], rev = [];
      document.querySelectorAll(".board-ssr .board-col").forEach(function (c) {
        shown.push(c.dataset.col + ":" + c.dataset.shown);
        if (c.dataset.rev) rev.push(c.dataset.col + ":" + c.dataset.rev);
      });
      p.set("shown", shown.join(","));
      p.set("rev", rev.join(","));
    }
    return p.toString();
  }
  function boardSearch() {
    var box = document.getElementById("boardSearch");
    return box && box.value ? encodeURIComponent(box.value) : "";
  }
  // board.js is a module and cannot reach into this closure; these two are
  // what it needs to send the same headers with its own fetch.
  window.gaosBoard = { state: boardState, search: boardSearch };

  // Set just before the form is asked to refresh; the next request the form
  // makes is that refresh, and is marked as one nobody clicked for.
  var refreshing = false;
  document.body.addEventListener("htmx:configRequest", function (e) {
    if (!onBoard()) return;
    var base = new URL("products/" + encodeURIComponent(main().dataset.product), document.baseURI).pathname;
    var path = new URL(e.detail.path, document.baseURI).pathname;
    if (path !== base && path.indexOf(base + "/") !== 0) return;
    e.detail.headers["X-Board-State"] = boardState();
    var elt = e.detail.elt;
    var q = boardSearch();
    // A link that clears the filters clears the search with them.
    if (q && !(elt && elt.closest && elt.closest("[data-board-reset]"))) e.detail.headers["X-Board-Search"] = q;
    if (refreshing && elt === boardForm()) {
      refreshing = false;
      e.detail.headers["X-GAOS-Refresh"] = "1";
    }
  });

  // A refresh waits while the operator is in the middle of something the
  // answer would take away: a dialog, a drag, a field of a form in the sheet.
  // The filter bar does not count -- a refresh never replaces it.
  var dragging = false;
  document.addEventListener("dragstart", function (e) {
    if (e.target.closest && e.target.closest(".board-card")) dragging = true;
  });
  ["dragend", "drop"].forEach(function (name) {
    document.addEventListener(name, function () { dragging = false; setTimeout(boardCatchUp, 0); });
  });
  function boardBusy() {
    if (dragging || document.querySelector(".modal-back:not([hidden])")) return true;
    var a = document.activeElement;
    return !!(a && a.closest && a.closest("#boSheet") && !a.closest("#boardFilters") && /^(INPUT|TEXTAREA|SELECT)$/.test(a.tagName));
  }

  // Hints are collected for 250 ms after the last one and for at most a
  // second after the first; a hidden tab collects them until it is shown.
  var boardTimer = null, boardFirst = 0, boardPending = false;
  function boardDirty() {
    if (document.hidden) { boardPending = true; return; }
    var now = Date.now();
    if (!boardTimer) boardFirst = now;
    clearTimeout(boardTimer);
    var at = where();
    boardTimer = setTimeout(function () {
      boardTimer = null;
      if (at !== where()) return; // the operator has left this board
      boardRefresh();
    }, Math.max(0, Math.min(250, boardFirst + 1000 - now)));
  }
  function boardRefresh() {
    var form = boardForm();
    if (!form || !window.htmx) return;
    if (boardBusy()) { boardPending = true; return; }
    boardPending = false;
    refreshing = true;
    htmx.trigger(form, "refresh");
  }
  function boardCatchUp() {
    if (boardPending && onBoard() && !document.hidden && !boardBusy()) { boardPending = false; boardDirty(); }
  }
  document.addEventListener("visibilitychange", boardCatchUp);
  document.addEventListener("gaos:modal-closed", boardCatchUp);
  document.addEventListener("focusout", function () { setTimeout(boardCatchUp, 0); });

  function onTask(hint) {
    var m = main();
    if (!m || m.dataset.tab !== "board" || !hint || hint.product !== m.dataset.product) return;
    if (boardForm()) { boardDirty(); return; }
    // No filter bar on screen (a template older than this script): the whole
    // pane, as before.
    var at = where();
    soon("board", function () {
      if (at !== where()) return;
      refreshPane("products/" + encodeURIComponent(m.dataset.product) + "?tab=board", at);
    });
  }

  // What a swap would otherwise take away. Any response may replace a column
  // (out of band) or the whole sheet; either way, if the same product's board
  // is still there afterwards, each scroller goes back where it was. And when
  // the swap answers a refresh, the cards each column held are remembered, so
  // the ones that are new to it can be marked.
  var kept = null;
  document.body.addEventListener("htmx:beforeSwap", function (e) {
    kept = null;
    var ssr = document.querySelector(".board-ssr"), m = main();
    if (!ssr || !m) return;
    var hdr = (e.detail.requestConfig && e.detail.requestConfig.headers) || {};
    var a = document.activeElement, more = a && a.closest ? a.closest(".board-more") : null, col = more && more.closest(".board-col");
    kept = { product: m.dataset.product, left: ssr.scrollLeft, top: {}, ids: hdr["X-GAOS-Refresh"] ? {} : null, more: col ? col.dataset.col : "" };
    ssr.querySelectorAll(".board-col").forEach(function (c) {
      var body = c.querySelector(".board-col-cards");
      kept.top[c.dataset.col] = body ? body.scrollTop : 0;
      if (kept.ids) {
        var set = {};
        c.querySelectorAll(".board-card").forEach(function (k) { set[k.dataset.card] = 1; });
        kept.ids[c.dataset.col] = set;
      }
    });
  });
  function markFresh(card) {
    card.classList.add("is-fresh");
    setTimeout(function () {
      card.classList.add("is-fading");
      setTimeout(function () { card.classList.remove("is-fresh", "is-fading"); }, 200);
    }, 1500);
  }
  document.body.addEventListener("htmx:afterSettle", function () {
    var k = kept;
    kept = null;
    var ssr = document.querySelector(".board-ssr"), m = main();
    if (!k || !ssr || !m || m.dataset.tab !== "board" || m.dataset.product !== k.product) return;
    if (ssr.scrollLeft !== k.left) ssr.scrollLeft = k.left;
    ssr.querySelectorAll(".board-col").forEach(function (c) {
      var id = c.dataset.col, body = c.querySelector(".board-col-cards");
      if (body && k.top[id] && body.scrollTop !== k.top[id]) body.scrollTop = k.top[id];
      if (k.ids && k.ids[id]) {
        c.querySelectorAll(".board-card").forEach(function (card) {
          if (!k.ids[id][card.dataset.card]) markFresh(card);
        });
      }
    });
    // "Show more" replaced the column its button was in; focus goes to the
    // button that took its place, or to the column's last card.
    if (k.more && document.activeElement === document.body) {
      var col = document.getElementById("col-" + k.more);
      var next = col && (col.querySelector(".board-more") || col.querySelector(".board-card:last-child .board-move, .board-card:last-child button"));
      if (next) next.focus({ preventScroll: true });
    }
  });

  function parse(e) {
    try { return JSON.parse(e.data); } catch (_) { return null; }
  }

  function open() {
    if (es || !signedIn() || typeof EventSource === "undefined") return;
    clearTimeout(retryTimer);
    es = new EventSource(new URL("events/stream", document.baseURI).href);
    es.addEventListener("open", function () {
      backoff = 3000;
      clearTimeout(offTimer);
      offTimer = null;
      offline(false);
      // Whatever happened while the stream was down -- or before sign-in --
      // is not in any hint this connection will send.
      get("ui/needs");
    });
    es.addEventListener("question", function (e) { onQuestion(parse(e)); });
    es.addEventListener("task", function (e) { onTask(parse(e)); });
    es.addEventListener("error", function () {
      if (!offTimer) offTimer = setTimeout(function () { offline(signedIn()); }, OFFLINE_AFTER);
      // CONNECTING: the browser is already retrying (retry: 3000). CLOSED: it
      // gave up -- a 401, a 501, a proxy error page -- and only we can retry.
      if (es && es.readyState === EventSource.CLOSED) {
        es.close();
        es = null;
        if (!signedIn()) { clearTimeout(offTimer); offTimer = null; offline(false); return; }
        retryTimer = setTimeout(open, backoff);
        backoff = Math.min(backoff * 2, 60000);
      }
    });
  }

  function close() {
    clearTimeout(retryTimer);
    clearTimeout(offTimer);
    offTimer = null;
    if (es) { es.close(); es = null; }
    offline(false);
  }

  // Sign-in and sign-out both arrive as a swap of the back-office sheet.
  document.body.addEventListener("htmx:oobAfterSwap", function (e) {
    if (!e.target || e.target.id !== "boSheet") return;
    if (signedIn()) { open(); offline(isOffline); } else close();
    runDeferred();
  });
  document.addEventListener("gaos:modal-closed", runDeferred);
  document.body.addEventListener("htmx:afterSwap", runDeferred);

  // The inbox's times are elapsed time and time left, not snapshots: re-tick
  // them from their datetime so "waiting 12m" keeps counting without a
  // refresh. Same format as relSpan in handlers_bo.go. Text only -- nothing
  // moves, so nothing animates.
  function span(ms) {
    var m = Math.floor(ms / 60000);
    if (m < 1) return "<1m";
    if (m < 60) return m + "m";
    var h = Math.floor(m / 60);
    if (h < 48) return h + "h";
    return Math.floor(h / 24) + "d";
  }
  function tick() {
    var now = Date.now();
    document.querySelectorAll("time[data-since]").forEach(function (el) {
      var t = Date.parse(el.getAttribute("data-since"));
      if (!isNaN(t)) el.textContent = "waiting " + span(Math.max(0, now - t));
    });
    // A card's lead time (D-081): elapsed since the card was planned, no prefix.
    document.querySelectorAll("time[data-elapsed-since]").forEach(function (el) {
      var t = Date.parse(el.getAttribute("data-elapsed-since"));
      if (!isNaN(t)) el.textContent = span(Math.max(0, now - t));
    });
    document.querySelectorAll("time[data-until]").forEach(function (el) {
      var t = Date.parse(el.getAttribute("data-until"));
      if (isNaN(t)) return;
      var left = t - now;
      el.textContent = left <= 0 ? "expires now" : "expires in " + span(left);
      el.classList.toggle("urgent", left < 3 * 24 * 3600 * 1000);
    });
  }
  setInterval(tick, 30000);
  document.body.addEventListener("htmx:afterSettle", tick);

  // A hidden tab keeps its stream; the browser is fine with one idle
  // connection, and coming back to a stale chip is worse.
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", open);
  else open();
})();
