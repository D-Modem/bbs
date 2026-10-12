// The web terminal: xterm.js on the BBS's /terminal/ws, the caller's bytes
// both ways in binary frames, as a telnet client's would go.
"use strict";
(function () {
  const status = document.getElementById("status");
  const again = document.getElementById("again");
  const term = new Terminal({
    cursorBlink: true,
    fontFamily: '"DejaVu Sans Mono", "Cascadia Mono", Consolas, "Liberation Mono", monospace',
    fontSize: 16,
    scrollback: 2000,
    theme: { background: "#000000", foreground: "#c0c0c0" },
  });
  const fit = new FitAddon.FitAddon();
  term.loadAddon(fit);
  term.open(document.getElementById("term"));
  window.addEventListener("resize", () => fit.fit());

  const encoder = new TextEncoder();
  let ws = null;
  const send = (bytes) => {
    if (ws && ws.readyState === WebSocket.OPEN) ws.send(bytes);
  };
  term.onData((d) => send(encoder.encode(d)));
  term.onBinary((d) => send(Uint8Array.from(d, (c) => c.charCodeAt(0) & 0xff)));

  function connect() {
    fit.fit();
    term.reset();
    again.hidden = true;
    status.textContent = "Connecting…";
    const scheme = location.protocol === "https:" ? "wss:" : "ws:";
    const url = `${scheme}//${location.host}/terminal/ws?cols=${term.cols}&rows=${term.rows}`;
    ws = new WebSocket(url, "bbs.terminal");
    ws.binaryType = "arraybuffer";
    ws.onopen = () => {
      status.textContent = "Connected";
      term.focus();
    };
    ws.onmessage = (e) => term.write(new Uint8Array(e.data));
    ws.onclose = () => {
      status.textContent = "Disconnected";
      again.hidden = false;
      ws = null;
    };
  }
  again.addEventListener("click", connect);
  connect();
})();
