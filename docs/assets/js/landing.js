// dens.chat landing. Each piece of art's behavior is its own block below.
(function () {
  "use strict";

  var root = document.documentElement;
  var still = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  root.classList.add("js");

  // ---------------------------------------------------------------- skin
  // Auto, light or dark. Auto follows the system; the others are kept where
  // the docs keep theirs, so the choice follows the visitor between pages.
  var skin = document.querySelector(".skin");
  if (skin) {
    var picked = root.classList.contains("dark") ? "dark" : root.classList.contains("light") ? "light" : "auto";
    var buttons = skin.querySelectorAll("button");
    var showSkin = function () {
      for (var i = 0; i < buttons.length; i++) {
        buttons[i].setAttribute("aria-pressed", buttons[i].value === picked ? "true" : "false");
      }
    };
    skin.addEventListener("click", function (e) {
      var b = e.target.closest("button");
      if (!b) return;
      picked = b.value;
      root.classList.remove("light", "dark");
      if (picked !== "auto") root.classList.add(picked);
      try {
        if (picked === "auto") localStorage.removeItem("color-theme");
        else localStorage.setItem("color-theme", picked);
      } catch (err) {}
      showSkin();
    });
    skin.hidden = false;
    showSkin();
  }

  // ---------------------------------------------------------------- EULAs
  // Each EULA is a <details>. Here it unrolls instead of popping open: the
  // cloud's all the way to the bottom of the page, yours by three lines.
  // Clicking the rod or the paper snaps it back up, fast, and clicks during
  // the snap are ignored, so an impatient second click can't send it back
  // down.
  var eulas = document.querySelectorAll(".eula");
  Array.prototype.forEach.call(eulas, function (eula) {
    var rod = eula.querySelector(".rod");
    var paper = eula.querySelector(".paper");
    var timer = null;

    function reach() {
      if (eula.dataset.reach === "page") {
        var top = paper.getBoundingClientRect().top + window.scrollY;
        var bottom = document.documentElement.scrollHeight;
        return Math.max(240, bottom - top - 40);
      }
      return eula.querySelector(".paper-in").scrollHeight;
    }

    function roll(open) {
      var from = paper.getBoundingClientRect().height;
      var to = open ? reach() : 0;
      var far = Math.abs(to - from);
      // Unrolling takes its time (that's the joke); rolling up is a snap.
      var ms = still ? 0 : open ? Math.min(3400, 420 + far * 0.55) : Math.min(380, 160 + far * 0.06);
      var ease = open ? "cubic-bezier(0.3, 0.7, 0.3, 1)" : "cubic-bezier(0.4, 0, 0.2, 1)";
      eula.dataset.state = open ? "open" : "closing";
      eula.classList.add("moving");
      paper.style.transition = "none";
      paper.style.height = from + "px";
      void paper.offsetHeight;
      paper.style.transition = "height " + ms + "ms " + ease;
      paper.style.height = to + "px";
      clearTimeout(timer);
      timer = setTimeout(function () {
        eula.classList.remove("moving");
        if (!open) {
          eula.open = false;
          eula.dataset.state = "closed";
          paper.style.height = "";
        }
      }, ms + 40);
    }

    function toggle(e) {
      e.preventDefault();
      if (eula.dataset.state === "closing") return;
      if (eula.dataset.state === "open") {
        roll(false);
      } else {
        eula.open = true;
        paper.style.height = "0px";
        roll(true);
      }
    }

    rod.addEventListener("click", toggle);
    paper.addEventListener("click", function (e) {
      if (eula.dataset.state === "open" && !e.target.closest("a")) toggle(e);
    });

    // The page's length changes with the window; keep the cloud's reaching it.
    var resized = null;
    window.addEventListener("resize", function () {
      clearTimeout(resized);
      resized = setTimeout(function () {
        if (eula.dataset.state === "open" && eula.dataset.reach === "page") {
          paper.style.transition = "none";
          paper.style.height = "0px";
          paper.style.height = reach() + "px";
        }
      }, 150);
    });
  });

  // ---------------------------------------------------------------- cameras
  // Five cameras on the rails while section 02 is on screen. Each follows the
  // pointer on a spring of its own and has tics: a twitch, a nod, a glance
  // away and a double take. The lens zooms when you come close, and if you
  // stop moving they patrol. Their cables swing with them. Pointing at Dens
  // in the table powers them down.
  var section = document.getElementById("encrypt");
  var box = document.querySelector(".cams");
  if (section && box) {
    var tempers = [
      { k: 0.09, damp: 0.8 },
      { k: 0.05, damp: 0.88 },
      { k: 0.16, damp: 0.68 },
      { k: 0.07, damp: 0.84 },
      { k: 0.12, damp: 0.74 },
    ];
    var cams = Array.prototype.map.call(box.querySelectorAll(".cam"), function (el, i) {
      return {
        el: el,
        head: el.querySelector(".cam-head"),
        lens: el.querySelector(".cam-lens"),
        cable: el.querySelector(".cam-cable"),
        side: el.classList.contains("cam--r") ? -1 : 1,
        t: tempers[i % tempers.length],
        a: 0,
        v: 0,
        z: 1,
        sag: 9,
        tic: -1,
        ticUntil: 0,
        ticBy: 0,
        nextTic: 1500 + Math.random() * 5000,
        phase: Math.random() * 6.28,
      };
    });
    var mx = window.innerWidth / 2;
    var my = window.innerHeight / 2;
    var moved = 0;
    var showing = false;
    var down = false;
    var raf = 0;

    var point = function (e) {
      mx = e.clientX;
      my = e.clientY;
      moved = performance.now();
    };
    window.addEventListener("pointermove", point, { passive: true });
    window.addEventListener("pointerdown", point, { passive: true });

    // The cable runs from the wall plate (5, 44) to the underside of the
    // head, (33, 41) before the head turns about its knuckle at (26, 32). It
    // hangs a little lower while the head swings.
    var cable = function (c) {
      var r = (c.a * Math.PI) / 180;
      var ax = 26 + 7 * Math.cos(r) - 9 * Math.sin(r);
      var ay = 32 + 7 * Math.sin(r) + 9 * Math.cos(r);
      var want = 9 + Math.min(9, Math.abs(c.v) * 1.6);
      c.sag += (want - c.sag) * 0.18;
      var qx = (5 + ax) / 2;
      var qy = Math.max(44, ay) + c.sag;
      c.cable.setAttribute("d", "M5 44Q" + qx.toFixed(1) + " " + qy.toFixed(1) + " " + ax.toFixed(1) + " " + ay.toFixed(1));
    };

    var aim = function (c, now) {
      var r = c.el.getBoundingClientRect();
      // the pivot, in the drawing at (26, 32) of 100 x 64, mirrored on the right
      var px = c.side > 0 ? r.left + r.width * 0.26 : r.right - r.width * 0.26;
      var py = r.top + r.height * 0.5;
      var target = Math.atan2(my - py, (mx - px) * c.side) * 57.29578;
      target = Math.max(-70, Math.min(70, target));
      var k = c.t.k;
      var damp = c.t.damp;
      if (down) {
        target = 38;
        k = 0.035;
        damp = 0.86;
      } else {
        if (now - moved > 4000) target = Math.sin(now / 1500 + c.phase) * 30;
        if (now > c.nextTic) {
          c.tic = Math.floor(Math.random() * 3);
          c.ticUntil = now + [150, 950, 280][c.tic];
          c.ticBy = c.tic === 1 ? Math.random() * 120 - 60 - target : (Math.random() < 0.5 ? -1 : 1) * (7 + Math.random() * 9);
          c.nextTic = now + 2500 + Math.random() * 6500;
        }
        if (now < c.ticUntil) {
          target += c.ticBy;
        } else if (c.tic === 1 && now < c.ticUntil + 320) {
          k = 0.32;
          damp = 0.6;
        }
      }
      c.v = (c.v + (target - c.a) * k) * damp;
      c.a += c.v;
      var near = Math.hypot(mx - px, my - py) < 240 && !down;
      c.z += ((near ? 1.32 : 1) - c.z) * 0.12;
      c.head.style.setProperty("--a", c.a.toFixed(2) + "deg");
      c.lens.style.setProperty("--z", c.z.toFixed(3));
      cable(c);
    };

    var frame = function (now) {
      raf = 0;
      if (!showing) return;
      for (var i = 0; i < cams.length; i++) aim(cams[i], now);
      raf = requestAnimationFrame(frame);
    };

    var show = function (v) {
      showing = v;
      document.body.classList.toggle("cams-on", v);
      if (v && !still && !raf) raf = requestAnimationFrame(frame);
    };

    new IntersectionObserver(function (entries) {
      show(entries[entries.length - 1].isIntersecting);
    }, { rootMargin: "-35% 0px -35% 0px" }).observe(section);

    var power = function (off) {
      if (off === down) return;
      down = off;
      document.body.classList.toggle("cams-off", off);
      if (still) {
        for (var i = 0; i < cams.length; i++) {
          cams[i].a = off ? 38 : 0;
          cams[i].head.style.setProperty("--a", cams[i].a + "deg");
          cable(cams[i]);
        }
      }
      if (!off) {
        document.body.classList.add("cams-boot");
        setTimeout(function () {
          document.body.classList.remove("cams-boot");
        }, 700);
      }
    };

    var table = section.querySelector(".compare table");
    if (table) {
      table.addEventListener("pointerover", function (e) {
        power(!!e.target.closest('[data-col="dens"]'));
      });
      table.addEventListener("pointerleave", function () {
        power(false);
      });
    }
  }

  // ---------------------------------------------------------------- the forest
  // The barcode's room is 260vh tall and its stage sticks; how far through
  // the room you are is --p, which the drawing and the words under it read.
  var room = document.querySelector(".forest-scroll");
  var stage = document.querySelector(".forest-stage");
  if (room && stage) {
    if (still) {
      // No scroll to drive it: the room collapses and the forest is grown.
      root.classList.remove("js");
    } else {
      var queued = false;
      var grow = function () {
        queued = false;
        var r = room.getBoundingClientRect();
        var span = r.height - window.innerHeight;
        var p = span > 0 ? Math.min(1, Math.max(0, -r.top / span)) : 1;
        stage.style.setProperty("--p", p.toFixed(4));
      };
      window.addEventListener("scroll", function () {
        if (!queued) {
          queued = true;
          requestAnimationFrame(grow);
        }
      }, { passive: true });
      window.addEventListener("resize", grow);
      grow();
    }
  }

  // The answer under the forest fades in once you reach it.
  var more = document.querySelector(".free-more");
  if (more) {
    if (still || !("IntersectionObserver" in window)) {
      more.classList.add("in");
    } else {
      new IntersectionObserver(function (entries, obs) {
        if (entries[0].isIntersecting) {
          more.classList.add("in");
          obs.disconnect();
        }
      }, { threshold: 0.3 }).observe(more);
    }
  }
})();
