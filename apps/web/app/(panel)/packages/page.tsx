"use client";

import { FormEvent, useCallback, useEffect, useState } from "react";
import { api, errMsg, fmtMB, limit, type Package } from "@/lib/api";
import { ErrorBox, Modal } from "@/components/ui";
import { useSession } from "../session";

type Kind = Package["kind"];

type Form = {
  name: string;
  kind: Kind;
  oversell: boolean;
  disk_gb: string;
  inodes: string;
  bandwidth_gb: string;
  max_sites: string;
  max_dbs: string;
  max_ftp: string;
  max_cron: string;
  max_mailboxes: string;
  max_users: string;
  cpu_pct: string;
  mem_mb: string;
  php_versions: string[];
};

const blank = (kind: Kind): Form => ({
  name: "",
  kind,
  oversell: false,
  disk_gb: "5",
  inodes: "0",
  bandwidth_gb: "0",
  max_sites: "5",
  max_dbs: "5",
  max_ftp: "5",
  max_cron: "5",
  max_mailboxes: "10",
  max_users: kind === "reseller" ? "10" : "0",
  cpu_pct: "0",
  mem_mb: "0",
  php_versions: [],
});

const fromPackage = (p: Package): Form => ({
  name: p.name,
  kind: p.kind,
  oversell: p.oversell,
  disk_gb: String(+(p.disk_mb / 1024).toFixed(2)),
  inodes: String(p.inodes),
  bandwidth_gb: String(p.bandwidth_gb),
  max_sites: String(p.max_sites),
  max_dbs: String(p.max_dbs),
  max_ftp: String(p.max_ftp),
  max_cron: String(p.max_cron),
  max_mailboxes: String(p.max_mailboxes ?? 0),
  max_users: String(p.max_users),
  cpu_pct: String(p.cpu_pct),
  mem_mb: String(p.mem_mb),
  php_versions: p.php_versions,
});

const num = (s: string) => Math.max(0, Math.floor(Number(s) || 0));

function toBody(f: Form) {
  return {
    name: f.name,
    kind: f.kind,
    oversell: f.oversell,
    disk_mb: Math.round((Number(f.disk_gb) || 0) * 1024),
    inodes: num(f.inodes),
    bandwidth_gb: num(f.bandwidth_gb),
    max_sites: num(f.max_sites),
    max_dbs: num(f.max_dbs),
    max_ftp: num(f.max_ftp),
    max_cron: num(f.max_cron),
    max_mailboxes: num(f.max_mailboxes),
    max_users: num(f.max_users),
    cpu_pct: num(f.cpu_pct),
    mem_mb: num(f.mem_mb),
    php_versions: f.php_versions,
  };
}

function NumField({
  label,
  value,
  onChange,
  hint,
  step,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  hint?: string;
  step?: string;
}) {
  return (
    <div>
      <label className="label">{label}</label>
      <input
        className="input"
        type="number"
        min="0"
        step={step ?? "1"}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
      {hint && <p className="mt-1 text-xs text-slate-400">{hint}</p>}
    </div>
  );
}

