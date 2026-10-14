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
