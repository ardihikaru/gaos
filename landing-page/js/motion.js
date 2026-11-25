/* GAOS landing page — the effects that need script.
 *
 *   the graph field   a canvas behind the page: drifting nodes joined by
 *                     edges, reacting to the pointer and to scrolling
 *   the hero glow     a soft light that follows the pointer
 *   the board tilt    the hero mock-up leans a few degrees toward the pointer
 *   the spotlight     a light under the pointer inside a card
 *   the count-up      the four figures count from zero when they scroll in
 *   scroll progress   the brass line under the header, and the header's shadow
 *   the section spy   the header link of the section in view
 *
 * LOADED ASYNC. Nothing on the page waits for this file and nothing is
 * hidden until it runs: every start state that hides content belongs to
 * theme.js and 04-motion.css. If this file never loads, the page is still
 * complete — it just holds still.
 *
 * It calls nothing (the CSP says connect-src 'none') and writes styles only
 * through CSSOM, which `style-src 'self'` permits.
 *
 * Classic script, wrapped in one function, no globals. */

(function () {
  var root = document.documentElement;
  var reduce = false, fine = false;
  try {
    reduce = matchMedia('(prefers-reduced-motion: reduce)').matches;
    fine = matchMedia('(hover:hover) and (pointer:fine)').matches;
  } catch (e) { }

  function ready(fn) {
    if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', fn);
    else fn();
  }

  /* One requestAnimationFrame loop for the whole file. A task returns true
   * while it still has work; when every task is idle the loop stops, and
   * kick() starts it again. */
  var tasks = [], running = false, last = 0;
  function frame(now) {
    var dt = Math.min(0.05, (now - last) / 1000 || 0.016);
    last = now;
    var busy = false;
    // A copy: a task may take itself off the list while the list is walked.
    var run = tasks.slice();
    for (var i = 0; i < run.length; i++) if (run[i](now, dt)) busy = true;
    if (busy && !document.hidden) requestAnimationFrame(frame);
    else running = false;
  }
  function kick() {
    if (running || document.hidden) return;
    running = true;
    last = performance.now();
    requestAnimationFrame(frame);
  }
  document.addEventListener('visibilitychange', function () { if (!document.hidden) kick() });

  /* An easing step that covers the same distance per second at 30, 60 or
   * 120 frames a second: c is the share closed in one 60Hz frame. */
  function ease(c, dt) { return 1 - Math.pow(1 - c, (dt || .016) * 60) }

  /* ------------------------------ colours ------------------------------
   * The field is drawn in the page's own brass and ink, read from the
   * tokens, and re-read whenever the theme changes. */
  function rgb(name, fallback) {
    var v = getComputedStyle(root).getPropertyValue(name).trim();
    var m = /^#([0-9a-f]{6})$/i.exec(v);
    if (!m) return fallback;
    var n = parseInt(m[1], 16);
    return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
  }

  /* --------------------------- the graph field --------------------------- */
  function startField() {
    var canvas = document.querySelector('.field');
    if (!canvas || !canvas.getContext) return;
    var ctx = canvas.getContext('2d');
    if (!ctx) return;

    var LINK = 170, NEAR = 200;
    var w = 0, h = 0, dpr = 1, nodes = [];
    var brass = [138, 103, 22], ink = [115, 110, 95];
    var px = -9999, py = -9999;           // pointer, in viewport pixels
    var scrollY = window.scrollY || 0;
    var pulse = null, nextPulse = 0;
    // 30 frames a second on touch devices. 30ms, not 33: two 16.7ms frames
    // must clear the gap with room for a late timestamp.
    var minGap = fine ? 0 : 30;
    var lastDraw = 0;

    function colours() {
      brass = rgb('--brass', brass);
      ink = rgb('--ink-3', ink);
    }

    function size() {
      // A phone's address bar collapsing changes only the height, mid-scroll.
      // Resetting the canvas there would clear it for a frame; the canvas is
      // stretched by CSS instead, which nobody sees at this size.
      if (!fine && w === window.innerWidth && Math.abs(h - window.innerHeight) < 160) return;
      dpr = Math.min(window.devicePixelRatio || 1, 2);
      w = window.innerWidth; h = window.innerHeight;
      canvas.width = Math.round(w * dpr);
      canvas.height = Math.round(h * dpr);
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      var want = Math.max(fine ? 24 : 14, Math.min(84, Math.round(w * h / 17000)));
      while (nodes.length < want) nodes.push(node());
      nodes.length = want;
      for (var i = 0; i < nodes.length; i++) {
        if (nodes[i].x > w) nodes[i].x = Math.random() * w;
        if (nodes[i].y > h) nodes[i].y = Math.random() * h;
      }
    }

    function node() {
      var a = Math.random() * Math.PI * 2, v = 6 + Math.random() * 8;
      return {
        x: Math.random() * (w || 1), y: Math.random() * (h || 1),
        vx: Math.cos(a) * v, vy: Math.sin(a) * v,
        r: 1.4 + Math.random() * 1.8,
        depth: .25 + Math.random() * .75,   // how far it shifts when the page scrolls
        ox: 0, oy: 0,                        // offset pushed by the pointer
        sx: 0, sy: 0                         // where it was drawn this frame
      };
    }

    function step(dt) {
      for (var i = 0; i < nodes.length; i++) {
        var n = nodes[i];
        n.x += n.vx * dt; n.y += n.vy * dt;
        // Wrap a full LINK outside the viewport: by then every edge to a node
        // still on screen has faded to nothing, so no line pops in or out.
        if (n.x < -LINK) n.x = w + LINK; else if (n.x > w + LINK) n.x = -LINK;
        if (n.y < -LINK) n.y = h + LINK; else if (n.y > h + LINK) n.y = -LINK;

        // Scrolling slides each node by its own depth: near ones move more.
        var y = n.y - scrollY * n.depth * .12;
        var span = h + 2 * LINK;
        y = ((y + LINK) % span + span) % span - LINK;

        // The pointer pushes nearby nodes away; they ease back when it leaves.
        var tx = 0, ty = 0;
        var dx = n.x + n.ox - px, dy = y + n.oy - py;
        var d = Math.sqrt(dx * dx + dy * dy);
        if (d < NEAR && d > .01) {
          var f = (1 - d / NEAR) * 26;
          tx = dx / d * f; ty = dy / d * f;
        }
        var k = ease(.08, dt);
        n.ox += (tx - n.ox) * k;
        n.oy += (ty - n.oy) * k;
        n.sx = n.x + n.ox; n.sy = y + n.oy;
      }
    }

    function draw(now) {
      ctx.clearRect(0, 0, w, h);
      var i, j, a, b, dx, dy, d2, d, al;
      var lr = ink[0] + ',' + ink[1] + ',' + ink[2];
      var br = brass[0] + ',' + brass[1] + ',' + brass[2];
      var pairs = [];

      ctx.lineWidth = 1;
      for (i = 0; i < nodes.length; i++) {
        a = nodes[i];
        for (j = i + 1; j < nodes.length; j++) {
          b = nodes[j];
          dx = a.sx - b.sx; dy = a.sy - b.sy;
          d2 = dx * dx + dy * dy;
          if (d2 > LINK * LINK) continue;
          d = Math.sqrt(d2);
          al = (1 - d / LINK) * .55;
          ctx.strokeStyle = 'rgba(' + lr + ',' + al.toFixed(3) + ')';
          ctx.beginPath(); ctx.moveTo(a.sx, a.sy); ctx.lineTo(b.sx, b.sy); ctx.stroke();
          pairs.push(i, j);
        }
      }

      // Edges from the pointer to the nodes around it, in brass.
      if (px > -999) {
        for (i = 0; i < nodes.length; i++) {
          a = nodes[i];
          dx = a.sx - px; dy = a.sy - py;
          d = Math.sqrt(dx * dx + dy * dy);
          if (d > NEAR) continue;
          al = (1 - d / NEAR) * .7;
          ctx.strokeStyle = 'rgba(' + br + ',' + al.toFixed(3) + ')';
          ctx.beginPath(); ctx.moveTo(px, py); ctx.lineTo(a.sx, a.sy); ctx.stroke();
        }
      }

      for (i = 0; i < nodes.length; i++) {
        a = nodes[i];
        ctx.fillStyle = 'rgba(' + br + ',' + (.45 + a.depth * .45).toFixed(3) + ')';
        ctx.beginPath(); ctx.arc(a.sx, a.sy, a.r, 0, 6.2832); ctx.fill();
      }

      // An event: every couple of seconds one edge carries a brass pulse
      // from one node to the other.
      if (!reduce) {
        if (!pulse && now > nextPulse && pairs.length) {
          var k = (Math.random() * (pairs.length / 2) | 0) * 2;
          pulse = { a: nodes[pairs[k]], b: nodes[pairs[k + 1]], t0: now };
        }
        if (pulse) {
          var t = (now - pulse.t0) / 900;
          if (t >= 1) { pulse = null; nextPulse = now + 1500 + Math.random() * 1200 }
          else {
            var e = 1 - Math.pow(1 - t, 3);
            var qx = pulse.a.sx + (pulse.b.sx - pulse.a.sx) * e;
            var qy = pulse.a.sy + (pulse.b.sy - pulse.a.sy) * e;
            ctx.fillStyle = 'rgba(' + br + ',' + (.9 * (1 - t * t)).toFixed(3) + ')';
            ctx.beginPath(); ctx.arc(qx, qy, 2.6, 0, 6.2832); ctx.fill();
            ctx.fillStyle = 'rgba(' + br + ',' + (.18 * (1 - t)).toFixed(3) + ')';
            ctx.beginPath(); ctx.arc(qx, qy, 7, 0, 6.2832); ctx.fill();
          }
        }
      }
    }

    colours();
    size();
    step(0);
    draw(0);

    var resizeTimer = 0;
    window.addEventListener('resize', function () {
      clearTimeout(resizeTimer);
      // Setting a canvas's size clears it, so it is redrawn at once.
      resizeTimer = setTimeout(function () { size(); step(0); draw(performance.now()) }, 120);
    });
    // The theme button writes data-theme; the system can change scheme too.
    new MutationObserver(function () { colours(); if (reduce) draw(0) })
      .observe(root, { attributes: true, attributeFilter: ['data-theme'] });
    try {
      matchMedia('(prefers-color-scheme: dark)').addEventListener('change', function () { colours(); if (reduce) draw(0) });
    } catch (e) { }

    // Reduced motion: the one frame drawn above is the whole effect.
    if (reduce) return;

    window.addEventListener('scroll', function () { scrollY = window.scrollY || 0 }, { passive: true });
    if (fine) {
      // Mouse only: a touch on a hybrid laptop would pin the edges where the
      // finger landed, with no leave event to release them.
      window.addEventListener('pointermove', function (e) {
        if (e.pointerType && e.pointerType !== 'mouse') return;
        px = e.clientX; py = e.clientY;
      }, { passive: true });
      var away = function () { px = py = -9999 };
      root.addEventListener('pointerleave', away);
      window.addEventListener('blur', away);
    }
    tasks.push(function (now, dt) {
      if (now - lastDraw < minGap) return true;
      step(minGap ? Math.min(.06, (now - lastDraw) / 1000) : dt);
      lastDraw = now;
      draw(now);
      return true;
    });
    kick();
  }

  /* ------------------- the hero glow and the board tilt -------------------
   * Both ease toward a target and stop when they arrive, so an idle pointer
   * costs nothing. Boxes are measured on entry and on resize, never inside
   * a frame. */
  function startHero() {
    if (reduce || !fine) return;
    var hero = document.querySelector('.hero');
    var glow = document.querySelector('.glow');
    var shot = document.querySelector('.shot-board');
    var board = shot && shot.querySelector('.board');
    if (!hero) return;

    var hx = 0, hy = 0;                       // the hero's page position
    var gx = 0, gy = 0, gtx = 0, gty = 0, glowOn = false;
    var rx = 0, ry = 0, rtx = 0, rty = 0;     // board rotation, degrees
    var box = null;

    function measure() {
      var r = hero.getBoundingClientRect();
      hx = r.left + window.scrollX; hy = r.top + window.scrollY;
    }
    measure();
    window.addEventListener('resize', measure);
    window.addEventListener('load', measure);

    hero.addEventListener('pointermove', function (e) {
      gtx = e.pageX - hx; gty = e.pageY - hy;
      if (!glowOn) { glowOn = true; gx = gtx; gy = gty; hero.classList.add('is-near') }
      kick();
    }, { passive: true });
    hero.addEventListener('pointerleave', function () { glowOn = false; hero.classList.remove('is-near') });

    if (board) {
      shot.addEventListener('pointerenter', function () { box = shot.getBoundingClientRect() });
      shot.addEventListener('pointermove', function (e) {
        if (!box) box = shot.getBoundingClientRect();
        var nx = (e.clientX - box.left) / box.width - .5;
        var ny = (e.clientY - box.top) / box.height - .5;
        rty = nx * 5; rtx = -ny * 5;          // at most 2.5 degrees each way
        kick();
      }, { passive: true });
      shot.addEventListener('pointerleave', function () { rtx = rty = 0; box = null; kick() });
      window.addEventListener('scroll', function () { box = null }, { passive: true });
    }

    tasks.push(function (now, dt) {
      var busy = false;
      if (glow) {
        var kg = ease(.12, dt);
        gx += (gtx - gx) * kg; gy += (gty - gy) * kg;
        if (Math.abs(gtx - gx) > .3 || Math.abs(gty - gy) > .3) busy = true;
        glow.style.transform = 'translate3d(' + gx.toFixed(1) + 'px,' + gy.toFixed(1) + 'px,0)';
      }
      if (board) {
        var kr = ease(.1, dt);
        rx += (rtx - rx) * kr; ry += (rty - ry) * kr;
        if (Math.abs(rtx - rx) > .01 || Math.abs(rty - ry) > .01) busy = true;
        else { rx = rtx; ry = rty }
        board.style.transform = (rx || ry)
          ? 'rotateX(' + rx.toFixed(3) + 'deg) rotateY(' + ry.toFixed(3) + 'deg)'
          : '';
      }
      return busy;
    });
  }

  /* ------------------------------ spotlight ------------------------------ */
  function startSpot() {
    if (reduce || !fine) return;
    // The box is read on each move: nothing in a frame has dirtied layout, so
    // the read is cheap, and it stays right while the page scrolls under a
    // still pointer.
    document.querySelectorAll('.card').forEach(function (card) {
      card.addEventListener('pointermove', function (e) {
        var box = card.getBoundingClientRect();
        card.style.setProperty('--mx', (e.clientX - box.left).toFixed(0) + 'px');
        card.style.setProperty('--my', (e.clientY - box.top).toFixed(0) + 'px');
      }, { passive: true });
    });
  }

  /* ------------------------------ count-up ------------------------------
   * The figure in the markup is the real one (deploy/check-stacks.sh reads
   * it there). This counts up to it and always ends on it. */
  function startCount() {
    if (reduce || !('IntersectionObserver' in window)) return;
    var els = document.querySelectorAll('.proof li > b');
    if (!els.length) return;
    var io = new IntersectionObserver(function (entries) {
      entries.forEach(function (en) {
        if (!en.isIntersecting) return;
        io.unobserve(en.target);
        var el = en.target, end = parseInt(el.textContent, 10);
        if (!(end > 1)) return;
        var t0 = 0;
        el.textContent = '0';
        tasks.push(function run(now) {
          if (!t0) t0 = now;
          var t = Math.min(1, (now - t0) / 900);
          el.textContent = String(Math.round(end * (1 - Math.pow(1 - t, 4))));
          if (t < 1) return true;
          el.textContent = String(end);
          tasks.splice(tasks.indexOf(run), 1);
          return false;
        });
        kick();
      });
    }, { threshold: .6 });
    els.forEach(function (el) { io.observe(el) });
  }

  /* ----------------- scroll progress and the header's shadow ----------------- */
  function startScroll() {
    var bar = document.querySelector('.progress');
    var queued = false;
    function apply() {
      queued = false;
      var y = window.scrollY || 0;
      var max = document.documentElement.scrollHeight - window.innerHeight;
      root.classList.toggle('scrolled', y > 8);
      if (bar) bar.style.transform = 'scaleX(' + (max > 0 ? Math.min(1, y / max) : 0).toFixed(4) + ')';
    }
    function onScroll() { if (!queued) { queued = true; requestAnimationFrame(apply) } }
    window.addEventListener('scroll', onScroll, { passive: true });
    window.addEventListener('resize', onScroll);
    apply();
  }

  /* ----------------------------- section spy ----------------------------- */
  function startSpy() {
    if (!('IntersectionObserver' in window)) return;
    var links = {};
    document.querySelectorAll('.nav .lnk[href^="#"]').forEach(function (a) { links[a.getAttribute('href').slice(1)] = a });
    var ids = Object.keys(links);
    if (!ids.length) return;
    var io = new IntersectionObserver(function (entries) {
      entries.forEach(function (en) {
        var a = links[en.target.id];
        if (!a) return;
        if (en.isIntersecting) {
          ids.forEach(function (id) { links[id].removeAttribute('aria-current') });
          a.setAttribute('aria-current', 'true');
        } else if (a.getAttribute('aria-current')) a.removeAttribute('aria-current');
      });
    }, { rootMargin: '-45% 0px -50% 0px' });
    ids.forEach(function (id) { var s = document.getElementById(id); if (s) io.observe(s) });
  }

  ready(function () {
    // Each effect stands alone: one that throws must not take the rest down.
    [startScroll, startSpy, startField, startHero, startSpot, startCount].forEach(function (fn) {
      try { fn() } catch (e) { }
    });
    root.classList.add('motion');
  });
})();
