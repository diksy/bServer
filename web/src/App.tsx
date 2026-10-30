import { useCallback, useEffect, useRef, useState } from "react";
import {
  api,
  type Group,
  type Server,
  type ServerInput,
  type VaultStatus,
} from "./api";
import UnlockScreen from "./components/UnlockScreen";
import InventorySidebar from "./components/InventorySidebar";
import ServerDialog from "./components/ServerDialog";
import TerminalPane from "./components/TerminalPane";
import BatchPanel from "./components/BatchPanel";
import SftpPane from "./components/SftpPane";
import Dashboard from "./components/Dashboard";
import Settings from "./components/Settings";
import GroupManager from "./components/GroupManager";
import QuickConnectDialog, { type QuickConnectInfo } from "./components/QuickConnectDialog";
import SnippetPanel from "./components/SnippetPanel";
import BroadcastPanel from "./components/BroadcastPanel";
import AlertsPanel from "./components/AlertsPanel";
import ServicesPanel from "./components/ServicesPanel";

type View = "welcome" | "term" | "batch" | "sftp" | "dash" | "settings" | "snippet" | "broadcast" | "alerts" | "services";
type Tab = {
  id: string;
  kind: View;
  title: string;
  serverId?: string;
  selected?: string[];
  quickId?: string;
  quickLabel?: string;
  quickSublabel?: string;
};
type Toast = { id: number; msg: string; kind: "info" | "error" | "success" };

let tabSeq = 0;
const newId = (p: string) => `${p}-${++tabSeq}`;

