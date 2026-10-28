// Klien API + tipe data bersama untuk SPA bServer.

export const token: string = (() => {
  const m = location.hash.match(/[#&]t=([a-f0-9]+)/);
  if (m) {
    sessionStorage.setItem("bserver_token", m[1]);
    history.replaceState(null, "", location.pathname + location.search);
    return m[1];
  }
  return sessionStorage.getItem("bserver_token") ?? "";
})();

export function wsURL(path: string, params: Record<string, string | number> = {}) {
  const proto = location.protocol === "https:" ? "wss" : "ws";
  const q = new URLSearchParams({ ...Object.fromEntries(Object.entries(params).map(([k, v]) => [k, String(v)])), t: token });
  return `${proto}://${location.host}${path}?${q.toString()}`;
}

export class ApiError extends Error {
  code: number;
  constructor(message: string, code: number) {
    super(message);
    this.code = code;
  }
}

async function req<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: {
      "X-Token": token,
      ...(body instanceof FormData ? {} : { "Content-Type": "application/json" }),
    },
    body: body === undefined ? undefined : body instanceof FormData ? body : JSON.stringify(body),
  });
  if (!res.ok) {
    let msg = res.statusText;
    try {
      const j = await res.json();
      msg = j.error || msg;
    } catch {
      /* ignore */
    }
    throw new ApiError(msg, res.status);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export type VaultStatus = {
  vaultExists: boolean;
  unlocked: boolean;
  autoLockMin: number;
  theme: "light" | "dark";
  version: string;
  dataDir: string;
  sshAgentSock: boolean;
};

export type Server = {
  id: string;
  name: string;
  host: string;
  port: number;
  username: string;
  authType: "password" | "key" | "agent";
  groupId: string | null;
  groupName: string | null;
  jumpServerId: string | null;
  jumpName: string | null;
  tags: string;
  color: string;
  notes: string;
  favorite: boolean;
  hostKeyFp: string | null;
  lastOkAt: string | null;
  lastLatencyMs: number | null;
  keyPath: string;
  hasPassword: boolean;
  hasPassphrase: boolean;
};

export type Group = { id: string; name: string; color: string };

export type ServerInput = {
  name: string;
  host: string;
  port: number;
  username: string;
  authType: string;
  groupId: string | null;
  jumpServerId: string | null;
  tags: string;
  color: string;
  notes: string;
  favorite: boolean;
  password?: string;
  keyPath?: string;
  passphrase?: string;
};

export type TestResult = {
  ok: boolean;
  latencyMs: number;
  error?: string;
  fingerprint?: string | null;
  hostKeyAlgo?: string | null;
};

export type BatchResult = {
  serverId: string;
  name: string;
  status: "ok" | "error" | "timeout";
  exitCode: number;
  durationMs: number;
  output: string;
  error?: string;
  truncated: boolean;
};

export type BatchSummary = { total: number; success: number; failed: number; timeout: number; elapsedMs: number };

export type Metric = {
  serverId: string;
  name: string;
  ok: boolean;
  error?: string;
  hostname?: string;
  kernel?: string;
  cpuPct: number;
  memUsedKb: number;
  memTotalKb: number;
  diskUsedKb: number;
  diskTotalKb: number;
  uptimeSec: number;
  load1: number;
  latencyMs: number;
};

export type MetricPoint = {
  ts: string;
  cpuPct: number;
  memUsedKb: number;
  memTotalKb: number;
  diskUsedKb: number;
  diskTotalKb: number;
  load1: number;
};

export type Alert = {
  id: number;
  serverId: string;
  serverName: string;
  ts: string;
  kind: "cpu" | "mem" | "disk" | string;
  value: number;
  threshold: number;
  message: string;
  acknowledged: boolean;
};

export type AlertThresholds = { cpuPct: number; memPct: number; diskPct: number };

export type SftpEntry = {
  name: string;
  size: number;
  mode: string;
  modTime: string;
  isDir: boolean;
};

export type LogEntry = {
  id: number;
  ts: string;
  serverId: string;
  serverName: string;
  source: string;
  command: string;
  exitCode: number;
  durationMs: number;
  excerpt: string;
};

export type Snippet = {
  id: string;
  name: string;
  body: string;
  tags: string;
  createdAt: string;
};

export type Service = {
  name: string;
  active: string;
  sub: string;
  description: string;
};

export type Container = {
  name: string;
  image: string;
  status: string;
};

export type ServicesResult = {
  systemd: Service[];
  docker: Container[];
  systemdError: string;
  dockerError: string;
};

export const api = {
  status: () => req<VaultStatus>("GET", "/api/status"),
  createVault: (passphrase: string) => req<{ ok: boolean }>("POST", "/api/vault/create", { passphrase }),
  unlock: (passphrase: string) => req<{ ok: boolean }>("POST", "/api/vault/unlock", { passphrase }),
  lock: () => req<{ ok: boolean }>("POST", "/api/vault/lock"),

  servers: (q = "") => req<Server[]>("GET", `/api/servers?q=${encodeURIComponent(q)}`),
  createServer: (inp: ServerInput) => req<Server>("POST", "/api/servers", inp),
  updateServer: (id: string, inp: ServerInput) => req<Server>("PATCH", `/api/servers/${id}`, inp),
  deleteServer: (id: string) => req<{ ok: boolean }>("DELETE", `/api/servers/${id}`),
  testServer: (id: string) => req<TestResult>("POST", `/api/servers/${id}/test`),
  testAdhoc: (input: ServerInput) => req<TestResult>("POST", "/api/test", input),
  quickConnect: (input: ServerInput) => req<{ quickId: string }>("POST", "/api/quick", input),
  resetHostKey: (id: string) => req<{ ok: boolean }>("POST", `/api/servers/${id}/knownhost/reset`),

  groups: () => req<Group[]>("GET", "/api/groups"),
  createGroup: (name: string, color: string) => req<Group>("POST", "/api/groups", { name, color }),
  updateGroup: (id: string, name: string, color: string) =>
    req<{ ok: boolean }>("PATCH", `/api/groups/${id}`, { name, color }),
  deleteGroup: (id: string) => req<{ ok: boolean }>("DELETE", `/api/groups/${id}`),

  importSSHConfig: (path = "") => req<{ imported: number; skipped: string[]; path: string; found: number }>("POST", "/api/import/sshconfig", { path }),
  importJSON: (payload: unknown) => req<{ imported: number; skipped: string[] }>("POST", "/api/import", payload),
  exportInventory: async () => {
    const res = await fetch("/api/export", { headers: { "X-Token": token } });
    if (!res.ok) throw new Error("export gagal: " + res.statusText);
    const blob = await res.blob();
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = "bserver-inventaris.json";
    a.click();
    setTimeout(() => URL.revokeObjectURL(a.href), 5000);
  },

  exec: (serverIds: string[], command: string, concurrency: number, timeoutSec: number) =>
    req<{ results: BatchResult[]; summary: BatchSummary }>("POST", "/api/exec", { serverIds, command, concurrency, timeoutSec }),

  logs: (limit = 100) => req<LogEntry[]>("GET", `/api/logs?limit=${limit}`),

  snippets: () => req<Snippet[]>("GET", "/api/snippets"),
  createSnippet: (name: string, body: string, tags: string) => req<Snippet>("POST", "/api/snippets", { name, body, tags }),
  updateSnippet: (id: string, name: string, body: string, tags: string) =>
    req<{ ok: boolean }>("PATCH", `/api/snippets/${id}`, { name, body, tags }),
  deleteSnippet: (id: string) => req<{ ok: boolean }>("DELETE", `/api/snippets/${id}`),

  sftpList: (serverId: string, path: string) =>
    req<{ path: string; entries: SftpEntry[] }>("GET", `/api/sftp/list?serverId=${serverId}&path=${encodeURIComponent(path)}`),
  sftpMkdir: (serverId: string, path: string) => req<{ ok: boolean }>("POST", "/api/sftp/mkdir", { serverId, path }),
  sftpRead: (serverId: string, path: string) =>
    req<{ path: string; content: string; size: number }>(
      "GET",
      `/api/sftp/read?serverId=${serverId}&path=${encodeURIComponent(path)}`
    ),
  sftpWrite: (serverId: string, path: string, content: string) =>
    req<{ ok: boolean; bytes: number }>("POST", "/api/sftp/write", { serverId, path, content }),
  sftpUpload: (serverId: string, path: string, file: File, onProgress: (pct: number) => void) =>
    new Promise<{ ok: boolean; bytes: number; remotePath: string }>((resolve, reject) => {
      const xhr = new XMLHttpRequest();
      xhr.open("POST", "/api/sftp/upload");
      xhr.setRequestHeader("X-Token", token);
      xhr.upload.onprogress = (e) => e.lengthComputable && onProgress(Math.round((e.loaded / e.total) * 100));
      xhr.onload = () => {
        try {
          const j = JSON.parse(xhr.responseText);
          if (xhr.status >= 200 && xhr.status < 300) resolve(j);
          else reject(new Error(j.error || "upload gagal"));
        } catch {
          reject(new Error("respons tidak valid"));
        }
      };
      xhr.onerror = () => reject(new Error("koneksi gagal"));
      const fd = new FormData();
      fd.set("serverId", serverId);
      fd.set("path", path);
      fd.set("file", file);
      xhr.send(fd);
    }),
  sftpDownload: async (serverId: string, path: string, onProgress: (pct: number) => void) => {
    const res = await fetch(`/api/sftp/download?serverId=${serverId}&path=${encodeURIComponent(path)}`, {
      headers: { "X-Token": token },
    });
    if (!res.ok) throw new Error(await res.text());
    const total = Number(res.headers.get("Content-Length") || 0);
    const reader = res.body!.getReader();
    const chunks: Uint8Array[] = [];
    let received = 0;
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      chunks.push(value);
      received += value.length;
      if (total) onProgress(Math.round((received / total) * 100));
    }
    const blob = new Blob(chunks as BlobPart[]);
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = path.split("/").pop() || "download";
    a.click();
    setTimeout(() => URL.revokeObjectURL(a.href), 5000);
    onProgress(100);
    return received;
  },

  metrics: (ids: string[]) => req<Metric[]>("GET", `/api/metrics?ids=${ids.join(",")}`),
  metricHistory: (serverId: string, limit = 120) =>
    req<MetricPoint[]>("GET", `/api/metrics/history?serverId=${encodeURIComponent(serverId)}&limit=${limit}`),

  getSettings: () => req<{ theme: string; autoLockMin: number; alertThresholds: AlertThresholds }>("GET", "/api/settings"),
  putSettings: (s: { theme?: string; autoLockMin?: number; alertThresholds?: AlertThresholds }) =>
    req<{ ok: boolean }>("PUT", "/api/settings", s),

  alerts: (limit = 50) => req<Alert[]>("GET", `/api/alerts?limit=${limit}`),
  alertsUnacked: () => req<{ count: number }>("GET", "/api/alerts/unacked"),
  ackAlerts: () => req<{ ok: boolean }>("POST", "/api/alerts/ack"),

  services: (serverId: string) =>
    req<ServicesResult>("GET", `/api/services?serverId=${encodeURIComponent(serverId)}`),
};

export function fmtBytes(kb: number) {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let v = kb * 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v < 10 && i > 0 ? v.toFixed(1) : Math.round(v)} ${units[i]}`;
}

export function fmtUptime(sec: number) {
  const d = Math.floor(sec / 86400);
  const h = Math.floor((sec % 86400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  if (d > 0) return `${d}h ${h}j`;
  if (h > 0) return `${h}j ${m}m`;
  return `${m}m`;
}

export function pct(used: number, total: number) {
  if (!total) return 0;
  return Math.min(100, Math.round((used / total) * 100));
}
