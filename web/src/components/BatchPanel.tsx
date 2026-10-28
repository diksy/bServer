import { useState } from "react";
import { api, ApiError, type BatchResult, type BatchSummary, type Server } from "../api";

type Props = { servers: Server[]; initialSelected: string[]; toast: (m: string, k?: "info" | "error" | "success") => void };

export default function BatchPanel({ servers, initialSelected, toast }: Props) {
  const [selected, setSelected] = useState<Set<string>>(new Set(initialSelected));
  const [command, setCommand] = useState("uptime");
  const [concurrency, setConcurrency] = useState(5);
  const [timeoutSec, setTimeoutSec] = useState(30);
  const [busy, setBusy] = useState(false);
  const [results, setResults] = useState<BatchResult[]>([]);
  const [summary, setSummary] = useState<BatchSummary | null>(null);

  const toggle = (id: string) => {
    const next = new Set(selected);
    next.has(id) ? next.delete(id) : next.add(id);
    setSelected(next);
  };

  async function run() {
    if (!selected.size) return toast("Pilih minimal satu server.", "error");
    if (!command.trim()) return toast("Perintah masih kosong.", "error");
    setBusy(true);
    setResults([]);
    setSummary(null);
    try {
      const r = await api.exec([...selected], command, concurrency, timeoutSec);
      setResults(r.results);
      setSummary(r.summary);
      toast(`Selesai: ${r.summary.success} sukses, ${r.summary.failed} gagal, ${r.summary.timeout} timeout.`, "success");
    } catch (e) {
      toast(e instanceof ApiError ? e.message : "Eksekusi gagal.", "error");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div>
      <div className="panel">
        <h2>Jalankan perintah ke banyak server</h2>
        <p className="hint">
          Perintah dijalankan paralel (batas konkurensi) dengan timeout per host. Output dibatasi 256 KB per server.
        </p>
        <div className="split" style={{ marginBottom: 8 }}>
          <div className="sidebar-title">Pilih server ({selected.size}/{servers.length})</div>
          <div className="row" style={{ gap: 6 }}>
            <button className="sm" onClick={() => setSelected(new Set(servers.map((s) => s.id)))}>
              Semua
            </button>
            <button className="sm" onClick={() => setSelected(new Set())}>
              Kosongkan
            </button>
          </div>
        </div>
        <div className="batch-grid">
          {servers.length === 0 && <div className="empty">Belum ada server.</div>}
          {servers.map((s) => (
            <label key={s.id} className="check">
              <input type="checkbox" checked={selected.has(s.id)} onChange={() => toggle(s.id)} />
              <span className="dot" style={{ background: s.color || "var(--primary)" }} />
              <span>{s.name}</span>
              <span style={{ color: "var(--muted)", fontSize: 11 }}>{s.host}</span>
            </label>
          ))}
        </div>

        <div style={{ marginTop: 12 }}>
          <label>Perintah</label>
          <textarea rows={3} value={command} onChange={(e) => setCommand(e.target.value)} placeholder="uptime && df -h /" />
        </div>
        <div className="row" style={{ marginTop: 10 }}>
          <div>
            <label>Konkurensi</label>
            <input type="number" min={1} max={20} value={concurrency} onChange={(e) => setConcurrency(Number(e.target.value))} style={{ width: 90 }} />
          </div>
          <div>
            <label>Timeout (detik)</label>
            <input type="number" min={5} max={600} value={timeoutSec} onChange={(e) => setTimeoutSec(Number(e.target.value))} style={{ width: 110 }} />
          </div>
          <div style={{ marginTop: 18 }}>
            <button className="primary" onClick={run} disabled={busy}>
              {busy ? "Menjalankan…" : "Jalankan"}
            </button>
          </div>
        </div>
      </div>

      {summary && (
        <div className="panel">
          <div className="row" style={{ gap: 10 }}>
            <b>{summary.total} host</b>
            <span className="badge ok">{summary.success} sukses</span>
            <span className="badge err">{summary.failed} gagal</span>
            <span className="badge warn">{summary.timeout} timeout</span>
            <span className="kv">
              <span>total <b>{(summary.elapsedMs / 1000).toFixed(2)} s</b></span>
            </span>
          </div>
        </div>
      )}

      {results.map((r, i) => (
        <div className="result-card" key={r.serverId + i}>
          <div className="result-head">
            <span className={`dot ${r.status === "ok" ? "ok" : "fail"}`} />
            <span className="name">{r.name}</span>
            <span className={`badge ${r.status === "ok" ? "ok" : r.status === "timeout" ? "warn" : "err"}`}>
              {r.status === "ok" ? "sukses" : r.status === "timeout" ? "timeout" : "gagal"}
            </span>
            <span className="kv">
              <span>exit <b>{r.exitCode}</b></span>
              <span>{r.durationMs} ms</span>
            </span>
            <span style={{ flex: 1 }} />
            {r.truncated && <span className="badge warn">output dipotong</span>}
          </div>
          <pre>{r.error ? `${r.error}\n\n${r.output}` : r.output || "(tanpa output)"}</pre>
        </div>
      ))}
    </div>
  );
}
