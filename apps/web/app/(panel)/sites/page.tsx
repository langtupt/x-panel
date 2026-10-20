"use client";
import { localeTag } from "@/lib/i18n";
import { t as translate } from "@/lib/i18n";

import { FormEvent, useCallback, useEffect, useState } from "react";
import {
  api,
  errMsg,
  type PHPList,
  type Site,
  type SiteResult,
  type SSLStatus,
  type Task,
  type User,
} from "@/lib/api";
import { useTask } from "@/lib/useTask";
import { ErrorBox, Modal } from "@/components/ui";
import { TaskCard } from "@/components/task";
import { SitePHP } from "@/components/site-php";
import { useSession } from "../session";

const sslBadge: Record<SSLStatus, { label: string; cls: string }> = {
  none: { label: "Chưa có SSL", cls: "bg-slate-100 text-slate-600" },
  pending: { label: "Đang cấp SSL", cls: "bg-brand-50 text-brand-700" },
  active: { label: "HTTPS", cls: "bg-emerald-50 text-emerald-700" },
  failed: { label: "Cấp SSL lỗi", cls: "bg-red-50 text-red-700" },
};

const splitNames = (s: string) =>
  s
    .split(/[\s,]+/)
    .map((x) => x.trim().toLowerCase())
    .filter(Boolean);

type FormState = {
  app_type: string;
  runtime: string;
  envText: string;
  sample: boolean;
  user_id: number | null;
  domain: string;
  www: boolean;
  aliases: string;
  php_version: string;
  doc_root: string;
};

