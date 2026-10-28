import { useState } from "react";
import { api, ApiError, type ServerInput } from "../api";

export type QuickConnectInfo = { quickId: string; label: string; sublabel: string };

type Props = {
  onClose: () => void;
  onConnect: (info: QuickConnectInfo) => void;
  toast: (msg: string, kind?: "info" | "error" | "success") => void;
};

// Dialog koneksi sekali pakai: kredensial tidak disimpan ke inventaris,
// hanya ditukar dengan quickId berumur pendek di memory server.
export default function QuickConnectDialog({ onClose, onConnect, toast }: Props) {
  const [host, setHost] = useState("");
  const [port, setPort] = useState(22);
  const [username, setUsername] = useState("");
  const [authType, setAuthType] = useState<"password" | "key" | "agent">("password");
  const [password, setPassword] = useState("");
  const [keyPath, setKeyPath] = useState("");
  const [passphrase, setPassphrase] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  async function connect() {
    setErr("");
    if (!host.trim() || !username.trim()) {
      setErr("Host dan username wajib diisi.");
      return;
    }
    setBusy(true);
    try {
      const payload: ServerInput = {
        name: `${username}@${host}`,
        host: host.trim(),
        port: Number(port) || 22,
        username: username.trim(),
        authType,
        groupId: null,
        jumpServerId: null,
        tags: "",
        color: "#2563EB",
        notes: "",
        favorite: false,
        password: authType === "password" ? password : undefined,
        keyPath: authType === "key" ? keyPath : undefined,
        passphrase: authType === "key" ? passphrase : undefined,
      };
      const r = await api.quickConnect(payload);
      onConnect({
        quickId: r.quickId,
        label: `${username.trim()}@${host.trim()}`,
        sublabel: `${host.trim()}:${Number(port) || 22}`,
      });
      onClose();
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : "Gagal membuka koneksi cepat.";
      setErr(msg);
      toast(msg, "error");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="modal">
        <h2>⚡ Quick connect</h2>
        <p style={{ marginTop: -6, color: "var(--muted)", fontSize: 12 }}>
          Hubungkan sekali tanpa menyimpan server ke inventaris. Kredensial hanya disimpan di memory selama 3 menit.
        </p>
        <div className="form-grid">
          <div>
            <label>Host / IP</label>
            <input
              value={host}
              onChange={(e) => setHost(e.target.value)}
              placeholder="192.168.1.10"
              autoFocus
              onKeyDown={(e) => e.key === "Enter" && connect()}
            />
          </div>
          <div>
            <label>Port</label>
            <input type="number" value={port} onChange={(e) => setPort(Number(e.target.value))} />
          </div>
          <div>
            <label>Username</label>
            <input
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder="root"
              onKeyDown={(e) => e.key === "Enter" && connect()}
            />
          </div>
          <div>
            <label>Metode auth</label>
            <select value={authType} onChange={(e) => setAuthType(e.target.value as typeof authType)}>
              <option value="password">Password</option>
              <option value="key">Private key</option>
              <option value="agent">SSH agent</option>
            </select>
          </div>
          {authType === "password" && (
            <div className="full">
              <label>Password SSH</label>
              <input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="password"
                onKeyDown={(e) => e.key === "Enter" && connect()}
              />
            </div>
          )}
          {authType === "key" && (
            <>
              <div className="full">
                <label>Path private key</label>
                <input value={keyPath} onChange={(e) => setKeyPath(e.target.value)} placeholder="~/.ssh/id_ed25519" />
              </div>
              <div className="full">
                <label>Passphrase key (opsional)</label>
                <input
                  type="password"
                  value={passphrase}
                  onChange={(e) => setPassphrase(e.target.value)}
                  placeholder="kosongkan bila key tanpa passphrase"
                />
              </div>
            </>
          )}
        </div>

        {err && <div className="err" style={{ marginTop: 12 }}>{err}</div>}

        <div className="modal-actions">
          <button onClick={onClose}>Batal</button>
          <button className="primary" onClick={connect} disabled={busy}>
            {busy ? "Menghubungkan…" : "Hubungkan"}
          </button>
        </div>
      </div>
    </div>
  );
}
