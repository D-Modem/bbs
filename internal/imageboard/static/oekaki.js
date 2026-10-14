// Oekaki: the drawing board of an oekaki board's post form, and the
// replay of any drawing that carries its strokes.  A drawing is posted as
// a PNG, its strokes in an "okST" chunk of it (zlib-compressed JSON; see
// oekaki.go): {"v":1,"w":..,"h":..,"bg":"#ffffff","s":[{"t":"p"|"e"|"f",
// "c":"#rrggbb","z":size,"p":[dt,x,y,...]}]}.
(function () {
  "use strict";

  var CHUNK = "okST", MAX_DT = 600000, BG = "#ffffff";
  var PALETTE = ["#000000", "#ffffff", "#7f7f7f", "#c3c3c3", "#880015", "#ed1c24", "#ff7f27", "#fff200",
    "#22b14c", "#b5e61d", "#00a2e8", "#99d9ea", "#3f48cc", "#7092be", "#a349a4", "#ffaec9", "#b97a57", "#efe4b0"];

  // --- PNG chunks ---

  var CRC = (function () {
    var t = new Uint32Array(256);
    for (var n = 0; n < 256; n++) {
      var c = n;
      for (var k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
      t[n] = c >>> 0;
    }
    return t;
  })();

  function crc32(b) {
    var c = 0xffffffff;
    for (var i = 0; i < b.length; i++) c = CRC[(c ^ b[i]) & 255] ^ (c >>> 8);
    return (c ^ 0xffffffff) >>> 0;
  }

  function through(u8, stream) {
    return new Response(new Blob([u8]).stream().pipeThrough(stream)).arrayBuffer().then(function (b) {
      return new Uint8Array(b);
    });
  }

  // a PNG with a chunk put in before its IEND
  function addChunk(png, type, data) {
    var end = png.length - 12;
    var chunk = new Uint8Array(12 + data.length), dv = new DataView(chunk.buffer);
    dv.setUint32(0, data.length);
    for (var i = 0; i < 4; i++) chunk[4 + i] = type.charCodeAt(i);
    chunk.set(data, 8);
    dv.setUint32(8 + data.length, crc32(chunk.subarray(4, 8 + data.length)));
    var out = new Uint8Array(png.length + chunk.length);
    out.set(png.subarray(0, end));
    out.set(chunk, end);
    out.set(png.subarray(end), end + chunk.length);
    return out;
  }

  function findChunk(png, type) {
    var dv = new DataView(png.buffer, png.byteOffset, png.byteLength);
    for (var i = 8; i + 12 <= png.length;) {
      var n = dv.getUint32(i), t = String.fromCharCode(png[i + 4], png[i + 5], png[i + 6], png[i + 7]);
      if (t === type) return png.subarray(i + 8, i + 8 + n);
      if (t === "IEND") break;
      i += 12 + n;
    }
    return null;
  }

  // a drawing's replay, from its PNG
  function readReplay(url) {
    return fetch(url).then(function (r) {
      if (!r.ok) throw new Error("the image is gone");
      return r.arrayBuffer();
    }).then(function (b) {
      var c = findChunk(new Uint8Array(b), CHUNK);
      if (!c) throw new Error("this image has no strokes");
      return through(c, new DecompressionStream("deflate"));
    }).then(function (j) {
      return JSON.parse(new TextDecoder().decode(j));
    });
  }

  // --- painting ---

  function hex(c) {
    return [parseInt(c.slice(1, 3), 16), parseInt(c.slice(3, 5), 16), parseInt(c.slice(5, 7), 16)];
  }

  // a stroke's points from..to (by point), onto ctx
  function paint(ctx, bg, s, from, to) {
    if (s.t === "f") {
      if (from === 0) floodFill(ctx, s.p[1], s.p[2], s.c);
      return;
    }
    ctx.strokeStyle = ctx.fillStyle = s.t === "e" ? bg : s.c;
    ctx.lineWidth = s.z;
    ctx.lineCap = ctx.lineJoin = "round";
    for (var i = from; i < to; i++) {
      var x = s.p[i * 3 + 1], y = s.p[i * 3 + 2];
      if (i === 0) {
        ctx.beginPath();
        ctx.arc(x, y, s.z / 2, 0, Math.PI * 2);
        ctx.fill();
      } else {
        ctx.beginPath();
        ctx.moveTo(s.p[i * 3 - 2], s.p[i * 3 - 1]);
        ctx.lineTo(x, y);
        ctx.stroke();
      }
    }
  }

  function redraw(ctx, rp) {
    ctx.fillStyle = rp.bg;
    ctx.fillRect(0, 0, rp.w, rp.h);
    rp.s.forEach(function (s) { paint(ctx, rp.bg, s, 0, s.p.length / 3); });
  }

  // fill the area of (x, y)'s colour (near enough: lines' soft edges are
  // taken in) with a colour
  function floodFill(ctx, x, y, colour) {
    var w = ctx.canvas.width, h = ctx.canvas.height;
    if (x < 0 || y < 0 || x >= w || y >= h) return;
    var img = ctx.getImageData(0, 0, w, h), d = img.data;
    var at = (y * w + x) * 4, tr = d[at], tg = d[at + 1], tb = d[at + 2];
    var f = hex(colour);
    if (Math.abs(tr - f[0]) + Math.abs(tg - f[1]) + Math.abs(tb - f[2]) < 8) return;
    var seen = new Uint8Array(w * h), stack = [x, y];
    function like(p) {
      var o = p * 4;
      return !seen[p] && Math.abs(d[o] - tr) + Math.abs(d[o + 1] - tg) + Math.abs(d[o + 2] - tb) <= 96;
    }
    while (stack.length) {
      var py = stack.pop(), px = stack.pop(), p = py * w + px;
      while (px > 0 && like(p - 1)) { px--; p--; }
      var up = false, down = false;
      for (; px < w && like(p); px++, p++) {
        seen[p] = 1;
        d[p * 4] = f[0]; d[p * 4 + 1] = f[1]; d[p * 4 + 2] = f[2]; d[p * 4 + 3] = 255;
        if (py > 0) {
          if (like(p - w)) { if (!up) { stack.push(px, py - 1); up = true; } } else up = false;
        }
        if (py < h - 1) {
          if (like(p + w)) { if (!down) { stack.push(px, py + 1); down = true; } } else down = false;
        }
      }
    }
    ctx.putImageData(img, 0, 0);
  }

  function drawTime(ms) {
    var s = Math.floor(ms / 1000);
    if (s >= 3600) return Math.floor(s / 3600) + "h" + String(Math.floor(s % 3600 / 60)).padStart(2, "0") + "m";
    if (s >= 60) return Math.floor(s / 60) + "m" + String(s % 60).padStart(2, "0") + "s";
    return s + "s";
  }

  function totalMS(rp) {
    var ms = 0;
    rp.s.forEach(function (s) { for (var i = 0; i < s.p.length; i += 3) ms += s.p[i]; });
    return ms;
  }

  // --- the drawing board ---

  var panel = document.getElementById("oekaki");
  var form = document.getElementById("postForm");
  if (panel && form) board();

  function board() {
    var canvas = document.getElementById("okCanvas"), ctx = canvas.getContext("2d", { willReadFrequently: true });
    var colour = document.getElementById("okColor"), size = document.getElementById("okSize");
    var status = document.getElementById("okStatus"), timeShown = document.getElementById("okTime");
    var fileInput = form.querySelector("input[name=upfile]");
    var rp = null, redo = [], cur = null, lastEnd = null, lastT = 0, attached = false;
    // a thread's image may be drawn: the browser is not to want a file
    // first (the submit below does: a drawing, or a file)
    var needed = fileInput.required;
    fileInput.required = false;

    var pal = document.getElementById("okPalette");
    PALETTE.forEach(function (c) {
      var b = document.createElement("button");
      b.type = "button";
      b.title = c;
      b.style.background = c;
      b.addEventListener("click", function () { colour.value = c; });
      pal.appendChild(b);
    });
    size.addEventListener("input", function () { document.getElementById("okSizeShown").textContent = size.value; });

    function tool() { return form.querySelector("input[name=okTool]:checked").value; }

    function open(w, h, strokes, bg) {
      w = Math.max(50, Math.min(1200, w | 0));
      h = Math.max(50, Math.min(1200, h | 0));
      rp = { v: 1, w: w, h: h, bg: bg || BG, s: strokes || [] };
      redo = [];
      lastEnd = null;
      canvas.width = w;
      canvas.height = h;
      redraw(ctx, rp);
      panel.hidden = false;
      attached = false;
      shown();
      panel.scrollIntoView({ block: "nearest" });
    }

    function shown() {
      timeShown.textContent = rp && rp.s.length ? "Drawing time " + drawTime(totalMS(rp)) + ", " + rp.s.length + " strokes" : "";
    }

    document.getElementById("okOpen").addEventListener("click", function () {
      if (rp && rp.s.length && !panel.hidden) return;
      if (rp && rp.s.length && !confirm("Start a new drawing? This one will be lost.")) { panel.hidden = false; return; }
      open(+document.getElementById("okW").value, +document.getElementById("okH").value);
    });
    document.getElementById("okClose").addEventListener("click", function () { panel.hidden = true; });

    function point(e) {
      var r = canvas.getBoundingClientRect(), z = cur ? cur.z : +size.value;
      var x = Math.round((e.clientX - r.left) * canvas.width / r.width);
      var y = Math.round((e.clientY - r.top) * canvas.height / r.height);
      return [Math.max(-z, Math.min(canvas.width + z, x)), Math.max(-z, Math.min(canvas.height + z, y))];
    }

    function since(now, from) { return from == null ? 0 : Math.min(MAX_DT, Math.round(now - from)); }

    canvas.addEventListener("pointerdown", function (e) {
      if (!rp || e.button !== 0) return;
      e.preventDefault();
      var now = performance.now(), pt = point(e), t = tool();
      if (t === "f") {
        if (pt[0] < 0 || pt[1] < 0 || pt[0] >= rp.w || pt[1] >= rp.h) return;
        var s = { t: "f", c: colour.value, z: 1, p: [since(now, lastEnd), pt[0], pt[1]] };
        rp.s.push(s);
        paint(ctx, rp.bg, s, 0, 1);
        lastEnd = now;
        redo = [];
        attached = false;
        shown();
        return;
      }
      canvas.setPointerCapture(e.pointerId);
      cur = { t: t, c: colour.value, z: +size.value, p: [since(now, lastEnd), pt[0], pt[1]] };
      lastT = now;
      paint(ctx, rp.bg, cur, 0, 1);
    });
    canvas.addEventListener("pointermove", function (e) {
      if (!cur) return;
      var now = performance.now(), pt = point(e), n = cur.p.length;
      if (pt[0] === cur.p[n - 2] && pt[1] === cur.p[n - 1]) return;
      cur.p.push(since(now, lastT), pt[0], pt[1]);
      lastT = now;
      paint(ctx, rp.bg, cur, cur.p.length / 3 - 1, cur.p.length / 3);
    });
    function end() {
      if (!cur) return;
      rp.s.push(cur);
      cur = null;
      lastEnd = performance.now();
      redo = [];
      attached = false;
      shown();
    }
    canvas.addEventListener("pointerup", end);
    canvas.addEventListener("pointercancel", end);

    function undo() {
      if (!rp || !rp.s.length) return;
      redo.push(rp.s.pop());
      redraw(ctx, rp);
      attached = false;
      shown();
    }
    function again() {
      if (!rp || !redo.length) return;
      var s = redo.pop();
      rp.s.push(s);
      paint(ctx, rp.bg, s, 0, s.p.length / 3);
      attached = false;
      shown();
    }
    document.getElementById("okUndo").addEventListener("click", undo);
    document.getElementById("okRedo").addEventListener("click", again);
    document.getElementById("okClear").addEventListener("click", function () {
      if (rp && rp.s.length && confirm("Clear the drawing?")) open(rp.w, rp.h);
    });
    document.addEventListener("keydown", function (e) {
      if (panel.hidden || !(e.ctrlKey || e.metaKey) || /^(INPUT|TEXTAREA)$/.test(document.activeElement.tagName)) return;
      if (e.key === "z" && !e.shiftKey) { e.preventDefault(); undo(); }
      if (e.key === "y" || e.key === "Z") { e.preventDefault(); again(); }
    });

    // the drawing, with its strokes, as the post's file
    function attach() {
      if (!rp || !rp.s.length) return Promise.reject(new Error("Draw something first."));
      var strokes = new TextEncoder().encode(JSON.stringify(rp));
      return Promise.all([
        new Promise(function (ok) { canvas.toBlob(ok, "image/png"); }).then(function (b) { return b.arrayBuffer(); }),
        through(strokes, new CompressionStream("deflate")),
      ]).then(function (r) {
        var png = addChunk(new Uint8Array(r[0]), CHUNK, r[1]);
        var dt = new DataTransfer();
        dt.items.add(new File([png], "oekaki.png", { type: "image/png" }));
        fileInput.files = dt.files;
        attached = true;
        status.textContent = "Drawing attached (" + rp.w + "x" + rp.h + ", drawn in " + drawTime(totalMS(rp)) + ").";
      });
    }
    document.getElementById("okUse").addEventListener("click", function () {
      attach().then(function () { panel.hidden = true; }, function (err) { alert(err.message); });
    });
    // posting with a drawing not yet attached: attach it first
    form.addEventListener("submit", function (e) {
      var drawn = rp && rp.s.length;
      if (needed && !drawn && !fileInput.files.length) {
        e.preventDefault();
        alert("A thread starts with a picture: draw one, or choose a file.");
        return;
      }
      if (!drawn || attached || (fileInput.files.length && fileInput.files[0].name !== "oekaki.png")) return;
      e.preventDefault();
      attach().then(function () { form.requestSubmit(); }, function (err) { alert(err.message); });
    });
    fileInput.addEventListener("change", function () { if (attached) { attached = false; status.textContent = ""; } });

    // drawing over another's drawing
    function drawOver(src, no) {
      status.textContent = "Loading the drawing...";
      return readReplay(src).then(function (old) {
        if (rp && rp.s.length && !confirm("Start from No." + no + "'s drawing? This one will be lost.")) return;
        document.getElementById("okW").value = old.w;
        document.getElementById("okH").value = old.h;
        open(old.w, old.h, old.s, old.bg);
        status.textContent = "Drawing over No." + no + ".";
        var com = document.getElementById("com");
        if (com && com.value.indexOf(">>" + no) < 0) com.value = ">>" + no + "\n" + com.value;
      }, function (err) { status.textContent = err.message; });
    }
    document.addEventListener("click", function (e) {
      var a = e.target.closest ? e.target.closest("a.drawOver") : null;
      if (!a) return;
      var resto = form.querySelector("input[name=resto]");
      if (!resto || resto.value !== a.dataset.thread) return; // not this page's thread: its page
      e.preventDefault();
      drawOver(a.dataset.src, a.closest(".post").id.slice(1));
    });
    var want = new URLSearchParams(location.search).get("draw");
    if (want) {
      var link = document.querySelector("#p" + CSS.escape(want) + " a.drawOver");
      if (link) drawOver(link.dataset.src, want);
    }
  }

  // --- replaying a drawing ---

  document.addEventListener("click", function (e) {
    var a = e.target.closest ? e.target.closest("a.replayLink") : null;
    if (!a) return;
    e.preventDefault();
    var file = a.closest(".file");
    var old = file.querySelector(".okReplay");
    if (old) { old.remove(); file.querySelector(".fileThumb").hidden = false; return; }
    readReplay(a.href).then(function (rp) { replay(file, rp); }, function (err) { alert(err.message); });
  });

  function replay(file, rp) {
    var box = document.createElement("div");
    box.className = "okReplay";
    var canvas = document.createElement("canvas");
    canvas.width = rp.w;
    canvas.height = rp.h;
    var bar = document.createElement("div");
    bar.className = "okReplayBar";
    var speed = document.createElement("select");
    [1, 2, 4, 8, 32].forEach(function (n) {
      var o = document.createElement("option");
      o.value = n;
      o.textContent = n + "×";
      if (n === 4) o.selected = true;
      speed.appendChild(o);
    });
    var again = document.createElement("button"), close = document.createElement("button"), clock = document.createElement("span");
    again.type = close.type = "button";
    again.textContent = "Again";
    close.textContent = "Close";
    bar.append("Speed ", speed, " ", again, " ", close, " ", clock);
    box.append(canvas, bar);
    var thumb = file.querySelector(".fileThumb");
    thumb.hidden = true;
    thumb.after(box);
    var ctx = canvas.getContext("2d", { willReadFrequently: true });
    var total = totalMS(rp), run = 0;

    function play() {
      var id = ++run, si = 0, pi = 0, at = 0, due = 0, last = null;
      ctx.fillStyle = rp.bg;
      ctx.fillRect(0, 0, rp.w, rp.h);
      function frame(now) {
        if (id !== run || !box.isConnected) return;
        if (last !== null) at += (now - last) * +speed.value;
        last = now;
        while (si < rp.s.length) {
          var s = rp.s[si];
          // the wait before each point: gaps between strokes cut to a second
          var wait = Math.min(s.p[pi * 3], pi === 0 ? 1000 : 500);
          if (due + wait > at) break;
          due += wait;
          paint(ctx, rp.bg, s, pi, pi + 1);
          if (++pi >= s.p.length / 3) { si++; pi = 0; }
        }
        clock.textContent = si < rp.s.length ? "stroke " + (si + 1) + " of " + rp.s.length : "drawn in " + drawTime(total);
        if (si < rp.s.length) requestAnimationFrame(frame);
      }
      requestAnimationFrame(frame);
    }
    again.addEventListener("click", play);
    close.addEventListener("click", function () { run++; box.remove(); thumb.hidden = false; });
    play();
  }
})();