function SiteForm({
  editing,
  owners,
  php,
  onDone,
  onCancel,
}: {
  editing: Site | null;
  owners: User[] | null; // null = the current user owns the site
  php: PHPList | null;
  onDone: (r: SiteResult) => void;
  onCancel: () => void;
}) {
  const [f, setF] = useState<FormState>(() =>
    editing
      ? {
          app_type: editing.app_type,
          runtime: editing.runtime,
          envText: Object.entries(editing.env ?? {})
            .map(([k, v]) => k + "=" + v)
            .join("\n"),
          sample: false,
          user_id: editing.user_id,
          domain: editing.domain,
          www: editing.aliases.includes("www." + editing.domain),
          aliases: editing.aliases
            .filter((a) => a !== "www." + editing.domain)
            .join(", "),
          php_version: editing.php_version,
          doc_root: editing.doc_root,
        }
      : {
          app_type: "php",
          runtime: "",
          envText: "",
          sample: false,
          user_id: owners?.[0]?.id ?? null,
          domain: "",
          www: true,
          aliases: "",
          php_version: php?.installed.at(-1) ?? "",
          doc_root: "",
        },
  );
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setSaving(true);
    setError("");
    const domain = f.domain.trim().toLowerCase();
    const aliases = splitNames(f.aliases);
    if (f.www && !domain.startsWith("www.")) aliases.unshift("www." + domain);
    const env = Object.fromEntries(
      f.envText
        .split("\n")
        .filter((line) => line.trim())
        .map((line) => {
          const i = line.indexOf("=");
          return i < 1
            ? [line, ""]
            : [line.slice(0, i).trim(), line.slice(i + 1)];
        }),
    );
    const body = {
      app_type: f.app_type,
      runtime: f.runtime,
      env,
      sample: f.sample,
      user_id: f.user_id,
      domain,
      aliases,
      php_version: f.php_version,
      doc_root: f.doc_root.trim(),
    };
    try {
      const r = editing
        ? await api<SiteResult>(`/sites/${editing.id}`, { method: "PUT", body })
        : await api<SiteResult>("/sites", { method: "POST", body });
      onDone(r);
    } catch (err) {
      setError(errMsg(err));
      setSaving(false);
    }
  }

  return (
    <form onSubmit={submit} className="card space-y-4 p-5">
      <h2 className="font-semibold">
        {translate(
          editing
            ? translate("Sửa website {0}", [editing.domain])
            : translate("Thêm website"),
        )}
      </h2>
      <div className="modal-grid">
        {owners && !editing && (
          <div>
            <label className="label">{translate("Tài khoản sở hữu")}</label>
            <select
              className="input"
              required
              value={f.user_id ?? ""}
              onChange={(e) =>
                setF({ ...f, user_id: Number(e.target.value) || null })
              }
            >
              {owners.length === 0 && (
                <option value="">
                  {translate("Chưa có tài khoản người dùng nào")}
                </option>
              )}
              {owners.map((u) => (
                <option key={u.id} value={u.id}>
                  {u.username}
                </option>
              ))}
            </select>
          </div>
        )}
        <div>
          <label className="label">{translate("Tên miền")}</label>
          <input
            className="input"
            required
            placeholder="example.com"
            disabled={!!editing}
            value={f.domain}
            onChange={(e) => setF({ ...f, domain: e.target.value })}
          />
          <label className="mt-2 flex items-center gap-2 text-sm text-slate-600">
            <input
              type="checkbox"
              checked={f.www}
              onChange={(e) => setF({ ...f, www: e.target.checked })}
            />
            {translate("Thêm www.")}{" "}
            {translate(f.domain || translate("tên-miền"))}
          </label>
        </div>
        <div>
          <label className="label">{translate("Tên miền phụ (alias)")}</label>
          <input
            className="input"
            placeholder="shop.example.com, example.net"
            value={f.aliases}
            onChange={(e) => setF({ ...f, aliases: e.target.value })}
          />
          <p className="mt-1 text-xs text-slate-400">
            {translate("Phân tách bằng dấu phẩy hoặc khoảng trắng.")}
          </p>
        </div>
        <div>
          <label className="label">{translate("Loại ứng dụng")}</label>
          <select
            className="input"
            value={f.app_type}
            onChange={(e) => setF({ ...f, app_type: e.target.value })}
          >
            {["html", "php", "laravel", "nodejs", "nextjs", "go"].map((t) => (
              <option key={t} value={t}>
                {translate(t)}
              </option>
            ))}
          </select>
          {editing && (
            <button
              type="button"
              className="btn-ghost text-xs"
              onClick={async () => {
                try {
                  const r = await api<{ app_type: string }>(
                    `/sites/${editing.id}/detect`,
                  );
                  setF((cur) => ({ ...cur, app_type: r.app_type }));
                } catch (e) {
                  setError(errMsg(e));
                }
              }}
            >
              {translate("Nhận diện từ mã nguồn")}
            </button>
          )}
          <label className="label">
            {translate("Phiên bản PHP (PHP / Laravel)")}
          </label>
          <select
            className="input"
            value={f.php_version}
            onChange={(e) => setF({ ...f, php_version: e.target.value })}
          >
            <option value="">{translate("Không dùng PHP (web tĩnh)")}</option>
            {php?.installed.map((v) => (
              <option key={v} value={v}>
                PHP {translate(v)}
              </option>
            ))}
          </select>
          {php && php.installed.length === 0 && (
            <p className="mt-1 text-xs text-amber-700">
              {translate("Máy chủ chưa cài phiên bản PHP nào.")}
            </p>
          )}
        </div>
        <div>
          <label className="label">
            {translate("Thư mục gốc (document root)")}
          </label>
          <div className="flex items-center rounded-lg border border-slate-300 bg-slate-50 text-sm">
            <span className="truncate px-3 text-slate-500">
              ~/www/{translate(f.domain || "domain")}/
            </span>
            <input
              className="input rounded-l-none border-0 border-l"
              placeholder={translate("(để trống) hoặc public")}
              value={f.doc_root}
              onChange={(e) => setF({ ...f, doc_root: e.target.value })}
            />
          </div>
          <p className="mt-1 text-xs text-slate-400">
            Laravel: <code>public</code>
          </p>
        </div>
      </div>
      <div>
        <label className="label">
          {translate("Phiên bản Node.js / Go (trống: mới nhất đã cài)")}
        </label>
        <input
          className="input"
          value={f.runtime}
          onChange={(e) => setF({ ...f, runtime: e.target.value })}
        />
      </div>
      <div>
        <label className="label">
          {translate("Biến môi trường (KEY=value, mỗi dòng một biến)")}
        </label>
        <textarea
          className="input font-mono"
          rows={5}
          value={f.envText}
          onChange={(e) => setF({ ...f, envText: e.target.value })}
        />
        <p className="text-xs text-slate-500">
          {translate("Lưu cấu hình rồi Build & restart để áp dụng.")}
        </p>
      </div>
      {!editing && (
        <label className="flex gap-2 text-sm">
          <input
            type="checkbox"
            checked={f.sample}
            onChange={(e) => setF({ ...f, sample: e.target.checked })}
          />
          {translate("Tạo dự án mẫu và build (chỉ thư mục trống)")}
        </label>
      )}
      <ErrorBox msg={error} />
      <div className="modal-actions">
        <button className="btn-primary" disabled={saving}>
          {translate(
            saving
              ? translate("Đang lưu…")
              : editing
                ? translate("Lưu")
                : translate("Tạo website"),
          )}
        </button>
        <button type="button" className="btn-ghost" onClick={onCancel}>
          {translate("Hủy")}
        </button>
      </div>
    </form>
  );
}

