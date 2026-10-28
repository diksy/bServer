import { useCallback, useEffect, useState } from "react";
import { api, fmtBytes, fmtUptime, pct, type Metric, type MetricPoint, type Server } from "../api";

type Props = { servers: Server[]; toast: (m: string, k?: "info" | "error" | "success") => void };

/** Sparkline sederhana (SVG inline, tanpa library) untuk satu deret nilai. */
function Sparkline({ points, color }: { points: number[]; color: string }) {
  if (points.length < 2)
    return (
      <div style={{ height: 40, display: "flex", alignItems: "center", fontSize: 11, opacity: 0.6 }}>
        belum ada riwayat
      </div>
    );
  const w = 100;
  const h = 40;
  const pad = 2;
  const max = Math.max(10, Math.ceil(Math.max(...points) / 10) * 10);
  const step = (w - pad * 2) / (points.length - 1);
  const coords = points.map((raw, i) => {
    const v = Math.max(0, Math.min(raw, max));
    const x = pad + i * step;
    const y = h - pad - (v / max) * (h - pad * 2);
    return `${x.toFixed(1)},${y.toFixed(1)}`;
  });
  return (
    <svg
      className="spark"
      viewBox={`0 0 ${w} ${h}`}
      preserveAspectRatio="none"
      role="img"
      aria-label="grafik riwayat"
      style={{ width: "100%", height: 40, display: "block" }}
    >
      <polyline
        points={coords.join(" ")}
        fill="none"
        stroke={color}
        strokeWidth={1.5}
        strokeLinejoin="round"
        strokeLinecap="round"
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  );
}

/** Satu baris riwayat: label, nilai terakhir, dan sparkline. */
function HistoryRow({ label, unit, points, color }: { label: string; unit: string; points: number[]; color: string }) {
  const last = points.length ? points[points.length - 1] : null;
  return (
    <div className="metric" style={{ marginTop: 6 }}>
      <div className="label">
        <span>{label}</span>
        <b>{last === null ? "—" : `${last.toFixed(1)}${unit}`}</b>
      </div>
      <Sparkline points={points} color={color} />
    </div>
  );
}

/** Panel riwayat metrik (CPU / RAM / disk) untuk satu server. */
function HistoryPanel({ points }: { points: MetricPoint[] }) {
  const cpu = points.map((p) => p.cpuPct);
  const ram = points.map((p) => pct(p.memUsedKb, p.memTotalKb));
  const disk = points.map((p) => pct(p.diskUsedKb, p.diskTotalKb));
  const lastTs = points.length ? new Date(points[points.length - 1].ts) : null;
  return (
    <div style={{ marginTop: 12, borderTop: "1px solid var(--border, #e5e7eb)", paddingTop: 8 }}>
      <div className="label" style={{ marginBottom: 2 }}>
        <span>Riwayat</span>
        <b>
          {points.length
            ? `${points.length} sampel${lastTs && !isNaN(lastTs.getTime()) ? ` · ${lastTs.toLocaleTimeString("id-ID")}` : ""}`
            : "—"}
        </b>
      </div>
      <HistoryRow label="CPU" unit="%" points={cpu} color="#2563EB" />
      <HistoryRow label="RAM" unit="%" points={ram} color="#16A34A" />
      <HistoryRow label="Disk /" unit="%" points={disk} color="#D97706" />
    </div>
  );
}

