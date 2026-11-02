/* GAOS Mission Control — the client half.
 *
 * Everything in here is behaviour the server has no opinion about: what the
 * browser is looking at, what the browser has focused, and how long a toast
 * stays up. NOTHING in here renders markup. If a function in this file ever
 * starts building HTML, the fragment it is building belongs in a Go template
 * and the reason it did not is worth writing down.
 *
 * htmx 2.0.4, vendored in static/htmx.min.js — no CDN, same rule the landing
 * page and pustaka follow: zero third-party requests at runtime.
 */
(function () {
  "use strict";

  var RM = window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  /* ---------- theme ----------
     Client-side, and it writes a cookie so the pre-paint script in page.html
     can restore the choice before the first paint on the next load. A server
     round-trip cannot beat first paint, which is why this is not a fragment. */
  function applyTheme(t) {
    document.documentElement.setAttribute("data-theme", t);
    var b = document.getElementById("themeBtn");
    if (b) b.setAttribute("aria-label", t === "dark" ? "Switch to light theme" : "Switch to dark theme");
    document.cookie = "gaos_theme=" + t + ";path=/;max-age=31536000;samesite=lax";
  }
  function toggleTheme() {
    applyTheme(document.documentElement.getAttribute("data-theme") === "dark" ? "light" : "dark");
  }

  /* ---------- sheets and modals ----------
     They are plain hidden attributes. The server decides whether a sheet has
     content; the client decides whether you are still looking at it. */
  function closeEl(id) {
    var el = document.getElementById(id);
    if (el) el.hidden = true;
  }

  document.addEventListener("click", function (e) {
    var t = e.target.closest("[data-close-sheet],[data-close-modal],[data-toggle],[data-pick],[data-nav],#themeBtn,#boTheme,.story-pick");
    if (!t) return;

    if (t.id === "themeBtn" || t.id === "boTheme") { toggleTheme(); return; }

    if (t.dataset.closeSheet) { closeEl(t.dataset.closeSheet); return; }
    if (t.dataset.closeModal) { closeEl(t.dataset.closeModal); return; }

    /* An inline form that only exists to be revealed — the BRD editor and the
       new-product block. Server state does not change, so neither should the
       server be asked. */
    if (t.dataset.toggle) {
      var f = document.getElementById(t.dataset.toggle);
      if (f) {
        f.hidden = !f.hidden;
        if (!f.hidden) f.scrollIntoView({ block: "nearest" });
      }
      return;
    }

    /* Wizard step 1: picking a product writes the hidden input the whole draft
       round-trips through, rather than firing a request per click. */
    if (t.dataset.pick) {
      var form = document.getElementById("cwForm");
      if (!form) return;
      var input = form.querySelector('input[name="product"]');
      if (input) input.value = t.dataset.pick;
      form.querySelectorAll("[data-pick]").forEach(function (b) {
        b.setAttribute("aria-pressed", String(b === t));
      });
      return;
    }

    if (t.dataset.nav) {
      var cur = document.querySelector(".desk.focus");
      var idx = cur ? parseInt(cur.dataset.idx, 10) : 0;
      var next = t.dataset.nav === "next" ? idx + 1 : idx - 1;
      if (window.htmx) htmx.ajax("GET", "ui/desk/" + Math.max(0, next), { target: "#sink", swap: "innerHTML" });
      return;
    }
  });

  /* The story checkboxes are hidden inputs behind a styled label, so the
     pressed state has to be mirrored for the CSS and for a screen reader. */
  document.addEventListener("change", function (e) {
    var lbl = e.target.closest(".story-pick");
    if (lbl && e.target.type === "checkbox") lbl.setAttribute("aria-pressed", String(e.target.checked));
  });

  /* ---------- keyboard ---------- */
  document.addEventListener("keydown", function (e) {
    var tag = (e.target.tagName || "").toUpperCase();
    if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;

    var cur = document.querySelector(".desk.focus");
    var idx = cur ? parseInt(cur.dataset.idx, 10) : 0;

    if (e.key === "ArrowRight" && window.htmx) htmx.ajax("GET", "ui/desk/" + (idx + 1), { target: "#sink", swap: "innerHTML" });
    if (e.key === "ArrowLeft" && window.htmx) htmx.ajax("GET", "ui/desk/" + Math.max(0, idx - 1), { target: "#sink", swap: "innerHTML" });
    if (e.key === " ") { e.preventDefault(); var p = document.getElementById("playBtn"); if (p) p.click(); }
    if (e.key === "/") { e.preventDefault(); var s = document.getElementById("search"); if (s) s.focus(); }
    if ((e.key === "m" || e.key === "M") && window.htmx) htmx.ajax("POST", "ui/desk/" + idx + "/review", { target: "#sink", swap: "innerHTML" });
    if (e.key === "t" || e.key === "T") toggleTheme();
  });

  /* ---------- search ----------
     A client-side jump, exactly as in the prototype. Server-side search over
     the graph is a real feature and belongs with the graph, not here. */
  document.addEventListener("input", function (e) {
    if (e.target.id !== "search") return;
    var q = e.target.value.trim().toLowerCase();
    if (!q) return;
    var hit = Array.prototype.find.call(document.querySelectorAll(".desk"), function (d) {
      return d.textContent.toLowerCase().indexOf(q) >= 0;
    });
    if (hit && window.htmx) htmx.ajax("GET", "ui/desk/" + hit.dataset.idx, { target: "#sink", swap: "innerHTML" });
  });

  /* ---------- post-swap housekeeping ----------
     Three things the server cannot know: where the carousel is scrolled, where
     the timeline is scrolled, and how long ago the toast appeared. */
  var toastTimer = null;

  function afterSwap(target) {
    if (!target || !target.id) return;

    if (target.id === "carousel") {
      var f = target.querySelector(".desk.focus");
      if (f && f.scrollIntoView) {
        f.scrollIntoView(RM ? { inline: "center", block: "nearest" }
                            : { behavior: "smooth", inline: "center", block: "nearest" });
      }
    }

    if (target.id === "tlRow") {
      var track = document.getElementById("track");
      /* Only chase the tail while live. Scrubbing to an old event and then
         being yanked back to the newest one is the single most annoying thing
         a timeline can do. */
      var chip = document.getElementById("modeChip");
      if (track && chip && chip.classList.contains("live")) track.scrollLeft = track.scrollWidth;
    }

    if (target.id === "toast") {
      clearTimeout(toastTimer);
      if (target.classList.contains("show")) {
        toastTimer = setTimeout(function () {
          var t = document.getElementById("toast");
          if (t) t.classList.remove("show");
        }, 2600);
      }
    }

    if (target.id === "iLogs" || target.id === "inspector") {
      var logs = document.getElementById("iLogs");
      if (logs) logs.scrollTop = logs.scrollHeight;
    }

    /* Sheet visibility is the SERVER's answer, carried in the hidden attribute
       of the swapped element. The client only closes them (data-close-sheet);
       it never decides to open one, so there is one source of truth for
       whether you are signed in and looking at the back office. */
  }

  document.body.addEventListener("htmx:afterSwap", function (e) { afterSwap(e.target); });
  document.body.addEventListener("htmx:oobAfterSwap", function (e) { afterSwap(e.target); });

  /* Boot: reflect the theme label, and settle the log scroll. */
  applyTheme(document.documentElement.getAttribute("data-theme") || "dark");
  afterSwap(document.getElementById("inspector"));
  // The board (M14.6). Native drag and drop, by delegation so it survives
  // every swap. It renders nothing (I-6): a drop sends the same move request
  // a card's buttons do, and the server's answer -- the re-rendered board, or
  // a toast saying why the state machine refused -- is what the page shows.
  document.addEventListener("dragstart", function (e) {
    var card = e.target.closest && e.target.closest(".board-card");
    if (!card) return;
    e.dataTransfer.setData("text/plain", card.dataset.product + "|" + card.dataset.card);
    e.dataTransfer.effectAllowed = "move";
  });
  document.addEventListener("dragover", function (e) {
    if (e.target.closest && e.target.closest(".board-col")) e.preventDefault();
  });
  document.addEventListener("drop", function (e) {
    var col = e.target.closest && e.target.closest(".board-col");
    if (!col || !window.htmx) return;
    e.preventDefault();
    var parts = (e.dataTransfer.getData("text/plain") || "").split("|");
    if (parts.length !== 2) return;
    htmx.ajax("PUT", "products/" + parts[0] + "/board/cards/" + parts[1] + "/move",
      { target: "#sink", swap: "innerHTML", values: { column: col.dataset.col } });
  });
})();
