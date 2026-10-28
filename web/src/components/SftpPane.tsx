import { useCallback, useEffect, useRef, useState } from "react";
import { api, fmtBytes, type Server, type SftpEntry } from "../api";
import FileEditor from "./FileEditor";

const MAX_EDIT_BYTES = 1 << 20;

type Props = { servers: Server[]; initialServerId?: string; toast: (m: string, k?: "info" | "error" | "success") => void };

export default function SftpPane({ servers, initialServerId, toast }: Props) {
  const [serverId, setServerId] = useState(initialServerId || servers[0]?.id || "");
  const [dir, setDir] = useState(".");
  const [path, setPath] = useState("");
  const [entries, setEntries] = useState<SftpEntry[]>([]);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [progress, setProgress] = useState<{ kind: "upload" | "download"; pct: number; label: string } | null>(null);
  const [editing, setEditing] = useState<string | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);

  const load = useCallback(
    async (target = dir) => {
      if (!serverId) return;
      setBusy(true);
      setErr("");
      try {
        const r = await api.sftpList(serverId, target);
        setDir(r.path);
        setPath(r.path);
        setEntries(
          [...r.entries].sort((a, b) => Number(b.isDir) - Number(a.isDir) || a.name.localeCompare(b.name))
        );
      } catch (e) {
        setErr(e instanceof Error ? e.message : "Gagal membaca direktori.");
      } finally {
        setBusy(false);
      }
    },
    [serverId, dir]
  );

  useEffect(() => {
    setDir(".");
    setEditing(null);
    load(".");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [serverId]);

  async function upload(files: FileList | null) {
    if (!files?.length || !serverId) return;
    for (const file of Array.from(files)) {
      setProgress({ kind: "upload", pct: 0, label: file.name });
      try {
        const r = await api.sftpUpload(serverId, dir, file, (pct) => setProgress({ kind: "upload", pct, label: file.name }));
        toast(`Terunggah: ${r.remotePath} (${fmtBytes(r.bytes / 1024)})`, "success");
      } catch (e) {
        toast(e instanceof Error ? e.message : "Upload gagal.", "error");
      }
    }
    setProgress(null);
    if (fileRef.current) fileRef.current.value = "";
    load(dir);
  }

  async function download(entry: SftpEntry) {
    const full = dir.replace(/\/$/, "") + "/" + entry.name;
    setProgress({ kind: "download", pct: 0, label: entry.name });
    try {
      const n = await api.sftpDownload(serverId, full, (pct) => setProgress({ kind: "download", pct, label: entry.name }));
      toast(`Terunduh: ${entry.name} (${fmtBytes(n / 1024)})`, "success");
    } catch (e) {
      toast(e instanceof Error ? e.message : "Download gagal.", "error");
    } finally {
      setProgress(null);
    }
  }

  async function mkdir() {
    const name = window.prompt("Nama folder baru:");
    if (!name) return;
    try {
      await api.sftpMkdir(serverId, dir.replace(/\/$/, "") + "/" + name);
      toast("Folder dibuat.", "success");
      load(dir);
    } catch (e) {
      toast(e instanceof Error ? e.message : "Gagal membuat folder.", "error");
    }
  }

  const parent = dir.replace(/\/+$/, "").split("/").slice(0, -1).join("/") || "/";
  const serverName = servers.find((s) => s.id === serverId)?.name || "";

  if (editing) {
    return (
      <FileEditor
        serverId={serverId}
        serverName={serverName}
        path={editing}
        onBack={() => {
          setEditing(null);
          load(dir);
        }}
        toast={toast}
      />
    );
  }


  return (
    <div className="panel">
      <h2>Transfer file (SFTP)</h2>
      <p className="hint">Telusuri direktori remote, unggah, dan unduh file dengan indikator progres.</p>

      <div className="row" style={{ marginBottom: 10 }}>
        <div style={{ width: 220 }}>
          <label>Server</label>
          <select value={serverId} onChange={(e) => setServerId(e.target.value)}>
            {servers.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </select>
        </div>
        <div className="grow">
          <label>Path remote</label>
          <input
            value={path}
            onChange={(e) => setPath(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && load(path)}
            className="mono"
          />
        </div>
        <div style={{ marginTop: 18 }} className="row">
          <button onClick={() => load(path)} disabled={busy || !serverId}>
            Buka
          </button>
          <button onClick={() => load(parent)} disabled={busy || !serverId}>
            ↑ Naik
          </button>
          <button onClick={() => load(".")} disabled={busy || !serverId}>
            ~ Home
          </button>
        </div>
      </div>

      <div className="row" style={{ marginBottom: 10 }}>
        <button onClick={() => fileRef.current?.click()} disabled={!serverId || busy}>
          Unggah file
        </button>
        <input ref={fileRef} type="file" multiple hidden onChange={(e) => upload(e.target.files)} />
        <button onClick={mkdir} disabled={!serverId || busy}>
          Folder baru
        </button>
        <span className="kv">
          <span>{entries.length} entri</span>
        </span>
        <span style={{ flex: 1 }} />
        <button className="sm" onClick={() => load(dir)} disabled={busy}>
          Segarkan
        </button>
      </div>

      {progress && (
        <div style={{ marginBottom: 10 }}>
          <div className="kv" style={{ marginBottom: 4 }}>
            <span>
              {progress.kind === "upload" ? "Mengunggah" : "Mengunduh"} <b>{progress.label}</b>
            </span>
            <span style={{ marginLeft: "auto" }}>{progress.pct}%</span>
          </div>
          <div className="progress">
            <span style={{ width: `${progress.pct}%` }} />
          </div>
        </div>
      )}

      {err && <div className="err" style={{ marginBottom: 8 }}>{err}</div>}

      <div className="scroll-y" style={{ maxHeight: "52vh", border: "1px solid var(--border)", borderRadius: 8 }}>
        <table>
          <thead>
            <tr>
              <th>Nama</th>
              <th style={{ width: 100 }}>Ukuran</th>
              <th style={{ width: 110 }}>Mode</th>
              <th style={{ width: 160 }}>Diubah</th>
              <th style={{ width: 170 }} />
            </tr>
          </thead>
          <tbody>
            {entries.length === 0 && (
              <tr>
                <td colSpan={5} className="empty">
                  {busy ? "Memuat…" : "Direktori kosong / belum dimuat."}
                </td>
              </tr>
            )}
            {entries.map((e) => (
              <tr key={e.name}>
                <td>
                  {e.isDir ? (
                    <button className="ghost sm" style={{ color: "var(--primary)" }} onClick={() => load(dir.replace(/\/$/, "") + "/" + e.name)}>
                      📁 {e.name}
                    </button>
                  ) : (
                    <span className="mono">📄 {e.name}</span>
                  )}
                </td>
                <td>{e.isDir ? "—" : fmtBytes(e.size / 1024)}</td>
                <td className="mono">{e.mode}</td>
                <td style={{ fontSize: 12, color: "var(--muted)" }}>{new Date(e.modTime).toLocaleString("id-ID")}</td>
                <td>
                  {!e.isDir && (
                    <div className="row" style={{ gap: 6 }}>
                      {e.size <= MAX_EDIT_BYTES ? (
                        <button
                          className="ghost sm"
                          title="Edit file"
                          onClick={() => setEditing(dir.replace(/\/$/, "") + "/" + e.name)}
                        >
                          ✎
                        </button>
                      ) : (
                        <button className="ghost sm" title="terlalu besar untuk diedit" disabled>
                          ✎
                        </button>
                      )}
                      <button className="sm" onClick={() => download(e)}>
                        Unduh
                      </button>
                    </div>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
