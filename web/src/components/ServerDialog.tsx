import { useState } from "react";
import { api, ApiError, type Group, type Server, type ServerInput, type TestResult } from "../api";

type Props = {
  server?: Server | null;
  groups: Group[];
  servers: Server[];
  onClose: () => void;
  onSaved: (s: Server) => void;
  onDelete?: (s: Server) => void;
  toast: (msg: string, kind?: "info" | "error" | "success") => void;
};

export default function ServerDialog({ server, groups, servers, onClose, onSaved, onDelete, toast }: Props) {
  const editing = !!server;
  const [form, setForm] = useState<ServerInput>({
    name: server?.name ?? "",
    host: server?.host ?? "",
    port: server?.port ?? 22,
    username: server?.username ?? "",
    authType: server?.authType ?? "key",
    groupId: server?.groupId ?? null,
    jumpServerId: server?.jumpServerId ?? null,
    tags: server?.tags ?? "",
    color: server?.color ?? "#2563EB",
    notes: server?.notes ?? "",
    favorite: server?.favorite ?? false,
    keyPath: server?.keyPath ?? "",
    password: "",
    passphrase: "",
  });
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [test, setTest] = useState<TestResult | null>(null);

  const set = <K extends keyof ServerInput>(k: K, v: ServerInput[K]) => setForm((f) => ({ ...f, [k]: v }));

  async function save() {
    setErr("");
    setBusy(true);
    try {
      const payload: ServerInput = { ...form, port: Number(form.port) || 22 };
      if (editing) {
        const s = await api.updateServer(server!.id, payload);
        onSaved(s);
        toast("Perubahan server disimpan.", "success");
        onClose();
      } else {
        const s = await api.createServer(payload);
        onSaved(s);
        toast("Server ditambahkan.", "success");
        onClose();
      }
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : "Gagal menyimpan server.");
    } finally {
      setBusy(false);
    }
  }

  async function runTest() {
    setErr("");
    if (!form.host.trim() || !form.username.trim()) {
      setErr("Host dan username wajib diisi sebelum menguji koneksi.");
      return;
    }
    setTest(null);
    setBusy(true);
    try {
      const payload: ServerInput = { ...form, port: Number(form.port) || 22 };
      // Mode edit menguji server tersimpan; mode tambah menguji kredensial form
      // langsung (tanpa menyimpan apa pun ke database).
      const res = editing && server ? await api.testServer(server.id) : await api.testAdhoc(payload);
      setTest(res);
      if (res.ok) toast(`Koneksi berhasil (${res.latencyMs} ms).`, "success");
      else toast(res.error || "Koneksi gagal.", "error");
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : "Uji koneksi gagal.");
    } finally {
      setBusy(false);
    }
  }

  async function resetKey() {
    if (!server) return;
    await api.resetHostKey(server.id);
    setTest(null);
    toast("Pin host key dihapus. Uji koneksi lagi untuk menerima fingerprint baru.", "info");
  }

  return (
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="modal">
        <h2>{editing ? "Ubah server" : "Tambah server"}</h2>
        <div className="form-grid">
          <div>
            <label>Nama</label>
            <input value={form.name} onChange={(e) => set("name", e.target.value)} placeholder="web-01" autoFocus />
          </div>
          <div>
            <label>Host / IP</label>
            <input value={form.host} onChange={(e) => set("host", e.target.value)} placeholder="192.168.1.10" />
          </div>
          <div>
            <label>Port</label>
            <input type="number" value={form.port} onChange={(e) => set("port", Number(e.target.value))} />
          </div>
          <div>
            <label>Username</label>
            <input value={form.username} onChange={(e) => set("username", e.target.value)} placeholder="root" />
          </div>
          <div>
            <label>Metode auth</label>
            <select value={form.authType} onChange={(e) => set("authType", e.target.value)}>
              <option value="key">Private key</option>
              <option value="password">Password</option>
              <option value="agent">SSH agent</option>
            </select>
          </div>
          <div>
            <label>Grup</label>
            <select
              value={form.groupId ?? ""}
              onChange={(e) => set("groupId", e.target.value || null)}
            >
              <option value="">— tanpa grup —</option>
              {groups.map((g) => (
                <option key={g.id} value={g.id}>
                  {g.name}
                </option>
              ))}
            </select>
          </div>

          {form.authType === "key" && (
            <>
              <div className="full">
                <label>Path private key</label>
                <input value={form.keyPath} onChange={(e) => set("keyPath", e.target.value)} placeholder="~/.ssh/id_ed25519" />
              </div>
              <div className="full">
                <label>Passphrase key (opsional)</label>
                <input
                  type="password"
                  value={form.passphrase}
                  onChange={(e) => set("passphrase", e.target.value)}
                  placeholder={server?.hasPassphrase ? "tersimpan — kosongkan bila tidak diubah" : "kosongkan bila key tanpa passphrase"}
                />
              </div>
            </>
          )}
          {form.authType === "password" && (
            <div className="full">
              <label>Password SSH</label>
              <input
                type="password"
                value={form.password}
                onChange={(e) => set("password", e.target.value)}
                placeholder={server?.hasPassword ? "tersimpan — kosongkan bila tidak diubah" : "password"}
              />
            </div>
          )}

          <div>
            <label>Jump host (1 hop)</label>
            <select value={form.jumpServerId ?? ""} onChange={(e) => set("jumpServerId", e.target.value || null)}>
              <option value="">— langsung —</option>
              {servers
                .filter((s) => s.id !== server?.id)
                .map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.name} ({s.host})
                  </option>
                ))}
            </select>
          </div>
          <div>
            <label>Tag (pisah koma)</label>
            <input value={form.tags} onChange={(e) => set("tags", e.target.value)} placeholder="produksi,web" />
          </div>
          <div>
            <label>Warna</label>
            <input type="color" value={form.color || "#2563EB"} onChange={(e) => set("color", e.target.value)} style={{ padding: 2, height: 34 }} />
          </div>
          <div>
            <label>Catatan</label>
            <input value={form.notes} onChange={(e) => set("notes", e.target.value)} placeholder="opsional" />
          </div>
        </div>

        {err && <div className="err" style={{ marginTop: 12 }}>{err}</div>}
        {test && (
          <div className="panel" style={{ marginTop: 12, padding: 12 }}>
            <div className="row" style={{ gap: 8 }}>
              <span className={`badge ${test.ok ? "ok" : "err"}`}>{test.ok ? "koneksi berhasil" : "gagal"}</span>
              <span className="kv">
                <span>latensi <b>{test.latencyMs} ms</b></span>
              </span>
            </div>
            {test.fingerprint && <div className="fp" style={{ marginTop: 6 }}>{test.hostKeyAlgo} {test.fingerprint}</div>}
            {test.error && (
              <>
                <div className="err" style={{ marginTop: 6 }}>{test.error}</div>
                {/BERUBAH/i.test(test.error) && editing && server && (
                  <button className="sm danger" style={{ marginTop: 8 }} onClick={resetKey}>
                    Terima host key baru
                  </button>
                )}
              </>
            )}
          </div>
        )}

        <div className="modal-actions">
          {editing && onDelete && (
            <button className="danger" style={{ marginRight: "auto" }} onClick={() => onDelete(server!)}>
              Hapus server
            </button>
          )}
          <button onClick={onClose}>Batal</button>
          <button onClick={() => runTest()} disabled={busy}>
            Uji koneksi
          </button>
          <button className="primary" onClick={save} disabled={busy || !form.host || !form.username}>
            {busy ? "Menyimpan…" : "Simpan"}
          </button>
        </div>
      </div>
    </div>
  );
}
