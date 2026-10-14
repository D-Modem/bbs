// The image boards work without this; with it, as 4chan's do: a post's
// number quotes it in the reply form, a thumbnail opens the image in
// place, a quote shows the post it quotes when hovered, and choosing a
// style needs no button.
(function () {
  "use strict";
  document.documentElement.classList.add("js");

  document.addEventListener("click", function (e) {
    var a = e.target.closest ? e.target.closest("a") : null;
    if (!a) return;
    // No.123: quote it in this page's form (one for this thread)
    if (a.classList.contains("quotePost")) {
      var com = document.getElementById("com");
      var form = document.getElementById("postForm");
      var resto = form && form.querySelector("input[name=resto]");
      var thread = a.closest(".thread");
      var here = thread && resto && ("t" + resto.value) === thread.id;
      if (!com || !here) return;
      e.preventDefault();
      var text = ">>" + a.dataset.no + "\n";
      var start = com.selectionStart || 0, end = com.selectionEnd || 0;
      com.value = com.value.slice(0, start) + text + com.value.slice(end);
      com.selectionStart = com.selectionEnd = start + text.length;
      com.focus();
      return;
    }
    // a video's thumbnail: the video, playing in place, and [Close]
    if (a.classList.contains("videoThumb") && !e.ctrlKey && !e.metaKey && !e.shiftKey && e.button === 0) {
      e.preventDefault();
      var file = a.closest(".file");
      var v = document.createElement("video");
      v.className = "expandedVideo";
      v.controls = true;
      v.autoplay = true;
      v.loop = true;
      v.src = a.href;
      a.hidden = true;
      a.after(v);
      var close = document.createElement("span");
      close.className = "videoClose";
      close.append(" [");
      var x = document.createElement("a");
      x.href = "#";
      x.className = "closeVideo";
      x.textContent = "Close";
      close.append(x, "]");
      file.querySelector(".fileText").append(close);
      return;
    }
    // Flash: Ruffle, in a sandboxed frame of no origin, handed the movie
    // (fetched here, with this page's cookies) and nothing else
    if (a.classList.contains("flashThumb") && !e.ctrlKey && !e.metaKey && !e.shiftKey && e.button === 0) {
      e.preventDefault();
      var ffile = a.closest(".file");
      var w = +a.dataset.w || 550, h = +a.dataset.h || 400;
      var room = Math.min(window.innerWidth - 80, w * 2), scale = Math.min(1, room / w, (window.innerHeight * 0.85) / h);
      var frame = document.createElement("iframe");
      frame.className = "expandedFlash";
      frame.setAttribute("sandbox", "allow-scripts");
      frame.setAttribute("allow", "autoplay; fullscreen");
      frame.width = Math.round(w * scale);
      frame.height = Math.round(h * scale);
      frame.src = "/chan/flash";
      var flv = a.dataset.flash === ".flv";
      var files = [fetch(a.href, {credentials: "same-origin"}).then(function (r) { return r.arrayBuffer(); })];
      if (flv) files.push(fetch("/chan/flvplayer.swf?w=" + w + "&h=" + h).then(function (r) { return r.arrayBuffer(); }));
      var onMessage = function (m) {
        if (m.source !== frame.contentWindow || m.data !== "ready") return;
        window.removeEventListener("message", onMessage);
        Promise.all(files).then(function (got) {
          var msg = flv ? {flv: got[0], player: got[1]} : {swf: got[0]};
          frame.contentWindow.postMessage(msg, "*", got);
        });
      };
      window.addEventListener("message", onMessage);
      a.hidden = true;
      a.after(frame);
      var fclose = document.createElement("span");
      fclose.className = "videoClose";
      fclose.append(" [");
      var fx = document.createElement("a");
      fx.href = "#";
      fx.className = "closeVideo";
      fx.textContent = "Close";
      fclose.append(fx, "]");
      ffile.querySelector(".fileText").append(fclose);
      return;
    }
    if (a.classList.contains("closeVideo")) {
      e.preventDefault();
      var f = a.closest(".file");
      var vid = f.querySelector("video.expandedVideo");
      if (vid) { vid.pause(); vid.remove(); }
      var fl = f.querySelector("iframe.expandedFlash");
      if (fl) fl.remove();
      f.querySelector(".fileThumb").hidden = false;
      a.closest(".videoClose").remove();
      return;
    }
    // a PDF's: its own tab, as the link says
    if (a.classList.contains("pdfThumb")) return;
    // a thumbnail: the image itself, and back
    if (a.classList.contains("fileThumb") && !e.ctrlKey && !e.metaKey && !e.shiftKey && e.button === 0) {
      var img = a.querySelector("img");
      if (!img) return;
      e.preventDefault();
      if (img.classList.contains("expanded")) {
        img.src = img.dataset.thumb;
        img.width = +img.dataset.w;
        img.height = +img.dataset.h;
        img.classList.remove("expanded");
      } else {
        img.dataset.thumb = img.getAttribute("src");
        img.dataset.w = img.width;
        img.dataset.h = img.height;
        img.removeAttribute("width");
        img.removeAttribute("height");
        img.classList.add("expanded");
        img.src = a.href;
      }
    }
  });

  // hovering >>123: the post, if it is on this page
  var preview = null;
  function hide() {
    if (preview) { preview.remove(); preview = null; }
    document.querySelectorAll(".post.highlight").forEach(function (p) { p.classList.remove("highlight"); });
  }
  document.addEventListener("mouseover", function (e) {
    var a = e.target.closest ? e.target.closest("a.quotelink") : null;
    if (!a || !a.dataset.no) return;
    var post = document.getElementById("p" + a.dataset.no);
    if (!post) return;
    var r = post.getBoundingClientRect();
    if (r.top >= 0 && r.bottom <= window.innerHeight) { // in sight: show where it is
      post.classList.add("highlight");
      return;
    }
    preview = document.createElement("div");
    preview.id = "quotePreview";
    var copy = post.cloneNode(true);
    copy.removeAttribute("id");
    copy.classList.add("reply");
    preview.appendChild(copy);
    document.body.appendChild(preview);
    var ar = a.getBoundingClientRect();
    preview.style.left = Math.max(4, Math.min(ar.right + 8, window.innerWidth - preview.offsetWidth - 8)) + window.scrollX + "px";
    preview.style.top = Math.max(4, ar.top - preview.offsetHeight / 2) + window.scrollY + "px";
  });
  document.addEventListener("mouseout", function (e) {
    var a = e.target.closest ? e.target.closest("a.quotelink") : null;
    if (a) hide();
  });

  // a poster's ID: their posts in the thread highlighted, or not again
  document.addEventListener("click", function (e) {
    var h = e.target.closest ? e.target.closest(".posteruid .hand") : null;
    if (!h) return;
    var posts = document.querySelectorAll('.post[data-id="' + CSS.escape(h.dataset.id) + '"]');
    var on = !(posts.length && posts[0].classList.contains("idHighlight"));
    document.querySelectorAll(".post.idHighlight").forEach(function (p) {
      p.classList.remove("idHighlight", "highlight");
    });
    if (on) posts.forEach(function (p) { p.classList.add("idHighlight", "highlight"); });
  });

  // a captcha that cannot be read: another
  document.querySelectorAll(".captchaNew").forEach(function (b) {
    b.addEventListener("click", function () {
      fetch("/chan/captcha.json").then(function (r) { return r.json(); }).then(function (j) {
        var row = b.closest(".captchaRow");
        row.querySelector("img.captcha").src = "/chan/captcha.png?t=" + j.token;
        row.querySelector("input[name=ctok]").value = j.token;
        var input = row.querySelector("input[name=captcha]");
        input.value = "";
        input.focus();
      });
    });
  });

  // the style: chosen as it is picked
  document.querySelectorAll(".styleSwitch select").forEach(function (sel) {
    sel.addEventListener("change", function () { sel.form.submit(); });
  });
})();
