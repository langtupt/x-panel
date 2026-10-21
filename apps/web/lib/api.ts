import { t as translate } from "@/lib/i18n";
export type Role = "admin" | "reseller" | "user";
export type Status = "active" | "suspended";

export interface User {
  id: number;
  username: string;
  email: string;
  role: Role;
  parent_id: number | null;
  parent_name: string | null;
  package_id: number | null;
  package_name: string | null;
  disk_limit_mb: number | null;
  disk_used_mb: number | null;
  linux_uid: number | null;
  totp_enabled: boolean;
  status: Status;
  created_at: string;
}

export interface Limits {
  disk_mb: number;
  inodes: number;
  bandwidth_gb: number;
  max_sites: number;
  max_dbs: number;
  max_ftp: number;
  max_cron: number;
  max_mailboxes: number;
  max_users: number;
  cpu_pct: number;
  mem_mb: number;
  php_versions: string[];
}

export interface Package extends Limits {
  id: number;
  owner_id: number;
  name: string;
  kind: "reseller" | "user";
  oversell: boolean;
  users_count: number;
  created_at: string;
}

export interface Overview {
  package: Package | null;
  quota_enabled: boolean;
  disk_used_mb: number | null;
  allocated_users?: number;
  allocated_disk_mb?: number;
}

export interface SystemInfo {
  ip_addresses: string[];
  cpu_model: string;
  cpu_percent: number;
  architecture: string;
  boot_id: string;
  swap_total_mb: number;
  swap_used_mb: number;
  disk_available_mb: number;
  network_rx_bytes: number;
  network_tx_bytes: number;
  hostname: string;
  os_name: string;
  kernel: string;
  cpu_count: number;
  uptime_sec: number;
  load: [number, number, number];
  mem_total_mb: number;
  mem_used_mb: number;
  disk_total_mb: number;
  disk_used_mb: number;
  quota_enabled: boolean;
  quota_mount: string;
}

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
    public data: Record<string, unknown> = {},
  ) {
    super(message);
  }
}

let refreshing: Promise<boolean> | null = null;

// One refresh at a time even when several requests hit 401 together.
function refreshSession(): Promise<boolean> {
  refreshing ??= fetch("/api/auth/refresh", {
    method: "POST",
    headers: { "X-XPanel": "1" },
  })
    .then((r) => r.ok)
    .catch(() => false)
    .finally(() => {
      refreshing = null;
    });
  return refreshing;
}

async function request<T>(
  path: string,
  opts: { method?: string; body?: unknown } = {},
  retry = true,
): Promise<T> {
  const res = await fetch("/api" + path, {
    method: opts.method ?? "GET",
    headers: {
      "X-XPanel": "1",
      ...(opts.body !== undefined
        ? { "Content-Type": "application/json" }
        : {}),
    },
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
  });

  if (res.status === 401 && retry && !path.startsWith("/auth/")) {
    if (await refreshSession()) return request<T>(path, opts, false);
  }
  if (!res.ok) {
    let data: Record<string, unknown> = {};
    try {
      data = await res.json();
    } catch {}
    throw new ApiError(
      res.status,
      typeof data.error === "string" ? data.error : res.statusText,
      data,
    );
  }
  return res.status === 204 ? (undefined as T) : res.json();
}

let operationID = 0;
export async function api<T>(
  path: string,
  opts: { method?: string; body?: unknown } = {},
): Promise<T> {
  const readOnlyFile =
    path === "/files/op" &&
    (opts.body as { request?: { op?: string } } | undefined)?.request?.op ===
      "read";
  const notify =
    typeof window !== "undefined" &&
    opts.method &&
    opts.method !== "GET" &&
    !path.startsWith("/auth/") &&
    path !== "/system/reboot" &&
    !readOnlyFile;
  const id = ++operationID;
  if (notify)
    window.dispatchEvent(
      new CustomEvent("xpanel:operation", {
        detail: { id, path, method: opts.method, status: "running" },
      }),
    );
  try {
    const result = await request<T>(path, opts);
    if (notify)
      window.dispatchEvent(
        new CustomEvent("xpanel:operation", {
          detail: { id, path, method: opts.method, status: "done", result },
        }),
      );
    return result;
  } catch (error) {
    if (notify)
      window.dispatchEvent(
        new CustomEvent("xpanel:operation", {
          detail: {
            id,
            path,
            method: opts.method,
            status: "failed",
            error: errMsg(error),
          },
        }),
      );
    throw error;
  }
}

export function errMsg(e: unknown) {
  return e instanceof ApiError
    ? translate(e.message)
    : translate("Lỗi không xác định");
}