export default function App() {
  const [status, setStatus] = useState<VaultStatus | null>(null);
  const [theme, setTheme] = useState<"light" | "dark">("light");
  const [servers, setServers] = useState<Server[]>([]);
  const [groups, setGroups] = useState<Group[]>([]);
  const [tabs, setTabs] = useState<Tab[]>([{ id: "welcome", kind: "welcome", title: "Ringkasan" }]);
  const [active, setActive] = useState("welcome");
  const [selected, setSelected] = useState<Server | null>(null);
  const [dialog, setDialog] = useState<{ open: boolean; server: Server | null }>({ open: false, server: null });
  const [toasts, setToasts] = useState<Toast[]>([]);
  const [sidebarOpen, setSidebarOpen] = useState(true);
  const [groupOpen, setGroupOpen] = useState(false);
  const [quickOpen, setQuickOpen] = useState(false);
  const [unacked, setUnacked] = useState(0);
  const loaded = useRef(false);
  // Fungsi kirim ke tiap terminal (diisi TerminalPane per tab; snippet dikirim
  // ke terminal tab aktif).
  const termSends = useRef<Record<string, ((d: string) => void) | undefined>>({});

  const toast = useCallback((msg: string, kind: Toast["kind"] = "info") => {
    const id = Date.now() + Math.random();
    setToasts((t) => [...t, { id, msg, kind }]);
    setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), 4500);
  }, []);

  const applyTheme = (t: "light" | "dark") => {
    setTheme(t);
    document.documentElement.dataset.theme = t;
    api.putSettings({ theme: t }).catch(() => undefined);
  };

  const refreshServers = useCallback(async () => {
    try {
      const [s, g] = await Promise.all([api.servers(), api.groups()]);
      setServers(s);
      setGroups(g);
    } catch (e) {
      if (e instanceof Error) toast(e.message, "error");
    }
  }, [toast]);

  const bootstrap = useCallback(async () => {
    try {
      const st = await api.status();
      setStatus(st);
      const t = (st.theme === "dark" ? "dark" : "light") as "light" | "dark";
      setTheme(t);
      document.documentElement.dataset.theme = t;
      if (st.unlocked) await refreshServers();
    } catch (e) {
      toast(e instanceof Error ? e.message : "Tidak bisa menghubungi server lokal.", "error");
    }
  }, [refreshServers, toast]);

  useEffect(() => {
    if (loaded.current) return;
    loaded.current = true;
    bootstrap();
  }, [bootstrap]);

  // auto-lock: cek status berkala
  useEffect(() => {
    if (!status?.unlocked) return;
    const t = setInterval(async () => {
      try {
        const st = await api.status();
        if (!st.unlocked) {
          setStatus(st);
          setTabs([{ id: "welcome", kind: "welcome", title: "Ringkasan" }]);
          setActive("welcome");
          toast("Vault terkunci otomatis karena tidak ada aktivitas.", "info");
        }
      } catch {
        /* ignore */
      }
    }, 20000);
    return () => clearInterval(t);
  }, [status?.unlocked, toast]);

  // polling jumlah alert belum dibaca (badge statusbar)
  const refreshUnacked = useCallback(async () => {
    try {
      const r = await api.alertsUnacked();
      setUnacked(r.count);
    } catch {
      /* ignore */
    }
  }, []);
  useEffect(() => {
    if (!status?.unlocked) return;
    refreshUnacked();
    const t = setInterval(refreshUnacked, 30000);
    return () => clearInterval(t);
  }, [status?.unlocked, refreshUnacked]);

  function openView(
    kind: "batch" | "sftp" | "dash" | "settings" | "snippet" | "broadcast" | "alerts" | "services" | "welcome",
    opts: { selected?: string[]; serverId?: string } = {}
  ) {
    if (kind === "welcome") {
      setActive("welcome");
      return;
    }
    setTabs((prev) => {
      const existing = prev.find((t) => t.kind === kind);
      if (existing) {
        setActive(existing.id);
        return prev;
      }
      const id = newId(kind);
      const titles: Record<string, string> = {
        batch: "Batch",
        sftp: "SFTP",
        dash: "Dashboard",
        settings: "Pengaturan",
        snippet: "Snippet",
        broadcast: "Broadcast",
        alerts: "Alert",
        services: "Layanan",
      };
      setActive(id);
      return [...prev, { id, kind, title: titles[kind] ?? kind, selected: opts.selected, serverId: opts.serverId }];
    });
  }

  function openTerminal(s: Server) {
    const id = newId("term");
    setTabs((prev) => [...prev, { id, kind: "term", title: s.name, serverId: s.id }]);
    setActive(id);
    setSelected(s);
    // di layar sempit sidebar menutup setelah memilih server
    if (window.innerWidth <= 900) setSidebarOpen(false);
  }

  function openQuickTerminal(info: QuickConnectInfo) {
    const id = newId("term");
    setTabs((prev) => [
      ...prev,
      { id, kind: "term", title: `⚡ ${info.label}`, quickId: info.quickId, quickLabel: info.label, quickSublabel: info.sublabel },
    ]);
    setActive(id);
    if (window.innerWidth <= 900) setSidebarOpen(false);
  }

  function closeTab(id: string) {
    setTabs((prev) => {
      const idx = prev.findIndex((t) => t.id === id);
      const next = prev.filter((t) => t.id !== id);
      if (!next.length) {
        setActive("welcome");
        return [{ id: "welcome", kind: "welcome", title: "Ringkasan" }];
      }
      if (active === id) setActive(next[Math.max(0, idx - 1)].id);
      return next;
    });
  }

  async function onSaved(s: Server) {
    setSelected(s);
    await refreshServers();
  }

  async function deleteServer(s: Server) {
    if (!window.confirm(`Hapus server "${s.name}"?`)) return;
    try {
      await api.deleteServer(s.id);
      setDialog({ open: false, server: null });
      setTabs((prev) => {
        const next = prev.filter((t) => t.serverId !== s.id);
        if (!next.length) {
          setActive("welcome");
          return [{ id: "welcome", kind: "welcome", title: "Ringkasan" }];
        }
        if (!next.some((t) => t.id === active)) setActive(next[next.length - 1].id);
        return next;
      });
      setSelected((cur) => (cur?.id === s.id ? null : cur));
      await refreshServers();
      toast("Server dihapus", "success");
    } catch (e) {
      toast(e instanceof Error ? e.message : "Gagal menghapus server.", "error");
    }
  }

  async function toggleFavorite(s: Server) {
    try {
      const payload: ServerInput = {
        name: s.name,
        host: s.host,
        port: s.port,
        username: s.username,
        authType: s.authType,
        groupId: s.groupId,
        jumpServerId: s.jumpServerId,
        tags: s.tags,
        color: s.color,
        notes: s.notes,
        favorite: !s.favorite,
        keyPath: s.keyPath,
      };
      await api.updateServer(s.id, payload);
      refreshServers();
    } catch (e) {
      toast(e instanceof Error ? e.message : "Gagal memperbarui favorit.", "error");
    }
  }

  async function lock() {
    await api.lock();
    setTabs([{ id: "welcome", kind: "welcome", title: "Ringkasan" }]);
    setActive("welcome");
    setServers([]);
    setStatus((s) => (s ? { ...s, unlocked: false } : s));
  }

  // Kirim isi snippet ke terminal tab aktif (semua tab terminal tetap
  // ter-mount; hanya satu yang terlihat).
  function runSnippet(body: string) {
    const send = termSends.current[active];
    if (send) {
      send(body.endsWith("\n") ? body : body + "\n");
      toast("Snippet dikirim ke terminal", "success");
      return;
    }
    toast("Buka terminal dulu untuk menjalankan snippet.", "info");
  }

  if (!status) {
    return (
      <div className="unlock-wrap">
        <div className="unlock-card">
          <h1>
            b<span>Server</span>
          </h1>
          <p>Memuat…</p>
        </div>
      </div>
    );
  }

  if (!status.unlocked) {
    return (
      <>
        <UnlockScreen
          hasVault={status.vaultExists}
          theme={theme}
          onToggleTheme={() => applyTheme(theme === "dark" ? "light" : "dark")}
          onDone={async () => {
            await bootstrap();
            await refreshServers();
            toast("Vault terbuka.", "success");
          }}
        />
        <Toasts toasts={toasts} />
      </>
    );
  }

  const current = tabs.find((t) => t.id === active) ?? tabs[0];
  const serverOf = (t: Tab) => (t.serverId ? servers.find((s) => s.id === t.serverId) : undefined);

  return (
    <div className="app">
      <div className="shell">
        <nav className="rail">
          <div className="logo">bS</div>
          <button className={current.kind === "welcome" ? "active" : ""} onClick={() => openView("welcome")} title="Server">
            <span style={{ fontSize: 16 }}>▤</span>
            Server
          </button>
          <button onClick={() => setQuickOpen(true)} title="Quick connect (tanpa simpan)">
            <span style={{ fontSize: 16 }}>⚡</span>
            Cepat
          </button>
          <button className={current.kind === "batch" ? "active" : ""} onClick={() => openView("batch")} title="Batch">
            <span style={{ fontSize: 16 }}>≡</span>
            Batch
          </button>
          <button className={current.kind === "sftp" ? "active" : ""} onClick={() => openView("sftp")} title="SFTP">
            <span style={{ fontSize: 16 }}>⇅</span>
            SFTP
          </button>
          <button className={current.kind === "dash" ? "active" : ""} onClick={() => openView("dash")} title="Dashboard">
            <span style={{ fontSize: 16 }}>◔</span>
            Metrik
          </button>
          <button className={current.kind === "snippet" ? "active" : ""} onClick={() => openView("snippet")} title="Snippet">
            <span style={{ fontSize: 16 }}>✂</span>
            Snippet
          </button>
          <button className={current.kind === "broadcast" ? "active" : ""} onClick={() => openView("broadcast")} title="Broadcast terminal">
            <span style={{ fontSize: 16 }}>📣</span>
            Broadcast
          </button>
          <button className={current.kind === "alerts" ? "active" : ""} onClick={() => openView("alerts")} title="Alert">
            <span style={{ fontSize: 16 }}>🔔</span>
            Alert
          </button>
          <button className={current.kind === "services" ? "active" : ""} onClick={() => openView("services")} title="Status layanan systemd & Docker">
            <span style={{ fontSize: 16 }}>🛠</span>
            Layanan
          </button>
          <div className="spacer" />
          <button className={current.kind === "settings" ? "active" : ""} onClick={() => openView("settings")} title="Pengaturan">
            <span style={{ fontSize: 16 }}>⚙</span>
            Atur
          </button>
        </nav>

        {sidebarOpen && (
          <>
            <div className="backdrop" onClick={() => setSidebarOpen(false)} />
            <InventorySidebar
              className={sidebarOpen ? "open" : ""}
              servers={servers}
              groups={groups}
              activeId={selected?.id}
              onOpen={openTerminal}
              onSelect={setSelected}
              onEdit={(s) => setDialog({ open: true, server: s })}
              onAdd={() => setDialog({ open: true, server: null })}
              onImport={() => openView("settings")}
              onToggleFavorite={toggleFavorite}
              onManageGroups={() => setGroupOpen(true)}
            />
          </>
        )}

        <main className="workspace">
          <div className="tabs">
            {tabs.map((t) => (
              <button key={t.id} className={`tab ${active === t.id ? "active" : ""}`} onClick={() => setActive(t.id)}>
                {t.title}
                {t.id !== "welcome" && (
                  <span
                    className="close"
                    onClick={(e) => {
                      e.stopPropagation();
                      closeTab(t.id);
                    }}
                  >
                    ×
                  </span>
                )}
              </button>
            ))}
            <div className="tab-actions">
              {selected && (
                <button className="sm" onClick={() => openTerminal(selected)} title="Terminal baru">
                  + Terminal
                </button>
              )}
              <button className="sm" onClick={() => setQuickOpen(true)} title="Quick connect">
                ⚡ Quick
              </button>
              <button className="ghost sm" onClick={() => setSidebarOpen((v) => !v)} title="Sembunyikan sidebar">
                ☰
              </button>
            </div>
          </div>

          <div className="tab-body" style={{ position: "relative", ...(current.kind === "term" ? { overflow: "hidden" } : {}) }}>
            {current.kind === "welcome" && (
              <Welcome
                servers={servers}
                selected={selected}
                onOpen={openTerminal}
                onAdd={() => setDialog({ open: true, server: null })}
                onBatch={() => openView("batch")}
                onDash={() => openView("dash")}
                onTest={async (s) => {
                  const r = await api.testServer(s.id);
                  toast(r.ok ? `${s.name}: terhubung (${r.latencyMs} ms)` : `${s.name}: ${r.error}`, r.ok ? "success" : "error");
                  refreshServers();
                }}
                onEdit={(s) => setDialog({ open: true, server: s })}
                status={status}
              />
            )}
            {current.kind === "batch" && <BatchPanel servers={servers} initialSelected={current.selected ?? []} toast={toast} />}
            {current.kind === "sftp" && <SftpPane servers={servers} initialServerId={current.serverId} toast={toast} />}
            {current.kind === "dash" && <Dashboard servers={servers} toast={toast} />}
            {current.kind === "snippet" && <SnippetPanel toast={toast} onRun={runSnippet} />}
            {current.kind === "broadcast" && <BroadcastPanel servers={servers} toast={toast} />}
            {current.kind === "alerts" && <AlertsPanel toast={toast} onAck={refreshUnacked} />}
            {current.kind === "services" && <ServicesPanel servers={servers} toast={toast} />}
            {current.kind === "settings" && (
              <Settings
                status={status}
                theme={theme}
                onTheme={applyTheme}
                onLock={lock}
                reload={refreshServers}
                toast={toast}
              />
            )}
            {tabs
              .filter((t) => t.kind === "term")
              .map((t) => {
                const sv = serverOf(t);
                return (
                  <div
                    key={t.id}
                    style={{
                      position: "absolute",
                      inset: 0,
                      display: "flex",
                      flexDirection: "column",
                      visibility: active === t.id ? "visible" : "hidden",
                    }}
                  >
                    {t.quickId ? (
                      <TerminalPane
                        label={t.quickLabel ?? t.title}
                        sublabel={t.quickSublabel ?? ""}
                        wsPath="/ws/quickterm"
                        wsParams={{ quickId: t.quickId }}
                        retryOnClose={false}
                        active={active === t.id}
                        onRegisterSend={(fn) => (termSends.current[t.id] = fn ?? undefined)}
                      />
                    ) : sv ? (
                      <TerminalPane
                        label={sv.name}
                        sublabel={`${sv.username}@${sv.host}:${sv.port}`}
                        wsPath="/ws/term"
                        wsParams={{ serverId: sv.id }}
                        active={active === t.id}
                        onRegisterSend={(fn) => (termSends.current[t.id] = fn ?? undefined)}
                      />
                    ) : (
                      <div className="empty">Server tidak ditemukan.</div>
                    )}
                  </div>
                );
              })}
          </div>

          <div className="statusbar">
            <span>🔒 vault terbuka</span>
            <span>{servers.length} server</span>
            <span>{tabs.length} tab</span>
            {unacked > 0 && (
              <button className="ghost sm" onClick={() => openView("alerts")} title="Alert belum dibaca">
                🔔 {unacked}
              </button>
            )}
            <span className="sep" />
            <button className="ghost sm" onClick={() => applyTheme(theme === "dark" ? "light" : "dark")}>
              {theme === "dark" ? "☀ Terang" : "☾ Gelap"}
            </button>
            <button className="ghost sm" onClick={lock}>
              Kunci
            </button>
            <span className="mono" style={{ fontSize: 11 }}>
              {status.version}
            </span>
          </div>
        </main>
      </div>

      {dialog.open && (
        <ServerDialog
          server={dialog.server}
          groups={groups}
          servers={servers}
          onClose={() => setDialog({ open: false, server: null })}
          onSaved={onSaved}
          onDelete={deleteServer}
          toast={toast}
        />
      )}
      {quickOpen && (
        <QuickConnectDialog onClose={() => setQuickOpen(false)} onConnect={openQuickTerminal} toast={toast} />
      )}
      {groupOpen && (
        <GroupManager
          groups={groups}
          servers={servers}
          onClose={() => setGroupOpen(false)}
          onChanged={refreshServers}
          toast={toast}
        />
      )}
      <Toasts toasts={toasts} />
    </div>
  );
}

