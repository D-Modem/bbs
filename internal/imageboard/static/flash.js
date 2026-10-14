// The Flash player's frame: sandboxed (no origin, no cookies, nothing of
// the page's), it takes a movie from the page that framed it and plays it
// in Ruffle.  A movie may not reach the network, open pages or call any
// script; an FLV is played by the board's own player movie, from a blob
// of this frame's.
(function () {
  "use strict";
  window.RufflePlayer = window.RufflePlayer || {};
  window.RufflePlayer.config = {
    publicPath: "/chan/ruffle/",
    polyfills: false,
    autoplay: "on",
    unmuteOverlay: "visible",
    splashScreen: false,
    letterbox: "on",
    allowScriptAccess: false,
    allowNetworking: "none",
    openUrlMode: "deny",
    showSwfDownload: false,
    upgradeToHttps: true,
    warnOnUnsupportedContent: false,
    logLevel: "error"
  };
  window.addEventListener("message", function (e) {
    if (e.source !== window.parent || !e.data || typeof e.data !== "object") return;
    var p = window.RufflePlayer.newest().createPlayer();
    document.getElementById("player").append(p);
    if (e.data.swf instanceof ArrayBuffer) {
      p.ruffle().load({data: new Uint8Array(e.data.swf)});
    } else if (e.data.flv instanceof ArrayBuffer && e.data.player instanceof ArrayBuffer) {
      var src = URL.createObjectURL(new Blob([e.data.flv], {type: "video/x-flv"}));
      // the player is ours: it may load the FLV (a blob of this frame)
      p.ruffle().load({data: new Uint8Array(e.data.player), parameters: {src: src}, allowNetworking: "all"});
    }
  });
  window.addEventListener("DOMContentLoaded", function () {
    window.parent.postMessage("ready", "*");
  });
})();