function SSLCell({
  site,
  task,
  onIssue,
  onDisable,
}: {
  site: Site;
  task: Task | null;
  onIssue: () => void;
  onDisable: () => void;
}) {
  const b = sslBadge[site.ssl_status];
  return (
    <div className="space-y-1">
      <span className={`rounded-full px-2 py-0.5 text-xs font-medium ${b.cls}`}>
        {translate(b.label)}
      </span>
      {site.ssl_status === "active" && site.ssl_expires_at && (
        <div className="text-xs text-slate-500">
          {translate("đến")}{" "}
          {translate(
            new Date(site.ssl_expires_at).toLocaleDateString(localeTag()),
          )}
        </div>
      )}
      <div className="flex gap-1.5">
        {site.ssl_status !== "pending" && !task && (
          <button
            className="text-xs text-brand-600 hover:underline"
            onClick={onIssue}
          >
            {translate(
              site.ssl_status === "active"
                ? translate("Cấp lại")
                : translate("Cấp Let's Encrypt"),
            )}
          </button>
        )}
        {site.ssl_status === "active" && (
          <button
            className="text-xs text-slate-500 hover:underline"
            onClick={onDisable}
          >
            {translate("Tắt")}
          </button>
        )}
      </div>
    </div>
  );
}

function SiteTask({ task, onDone }: { task: Task; onDone: () => void }) {
  const t = useTask(task, onDone);
  if (!t) return null;
  return <TaskCard task={t} defaultOpen={t.status === "failed"} />;
}