function PackageForm({
  initial,
  editing,
  isAdmin,
  phpVersions,
  onDone,
  onCancel,
}: {
  initial: Form;
  editing: Package | null;
  isAdmin: boolean;
  phpVersions: string[];
  onDone: () => void;
  onCancel: () => void;
}) {
  const [f, setF] = useState(initial);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const set = (k: keyof Form) => (v: string) => setF({ ...f, [k]: v });

  async function submit(e: FormEvent) {
    e.preventDefault();
    setSaving(true);
    setError("");
    try {
      if (editing)
        await api(`/packages/${editing.id}`, {
          method: "PUT",
          body: toBody(f),
        });
      else await api("/packages", { method: "POST", body: toBody(f) });
      onDone();
    } catch (err) {
      setError(errMsg(err));
    } finally {
      setSaving(false);
    }
  }

  const togglePHP = (v: string) =>
    setF({
      ...f,
      php_versions: f.php_versions.includes(v)
        ? f.php_versions.filter((x) => x !== v)
        : [...f.php_versions, v],
    });

  return (
    <form onSubmit={submit} className="card package-form space-y-5 p-5">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="font-semibold">
          {editing ? `Sửa gói “${editing.name}”` : "Tạo gói mới"}
        </h2>
        <span className="text-xs text-slate-400">0 = không giới hạn</span>
      </div>
      <fieldset className="modal-section"><legend>Thông tin gói</legend>
      <div className="modal-grid">
        <div className="modal-span-2">
          <label className="label">Tên gói</label>
          <input
            className="input"
            required
            maxLength={64}
            value={f.name}
            onChange={(e) => set("name")(e.target.value)}
          />
        </div>
        {isAdmin && (
          <div>
            <label className="label">Dành cho</label>
            <select
              className="input"
              disabled={!!editing}
              value={f.kind}
              onChange={(e) =>
                setF({ ...blank(e.target.value as Kind), name: f.name })
              }
            >
              <option value="user">Người dùng</option>
              <option value="reseller">Đại lý</option>
            </select>
          </div>
        )}
      </div></fieldset>
      <fieldset className="modal-section"><legend>Dung lượng & lưu lượng</legend>
      <div className="modal-grid">
        <NumField
          label="Dung lượng (GB)"
          step="0.5"
          value={f.disk_gb}
          onChange={set("disk_gb")}
          hint={
            f.kind === "reseller"
              ? "Tổng dung lượng được cấp cho khách"
              : undefined
          }
        />
        <NumField
          label="Số inode (file)"
          value={f.inodes}
          onChange={set("inodes")}
        />
        <NumField
          label="Băng thông (GB/tháng)"
          value={f.bandwidth_gb}
          onChange={set("bandwidth_gb")}
        />
      </div></fieldset>
      <fieldset className="modal-section"><legend>Giới hạn dịch vụ</legend>
      <div className="modal-grid">
        <NumField
          label="Số website"
          value={f.max_sites}
          onChange={set("max_sites")}
        />
        <NumField
          label="Số database"
          value={f.max_dbs}
          onChange={set("max_dbs")}
        />
        <NumField
          label="Số tài khoản FTP"
          value={f.max_ftp}
          onChange={set("max_ftp")}
        />
        <NumField
          label="Số cron job"
          value={f.max_cron}
          onChange={set("max_cron")}
        />
        <NumField
          label="Số hộp thư email"
          value={f.max_mailboxes}
          onChange={set("max_mailboxes")}
        />
      </div></fieldset>
      <fieldset className="modal-section"><legend>Tài nguyên xử lý</legend>
      <div className="modal-grid">
        <NumField
          label="CPU (%)"
          value={f.cpu_pct}
          onChange={set("cpu_pct")}
          hint="100% = 1 nhân"
        />
        <NumField label="RAM (MB)" value={f.mem_mb} onChange={set("mem_mb")} />
        {f.kind === "reseller" && (
          <NumField
            label="Số tài khoản tối đa"
            value={f.max_users}
            onChange={set("max_users")}
          />
        )}
      </div></fieldset>

      <fieldset className="modal-section">
        <legend>Phiên bản PHP được phép</legend>
        <div className="flex flex-wrap gap-2">
          {phpVersions.map((v) => (
            <label
              key={v}
              className={`cursor-pointer rounded-lg border px-3 py-1.5 text-sm ${
                f.php_versions.includes(v)
                  ? "border-brand-500 bg-brand-50 text-brand-700"
                  : "border-slate-300"
              }`}
            >
              <input
                type="checkbox"
                className="sr-only"
                checked={f.php_versions.includes(v)}
                onChange={() => togglePHP(v)}
              />
              PHP {v}
            </label>
          ))}
        </div>
        <p className="mt-1 text-xs text-slate-400">
          Không chọn = tất cả phiên bản.
        </p>
      </fieldset>

      {f.kind === "reseller" && (
        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={f.oversell}
            onChange={(e) => setF({ ...f, oversell: e.target.checked })}
          />
          Cho phép bán vượt dung lượng (oversell)
        </label>
      )}

      <ErrorBox msg={error} />
      <div className="modal-actions">
        <button className="btn-primary" disabled={saving}>
          {saving ? "Đang lưu…" : editing ? "Lưu thay đổi" : "Tạo gói"}
        </button>
        <button type="button" className="btn-ghost" onClick={onCancel}>
          Hủy
        </button>
      </div>
    </form>
  );
}

