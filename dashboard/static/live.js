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

  function get(url, then) {
    if (!window.htmx) return;
    var p = htmx.ajax("GET", url, { target: "#sink", swap: "innerHTML" });
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

  var deferred = null;
  function refreshPane(url) {
    if (busy()) { deferred = url; return; }
    deferred = null;
    keepPlace(url);
  }
  // A deferred refresh runs once the dialog closes or the field is left.
  document.addEventListener("focusout", function () {
    setTimeout(function () { if (deferred && !busy()) refreshPane(deferred); }, 0);
  });

  function main() { return document.getElementById("boMain"); }

  function onQuestion() {
    soon("chip", function () { get("ui/needs"); });
    var m = main();
    if (m && m.dataset.view === "inbox") {
      var f = m.dataset.filter;
      soon("inbox", function () { refreshPane("ui/bo/inbox" + (f ? "?product=" + encodeURIComponent(f) : "")); });
    }
  }

  function onTask(hint) {
    var m = main();
    if (!m || m.dataset.tab !== "board" || !hint || hint.product !== m.dataset.product) return;
    soon("board", function () {
      var host = document.getElementById("board-host");
      if (host && host.style.display !== "none") {
        host.dispatchEvent(new CustomEvent("gaos:board-refresh"));
      } else {
        refreshPane("products/" + encodeURIComponent(m.dataset.product) + "?tab=board");
      }
    });
  }

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
    if (deferred && !busy()) refreshPane(deferred);
  });
  document.addEventListener("gaos:modal-closed", function () {
    if (deferred && !busy()) refreshPane(deferred);
  });
  document.body.addEventListener("htmx:afterSwap", function () {
    if (deferred && !busy()) refreshPane(deferred);
  });

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
