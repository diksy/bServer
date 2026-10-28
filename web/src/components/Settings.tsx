import { useEffect, useRef, useState } from "react";
import { api, type LogEntry, type VaultStatus } from "../api";

type Props = {
  status: VaultStatus | null;
  theme: "light" | "dark";
  onTheme: (t: "light" | "dark") => void;
  onLock: () => void;
  reload: () => void;
  toast: (m: string, k?: "info" | "error" | "success") => void;
};

export default function Settings({ status, theme, onTheme, onLock, reload, toast }: Props) {
  const [autoLock, setAutoLock] = useState(10);
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [busy, setBusy] = useState(false);
  const [thresholds, setThresholds] = useState({ cpuPct: 0, memPct: 0, diskPct: 0 });
  const [savingThresholds, setSavingThresholds] = useState(false);
  const jsonRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    api
      .getSettings()
      .then((s) => {
        setAutoLock(s.autoLockMin);
        if (s.alertThresholds) setThresholds(s.alertThresholds);
      })
      .catch(() => undefined);
    api.logs(50).then(setLogs).catch(() => undefined);
  }, []);

  async function saveThresholds() {
    setSavingThresholds(true);
    try {
      await api.putSettings({ alertThresholds: thresholds });
      toast("Ambang alert disimpan.", "success");
    } catch (e) {
      toast(e instanceof Error ? e.message : "Gagal menyimpan ambang alert.", "error");
    } finally {
      setSavingThresholds(false);
    }
  }

  async function saveAutoLock(v: number) {
    setAutoLock(v);
    try {
      await api.putSettings({ autoLockMin: v });
      toast("Auto-lock diperbarui.", "success");
    } catch {
      toast("Gagal menyimpan pengaturan.", "error");
    }
  }

  async function importSSHConfig() {
    setBusy(true);
    try {
      const r = await api.importSSHConfig();
      toast(`${r.imported} server diimpor dari ${r.path}. ${r.skipped.length ? r.skipped.length + " dilewati." : ""}`, "success");
      reload();
    } catch (e) {
      toast(e instanceof Error ? e.message : "Impor gagal.", "error");
    } finally {
      setBusy(false);
    }
  }

  async function importJson(file: File) {
    try {
      const parsed = JSON.parse(await file.text());
      const r = await api.importJSON(parsed);
      toast(`${r.imported} server diimpor. ${r.skipped.length ? r.skipped.length + " dilewati." : ""}`, "success");
      reload();
    } catch (e) {
      toast(e instanceof Error ? e.message : "JSON tidak valid.", "error");
    }
  }

  return (
    <div>
      <div className="panel">
        <h2>Pengaturan</h2>
        <p className="hint">Semua data tersimpan lokal di folder <span className="mono">{status?.dataDir}</span>.</p>
        <div className="grid2">
          <div>
            <label>Tema</label>
            <div className="row" style={{ gap: 6 }}>
              <button className={theme === "light" ? "primary" : ""} onClick={() => onTheme("light")}>
                ☀ Terang
              </button>
              <button className={theme === "dark" ? "primary" : ""} onClick={() => onTheme("dark")}>
                ☾ Gelap
              </button>
            </div>
          </div>
          <div>
            <label>Auto-lock idle (menit)</label>
            <input
              type="number"
              min={1}
              max={240}
              value={autoLock}
              onChange={(e) => saveAutoLock(Number(e.target.value) || 10)}
              style={{ width: 120 }}
            />
          </div>
          <div>
            <label>Inventaris</label>
            <div className="row" style={{ gap: 6 }}>
              <button onClick={importSSHConfig} disabled={busy}>
                Impor ~/.ssh/config
              </button>
              <button onClick={() => jsonRef.current?.click()}>Impor JSON</button>
              <button
                onClick={async () => {
                  try {
                    await api.exportInventory();
                  } catch {
                    toast("Ekspor gagal.", "error");
                  }
                }}
              >
                Ekspor JSON
              </button>
              <input
                ref={jsonRef}
                type="file"
                accept=".json"
                hidden
                onChange={(e) => e.target.files?.[0] && importJson(e.target.files[0])}
              />
            </div>
          </div>
          <div>
            <label>Vault</label>
            <div className="row" style={{ gap: 8 }}>
              <span className="badge ok">terbuka</span>
              <button onClick={onLock}>Kunci vault</button>
            </div>
          </div>
        </div>
      </div>

      <div className="panel">
        <h2>Ambang alert</h2>
        <p className="hint">
          Notifikasi lokal saat metrik melewati ambang (diperiksa tiap siklus sampling). Isi <b>0</b> untuk mematikan.
        </p>
        <div className="grid2">
          <div>
            <label>CPU (%)</label>
            <input
              type="number"
              min={0}
              max={100}
              value={thresholds.cpuPct}
              onChange={(e) =>
                setThresholds((t) => ({ ...t, cpuPct: Math.max(0, Math.min(100, Number(e.target.value) || 0)) }))
              }
              style={{ width: 120 }}
            />
          </div>
          <div>
            <label>RAM (%)</label>
            <input
              type="number"
              min={0}
              max={100}
              value={thresholds.memPct}
              onChange={(e) =>
                setThresholds((t) => ({ ...t, memPct: Math.max(0, Math.min(100, Number(e.target.value) || 0)) }))
              }
              style={{ width: 120 }}
            />
          </div>
          <div>
            <label>Disk (%)</label>
            <input
              type="number"
              min={0}
              max={100}
              value={thresholds.diskPct}
              onChange={(e) =>
                setThresholds((t) => ({ ...t, diskPct: Math.max(0, Math.min(100, Number(e.target.value) || 0)) }))
              }
              style={{ width: 120 }}
            />
          </div>
          <div>
            <label>&nbsp;</label>
            <button className="primary" onClick={saveThresholds} disabled={savingThresholds}>
              {savingThresholds ? "Menyimpan…" : "Simpan ambang"}
            </button>
          </div>
        </div>
      </div>

      <div className="panel">
        <div className="split">
          <div>
            <h2>Log aktivitas</h2>
            <p className="hint">Perintah dari batch &amp; terminal (secret diredaksi otomatis).</p>
          </div>
          <button className="sm" onClick={() => api.logs(50).then(setLogs).catch(() => undefined)}>
            Segarkan
          </button>
        </div>
        <div className="scroll-y" style={{ maxHeight: 320 }}>
          <table>
            <thead>
              <tr>
                <th style={{ width: 150 }}>Waktu</th>
                <th style={{ width: 120 }}>Server</th>
                <th style={{ width: 70 }}>Sumber</th>
                <th>Perintah</th>
                <th style={{ width: 60 }}>Exit</th>
                <th style={{ width: 80 }}>Durasi</th>
              </tr>
            </thead>
            <tbody>
              {logs.length === 0 && (
                <tr>
                  <td colSpan={6} className="empty">
                    Belum ada aktivitas tercatat.
                  </td>
                </tr>
              )}
              {logs.map((l) => (
                <tr key={l.id}>
                  <td style={{ fontSize: 12, color: "var(--muted)" }}>{new Date(l.ts).toLocaleString("id-ID")}</td>
                  <td>{l.serverName || "—"}</td>
                  <td>
                    <span className="badge">{l.source}</span>
                  </td>
                  <td className="mono" style={{ maxWidth: 320, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                    {l.command}
                  </td>
                  <td>
                    <span className={`badge ${l.exitCode === 0 ? "ok" : "err"}`}>{l.exitCode}</span>
                  </td>
                  <td style={{ fontSize: 12, color: "var(--muted)" }}>{l.durationMs} ms</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      <div className="panel">
        <h2>Tentang prototype</h2>
        <p className="hint">
          bServer {status?.version} · SSH core Go · SPA React + xterm.js · bind 127.0.0.1 dengan token sesi.
          SSH agent: {status?.sshAgentSock ? "terdeteksi" : "tidak aktif"}.
        </p>
      </div>
    </div>
  );
}
