import { useState } from "react";
import { api, ApiError } from "../api";

type Props = {
  hasVault: boolean;
  onDone: () => void;
  theme: "light" | "dark";
  onToggleTheme: () => void;
};

export default function UnlockScreen({ hasVault, onDone, theme, onToggleTheme }: Props) {
  const [pass, setPass] = useState("");
  const [pass2, setPass2] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const create = !hasVault;

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setErr("");
    if (create) {
      if (pass.length < 8) return setErr("Passphrase minimal 8 karakter.");
      if (pass !== pass2) return setErr("Konfirmasi passphrase tidak sama.");
    }
    setBusy(true);
    try {
      if (create) await api.createVault(pass);
      else await api.unlock(pass);
      onDone();
    } catch (e2) {
      setErr(e2 instanceof ApiError ? e2.message : "Gagal membuka vault.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="unlock-wrap">
      <div className="unlock-card">
        <div className="split" style={{ marginBottom: 8 }}>
          <h1>
            b<span>Server</span>
          </h1>
          <button className="ghost sm" onClick={onToggleTheme} title="Ganti tema">
            {theme === "dark" ? "☀ Terang" : "☾ Gelap"}
          </button>
        </div>
        <p>
          {create
            ? "Buat master password untuk mengenkripsi kredensial SSH di perangkat ini."
            : "Vault terkunci. Masukkan master password untuk membuka kredensial."}
        </p>
        <form onSubmit={submit}>
          <div>
            <label>{create ? "Master password baru" : "Master password"}</label>
            <input
              type="password"
              autoFocus
              value={pass}
              onChange={(e) => setPass(e.target.value)}
              placeholder="minimal 8 karakter"
            />
          </div>
          {create && (
            <div>
              <label>Ulangi master password</label>
              <input type="password" value={pass2} onChange={(e) => setPass2(e.target.value)} />
            </div>
          )}
          {err && <div className="err">{err}</div>}
          <button className="primary" disabled={busy || !pass}>
            {busy ? "Memproses…" : create ? "Buat vault" : "Buka vault"}
          </button>
          <div className="hint" style={{ fontSize: 11, color: "var(--muted)" }}>
            Data lokal: Argon2id + AES-256-GCM. Passphrase tidak dapat dipulihkan.
          </div>
        </form>
      </div>
    </div>
  );
}
