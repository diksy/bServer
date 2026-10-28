import { useCallback, useEffect, useState } from "react";
import { api, ApiError, type Snippet } from "../api";

type Props = {
  toast: (m: string, k?: "info" | "error" | "success") => void;
  onRun: (body: string) => void;
};

// preview mengambil baris pertama perintah agar daftar tetap ringkas.
function preview(body: string) {
  const line = body.split("\n").find((l) => l.trim() !== "") ?? "";
  return line.length > 110 ? line.slice(0, 110) + "…" : line || "(kosong)";
}

export default function SnippetPanel({ toast, onRun }: Props) {
  const [list, setList] = useState<Snippet[]>([]);
  const [loading, setLoading] = useState(true);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [body, setBody] = useState("");
  const [tags, setTags] = useState("");
  const [busy, setBusy] = useState(false);

  const refresh = useCallback(async () => {
    try {
      setList(await api.snippets());
    } catch (e) {
      toast(e instanceof ApiError ? e.message : "Gagal memuat snippet.", "error");
    } finally {
      setLoading(false);
    }
  }, [toast]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  function reset() {
    setEditingId(null);
    setName("");
    setBody("");
    setTags("");
  }

  function startEdit(sn: Snippet) {
    setEditingId(sn.id);
    setName(sn.name);
    setBody(sn.body);
    setTags(sn.tags);
  }

  async function save() {
    if (!name.trim()) return toast("Nama snippet wajib diisi.", "error");
    if (!body.trim()) return toast("Isi perintah masih kosong.", "error");
    setBusy(true);
    try {
      if (editingId) {
        await api.updateSnippet(editingId, name.trim(), body, tags.trim());
        toast("Snippet diperbarui", "success");
      } else {
        await api.createSnippet(name.trim(), body, tags.trim());
        toast("Snippet disimpan", "success");
      }
      reset();
      await refresh();
    } catch (e) {
      toast(e instanceof ApiError ? e.message : "Gagal menyimpan snippet.", "error");
    } finally {
      setBusy(false);
    }
  }

  async function remove(sn: Snippet) {
    if (!window.confirm(`Hapus snippet "${sn.name}"?`)) return;
    try {
      await api.deleteSnippet(sn.id);
      if (editingId === sn.id) reset();
      await refresh();
      toast("Snippet dihapus", "success");
    } catch (e) {
      toast(e instanceof ApiError ? e.message : "Gagal menghapus snippet.", "error");
    }
  }

  async function copy(sn: Snippet) {
    try {
      await navigator.clipboard.writeText(sn.body);
      toast("Disalin", "success");
    } catch {
      // fallback untuk browser/konteks tanpa Clipboard API (mis. http non-loopback)
      const ta = document.createElement("textarea");
      ta.value = sn.body;
      ta.style.position = "fixed";
      ta.style.opacity = "0";
      document.body.appendChild(ta);
      ta.select();
      let ok = false;
      try {
        ok = document.execCommand("copy");
      } catch {
        ok = false;
      }
      ta.remove();
      toast(ok ? "Disalin" : "Gagal menyalin (clipboard tidak tersedia).", ok ? "success" : "error");
    }
  }

  return (
    <div>
      <div className="panel">
        <h2>{editingId ? "Ubah snippet" : "Snippet perintah"}</h2>
        <p className="hint">
          Simpan perintah yang sering dipakai, lalu jalankan langsung di terminal yang sedang aktif atau salin ke clipboard.
        </p>
        <div className="row" style={{ alignItems: "flex-start" }}>
          <div style={{ flex: 1, minWidth: 200 }}>
            <label>Nama</label>
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="mis. Cek disk"
              style={{ width: "100%" }}
            />
          </div>
          <div style={{ flex: 1, minWidth: 160 }}>
            <label>Tag</label>
            <input
              value={tags}
              onChange={(e) => setTags(e.target.value)}
              placeholder="disk, pemeliharaan"
              style={{ width: "100%" }}
            />
          </div>
        </div>
        <div style={{ marginTop: 10 }}>
          <label>Isi perintah</label>
          <textarea
            rows={4}
            value={body}
            onChange={(e) => setBody(e.target.value)}
            placeholder="df -h / && du -sh /var/log/* | sort -h | tail -5"
            style={{ width: "100%", fontFamily: "var(--mono)" }}
          />
        </div>
        <div className="row" style={{ marginTop: 10 }}>
          <button className="primary" onClick={save} disabled={busy}>
            {busy ? "Menyimpan…" : editingId ? "Simpan perubahan" : "Tambah snippet"}
          </button>
          {editingId && (
            <button className="ghost" onClick={reset} disabled={busy}>
              Batal
            </button>
          )}
        </div>
      </div>

      <div className="panel">
        <div className="split" style={{ marginBottom: 4 }}>
          <div className="sidebar-title">Daftar snippet ({list.length})</div>
          <button className="sm" onClick={refresh} disabled={loading}>
            Muat ulang
          </button>
        </div>
        {loading && <div className="empty">Memuat…</div>}
        {!loading && list.length === 0 && (
          <div className="empty">
            <h3>Belum ada snippet</h3>
            <p>Tambahkan perintah pertama lewat form di atas.</p>
          </div>
        )}
        {list.map((sn) => (
          <div
            key={sn.id}
            style={{
              display: "flex",
              alignItems: "center",
              gap: 10,
              flexWrap: "wrap",
              border: "1px solid var(--border)",
              borderRadius: 8,
              padding: "10px 12px",
              marginTop: 8,
            }}
          >
            <div style={{ flex: 1, minWidth: 180 }}>
              <div className="row" style={{ gap: 8 }}>
                <b style={{ fontSize: 13 }}>{sn.name}</b>
                {sn.tags && <span className="badge">{sn.tags}</span>}
              </div>
              <div className="mono" style={{ fontSize: 11, color: "var(--muted)", wordBreak: "break-word", marginTop: 2 }}>
                {preview(sn.body)}
              </div>
            </div>
            <div className="row" style={{ gap: 6 }}>
              <button className="primary sm" onClick={() => onRun(sn.body)} title="Jalankan di terminal aktif">
                ▶ Jalankan
              </button>
              <button className="sm" onClick={() => copy(sn)} title="Salin ke clipboard">
                📋 Salin
              </button>
              <button className="sm" onClick={() => startEdit(sn)} title="Ubah snippet">
                ✎ Ubah
              </button>
              <button className="sm" onClick={() => remove(sn)} title="Hapus snippet">
                🗑 Hapus
              </button>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
