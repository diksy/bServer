import { useEffect, useRef } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { wsURL } from "../api";

type Props = {
  label: string;
  sublabel: string;
  wsPath: string;
  wsParams: Record<string, string | number>;
  /** Quick connect memakai id sekali pakai: jangan coba sambung ulang otomatis. */
  retryOnClose?: boolean;
  /** Mendaftarkan fungsi kirim input ke terminal ini (dipakai App untuk snippet). */
  onRegisterSend?: (fn: ((d: string) => void) | null) => void;
};

export default function TerminalPane({ label, sublabel, wsPath, wsParams, retryOnClose = true, onRegisterSend }: Props) {
  const hostRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const statusRef = useRef<HTMLSpanElement>(null);
  const reconnectRef = useRef<(() => void) | null>(null);
  // Disimpan di ref agar callback baru dari parent tidak memicu effect utama
  // (yang akan memutus & menyambung ulang terminal).
  const registerRef = useRef(onRegisterSend);
  registerRef.current = onRegisterSend;

  // wsParams dibandingkan lewat JSON agar effect tidak jalan ulang tiap render.
  const paramsKey = JSON.stringify(wsParams);

  useEffect(() => {
    const term = new Terminal({
      fontFamily: '"JetBrains Mono", ui-monospace, monospace',
      fontSize: 13,
      cursorBlink: true,
      scrollback: 5000,
      theme: {
        background: "#0b1220",
        foreground: "#e2e8f0",
        cursor: "#60a5fa",
        selectionBackground: "#1e3a8a",
        black: "#0b1220",
        brightBlack: "#64748b",
      },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(hostRef.current!);
    termRef.current = term;
    try {
      fit.fit();
    } catch {
      /* ignore */
    }
    term.writeln(`\x1b[38;5;75mMenghubungkan ke ${label} (${sublabel})…\x1b[0m`);

    let disposed = false;
    let retry = 0;

    const setStatus = (text: string) => {
      if (statusRef.current) statusRef.current.textContent = text;
    };

    // send mengirim data mentah (mis. isi snippet) ke sesi terminal ini.
    const send = (d: string) => {
      const ws = wsRef.current;
      if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ t: "in", d }));
    };

    function connect() {
      if (disposed) return;
      setStatus("menghubungkan…");
      const params = JSON.parse(paramsKey) as Record<string, string | number>;
      const ws = new WebSocket(wsURL(wsPath, { ...params, cols: term.cols, rows: term.rows }));
      wsRef.current = ws;
      ws.onopen = () => {
        retry = 0;
        setStatus("terhubung");
        term.focus();
        registerRef.current?.(send);
      };
      ws.onmessage = (ev) => {
        try {
          const m = JSON.parse(ev.data);
          if (m.t === "out") term.write(m.d);
          else if (m.t === "error") {
            term.writeln(`\r\n\x1b[31m${m.error}\x1b[0m`);
            setStatus("gagal");
          } else if (m.t === "exit") {
            setStatus("terputus");
          }
        } catch {
          term.write(ev.data);
        }
      };
      ws.onclose = () => {
        if (disposed) return;
        setStatus("terputus");
        registerRef.current?.(null);
        if (retryOnClose && retry < 2) {
          retry++;
          setTimeout(connect, 800);
        }
      };
    }

    const onData = term.onData((d) => send(d));
    const onResize = () => {
      try {
        fit.fit();
      } catch {
        return;
      }
      const ws = wsRef.current;
      if (ws && ws.readyState === WebSocket.OPEN)
        ws.send(JSON.stringify({ t: "resize", rows: term.rows, cols: term.cols }));
    };
    reconnectRef.current = () => {
      term.reset();
      connect();
    };
    const ro = new ResizeObserver(onResize);
    ro.observe(hostRef.current!);
    window.addEventListener("resize", onResize);
    connect();

    return () => {
      disposed = true;
      ro.disconnect();
      window.removeEventListener("resize", onResize);
      onData.dispose();
      wsRef.current?.close();
      term.dispose();
      registerRef.current?.(null);
    };
  }, [label, sublabel, wsPath, paramsKey, retryOnClose]);

  return (
    <div style={{ height: "100%", display: "flex", flexDirection: "column" }}>
      <div className="term-toolbar">
        <span className="dot" />
        <b style={{ color: "var(--text)" }}>{label}</b>
        <span className="mono">{sublabel}</span>
        <span ref={statusRef} className="badge">
          memulai
        </span>
        <span style={{ flex: 1 }} />
        {retryOnClose && (
          <button className="sm" onClick={() => reconnectRef.current?.()}>
            Sambung ulang
          </button>
        )}
      </div>
      <div className="term-wrap" ref={hostRef} style={{ flex: 1 }} />
    </div>
  );
}
