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
    var t = e.target.closest("[data-close-sheet],[data-close-modal],[data-answer-open],[data-timing-open],[data-sheet-open],[data-toggle],[data-pick],[data-nav],#themeBtn,#boTheme,.story-pick");
    if (!t) return;

    if (t.id === "themeBtn" || t.id === "boTheme") { toggleTheme(); return; }

    if (t.dataset.closeSheet) { closeEl(t.dataset.closeSheet); return; }
    // e.detail is 0 for a click synthesised by Enter or Space: a keyboard close.
    if (t.dataset.closeModal) { closeModal(t.dataset.closeModal, e.detail === 0); return; }
    if (t.dataset.answerOpen) { openAnswer(t, e.detail === 0); return; }
    if (t.dataset.timingOpen || t.dataset.sheetOpen) { openTiming(t, e.detail === 0); return; }

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
    if (openModal()) return; // a dialog owns the keyboard while it is up

    if (e.key === "/") { e.preventDefault(); var s = document.getElementById("search"); if (s) s.focus(); }
    if (e.key === "t" || e.key === "T") toggleTheme();
  });

  /* ---------- search ----------
     A client-side filter over the product cards. Server-side search over the
     graph is a real feature and belongs with the graph, not here. */
  document.addEventListener("input", function (e) {
    if (e.target.id !== "search") return;
    var q = e.target.value.trim().toLowerCase();
    document.querySelectorAll(".mcard").forEach(function (c) {
      c.hidden = q !== "" && c.textContent.toLowerCase().indexOf(q) < 0;
    });
  });

  /* ---------- modals ----------
     A modal is open while its .modal-back is not hidden. The answer sheet
     (.modal-anim) has an enter and an exit; the wizard does not, because every
     step re-renders it and would replay the entrance. */
  var trigger = null;      // what opened the answer sheet: focus goes back here
  var openedByKey = false; // a key opened it: then it appears without motion

  function openModal() {
    var all = document.querySelectorAll(".modal-back:not([hidden])");
    return all.length ? all[all.length - 1] : null;
  }

  function closeModal(id, instant) {
    var back = document.getElementById(id);
    if (!back || back.hidden) return;
    if (back.classList.contains("modal-anim") && !instant && !RM) {
      back.removeAttribute("data-instant");
      back.setAttribute("data-state", "closing");
      setTimeout(function () { finishClose(back); }, 150);
    } else {
      finishClose(back);
    }
    if (id === "answerModal" || id === "timingModal" || id === "conversationModal" || id === "sendBackModal") returnFocus();
  }
  function finishClose(back) {
    back.hidden = true;
    back.removeAttribute("data-state");
    back.removeAttribute("data-instant");
    // live.js holds back pane refreshes while a dialog is up.
    document.dispatchEvent(new CustomEvent("gaos:modal-closed"));
  }

  /* Focus goes back to whatever opened the sheet. An answered question's row
     is gone by then, so the next row takes it -- Enter answers the next agent
     -- and failing that the card on the board, then the page title. */
  function returnFocus() {
    var t = trigger;
    trigger = null;
    if (!t) return;
    var el = t.id && document.getElementById(t.id);
    if (!el && t.row != null) {
      var rows = document.querySelectorAll(".inbox-row");
      el = rows[Math.min(t.row, rows.length - 1)];
    }
    if (!el && t.card) el = document.querySelector('[data-card-id="' + CSS.escape(t.card) + '"],[data-card="' + CSS.escape(t.card) + '"]');
    if (!el) el = document.getElementById("inboxTitle") || document.querySelector("#boMain h2");
    if (el) el.focus({ preventScroll: false });
  }

  function openAnswer(t, byKey) {
    if (!window.htmx) return;
    var rows = Array.prototype.slice.call(document.querySelectorAll(".inbox-row"));
    var card = t.closest("[data-card-id],[data-card]");
    trigger = {
      id: t.id || "",
      row: rows.indexOf(t) >= 0 ? rows.indexOf(t) : null,
      card: card ? (card.dataset.cardId || card.dataset.card) : null,
    };
    openedByKey = byKey;
    htmx.ajax("GET", t.dataset.answerOpen, { target: "#sink", swap: "innerHTML" });
  }

  // A card's timing sheet (D-081): the same trigger bookkeeping as the answer
  // sheet, so focus goes back to the chip on close. showAnswer opens it too --
  // it has no form, and showAnswer's form steps find nothing and pass.
  function openTiming(t, byKey) {
    if (!window.htmx) return;
    var card = t.closest("[data-card-id],[data-card]");
    trigger = { id: t.id || "", row: null, card: card ? (card.dataset.cardId || card.dataset.card) : null };
    openedByKey = byKey;
    htmx.ajax("GET", t.dataset.timingOpen || t.dataset.sheetOpen, { target: "#sink", swap: "innerHTML" });
  }

  // Runs when the server swaps the sheet in.
  function showAnswer(back) {
    if (back.hidden) return;
    if (openedByKey || RM) back.setAttribute("data-instant", "");
    // Reading a layout property commits the closed state (opacity 0,
    // scale .96) so data-state="open" transitions from it. Not
    // requestAnimationFrame: a tab the browser is not painting never runs it,
    // and the sheet would stay invisible while it holds the focus. Not a class:
    // htmx's settle step rewrites a swapped element's class 20ms later.
    void back.offsetWidth;
    back.setAttribute("data-state", "open");
    openedByKey = false;
    var mod = /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent) ? "⌘↵" : "Ctrl ↵";
    back.querySelectorAll("[data-mod-enter]").forEach(function (k) { k.textContent = mod; });
    syncAnswer();
    var first = back.querySelector('input[name="choice"]:checked');
    if (first && first.value === "4") first = back.querySelector("#answerOwn");
    (first || back.querySelector(".modal")).focus();
  }

  /* One field carries name="text" at a time: the note for 1-3, the textarea for
     4. Submit stays disabled until the answer is sendable, and the hint says
     why rather than an alert. */
  function syncAnswer() {
    var form = document.getElementById("answerForm");
    if (!form) return;
    var picked = form.querySelector('input[name="choice"]:checked');
    var n = picked ? picked.value : "";
    var own = form.querySelector("#answerOwn");
    var note = form.querySelector("#answerNote");
    var noteField = form.querySelector("#answerNoteField");
    var hint = form.querySelector("#answerHint");
    var send = form.querySelector('button[type="submit"]');
    var other = n === "4";
    if (own) { if (other) own.setAttribute("name", "text"); else own.removeAttribute("name"); }
    if (note) { if (other) note.removeAttribute("name"); else note.setAttribute("name", "text"); }
    if (noteField) noteField.hidden = other;
    var ok = n !== "" && (!other || (own && own.value.trim() !== ""));
    if (send && !form.classList.contains("htmx-request")) send.disabled = !ok;
    if (hint) {
      hint.classList.toggle("warn", !ok && other && own && own.value !== "");
      if (!n) hint.textContent = "Pick one of the four answers.";
      else if (other && !ok) hint.textContent = "Write your answer to send option 4.";
      else if (other) hint.textContent = "Sends your own words.";
      else if (n === "1" && form.querySelector('input[value="1"]') && form.querySelector(".rec")) hint.textContent = "Sends option 1, the recommendation.";
      else hint.textContent = "Sends option " + n + ".";
    }
  }

  function pick(n) {
    var form = document.getElementById("answerForm");
    var r = form && form.querySelector('input[name="choice"][value="' + n + '"]');
    if (!r) return;
    r.checked = true;
    syncAnswer();
    if (n === "4") form.querySelector("#answerOwn").focus();
    else r.focus();
  }

  document.addEventListener("change", function (e) {
    if (e.target.name === "choice" && e.target.closest("#answerForm")) syncAnswer();
  });
  document.addEventListener("input", function (e) {
    if (e.target.id !== "answerOwn") return;
    var r = document.getElementById("answerOther");
    if (r && !r.checked && e.target.value !== "") r.checked = true;
    syncAnswer();
  });
  document.addEventListener("focusin", function (e) {
    if (e.target.id !== "answerOwn") return;
    var r = document.getElementById("answerOther");
    if (r && !r.checked) { r.checked = true; syncAnswer(); }
  });

  var submittedByKey = false;
  document.addEventListener("click", function (e) {
    if (e.target.closest && e.target.closest('#answerForm button[type="submit"]')) submittedByKey = e.detail === 0;
  }, true);

  /* The dialog's keyboard: Esc closes, Tab stays inside, 1-4 pick, Cmd/Ctrl+Enter
     sends. Captured, so the page's own shortcuts (/, T) never see these keys. */
  document.addEventListener("keydown", function (e) {
    var back = openModal();
    if (!back) return;
    var inText = /^(TEXTAREA|SELECT)$/.test(e.target.tagName) ||
      (e.target.tagName === "INPUT" && !/^(radio|checkbox|button|submit)$/.test(e.target.type));

    if (e.key === "Escape") {
      // Esc in a textarea with words in it steps out of the field first, so
      // one stray key does not throw away what the operator wrote.
      if (e.target.id === "answerOwn" && e.target.value.trim() !== "") {
        e.preventDefault();
        var r = document.getElementById("answerOther");
        if (r) r.focus();
        return;
      }
      e.preventDefault();
      closeModal(back.id, true);
      return;
    }
    if (e.key === "Tab") {
      var f = Array.prototype.filter.call(
        back.querySelectorAll('button:not([disabled]),[href],input:not([disabled]),textarea:not([disabled]),select:not([disabled]),summary,[tabindex]:not([tabindex="-1"])'),
        function (el) { return el.offsetParent !== null && !(el.type === "radio" && !el.checked); });
      if (!f.length) return;
      var first = f[0], last = f[f.length - 1];
      if (e.shiftKey && (document.activeElement === first || !back.contains(document.activeElement))) { e.preventDefault(); last.focus(); }
      else if (!e.shiftKey && (document.activeElement === last || !back.contains(document.activeElement))) { e.preventDefault(); first.focus(); }
      return;
    }
    if (back.id !== "answerModal") return;
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      var form = document.getElementById("answerForm");
      var send = form && form.querySelector('button[type="submit"]');
      if (send && !send.disabled) { submittedByKey = true; form.requestSubmit(send); }
      else if (form) {
        var h = form.querySelector("#answerHint");
        if (h) h.classList.add("warn");
      }
      return;
    }
    if (!inText && !e.metaKey && !e.ctrlKey && !e.altKey && /^[1-4]$/.test(e.key)) {
      e.preventDefault();
      pick(e.key);
    }
  }, true);

  // The answer landed: close the sheet. A refusal keeps it open, and the
  // toast says why.
  // The send-back landed (D-084): close its sheet. A refusal keeps it open.
  document.body.addEventListener("htmx:afterRequest", function (e) {
    if (!e.detail.elt || e.detail.elt.id !== "sendBackForm") return;
    var st = e.detail.xhr ? e.detail.xhr.status : 0;
    if (st >= 200 && st < 300) closeModal("sendBackModal", false);
  });

  document.body.addEventListener("htmx:afterRequest", function (e) {
    if (!e.detail.elt || e.detail.elt.id !== "answerForm") return;
    // Not detail.successful: page.html swaps 4xx bodies (the refusal toast),
    // which htmx then counts as a success. The status is the truth.
    var st = e.detail.xhr ? e.detail.xhr.status : 0;
    if (st >= 200 && st < 300) closeModal("answerModal", submittedByKey);
    submittedByKey = false;
  });

  /* ---------- post-swap housekeeping ----------
     The one thing the server cannot know: how long ago the toast appeared. */
  var toastTimer = null;

  function afterSwap(target) {
    if (!target || !target.id) return;

    if (target.id === "answerModal" || target.id === "timingModal" || target.id === "conversationModal" || target.id === "sendBackModal") showAnswer(target);

    if (target.id === "toast") {
      clearTimeout(toastTimer);
      if (target.classList.contains("show")) {
        toastTimer = setTimeout(function () {
          var t = document.getElementById("toast");
          if (t) t.classList.remove("show");
        }, 2600);
      }
    }

    /* Sheet visibility is the SERVER's answer, carried in the hidden attribute
       of the swapped element. The client only closes them (data-close-sheet);
       it never decides to open one, so there is one source of truth for
       whether you are signed in and looking at the back office. */
  }

  // The sisflow board (T-165): mounted over the server-rendered one whenever
  // a Board tab arrives, loaded only then. A browser without ES modules, or a
  // failed load, keeps the server board, which works on its own.
  function mountBoard(root) {
    var host = (root || document).querySelector && (root || document).querySelector("#board-host");
    if (!host) return;
    import(new URL("static/board.js", document.baseURI).href)
      .then(function (m) { m.mount(host); })
      .catch(function () { /* the server board stays */ });
  }
  /* ---------- the back-office bar ----------
     On a narrow screen the menu scrolls sideways. Two jobs the server cannot
     do: keep the current item in view after every swap, and say which side
     has more (data-more drives the edge fade in app.css). No smooth scroll —
     switching tabs happens tens of times a day and must feel instant. */
  function syncMenuFade(menu) {
    var max = menu.scrollWidth - menu.clientWidth;
    var start = menu.scrollLeft > 1, end = menu.scrollLeft < max - 1;
    var v = start && end ? "both" : start ? "start" : end ? "end" : "";
    if (v) menu.setAttribute("data-more", v); else menu.removeAttribute("data-more");
  }
  function syncMenu() {
    var menu = document.querySelector("#boSheet .bo-menu");
    if (!menu) return;
    var cur = menu.querySelector('[aria-current="page"]');
    // "nearest" moves nothing when the item already shows; scroll-padding in
    // app.css keeps it clear of the faded edge when it does move.
    if (cur && menu.scrollWidth > menu.clientWidth) cur.scrollIntoView({ inline: "nearest", block: "nearest", behavior: "instant" });
    syncMenuFade(menu);
  }
  document.addEventListener("scroll", function (e) {
    if (e.target.classList && e.target.classList.contains("bo-menu")) syncMenuFade(e.target);
  }, true);
  window.addEventListener("resize", syncMenu);

  document.body.addEventListener("htmx:afterSwap", function (e) { afterSwap(e.target); mountBoard(e.target); syncMenu(); });
  document.body.addEventListener("htmx:oobAfterSwap", function (e) { afterSwap(e.target); mountBoard(e.target); if (e.target.id === "boSheet") syncMenu(); });

  /* Boot: reflect the theme label. */
  applyTheme(document.documentElement.getAttribute("data-theme") || "dark");
  mountBoard(document);
  syncMenu();
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