export default function PackagesPage() {
  const me = useSession();
  const isAdmin = me.role === "admin";
  const [list, setList] = useState<Package[]>([]);
  const [phpVersions, setPhpVersions] = useState<string[]>([]);
  const [form, setForm] = useState<{
    initial: Form;
    editing: Package | null;
  } | null>(null);
  const [deleting, setDeleting] = useState<Package | null>(null);
  const [error, setError] = useState("");

  const load = useCallback(() => {
    api<{ packages: Package[]; php_versions: string[] }>("/packages")
      .then((r) => {
        setList(r.packages);
        setPhpVersions(r.php_versions);
      })
      .catch((e) => setError(errMsg(e)));
  }, []);
  useEffect(load, [load]);

  async function confirmDelete() {
    if (!deleting) return;
    try {
      await api(`/packages/${deleting.id}`, { method: "DELETE" });
      setDeleting(null);
      load();
    } catch (e) {
      setError(errMsg(e));
      setDeleting(null);
    }
  }

  const groups: { kind: Kind; title: string }[] = isAdmin
    ? [
        { kind: "reseller", title: "Gói đại lý" },
        { kind: "user", title: "Gói người dùng" },
      ]
    : [{ kind: "user", title: "Gói cho khách hàng" }];

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">Gói dịch vụ</h1>
          <p className="text-sm text-slate-500">
            Giới hạn tài nguyên áp dụng cho từng tài khoản.
          </p>
        </div>
        {!form && (
          <button
            className="btn-primary"
            onClick={() => setForm({ initial: blank("user"), editing: null })}
          >
            + Tạo gói
          </button>
        )}
      </div>

      {form && (
        <Modal size="large" title="Gói dịch vụ" onClose={() => setForm(null)}>
          <PackageForm
            key={form.editing?.id ?? "new"}
            {...form}
            isAdmin={isAdmin}
            phpVersions={phpVersions}
            onCancel={() => setForm(null)}
            onDone={() => {
              setForm(null);
              load();
            }}
          />
        </Modal>
      )}
      <ErrorBox msg={error} />

      {groups.map((g) => {
        const items = list.filter((p) => p.kind === g.kind);
        return (
          <section key={g.kind} className="space-y-3">
            <h2 className="text-sm font-semibold uppercase tracking-wide text-slate-500">
              {g.title}
            </h2>
            {items.length === 0 && (
              <p className="text-sm text-slate-400">Chưa có gói nào.</p>
            )}
            <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
              {items.map((p) => (
                <div key={p.id} className="card flex flex-col p-5">
                  <div className="flex items-start justify-between gap-2">
                    <div>
                      <h3 className="font-semibold">{p.name}</h3>
                      <p className="text-xs text-slate-500">
                        {p.users_count} tài khoản đang dùng
                      </p>
                    </div>
                    {p.oversell && (
                      <span className="rounded bg-amber-50 px-2 py-0.5 text-xs text-amber-700">
                        Oversell
                      </span>
                    )}
                  </div>
                  <dl className="mt-4 grid grid-cols-2 gap-x-4 gap-y-1.5 text-sm">
                    <dt className="text-slate-500">Dung lượng</dt>
                    <dd>{limit(p.disk_mb, fmtMB)}</dd>
                    {p.kind === "reseller" && (
                      <>
                        <dt className="text-slate-500">Tài khoản</dt>
                        <dd>{limit(p.max_users)}</dd>
                      </>
                    )}
                    <dt className="text-slate-500">Website</dt>
                    <dd>{limit(p.max_sites)}</dd>
                    <dt className="text-slate-500">Database</dt>
                    <dd>{limit(p.max_dbs)}</dd>
                    <dt className="text-slate-500">FTP</dt>
                    <dd>{limit(p.max_ftp)}</dd>
                    <dt className="text-slate-500">Băng thông</dt>
                    <dd>{limit(p.bandwidth_gb, (n) => `${n} GB`)}</dd>
                    <dt className="text-slate-500">CPU / RAM</dt>
                    <dd>
                      {limit(p.cpu_pct, (n) => `${n}%`)} /{" "}
                      {limit(p.mem_mb, fmtMB)}
                    </dd>
                    <dt className="text-slate-500">PHP</dt>
                    <dd>
                      {p.php_versions.length
                        ? p.php_versions.join(", ")
                        : "Tất cả"}
                    </dd>
                  </dl>
                  <div className="mt-auto flex gap-2 pt-4">
                    <button
                      className="btn-ghost px-3 py-1 text-xs"
                      onClick={() =>
                        setForm({ initial: fromPackage(p), editing: p })
                      }
                    >
                      Sửa
                    </button>
                    <button
                      className="btn-ghost px-3 py-1 text-xs text-red-600"
                      disabled={p.users_count > 0}
                      title={
                        p.users_count > 0 ? "Gói đang được sử dụng" : undefined
                      }
                      onClick={() => setDeleting(p)}
                    >
                      Xóa
                    </button>
                  </div>
                </div>
              ))}
            </div>
          </section>
        );
      })}

      {deleting && (
        <Modal title="Xóa gói dịch vụ" onClose={() => setDeleting(null)}>
          <p className="text-sm text-slate-600">
            Xóa gói <b>{deleting.name}</b>? Thao tác không thể hoàn tác.
          </p>
          <div className="mt-5 flex justify-end gap-2">
            <button className="btn-ghost" onClick={() => setDeleting(null)}>
              Hủy
            </button>
            <button
              className="btn bg-red-600 text-white hover:bg-red-700"
              onClick={confirmDelete}
            >
              Xóa
            </button>
          </div>
        </Modal>
      )}
    </div>
  );
}
