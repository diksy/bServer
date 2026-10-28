import { useEffect, useState } from "react";
import { api, fmtBytes } from "../api";

type Props = {
  serverId: string;
  serverName: string;
  path: string;
  onBack: () => void;
  toast: (m: string, k?: "info" | "error" | "success") => void;
};

// FileEditor membuka satu file teks remote lewat SFTP, menampilkannya di
// textarea, lalu menyimpan kembali isinya (maks 1 MB, lihat maxEditBytes di backend).
export default function FileEditor({ serverId, serverName, path, onBack, toast }: Props) {
  const [content, setContent] = useState("");
  const [original, setOriginal] = useState("");
  const [size, setSize] = useState(0);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState("");

  useEffect(() => {
    let alive = true;
    setLoading(true);
    setErr("");
    api
      .sftpRead(serverId, path)
      .then((r) => {
        if (!alive) return;
        setContent(r.content);
        setOriginal(r.content);
        setSize(r.size);
      })
      .catch((e: unknown) => {
        const msg = e instanceof Error ? e.message : "Gagal membuka file.";
        if (!alive) return;
        setErr(msg);
        toast(msg, "error");
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [serverId, path]);

  const dirty = content !== original;

  function leave() {
    if (dirty && !window.confirm("Ada perubahan belum disimpan. Tinggalkan editor?")) return;
    onBack();
  }

  async function save() {
    if (saving) return;
    setSaving(true);
    try {
      const r = await api.sftpWrite(serverId, path, content);
      setOriginal(content);
      setSize(r.bytes);
      toast("File disimpan", "success");
      onBack();
    } catch (e) {
      toast(e instanceof Error ? e.message : "Gagal menyimpan file.", "error");
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="panel">
      <div className="row" style={{ marginBottom: 8 }}>
        <button className="sm" onClick={leave}>
          ← Kembali
        </button>
        <div className="grow" style={{ minWidth: 0 }}>
          <div style={{ fontWeight: 600 }}>
            {serverName} {dirty && <span style={{ color: "var(--muted)", fontWeight: 400 }}>• belum disimpan</span>}
          </div>
          <div className="mono" style={{ fontSize: 12, color: "var(--muted)", overflowWrap: "anywhere" }}>
            {path}
          </div>
        </div>
        <span className="kv">
          <span>{fmtBytes(size / 1024)}</span>
        </span>
      </div>

      {err ? (
        <div className="err" style={{ marginBottom: 8 }}>
          {err}
        </div>
      ) : loading ? (
        <div className="empty" style={{ padding: 24 }}>
          Memuat file…
        </div>
      ) : (
        <textarea
          className="mono"
          value={content}
          onChange={(e) => setContent(e.target.value)}
          spellCheck={false}
          disabled={saving}
          style={{
            width: "100%",
            height: "58vh",
            resize: "vertical",
            fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
            fontSize: 13,
            lineHeight: 1.5,
            whiteSpace: "pre",
            overflow: "auto",
            tabSize: 2,
          }}
        />
      )}

      <div className="row" style={{ marginTop: 10 }}>
        <button onClick={save} disabled={loading || saving || !!err || !dirty}>
          {saving ? "Menyimpan…" : "Simpan"}
        </button>
        <button className="ghost" onClick={leave} disabled={saving}>
          Batal
        </button>
        <span style={{ flex: 1 }} />
        <span className="hint">Maksimal 1 MB. Untuk file lebih besar gunakan Unduh/Upload.</span>
      </div>
    </div>
  );
}
