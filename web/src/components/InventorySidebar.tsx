import { useMemo, useState } from "react";
import type { Group, Server } from "../api";

type Props = {
  servers: Server[];
  groups: Group[];
  activeId?: string;
  onOpen: (s: Server) => void;
  onSelect: (s: Server) => void;
  onEdit: (s: Server) => void;
  onAdd: () => void;
  onImport: () => void;
  onToggleFavorite: (s: Server) => void;
  onManageGroups: () => void;
  className?: string;
};

export default function InventorySidebar({
  servers,
  groups,
  activeId,
  onOpen,
  onSelect,
  onEdit,
  onAdd,
  onImport,
  onToggleFavorite,
  onManageGroups,
  className,
}: Props) {
  const [q, setQ] = useState("");
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());

  const filtered = useMemo(() => {
    const s = q.trim().toLowerCase();
    if (!s) return servers;
    return servers.filter((x) =>
      [x.name, x.host, x.username, x.tags, x.groupName ?? "", x.notes].join(" ").toLowerCase().includes(s)
    );
  }, [servers, q]);

  const sections = useMemo(() => {
    const map = new Map<string, { name: string; color: string; items: Server[] }>();
    for (const s of filtered) {
      const key = s.groupId ?? "__none";
      if (!map.has(key)) {
        const g = groups.find((x) => x.id === s.groupId);
        map.set(key, { name: g?.name ?? "Tanpa grup", color: g?.color || "#64748B", items: [] });
      }
      map.get(key)!.items.push(s);
    }
    return [...map.entries()].sort((a, b) => (a[0] === "__none" ? 1 : b[0] === "__none" ? -1 : a[1].name.localeCompare(b[1].name)));
  }, [filtered, groups]);

  const toggleGroup = (id: string) => {
    const n = new Set(collapsed);
    n.has(id) ? n.delete(id) : n.add(id);
    setCollapsed(n);
  };

  return (
    <aside className={`sidebar ${className ?? ""}`}>
      <div className="sidebar-head">
        <div className="split">
          <span className="sidebar-title">Inventaris</span>
          <span className="badge">{servers.length}</span>
        </div>
        <input placeholder="Cari server, host, tag…" value={q} onChange={(e) => setQ(e.target.value)} />
        <div className="row" style={{ gap: 6 }}>
          <button className="primary sm grow" onClick={onAdd}>
            + Tambah server
          </button>
          <button className="sm" onClick={onImport} title="Impor ~/.ssh/config">
            Impor
          </button>
        </div>
        <button className="ghost sm" onClick={onManageGroups} style={{ textAlign: "left" }}>
          ⚙ Kelola grup
        </button>
      </div>
      <div className="sidebar-list">
        {sections.length === 0 && (
          <div className="empty">
            <h3>{servers.length ? "Tidak ada hasil" : "Belum ada server"}</h3>
            <p>{servers.length ? "Coba kata kunci lain." : "Tambahkan server pertama untuk mulai."}</p>
          </div>
        )}
        {sections.map(([gid, sec]) => (
          <div key={gid}>
            <div className="group-head" onClick={() => toggleGroup(gid)}>
              <span>{collapsed.has(gid) ? "▸" : "▾"}</span>
              <span style={{ color: sec.color }}>●</span>
              <span>{sec.name}</span>
              <span className="group-count">{sec.items.length}</span>
            </div>
            {!collapsed.has(gid) &&
              sec.items.map((s) => {
                const ok = !!s.lastOkAt;
                return (
                  <div
                    key={s.id}
                    className={`server-row ${activeId === s.id ? "active" : ""}`}
                    onClick={() => {
                      onSelect(s);
                      onOpen(s);
                    }}
                    title={`${s.username}@${s.host}:${s.port}`}
                  >
                    <span className={`dot ${ok ? "ok" : ""}`} />
                    <div style={{ minWidth: 0, flex: 1 }}>
                      <div className="name">{s.name}</div>
                      <div className="meta">
                        {s.host}:{s.port}
                        {s.lastLatencyMs != null && ok ? ` · ${s.lastLatencyMs} ms` : ""}
                      </div>
                    </div>
                    {s.favorite && <span className="pin">★</span>}
                    <button
                      className="ghost sm"
                      title="Favorit"
                      onClick={(e) => {
                        e.stopPropagation();
                        onToggleFavorite(s);
                      }}
                    >
                      {s.favorite ? "★" : "☆"}
                    </button>
                    <button
                      className="ghost sm"
                      title="Ubah server"
                      onClick={(e) => {
                        e.stopPropagation();
                        onEdit(s);
                      }}
                    >
                      ✎
                    </button>
                  </div>
                );
              })}
          </div>
        ))}
      </div>
    </aside>
  );
}