function Toasts({ toasts }: { toasts: Toast[] }) {
  if (!toasts.length) return null;
  return (
    <div className="toasts">
      {toasts.map((t) => (
        <div key={t.id} className={`toast ${t.kind}`}>
          {t.msg}
        </div>
      ))}
    </div>
  );
}

function Welcome({
  servers,
  selected,
  onOpen,
  onAdd,
  onBatch,
  onDash,
  onTest,
  onEdit,
  status,
}: {
  servers: Server[];
  selected: Server | null;
  onOpen: (s: Server) => void;
  onAdd: () => void;
  onBatch: () => void;
  onDash: () => void;
  onTest: (s: Server) => void;
  onEdit: (s: Server) => void;
  status: VaultStatus;
}) {
  return (
    <div>
      <div className="panel">
        <div className="split">
          <div>
            <h2>Selamat datang di bServer</h2>
            <p className="hint">Kelola server SSH dari satu tempat: terminal, batch exec, SFTP, dan metrik — semua lokal.</p>
          </div>
          <div className="row">
            <button onClick={onAdd} className="primary">
              + Tambah server
            </button>
            <button onClick={onBatch}>Batch perintah</button>
            <button onClick={onDash}>Lihat metrik</button>
          </div>
        </div>
        <div className="grid2" style={{ marginTop: 8 }}>
          <Stat label="Server terdaftar" value={String(servers.length)} />
          <Stat label="Vault" value="Argon2id + AES-256-GCM" />
          <Stat label="Data lokal" value={status.dataDir} mono />
          <Stat label="SSH agent" value={status.sshAgentSock ? "terdeteksi" : "tidak aktif"} />
        </div>
      </div>

      {selected ? (
        <div className="panel">
          <div className="split">
            <div>
              <h2>{selected.name}</h2>
              <p className="hint">
                {selected.username}@{selected.host}:{selected.port} · auth {selected.authType}
                {selected.jumpName ? ` · jump ${selected.jumpName}` : ""}
              </p>
            </div>
            <div className="row">
              <button className="primary" onClick={() => onOpen(selected)}>
                Buka terminal
              </button>
              <button onClick={() => onTest(selected)}>Uji koneksi</button>
              <button onClick={() => onEdit(selected)}>Ubah</button>
            </div>
          </div>
          <div className="grid2">
            <Stat label="Latensi terakhir" value={selected.lastLatencyMs != null ? `${selected.lastLatencyMs} ms` : "belum diuji"} />
            <Stat label="Terakhir OK" value={selected.lastOkAt ? new Date(selected.lastOkAt).toLocaleString("id-ID") : "—"} />
            <Stat label="Tag" value={selected.tags || "—"} />
            <Stat label="Fingerprint host key" value={selected.hostKeyFp || "belum terpin"} mono />
          </div>
          {selected.notes && <p className="hint" style={{ marginTop: 10 }}>Catatan: {selected.notes}</p>}
        </div>
      ) : (
        <div className="panel empty">
          <h3>Pilih server di sidebar</h3>
          <p>Klik server untuk membuka terminal, atau tambah server baru untuk memulai.</p>
        </div>
      )}
    </div>
  );
}

function Stat({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div style={{ border: "1px solid var(--border)", borderRadius: 8, padding: "10px 12px" }}>
      <div style={{ fontSize: 11, color: "var(--muted)", marginBottom: 3 }}>{label}</div>
      <div className={mono ? "mono" : ""} style={{ fontSize: 13, wordBreak: "break-word" }}>
        {value}
      </div>
    </div>
  );
}