export default function Dashboard({ servers, toast }: Props) {
  const [selected, setSelected] = useState<Set<string>>(new Set(servers.slice(0, 3).map((s) => s.id)));
  const [metrics, setMetrics] = useState<Record<string, Metric>>({});
  const [history, setHistory] = useState<Record<string, MetricPoint[]>>({});
  const [busy, setBusy] = useState(false);
  const [auto, setAuto] = useState(true);
  const [lastAt, setLastAt] = useState<string>("");

  const refresh = useCallback(
    async (ids: string[]) => {
      if (!ids.length) {
        setMetrics({});
        setHistory({});
        return;
      }
      setBusy(true);
      try {
        const [list, hist] = await Promise.all([
          api.metrics(ids),
          Promise.all(ids.map((id) => api.metricHistory(id).catch(() => [] as MetricPoint[]))),
        ]);
        setMetrics(Object.fromEntries(list.map((m) => [m.serverId, m])));
        setHistory(Object.fromEntries(ids.map((id, i) => [id, hist[i]])));
        setLastAt(new Date().toLocaleTimeString("id-ID"));
      } catch (e) {
        toast(e instanceof Error ? e.message : "Gagal mengambil metrik.", "error");
      } finally {
        setBusy(false);
      }
    },
    [toast]
  );

  // Muat ulang hanya riwayat (dipakai timer 60 s, lebih murah dari snapshot SSH).
  const refreshHistory = useCallback(async (ids: string[]) => {
    if (!ids.length) return;
    try {
      const hist = await Promise.all(ids.map((id) => api.metricHistory(id).catch(() => [] as MetricPoint[])));
      setHistory(Object.fromEntries(ids.map((id, i) => [id, hist[i]])));
    } catch {
      /* diamkan: riwayat bersifat pelengkap */
    }
  }, []);

  const ids = [...selected];
  const key = ids.join(",");

  useEffect(() => {
    refresh(ids);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);

  useEffect(() => {
    if (!auto) return;
    const t = setInterval(() => refresh(ids), 10000);
    return () => clearInterval(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [auto, key]);

  useEffect(() => {
    if (!ids.length) return;
    const t = setInterval(() => refreshHistory(ids), 60000);
    return () => clearInterval(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);

  const toggle = (id: string) => {
    const n = new Set(selected);
    n.has(id) ? n.delete(id) : n.add(id);
    setSelected(n);
    setMetrics((m) => {
      const c = { ...m };
      delete c[id];
      return c;
    });
    setHistory((h) => {
      const c = { ...h };
      delete c[id];
      return c;
    });
  };

  return (
    <div>
      <div className="panel">
        <div className="split">
          <div>
            <h2>Dashboard metrik</h2>
            <p className="hint">Snapshot real-time via SSH (CPU, RAM, disk, load, uptime) + riwayat tersimpan.</p>
          </div>
          <div className="row">
            <span className="kv">{lastAt && <span>diperbarui <b>{lastAt}</b></span>}</span>
            <label className="check" style={{ fontSize: 12 }}>
              <input type="checkbox" checked={auto} onChange={(e) => setAuto(e.target.checked)} />
              auto 10 s
            </label>
            <button onClick={() => refresh(ids)} disabled={busy || !ids.length}>
              {busy ? "Memuat…" : "Segarkan"}
            </button>
          </div>
        </div>
        <div className="batch-grid" style={{ marginTop: 10 }}>
          {servers.map((s) => (
            <label key={s.id} className="check">
              <input type="checkbox" checked={selected.has(s.id)} onChange={() => toggle(s.id)} />
              <span className="dot" style={{ background: s.color || "var(--primary)" }} />
              <span>{s.name}</span>
            </label>
          ))}
          {servers.length === 0 && <div className="empty">Belum ada server untuk dipantau.</div>}
        </div>
      </div>

      <div className="metrics-grid" style={{ marginTop: 12 }}>
        {ids.map((id) => {
          const m = metrics[id];
          const s = servers.find((x) => x.id === id);
          const hist = history[id] ?? [];
          if (!m)
            return (
              <div className="metric-card" key={id}>
                <h3>
                  <span className="dot busy" /> {s?.name ?? id}
                </h3>
                <div className="sub">memuat metrik…</div>
                <HistoryPanel points={hist} />
              </div>
            );
          if (!m.ok)
            return (
              <div className="metric-card" key={id}>
                <h3>
                  <span className="dot fail" /> {m.name}
                </h3>
                <div className="sub">{s ? `${s.username}@${s.host}` : ""}</div>
                <div className="err">{m.error}</div>
                <HistoryPanel points={hist} />
              </div>
            );
          const ram = pct(m.memUsedKb, m.memTotalKb);
          const disk = pct(m.diskUsedKb, m.diskTotalKb);
          return (
            <div className="metric-card" key={id}>
              <h3>
                <span className="dot ok" /> {m.name}
              </h3>
              <div className="sub">
                {m.hostname} · {m.kernel} · latency {m.latencyMs} ms
              </div>
              <div className="metric">
                <div className="label">
                  <span>CPU</span>
                  <b>{m.cpuPct.toFixed(1)}%</b>
                </div>
                <div className={`meter ${m.cpuPct > 85 ? "danger" : m.cpuPct > 65 ? "warn" : ""}`}>
                  <span style={{ width: `${Math.min(100, m.cpuPct)}%` }} />
                </div>
              </div>
              <div className="metric">
                <div className="label">
                  <span>RAM</span>
                  <b>
                    {fmtBytes(m.memUsedKb)} / {fmtBytes(m.memTotalKb)}
                  </b>
                </div>
                <div className={`meter ${ram > 90 ? "danger" : ram > 75 ? "warn" : ""}`}>
                  <span style={{ width: `${ram}%` }} />
                </div>
              </div>
              <div className="metric">
                <div className="label">
                  <span>Disk /</span>
                  <b>
                    {fmtBytes(m.diskUsedKb)} / {fmtBytes(m.diskTotalKb)}
                  </b>
                </div>
                <div className={`meter ${disk > 90 ? "danger" : disk > 75 ? "warn" : ""}`}>
                  <span style={{ width: `${disk}%` }} />
                </div>
              </div>
              <div className="row" style={{ gap: 14, marginTop: 10 }}>
                <span className="kv">
                  <span>load <b>{m.load1.toFixed(2)}</b></span>
                </span>
                <span className="kv">
                  <span>uptime <b>{fmtUptime(m.uptimeSec)}</b></span>
                </span>
                <span className="kv">
                  <span>disk <b>{disk}%</b></span>
                </span>
              </div>
              <HistoryPanel points={hist} />
            </div>
          );
        })}
      </div>
    </div>
  );
}
