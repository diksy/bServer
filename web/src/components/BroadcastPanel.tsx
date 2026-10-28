import { useRef, useState } from "react";
import type { Server } from "../api";
import TerminalPane from "./TerminalPane";

type Props = { servers: Server[]; toast: (m: string, k?: "info" | "error" | "success") => void };

export default function BroadcastPanel({ servers, toast }: Props) {
  const [selected, setSelected] = useState<string[]>([]);
  const [started, setStarted] = useState(false);
  const [cmd, setCmd] = useState("");
  const [sentCount, setSentCount] = useState(0);
  // Fungsi kirim per server yang sedang ter-mount (diisi TerminalPane).
  const sendsRef = useRef(new Map<string, (d: string) => void>());
  const inputRef = useRef<HTMLInputElement>(null);

  const picked = servers.filter((s) => selected.includes(s.id));

  const toggle = (id: string) =>
    setSelected((prev) => (prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]));

  function start() {
    if (!selected.length) return toast("Pilih minimal satu server.", "error");
    sendsRef.current.clear();
    setSentCount(0);
    setStarted(true);
  }

  function finish() {
    setStarted(false);
    setCmd("");
    setSentCount(0);
    sendsRef.current.clear();
  }

  // Kirim satu perintah ke semua terminal yang sudah terhubung.
  function sendAll() {
    const text = cmd;
    if (!text.trim()) return toast("Perintah masih kosong.", "error");
    const fns = [...sendsRef.current.values()];
    if (!fns.length) return toast("Belum ada terminal yang terhubung.", "info");
    for (const fn of fns) fn(text + "\n");
    setCmd("");
    setSentCount(fns.length);
    inputRef.current?.focus();
  }

  if (!started) {
    return (
      <div className="panel">
        <h2>Broadcast terminal</h2>
        <p className="hint">
          Buka beberapa terminal sekaligus, lalu kirim satu perintah ke semuanya secara bersamaan (synchronized panes).
          Perintah dikirim ke shell masing-masing server persis seperti Anda mengetiknya.
        </p>
        <div className="split" style={{ marginBottom: 8 }}>
          <div className="sidebar-title">
            Pilih server ({selected.length}/{servers.length})
          </div>
          <div className="row" style={{ gap: 6 }}>
            <button className="sm" onClick={() => setSelected(servers.map((s) => s.id))} disabled={!servers.length}>
              Semua
            </button>
            <button className="sm" onClick={() => setSelected([])} disabled={!selected.length}>
              Kosongkan
            </button>
          </div>
        </div>
        <div className="batch-grid">
          {servers.length === 0 && <div className="empty">Belum ada server.</div>}
          {servers.map((s) => (
            <label key={s.id} className="check">
              <input type="checkbox" checked={selected.includes(s.id)} onChange={() => toggle(s.id)} />
              <span className="dot" style={{ background: s.color || "var(--primary)" }} />
              <span>{s.name}</span>
              <span style={{ color: "var(--muted)", fontSize: 11 }}>{s.host}</span>
            </label>
          ))}
        </div>
        <div className="row" style={{ marginTop: 12 }}>
          <button className="primary" onClick={start} disabled={!selected.length}>
            Mulai broadcast
          </button>
          <span className="hint" style={{ margin: 0 }}>
            {selected.length ? `${selected.length} terminal akan dibuka.` : "Belum ada terminal dipilih."}
          </span>
        </div>
      </div>
    );
  }

  return (
    <div>
      <div className="panel">
        <div className="split" style={{ marginBottom: 8 }}>
          <div>
            <h2>Broadcast aktif</h2>
            <p className="hint" style={{ margin: 0 }}>
              {picked.length} terminal target
              {sentCount > 0 ? ` · perintah terakhir dikirim ke ${sentCount} terminal` : ""}
            </p>
          </div>
          <button className="ghost sm" onClick={finish} title="Tutup semua terminal dan kembali ke pemilihan">
            Selesai
          </button>
        </div>
        <div className="broadcast-input">
          <input
            ref={inputRef}
            autoFocus
            value={cmd}
            onChange={(e) => setCmd(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                sendAll();
              }
            }}
            placeholder="Ketik perintah, Enter untuk kirim ke semua terminal…"
            spellCheck={false}
            autoComplete="off"
          />
          <button className="primary" onClick={sendAll}>
            Kirim
          </button>
        </div>
      </div>

      <div className="broadcast-grid" style={picked.length <= 1 ? { gridTemplateColumns: "1fr" } : undefined}>
        {picked.map((s) => (
          <div className="broadcast-cell" key={s.id}>
            <div className="bc-label">
              <span className="dot" style={{ background: s.color || "var(--primary)" }} />
              <b>{s.name}</b>
              <span className="mono">
                {s.username}@{s.host}:{s.port}
              </span>
            </div>
            <div className="bc-term">
              <TerminalPane
                label={s.name}
                sublabel={`${s.username}@${s.host}:${s.port}`}
                wsPath="/ws/term"
                wsParams={{ serverId: s.id }}
                retryOnClose={false}
                onRegisterSend={(fn) => {
                  if (fn) sendsRef.current.set(s.id, fn);
                  else sendsRef.current.delete(s.id);
                }}
              />
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
