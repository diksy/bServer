import { useCallback, useEffect, useState } from "react";
import { api, type Server, type ServicesResult } from "../api";

type Props = { servers: Server[]; toast: (m: string, k?: "info" | "error" | "success") => void };

/** Badge warna untuk status unit systemd (active/failed/lain). */
function serviceBadge(s: { active: string; sub: string }) {
  const active = (s.active || "").toLowerCase();
  const sub = (s.sub || "").toLowerCase();
  if (active === "active" || sub === "running") return <span className="badge ok">{s.active || s.sub}</span>;
  if (active === "failed" || sub === "failed") return <span className="badge err">{s.active || s.sub}</span>;
  return <span className="badge warn">{s.active || s.sub || "—"}</span>;
}

/** Badge warna untuk status container Docker ("Up" / "Exited" / lain). */
function containerBadge(status: string) {
  const s = (status || "").toLowerCase();
  if (s.startsWith("up")) return <span className="badge ok">{status}</span>;
  if (s.startsWith("exited") || s.includes("dead")) return <span className="badge err">{status}</span>;
  if (s.includes("restarting") || s.includes("created") || s.includes("paused")) {
    return <span className="badge warn">{status}</span>;
  }
  return <span className="badge">{status || "—"}</span>;
}

/** Panel status layanan systemd + container Docker pada server remote. */
export default function ServicesPanel({ servers, toast }: Props) {
  const [serverId, setServerId] = useState<string>(servers[0]?.id ?? "");
  const [result, setResult] = useState<ServicesResult | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(
    async (id: string) => {
      if (!id) {
        setResult(null);
        return;
      }
      setBusy(true);
      try {
        setResult(await api.services(id));
      } catch (e) {
        setResult(null);
        toast(e instanceof Error ? e.message : "Gagal memuat status layanan.", "error");
      } finally {
        setBusy(false);
      }
    },
    [toast]
  );

  // Muat saat server dipilih / daftar server berubah.
  useEffect(() => {
    if (!servers.length) {
      setServerId("");
      setResult(null);
      return;
    }
    if (!serverId || !servers.some((s) => s.id === serverId)) {
      // default ke server pertama (atau pulihkan bila server dihapus)
      setServerId(servers[0].id);
      return;
    }
    load(serverId);
  }, [serverId, servers, load]);

  const systemd = result?.systemd ?? [];
  const docker = result?.docker ?? [];

  return (
    <div className="panel">
      <div className="split">
        <div>
          <h2>Layanan &amp; container</h2>
          <p className="hint">
            Status unit systemd dan container Docker pada server remote (perintah dijalankan lewat SSH).
          </p>
        </div>
        <div className="row">
          <select value={serverId} onChange={(e) => setServerId(e.target.value)} disabled={!servers.length} style={{ minWidth: 180 }}>
            {servers.length === 0 && <option value="">(belum ada server)</option>}
            {servers.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name} — {s.host}
              </option>
            ))}
          </select>
          <button className="sm" onClick={() => load(serverId)} disabled={busy || !serverId}>
            {busy ? "Memuat…" : "Segarkan"}
          </button>
        </div>
      </div>

      {!servers.length && <div className="empty">Belum ada server. Tambahkan server terlebih dahulu.</div>}

      {servers.length > 0 && busy && !result && <div className="empty">Memuat status layanan…</div>}

      {servers.length > 0 && result && (
        <>
          <h3 style={{ marginTop: 14 }}>Layanan systemd</h3>
          {result.systemdError ? (
            <p className="hint" style={{ color: "var(--danger)" }}>
              systemd tidak tersedia: {result.systemdError}
            </p>
          ) : systemd.length === 0 ? (
            <div className="empty">Tidak ada unit service yang dilaporkan.</div>
          ) : (
            <div className="scroll-y" style={{ maxHeight: 340 }}>
              <table>
                <thead>
                  <tr>
                    <th style={{ width: 240 }}>Nama</th>
                    <th style={{ width: 90 }}>Status</th>
                    <th style={{ width: 100 }}>Sub</th>
                    <th>Deskripsi</th>
                  </tr>
                </thead>
                <tbody>
                  {systemd.map((s, i) => (
                    <tr key={`${s.name}-${i}`}>
                      <td className="mono" style={{ fontSize: 12 }}>
                        {s.name}
                      </td>
                      <td>{serviceBadge(s)}</td>
                      <td className="mono" style={{ fontSize: 12, color: "var(--muted)" }}>
                        {s.sub || "—"}
                      </td>
                      <td style={{ fontSize: 13 }}>{s.description || "—"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          <h3 style={{ marginTop: 18 }}>Container Docker</h3>
          {result.dockerError ? (
            <p className="hint" style={{ color: "var(--danger)" }}>
              docker tidak tersedia: {result.dockerError}
            </p>
          ) : docker.length === 0 ? (
            <div className="empty">Tidak ada container.</div>
          ) : (
            <div className="scroll-y" style={{ maxHeight: 340 }}>
              <table>
                <thead>
                  <tr>
                    <th style={{ width: 240 }}>Nama</th>
                    <th>Image</th>
                    <th style={{ width: 220 }}>Status</th>
                  </tr>
                </thead>
                <tbody>
                  {docker.map((c, i) => (
                    <tr key={`${c.name}-${i}`}>
                      <td className="mono" style={{ fontSize: 12 }}>
                        {c.name}
                      </td>
                      <td className="mono" style={{ fontSize: 12, color: "var(--muted)" }}>
                        {c.image}
                      </td>
                      <td>{containerBadge(c.status)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}
    </div>
  );
}
