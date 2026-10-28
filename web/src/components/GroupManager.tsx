import { useState } from "react";
import { api, ApiError, type Group, type Server } from "../api";

type Props = {
  groups: Group[];
  servers: Server[];
  onClose: () => void;
  onChanged: () => Promise<void> | void;
  toast: (msg: string, kind?: "info" | "error" | "success") => void;
};

const PRESETS = ["#2563EB", "#3B82F6", "#16A34A", "#D97706", "#DC2626", "#7C3AED", "#0891B2", "#64748B"];

export default function GroupManager({ groups, servers, onClose, onChanged, toast }: Props) {
  const [name, setName] = useState("");
  const [color, setColor] = useState(PRESETS[0]);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editName, setEditName] = useState("");

  const countOf = (id: string) => servers.filter((s) => s.groupId === id).length;

  async function create() {
    const n = name.trim();
    if (!n) return setErr("Nama grup wajib diisi.");
    setBusy(true);
    setErr("");
    try {
      await api.createGroup(n, color);
      setName("");
      await onChanged();
      toast(`Grup "${n}" dibuat.`, "success");
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : "Gagal membuat grup.");
    } finally {
      setBusy(false);
    }
  }

  async function rename(g: Group) {
    const next = editName.trim();
    if (!next || next === g.name) {
      setEditingId(null);
      return;
    }
    setBusy(true);
    setErr("");
    try {
      await api.updateGroup(g.id, next, g.color);
      setEditingId(null);
      await onChanged();
      toast("Nama grup diperbarui.", "success");
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : "Gagal mengubah grup.");
    } finally {
      setBusy(false);
    }
  }

  async function recolor(g: Group, c: string) {
    setBusy(true);
    try {
      await api.updateGroup(g.id, g.name, c);
      await onChanged();
      toast("Warna grup diperbarui.", "success");
    } catch (e) {
      toast(e instanceof ApiError ? e.message : "Gagal mengubah warna.", "error");
    } finally {
      setBusy(false);
    }
  }

  async function remove(g: Group) {
    const n = countOf(g.id);
    const msg = n
      ? `Hapus grup "${g.name}"? ${n} server akan dipindah ke "Tanpa grup".`
      : `Hapus grup "${g.name}"?`;
    if (!window.confirm(msg)) return;
    setBusy(true);
    setErr("");
    try {
      await api.deleteGroup(g.id);
      await onChanged();
      toast("Grup dihapus.", "success");
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : "Gagal menghapus grup.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="modal" style={{ maxWidth: 520 }}>
        <h2>Kelola grup</h2>
        <p className="hint" style={{ color: "var(--muted)", fontSize: 12, marginTop: -8 }}>
          Grup dipakai untuk mengelompokkan server di sidebar. Menghapus grup tidak menghapus server.
        </p>

        <div className="scroll-y" style={{ maxHeight: 260, border: "1px solid var(--border)", borderRadius: 8, marginBottom: 14 }}>
          <table>
            <thead>
              <tr>
                <th>Grup</th>
                <th style={{ width: 90 }}>Server</th>
                <th style={{ width: 150 }} />
              </tr>
            </thead>
            <tbody>
              {groups.length === 0 && (
                <tr>
                  <td colSpan={3} className="empty">
                    Belum ada grup.
                  </td>
                </tr>
              )}
              {groups.map((g) => (
                <tr key={g.id}>
                  <td>
                    {editingId === g.id ? (
                      <input
                        autoFocus
                        value={editName}
                        onChange={(e) => setEditName(e.target.value)}
                        onKeyDown={(e) => {
                          if (e.key === "Enter") rename(g);
                          if (e.key === "Escape") setEditingId(null);
                        }}
                        style={{ maxWidth: 220 }}
                      />
                    ) : (
                      <span style={{ display: "inline-flex", alignItems: "center", gap: 7 }}>
                        <span style={{ color: g.color || "var(--primary)" }}>●</span>
                        {g.name}
                      </span>
                    )}
                  </td>
                  <td>
                    <span className="badge">{countOf(g.id)}</span>
                  </td>
                  <td>
                    <div className="row" style={{ gap: 4, justifyContent: "flex-end" }}>
                      {editingId === g.id ? (
                        <>
                          <button className="sm primary" onClick={() => rename(g)} disabled={busy}>
                            Simpan
                          </button>
                          <button className="sm" onClick={() => setEditingId(null)}>
                            Batal
                          </button>
                        </>
                      ) : (
                        <>
                          <label className="color-dot" title="Warna grup">
                            <input
                              type="color"
                              value={g.color || "#2563EB"}
                              onChange={(e) => recolor(g, e.target.value)}
                              style={{ width: 22, height: 22, padding: 0, border: "none", background: "transparent", cursor: "pointer" }}
                            />
                          </label>
                          <button
                            className="sm"
                            onClick={() => {
                              setEditingId(g.id);
                              setEditName(g.name);
                            }}
                          >
                            Ubah
                          </button>
                          <button className="sm danger" onClick={() => remove(g)} disabled={busy}>
                            Hapus
                          </button>
                        </>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        <div className="panel" style={{ padding: 12 }}>
          <label style={{ marginBottom: 8 }}>Grup baru</label>
          <div className="row" style={{ gap: 8 }}>
            <input
              className="grow"
              placeholder="mis. Produksi, Homelab"
              value={name}
              onChange={(e) => setName(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && create()}
            />
            <button className="primary" onClick={create} disabled={busy || !name.trim()}>
              Tambah
            </button>
          </div>
          <div className="row" style={{ gap: 6, marginTop: 10 }}>
            {PRESETS.map((c) => (
              <button
                key={c}
                title={c}
                onClick={() => setColor(c)}
                style={{
                  width: 24,
                  height: 24,
                  padding: 0,
                  borderRadius: 999,
                  background: c,
                  border: color === c ? "2px solid var(--text)" : "1px solid var(--border)",
                }}
              />
            ))}
            <span className="kv" style={{ marginLeft: 6 }}>
              <span>warna terpilih</span>
            </span>
          </div>
        </div>

        {err && <div className="err" style={{ marginTop: 10 }}>{err}</div>}

        <div className="modal-actions">
          <button onClick={onClose}>Tutup</button>
        </div>
      </div>
    </div>
  );
}