export const roleLabel: Record<Role, string> = {
  admin: "Quản trị",
  reseller: "Đại lý",
  user: "Người dùng",
};

export function fmtMB(mb: number) {
  return mb >= 1024 ? `${+(mb / 1024).toFixed(1)} GB` : `${mb} MB`;
}

/** Formats a limit where 0 means unlimited. */
export function limit(v: number, fmt: (n: number) => string = String) {
  return v === 0 ? "∞" : fmt(v);
}

export type SSLStatus = "none" | "pending" | "active" | "failed";

export interface Site {
  app_type: string;
  runtime: string;
  port: number;
  env: Record<string, string>;
  deploy_script?: string;
  id: number;
  user_id: number;
  username: string;
  domain: string;
  aliases: string[];
  php_version: string;
  doc_root: string;
  ssl_status: SSLStatus;
  ssl_expires_at: string | null;
  ssl_type: "" | "letsencrypt" | "custom" | "selfsigned";
  ssl_issuer: string;
  created_at: string;
}

export interface PanelSSL {
  domain: string;
  info: {
    subject: string;
    issuer: string;
    names: string[] | null;
    not_after: string;
    self_signed: boolean;
  };
}

export interface SiteResult extends Site {
  warnings?: string[];
  task?: Task;
}

export type TaskStatus = "pending" | "running" | "done" | "failed";

export interface Task {
  id: number;
  user_id: number | null;
  type: string;
  target: string;
  status: TaskStatus;
  log: string;
  created_at: string;
  started_at: string | null;
  finished_at: string | null;
}

export interface PHPList {
  supported: string[];
  installed: string[];
}

export const taskLabel: Record<string, string> = {
  "site.build": "Build website",
  "app.install": "Cài ứng dụng",
  "ssl.issue": "Cấp SSL",
  "php.install": "Cài PHP",
  "php.manage": "Quản lý PHP",
  "pma.install": "Cài phpMyAdmin",
};

export const isOpen = (t: Task) =>
  t.status === "pending" || t.status === "running";

export interface Database {
  engine: "mariadb" | "postgresql";
  id: number;
  user_id: number;
  username: string;
  name: string;
  db_user: string;
  size_bytes: number | null;
  created_at: string;
}

export function fmtBytes(b: number) {
  if (b < 1024) return `${b} B`;
  if (b < 1024 ** 2) return `${(b / 1024).toFixed(1)} KB`;
  if (b < 1024 ** 3) return `${(b / 1024 ** 2).toFixed(1)} MB`;
  return `${(b / 1024 ** 3).toFixed(2)} GB`;
}

/** Random password using only characters the server accepts for DB passwords. */
export function genPassword(len = 20) {
  const chars =
    "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!@#%^*_-+=";
  const rnd = crypto.getRandomValues(new Uint32Array(len));
  return Array.from(rnd, (n) => chars[n % chars.length]).join("");
}

export interface FSEntry {
  name: string;
  type: "dir" | "file" | "link";
  size: number;
  mode: string;
  mtime: string;
  target?: string;
}

export interface FtpAccount {
  id: number;
  user_id: number;
  username: string;
  login: string;
  dir: string;
  created_at: string;
}

export interface PMAStatus {
  installed: boolean;
  php_version: string;
}

/** Joins path segments into a clean absolute path inside a home directory. */
export function joinPath(...parts: string[]) {
  const out: string[] = [];
  for (const seg of parts.join("/").split("/")) {
    if (!seg || seg === ".") continue;
    if (seg === "..") out.pop();
    else out.push(seg);
  }
  return "/" + out.join("/");
}

/** /api/me: the user plus who is impersonating them, if anyone. */
export interface Me extends User {
  impersonator: { id: number; username: string } | null;
  panel_version?: string;
}

export interface CronJob {
  id: number;
  user_id: number;
  username: string;
  schedule: string;
  command: string;
  enabled: boolean;
  created_at: string;
}

export interface AuditRow {
  id: number;
  actor: string | null;
  action: string;
  target_type: string;
  target_id: number | null;
  ip: string;
  meta: Record<string, unknown>;
  created_at: string;
}

export interface AppInfo {
  id: string;
  name: string;
  category: string;
  description: string;
  multi_version: boolean;
  builtin: boolean;
  installed: string[];
  available: string[];
  checked_at?: string;
  source?: string;
  version_labels?: Record<string, string>;
  installed_labels?: Record<string, string>;
  error?: string;
}

/** phpMyAdmin / Adminer run on their own port (separate browser origin). */
export function dbToolURL(tool: "phpmyadmin" | "adminer") {
  return `https://${window.location.hostname}:8890/${tool}/`;
}