export default function SitesPage() {
  const me = useSession();
  const manager = me.role !== "user";
  const [list, setList] = useState<Site[]>([]);
  const [owners, setOwners] = useState<User[]>([]);
  const [php, setPhp] = useState<PHPList | null>(null);
  const [form, setForm] = useState<{ editing: Site | null } | null>(null);
  const [deleting, setDeleting] = useState<Site | null>(null);
  const [tasks, setTasks] = useState<Record<number, Task>>({});
  const [notes, setNotes] = useState<string[]>([]);
  const [phpSite, setPHPSite] = useState<Site | null>(null);
  const [logs, setLogs] = useState<{ domain: string; log: string } | null>(
    null,
  );
  const [error, setError] = useState("");

  const load = useCallback(() => {
    api<Site[]>("/sites")
      .then(setList)
      .catch((e) => setError(errMsg(e)));
  }, []);

  useEffect(() => {
    load();
    api<PHPList>("/php")
      .then(setPhp)
      .catch(() => setPhp({ supported: [], installed: [] }));
    if (manager) {
      api<{ users: User[] }>("/users")
        .then((r) =>
          setOwners(
            r.users.filter((u) => u.role === "user" && u.linux_uid !== null),
          ),
        )
        .catch((e) => setError(errMsg(e)));
    }
  }, [load, manager]);

  function onSaved(r: SiteResult) {
    setForm(null);
    setNotes(r.warnings ?? []);
    if (r.task) setTasks((t) => ({ ...t, [r.id]: r.task! }));
    load();
  }

  async function issueSSL(s: Site) {
    setError("");
    try {
      const t = await api<Task>(`/sites/${s.id}/ssl`, { method: "POST" });
      setTasks((cur) => ({ ...cur, [s.id]: t }));
      load();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  async function disableSSL(s: Site) {
    try {
      await api(`/sites/${s.id}/ssl`, { method: "DELETE" });
      load();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  async function confirmDelete() {
    if (!deleting) return;
    try {
      await api(`/sites/${deleting.id}`, { method: "DELETE" });
      load();
    } catch (e) {
      setError(errMsg(e));
    }
    setDeleting(null);
  }

  const taskDone = useCallback(
    (siteId: number) => () => {
      load();
      setTimeout(
        () =>
          setTasks((t) => {
            const { [siteId]: _, ...rest } = t;
            return t[siteId]?.status === "done" ? rest : t;
          }),
        0,
      );
    },
    [load],
  );

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">Website</h1>
          <p className="text-sm text-slate-500">
            {translate(
              "Mỗi website có vhost nginx và PHP-FPM chạy bằng user của tài khoản.",
            )}
          </p>
        </div>
        {!form && (
          <button
            className="btn-primary"
            onClick={() => setForm({ editing: null })}
          >
            {translate("+ Thêm website")}
          </button>
        )}
      </div>

      {form && (
        <Modal size="large" title="Website" onClose={() => setForm(null)}>
          <SiteForm
            key={form.editing?.id ?? "new"}
            editing={form.editing}
            owners={manager ? owners : null}
            php={php}
            onCancel={() => setForm(null)}
            onDone={onSaved}
          />
        </Modal>
      )}
      <ErrorBox msg={error} />
      {notes.map((n) => (
        <p
          key={n}
          className="rounded-lg bg-amber-50 px-3 py-2 text-sm text-amber-800"
        >
          {translate(n)}
        </p>
      ))}

      <div className="card overflow-x-auto">
        <table className="w-full text-left text-sm">
          <thead className="border-b border-slate-200 bg-slate-50 text-xs uppercase text-slate-500">
            <tr>
              <th className="px-4 py-3">{translate("Tên miền")}</th>
              {manager && (
                <th className="px-4 py-3">{translate("Tài khoản")}</th>
              )}
              <th className="px-4 py-3">{translate("Ứng dụng")}</th>
              <th className="px-4 py-3">SSL</th>
              <th className="px-4 py-3" />
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100">
            {list.map((s) => (
              <tr key={s.id} className="align-top">
                <td className="px-4 py-3">
                  <a
                    className="font-medium text-brand-700 hover:underline"
                    target="_blank"
                    rel="noreferrer"
                    href={`${s.ssl_status === "active" ? "https" : "http"}://${s.domain}`}
                  >
                    {s.domain}
                  </a>
                  {s.aliases.length > 0 && (
                    <div className="text-xs text-slate-500">
                      {translate(s.aliases.join(", "))}
                    </div>
                  )}
                  <div className="text-xs text-slate-400">
                    ~/www/{s.domain}
                    {translate(s.doc_root && "/" + s.doc_root)}
                  </div>
                  {tasks[s.id] && (
                    <div className="mt-2 max-w-xl">
                      <SiteTask task={tasks[s.id]} onDone={taskDone(s.id)} />
                    </div>
                  )}
                </td>
                {manager && (
                  <td className="px-4 py-3 text-slate-600">{s.username}</td>
                )}
                <td className="px-4 py-3">
                  {translate(s.app_type)}{" "}
                  {translate(s.php_version || s.runtime)}
                  {s.port > 0 && (
                    <div className="text-xs text-slate-500">
                      127.0.0.1:{translate(s.port)}
                    </div>
                  )}
                </td>
                <td className="px-4 py-3">
                  <SSLCell
                    site={s}
                    task={tasks[s.id] ?? null}
                    onIssue={() => issueSSL(s)}
                    onDisable={() => disableSSL(s)}
                  />
                </td>
                <td className="px-4 py-3">
                  <div className="flex justify-end gap-1.5">
                    {["php", "laravel"].includes(s.app_type) && (
                      <button
                        className="btn-ghost px-2.5 py-1 text-xs"
                        onClick={() => setPHPSite(s)}
                      >
                        PHP
                      </button>
                    )}
                    <button
                      className="btn-ghost px-2.5 py-1 text-xs"
                      disabled={!!tasks[s.id]}
                      onClick={async () => {
                        try {
                          const t = await api<Task>(`/sites/${s.id}/build`, {
                            method: "POST",
                          });
                          setTasks((cur) => ({ ...cur, [s.id]: t }));
                        } catch (e) {
                          setError(errMsg(e));
                        }
                      }}
                    >
                      Build &amp; restart
                    </button>
                    <button
                      className="btn-ghost px-2.5 py-1 text-xs"
                      onClick={async () => {
                        try {
                          const r = await api<{ log: string }>(
                            `/sites/${s.id}/logs`,
                          );
                          setLogs({ domain: s.domain, log: r.log });
                        } catch (e) {
                          setError(errMsg(e));
                        }
                      }}
                    >
                      Logs
                    </button>
                    <button
                      className="btn-ghost px-2.5 py-1 text-xs"
                      onClick={() => setForm({ editing: s })}
                    >
                      {translate("Sửa")}
                    </button>
                    <button
                      className="btn-ghost px-2.5 py-1 text-xs text-red-600"
                      onClick={() => setDeleting(s)}
                    >
                      {translate("Xóa")}
                    </button>
                  </div>
                </td>
              </tr>
            ))}
            {list.length === 0 && (
              <tr>
                <td
                  colSpan={5}
                  className="px-4 py-8 text-center text-slate-500"
                >
                  {translate("Chưa có website nào.")}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {phpSite && (
        <SitePHP
          site={phpSite}
          admin={me?.role === "admin"}
          onClose={() => setPHPSite(null)}
        />
      )}
      {logs && (
        <Modal
          size="large"
          title={translate(`Logs ${logs.domain}`)}
          onClose={() => setLogs(null)}
        >
          <pre className="max-h-96 overflow-auto whitespace-pre-wrap text-xs">
            {logs.log || translate("Chưa có nhật ký")}
          </pre>
        </Modal>
      )}
      {deleting && (
        <Modal
          title={translate("Xóa website {0}", [deleting.domain])}
          onClose={() => setDeleting(null)}
        >
          <p className="text-sm text-slate-600">
            {translate(
              "Cấu hình nginx/PHP và chứng chỉ SSL của website sẽ bị gỡ. Mã nguồn trong",
            )}{" "}
            <code className="mx-1">~/www/{deleting.domain}</code>{" "}
            {translate("được giữ lại.")}
          </p>
          <div className="mt-5 flex justify-end gap-2">
            <button className="btn-ghost" onClick={() => setDeleting(null)}>
              {translate("Hủy")}
            </button>
            <button
              className="btn bg-red-600 text-white hover:bg-red-700"
              onClick={confirmDelete}
            >
              {translate("Xóa")}
            </button>
          </div>
        </Modal>
      )}
    </div>
  );
}
