"use client";
import { t as translate } from "@/lib/i18n";

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
      <label className="label">{translate(label)}</label>
      <input
        className="input"
        type="number"
        min="0"
        step={step ?? "1"}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
      {hint && <p className="mt-1 text-xs text-slate-400">{translate(hint)}</p>}
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
          {translate(
            editing
              ? translate("Sửa gói “{0}”", [editing.name])
              : translate("Tạo gói mới"),
          )}
        </h2>
        <span className="text-xs text-slate-400">
          {translate("0 = không giới hạn")}
        </span>
      </div>
      <fieldset className="modal-section">
        <legend>{translate("Thông tin gói")}</legend>
        <div className="modal-grid">
          <div className="modal-span-2">
            <label className="label">{translate("Tên gói")}</label>
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
              <label className="label">{translate("Dành cho")}</label>
              <select
                className="input"
                disabled={!!editing}
                value={f.kind}
                onChange={(e) =>
                  setF({ ...blank(e.target.value as Kind), name: f.name })
                }
              >
                <option value="user">{translate("Người dùng")}</option>
                <option value="reseller">{translate("Đại lý")}</option>
              </select>
            </div>
          )}
        </div>
      </fieldset>
      <fieldset className="modal-section">
        <legend>{translate("Dung lượng & lưu lượng")}</legend>
        <div className="modal-grid">
          <NumField
            label={translate("Dung lượng (GB)")}
            step="0.5"
            value={f.disk_gb}
            onChange={set("disk_gb")}
            hint={
              f.kind === "reseller"
                ? translate("Tổng dung lượng được cấp cho khách")
                : undefined
            }
          />
          <NumField
            label={translate("Số inode (file)")}
            value={f.inodes}
            onChange={set("inodes")}
          />
          <NumField
            label={translate("Băng thông (GB/tháng)")}
            value={f.bandwidth_gb}
            onChange={set("bandwidth_gb")}
          />
        </div>
      </fieldset>
      <fieldset className="modal-section">
        <legend>{translate("Giới hạn dịch vụ")}</legend>
        <div className="modal-grid">
          <NumField
            label={translate("Số website")}
            value={f.max_sites}
            onChange={set("max_sites")}
          />
          <NumField
            label={translate("Số database")}
            value={f.max_dbs}
            onChange={set("max_dbs")}
          />
          <NumField
            label={translate("Số tài khoản FTP")}
            value={f.max_ftp}
            onChange={set("max_ftp")}
          />
          <NumField
            label={translate("Số cron job")}
            value={f.max_cron}
            onChange={set("max_cron")}
          />
          <NumField
            label={translate("Số hộp thư email")}
            value={f.max_mailboxes}
            onChange={set("max_mailboxes")}
          />
        </div>
      </fieldset>
      <fieldset className="modal-section">
        <legend>{translate("Tài nguyên xử lý")}</legend>
        <div className="modal-grid">
          <NumField
            label="CPU (%)"
            value={f.cpu_pct}
            onChange={set("cpu_pct")}
            hint={translate("100% = 1 nhân")}
          />
          <NumField
            label="RAM (MB)"
            value={f.mem_mb}
            onChange={set("mem_mb")}
          />
          {f.kind === "reseller" && (
            <NumField
              label={translate("Số tài khoản tối đa")}
              value={f.max_users}
              onChange={set("max_users")}
            />
          )}
        </div>
      </fieldset>

      <fieldset className="modal-section">
        <legend>{translate("Phiên bản PHP được phép")}</legend>
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
              PHP {translate(v)}
            </label>
          ))}
        </div>
        <p className="mt-1 text-xs text-slate-400">
          {translate("Không chọn = tất cả phiên bản.")}
        </p>
      </fieldset>

      {f.kind === "reseller" && (
        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={f.oversell}
            onChange={(e) => setF({ ...f, oversell: e.target.checked })}
          />
          {translate("Cho phép bán vượt dung lượng (oversell)")}
        </label>
      )}

      <ErrorBox msg={error} />
      <div className="modal-actions">
        <button className="btn-primary" disabled={saving}>
          {translate(
            saving
              ? translate("Đang lưu…")
              : editing
                ? translate("Lưu thay đổi")
                : translate("Tạo gói"),
          )}
        </button>
        <button type="button" className="btn-ghost" onClick={onCancel}>
          {translate("Hủy")}
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
        { kind: "reseller", title: translate("Gói đại lý") },
        { kind: "user", title: translate("Gói người dùng") },
      ]
    : [{ kind: "user", title: translate("Gói cho khách hàng") }];

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">{translate("Gói dịch vụ")}</h1>
          <p className="text-sm text-slate-500">
            {translate("Giới hạn tài nguyên áp dụng cho từng tài khoản.")}
          </p>
        </div>
        {!form && (
          <button
            className="btn-primary"
            onClick={() => setForm({ initial: blank("user"), editing: null })}
          >
            {translate("+ Tạo gói")}
          </button>
        )}
      </div>

      {form && (
        <Modal
          size="large"
          title={translate("Gói dịch vụ")}
          onClose={() => setForm(null)}
        >
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
              {translate(g.title)}
            </h2>
            {items.length === 0 && (
              <p className="text-sm text-slate-400">
                {translate("Chưa có gói nào.")}
              </p>
            )}
            <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
              {items.map((p) => (
                <div key={p.id} className="card flex flex-col p-5">
                  <div className="flex items-start justify-between gap-2">
                    <div>
                      <h3 className="font-semibold">{p.name}</h3>
                      <p className="text-xs text-slate-500">
                        {translate(p.users_count)}{" "}
                        {translate("tài khoản đang dùng")}
                      </p>
                    </div>
                    {p.oversell && (
                      <span className="rounded bg-amber-50 px-2 py-0.5 text-xs text-amber-700">
                        Oversell
                      </span>
                    )}
                  </div>
                  <dl className="mt-4 grid grid-cols-2 gap-x-4 gap-y-1.5 text-sm">
                    <dt className="text-slate-500">
                      {translate("Dung lượng")}
                    </dt>
                    <dd>{translate(limit(p.disk_mb, fmtMB))}</dd>
                    {p.kind === "reseller" && (
                      <>
                        <dt className="text-slate-500">
                          {translate("Tài khoản")}
                        </dt>
                        <dd>{limit(p.max_users)}</dd>
                      </>
                    )}
                    <dt className="text-slate-500">Website</dt>
                    <dd>{translate(limit(p.max_sites))}</dd>
                    <dt className="text-slate-500">Database</dt>
                    <dd>{translate(limit(p.max_dbs))}</dd>
                    <dt className="text-slate-500">FTP</dt>
                    <dd>{translate(limit(p.max_ftp))}</dd>
                    <dt className="text-slate-500">
                      {translate("Băng thông")}
                    </dt>
                    <dd>
                      {translate(limit(p.bandwidth_gb, (n) => `${n} GB`))}
                    </dd>
                    <dt className="text-slate-500">CPU / RAM</dt>
                    <dd>
                      {translate(limit(p.cpu_pct, (n) => `${n}%`))} /
                      {translate(" ")}
                      {translate(limit(p.mem_mb, fmtMB))}
                    </dd>
                    <dt className="text-slate-500">PHP</dt>
                    <dd>
                      {translate(
                        p.php_versions.length
                          ? p.php_versions.join(", ")
                          : translate("Tất cả"),
                      )}
                    </dd>
                  </dl>
                  <div className="mt-auto flex gap-2 pt-4">
                    <button
                      className="btn-ghost px-3 py-1 text-xs"
                      onClick={() =>
                        setForm({ initial: fromPackage(p), editing: p })
                      }
                    >
                      {translate("Sửa")}
                    </button>
                    <button
                      className="btn-ghost px-3 py-1 text-xs text-red-600"
                      disabled={p.users_count > 0}
                      title={translate(
                        p.users_count > 0
                          ? translate("Gói đang được sử dụng")
                          : undefined,
                      )}
                      onClick={() => setDeleting(p)}
                    >
                      {translate("Xóa")}
                    </button>
                  </div>
                </div>
              ))}
            </div>
          </section>
        );
      })}

      {deleting && (
        <Modal
          title={translate("Xóa gói dịch vụ")}
          onClose={() => setDeleting(null)}
        >
          <p className="text-sm text-slate-600">
            {translate("Xóa gói")} <b>{deleting.name}</b>{" "}
            {translate("? Thao tác không thể hoàn tác.")}
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
