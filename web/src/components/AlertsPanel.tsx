import { useCallback, useEffect, useState } from "react";
import { api, type Alert } from "../api";

type Props = { toast: (m: string, k?: "info" | "error" | "success") => void; onAck?: () => void };

const KIND_LABEL: Record<string, string> = { cpu: "CPU", mem: "RAM", disk: "Disk" };

/** Panel daftar alert lokal (ambang metrik) + aksi tandai dibaca. */
export default function AlertsPanel({ toast, onAck }: Props) {
  const [list, setList] = useState<Alert[]>([]);
  const [busy, setBusy] = useState(false);
  const [showAcked, setShowAcked] = useState(true);

  const load = useCallback(async () => {
    setBusy(true);
    try {
      setList(await api.alerts(200));
    } catch (e) {
      toast(e instanceof Error ? e.message : "Gagal memuat alert.", "error");
    } finally {
      setBusy(false);
    }
  }, [toast]);

  useEffect(() => {
    load();
  }, [load]);

  async function ackAll() {
    try {
      await api.ackAlerts();
      await load();
      onAck?.();
      toast("Semua alert ditandai dibaca.", "success");
    } catch (e) {
      toast(e instanceof Error ? e.message : "Gagal menandai alert.", "error");
    }
  }

  const visible = showAcked ? list : list.filter((a) => !a.acknowledged);
  const unacked = list.filter((a) => !a.acknowledged).length;

  return (
    <div className="panel">
      <div className="split">
        <div>
          <h2>Alert lokal</h2>
          <p className="hint">
            Peringatan saat CPU/RAM/disk melewati ambang. {unacked > 0 ? `${unacked} belum dibaca.` : "Tidak ada yang belum dibaca."}
          </p>
        </div>
        <div className="row">
          <label className="check" style={{ fontSize: 12 }}>
            <input type="checkbox" checked={showAcked} onChange={(e) => setShowAcked(e.target.checked)} />
            tampilkan yang sudah dibaca
          </label>
          <button className="sm" onClick={load} disabled={busy}>
            {busy ? "Memuat…" : "Segarkan"}
          </button>
          <button className="sm" onClick={ackAll} disabled={!unacked}>
            Tandai semua dibaca
          </button>
        </div>
      </div>

      <div className="scroll-y" style={{ maxHeight: 520, marginTop: 10 }}>
        <table>
          <thead>
            <tr>
              <th style={{ width: 160 }}>Waktu</th>
              <th style={{ width: 140 }}>Server</th>
              <th style={{ width: 80 }}>Jenis</th>
              <th>Pesan</th>
              <th style={{ width: 110 }}>Nilai / ambang</th>
              <th style={{ width: 90 }}>Status</th>
            </tr>
          </thead>
          <tbody>
            {visible.length === 0 && (
              <tr>
                <td colSpan={6} className="empty">
                  Belum ada alert. Atur ambang di Pengaturan untuk mulai memantau.
                </td>
              </tr>
            )}
            {visible.map((a) => (
              <tr key={a.id}>
                <td style={{ fontSize: 12, color: "var(--muted)" }}>
                  {a.ts && !isNaN(new Date(a.ts).getTime()) ? new Date(a.ts).toLocaleString("id-ID") : "—"}
                </td>
                <td>{a.serverName || a.serverId || "—"}</td>
                <td>
                  <span className={`badge ${a.kind === "disk" ? "warn" : "err"}`}>{KIND_LABEL[a.kind] ?? a.kind}</span>
                </td>
                <td style={{ fontSize: 13 }}>{a.message}</td>
                <td className="mono" style={{ fontSize: 12 }}>
                  {a.value.toFixed(1)}% / {a.threshold.toFixed(1)}%
                </td>
                <td>
                  {a.acknowledged ? <span className="badge ok">dibaca</span> : <span className="badge err">baru</span>}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
