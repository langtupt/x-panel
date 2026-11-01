"use client";
import { t as translate } from "@/lib/i18n";

import { FormEvent, useCallback, useEffect, useState } from "react";
import {
  api,
  errMsg,
  fmtMB,
  roleLabel,
  type Package,
  type Role,
  type User,
} from "@/lib/api";
import { ErrorBox, Modal, UsageBar } from "@/components/ui";
import { useSession } from "../session";

const roleBadge: Record<Role, string> = {
  admin: "bg-purple-50 text-purple-700",
  reseller: "bg-brand-50 text-brand-700",
  user: "bg-slate-100 text-slate-700",
};

function PackageSelect({
  packages,
  role,
  value,
  onChange,
  required,
}: {
  packages: Package[];
  role: Role;
  value: number | null;
  onChange: (id: number | null) => void;
  required: boolean;
}) {
  const options = packages.filter((p) => p.kind === role);
  return (
    <select
      className="input"
      required={required}
      value={value ?? ""}
      onChange={(e) => onChange(e.target.value ? Number(e.target.value) : null)}
    >
      <option value="">
        {translate(
          required ? translate("— Chọn gói —") : translate("Không giới hạn"),
        )}
      </option>
      {options.map((p) => (
        <option key={p.id} value={p.id}>
          {p.name}
        </option>
      ))}
    </select>
  );
}

// Dates are handled as "YYYY-MM-DD" in the server's calendar.
const ymd = (d: Date) =>
  `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
const todayYMD = () => ymd(new Date());
function addMonths(from: string, months: number) {
  const [y, m, d] = from.split("-").map(Number);
  const t = new Date(y, m - 1 + months, d);
  return ymd(t);
}
const fmtDate = (s: string) => s.slice(0, 10).split("-").reverse().join("/");
// Days left until the expiry date (negative = expired).
function daysLeft(s: string) {
  const [y, m, d] = s.slice(0, 10).split("-").map(Number);
  const now = new Date();
  const a = Date.UTC(y, m - 1, d);
  const b = Date.UTC(now.getFullYear(), now.getMonth(), now.getDate());
  return Math.round((a - b) / 86400000);
}

// ExpiryPicker: a date with quick "+N months" buttons; "" = never expires.
function ExpiryPicker({
  value,
  base,
  onChange,
}: {
  value: string;
  base?: string; // extend from this date (current expiry) when later than today
  onChange: (v: string) => void;
}) {
  const from = base && base > todayYMD() ? base : todayYMD();
  return (
    <div className="space-y-2">
      <input
        className="input"
        type="date"
        min={todayYMD()}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
      <div className="flex flex-wrap gap-1.5">
        {[1, 3, 6, 12, 24].map((n) => (
          <button
            type="button"
            key={n}
            className="rounded-md border border-slate-300 px-2 py-0.5 text-xs hover:bg-slate-50"
            onClick={() => onChange(addMonths(from, n))}
          >
            +{n < 12 ? translate("{0} tháng", [n]) : translate("{0} năm", [n / 12])}
          </button>
        ))}
        <button
          type="button"
          className={`rounded-md border px-2 py-0.5 text-xs ${value === "" ? "border-brand-500 bg-brand-50 text-brand-700" : "border-slate-300 hover:bg-slate-50"}`}
          onClick={() => onChange("")}
        >
          {translate("Không thời hạn")}
        </button>
      </div>
    </div>
  );
}

function ExpiryCell({ u }: { u: User }) {
  if (u.role !== "user") return <span className="text-slate-400">—</span>;
  if (!u.expires_at)
    return <span className="text-slate-400">{translate("Không thời hạn")}</span>;
  const left = daysLeft(u.expires_at);
  return (
    <div className="whitespace-nowrap">
      <div
        className={
          left < 0
            ? "font-medium text-red-600"
            : left <= 7
              ? "font-medium text-amber-600"
              : ""
        }
      >
        {fmtDate(u.expires_at)}
      </div>
      <div className="text-xs text-slate-500">
        {left < 0
          ? translate("Đã hết hạn {0} ngày", [-left])
          : left === 0
            ? translate("Hết hạn hôm nay")
            : translate("Còn {0} ngày", [left])}
      </div>
    </div>
  );
}

function CreateUserForm({
  creatable,
  packages,
  requirePackage,
  onCreated,
}: {
  creatable: Role[];
  packages: Package[];
  requirePackage: boolean;
  onCreated: () => void;
}) {
  const empty = {
    username: "",
    password: "",
    email: "",
    role: creatable[0],
    package_id: null as number | null,
    expires_at: "",
  };
  const [form, setForm] = useState(empty);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setError("");
    setSaving(true);
    try {
      await api("/users", {
        method: "POST",
        body: {
          ...form,
          expires_at:
            form.role === "user" && form.expires_at ? form.expires_at : null,
        },
      });
      setForm(empty);
      onCreated();
    } catch (err) {
      setError(errMsg(err));
    } finally {
      setSaving(false);
    }
  }

  return (
    <form onSubmit={submit} className="modal-grid">
      <div>
        <label className="label">{translate("Tên đăng nhập")}</label>
        <input
          className="input"
          required
          pattern="[a-z][a-z0-9_]{2,31}"
          title={translate("3-32 ký tự: a-z, 0-9, _ (bắt đầu bằng chữ)")}
          value={form.username}
          onChange={(e) => setForm({ ...form, username: e.target.value })}
        />
      </div>
      <div>
        <label className="label">{translate("Mật khẩu")}</label>
        <input
          className="input"
          type="password"
          required
          minLength={10}
          autoComplete="new-password"
          value={form.password}
          onChange={(e) => setForm({ ...form, password: e.target.value })}
        />
      </div>
      <div>
        <label className="label">Email</label>
        <input
          className="input"
          type="email"
          value={form.email}
          onChange={(e) => setForm({ ...form, email: e.target.value })}
        />
      </div>
      <div>
        <label className="label">{translate("Loại")}</label>
        <select
          className="input"
          value={form.role}
          onChange={(e) =>
            setForm({ ...form, role: e.target.value as Role, package_id: null })
          }
        >
          {creatable.map((r) => (
            <option key={r} value={r}>
              {translate(roleLabel[r])}
            </option>
          ))}
        </select>
      </div>
      <div>
        <label className="label">{translate("Gói dịch vụ")}</label>
        <PackageSelect
          packages={packages}
          role={form.role}
          value={form.package_id}
          required={requirePackage}
          onChange={(package_id) => setForm({ ...form, package_id })}
        />
      </div>
      {form.role === "user" && (
        <div className="sm:col-span-2">
          <label className="label">{translate("Ngày hết hạn hosting")}</label>
          <ExpiryPicker
            value={form.expires_at}
            onChange={(expires_at) => setForm({ ...form, expires_at })}
          />
          <p className="mt-1 text-xs text-slate-500">
            {translate(
              "Khi hết hạn, tài khoản tự tạm ngưng và website hiện thông báo hết hạn. Khách được nhắc qua email trước 7, 3 và 1 ngày.",
            )}
          </p>
        </div>
      )}
      <button className="btn-primary" disabled={saving}>
        {translate(
          saving ? translate("Đang tạo…") : translate("Tạo tài khoản"),
        )}
      </button>
      <div className="sm:col-span-2">
        <ErrorBox msg={error} />
      </div>
    </form>
  );
}

type Dialog =
  | { kind: "edit"; user: User; canPackage: boolean }
  | { kind: "package"; user: User }
  | { kind: "expiry"; user: User }
  | { kind: "password"; user: User }
  | { kind: "delete"; user: User };

function UserDialog({
  dialog,
  packages,
  requirePackage,
  onClose,
  onDone,
}: {
  dialog: Dialog;
  packages: Package[];
  requirePackage: boolean;
  onClose: () => void;
  onDone: () => void;
}) {
  const u = dialog.user;
  const [pkg, setPkg] = useState<number | null>(u.package_id);
  const [expiry, setExpiry] = useState(u.expires_at?.slice(0, 10) ?? "");
  const [email, setEmail] = useState(u.email ?? "");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function run(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      if (dialog.kind === "edit") {
        // Only what changed is sent; each part is checked by the server.
        if (email.trim() !== (u.email ?? ""))
          await api(`/users/${u.id}/email`, {
            method: "PATCH",
            body: { email: email.trim() },
          });
        if (dialog.canPackage && pkg !== u.package_id)
          await api(`/users/${u.id}/package`, {
            method: "PATCH",
            body: { package_id: pkg },
          });
        if (u.role === "user" && expiry !== (u.expires_at?.slice(0, 10) ?? ""))
          await api(`/users/${u.id}/expiry`, {
            method: "PATCH",
            body: { expires_at: expiry || null },
          });
        if (password)
          await api(`/users/${u.id}/password`, {
            method: "PATCH",
            body: { password },
          });
      }
      if (dialog.kind === "package")
        await api(`/users/${u.id}/package`, {
          method: "PATCH",
          body: { package_id: pkg },
        });
      if (dialog.kind === "expiry")
        await api(`/users/${u.id}/expiry`, {
          method: "PATCH",
          body: { expires_at: expiry || null },
        });
      if (dialog.kind === "password")
        await api(`/users/${u.id}/password`, {
          method: "PATCH",
          body: { password },
        });
      if (dialog.kind === "delete")
        await api(`/users/${u.id}`, { method: "DELETE" });
      onDone();
    } catch (err) {
      setError(errMsg(err));
      setBusy(false);
    }
  }

  const titles = {
    edit: translate("Sửa tài khoản"),
    package: translate("Đổi gói dịch vụ"),
    expiry: translate("Hạn sử dụng hosting"),
    password: translate("Đặt lại mật khẩu"),
    delete: translate("Xóa tài khoản"),
  };
  return (
    <Modal
      title={`${translate(titles[dialog.kind])}: ${u.username}`}
      onClose={onClose}
    >
      <form onSubmit={run} className="space-y-4">
        {dialog.kind === "edit" && (
          <>
            <div className="grid gap-3 sm:grid-cols-2">
              <div>
                <label className="label">{translate("Tên đăng nhập")}</label>
                <input className="input bg-slate-50" value={u.username} disabled />
                <p className="mt-1 text-xs text-slate-500">
                  {translate("Không đổi được (là tên user Linux và tiền tố database/FTP).")}
                </p>
              </div>
              <div>
                <label className="label">Email</label>
                <input
                  className="input"
                  type="email"
                  autoFocus
                  placeholder="khach@congty.vn"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                />
                <p className="mt-1 text-xs text-slate-500">
                  {translate("Nhận email nhắc hạn và cảnh báo.")}
                </p>
              </div>
            </div>
            {dialog.canPackage && u.role !== "admin" && (
              <div>
                <label className="label">{translate("Gói dịch vụ")}</label>
                <PackageSelect
                  packages={packages}
                  role={u.role}
                  value={pkg}
                  onChange={setPkg}
                  required={requirePackage}
                />
              </div>
            )}
            {u.role === "user" && (
              <div>
                <label className="label">{translate("Ngày hết hạn hosting")}</label>
                <ExpiryPicker
                  value={expiry}
                  base={u.expires_at?.slice(0, 10)}
                  onChange={setExpiry}
                />
                {u.suspend_reason === "expired" && (
                  <p className="mt-1 text-xs text-red-600">
                    {translate(
                      "Tài khoản đang tạm ngưng vì hết hạn: đặt ngày mới sẽ mở lại tài khoản và website ngay.",
                    )}
                  </p>
                )}
              </div>
            )}
            <div>
              <label className="label">{translate("Mật khẩu mới")}</label>
              <input
                className="input"
                type="password"
                minLength={10}
                autoComplete="new-password"
                placeholder={translate("Để trống nếu không đổi")}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
            </div>
          </>
        )}
        {dialog.kind === "package" && (
          <PackageSelect
            packages={packages}
            role={u.role}
            value={pkg}
            onChange={setPkg}
            required={requirePackage}
          />
        )}
        {dialog.kind === "expiry" && (
          <>
            <ExpiryPicker
              value={expiry}
              base={u.expires_at?.slice(0, 10)}
              onChange={setExpiry}
            />
            <p className="text-xs text-slate-500">
              {u.suspend_reason === "expired"
                ? translate(
                    "Tài khoản đang tạm ngưng vì hết hạn: đặt ngày mới (từ hôm nay trở đi) sẽ mở lại tài khoản và website ngay.",
                  )
                : translate(
                    "Khi hết hạn, tài khoản tự tạm ngưng và website hiện thông báo hết hạn. Khách được nhắc qua email trước 7, 3 và 1 ngày.",
                  )}
            </p>
          </>
        )}
        {dialog.kind === "password" && (
          <>
            <input
              className="input"
              type="password"
              required
              minLength={10}
              autoFocus
              autoComplete="new-password"
              placeholder={translate("Mật khẩu mới (tối thiểu 10 ký tự)")}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
            <p className="text-xs text-slate-500">
              {translate(
                "Mọi phiên đăng nhập hiện tại của tài khoản sẽ bị đăng xuất.",
              )}
            </p>
          </>
        )}
        {dialog.kind === "delete" && (
          <p className="text-sm text-slate-600">
            {translate("Xóa vĩnh viễn")} <b>{u.username}</b>
            {translate(
              u.linux_uid !== null &&
                translate(" cùng user Linux và toàn bộ dữ liệu trong /home/") +
                  u.username,
            )}
            {translate(". Không thể hoàn tác.")}
          </p>
        )}
        <ErrorBox msg={error} />
        <div className="modal-actions">
          <button type="button" className="btn-ghost" onClick={onClose}>
            {translate("Hủy")}
          </button>
          <button
            className={
              dialog.kind === "delete"
                ? "btn bg-red-600 text-white hover:bg-red-700"
                : "btn-primary"
            }
            disabled={busy}
          >
            {translate(
              busy
                ? translate("Đang xử lý…")
                : dialog.kind === "delete"
                  ? translate("Xóa")
                  : translate("Lưu"),
            )}
          </button>
        </div>
      </form>
    </Modal>
  );
}

export default function UsersPage() {
  const me = useSession();
  const [creating, setCreating] = useState(false);
  const [list, setList] = useState<User[]>([]);
  const [packages, setPackages] = useState<Package[]>([]);
  const [quotaOn, setQuotaOn] = useState(true);
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const [error, setError] = useState("");
  const isAdmin = me.role === "admin";
  const creatable: Role[] = isAdmin ? ["user", "reseller"] : ["user"];
  const requirePackage = !isAdmin;

  const load = useCallback(() => {
    api<{ users: User[]; quota_enabled: boolean }>("/users")
      .then((r) => {
        setList(r.users);
        setQuotaOn(r.quota_enabled);
      })
      .catch((e) => setError(errMsg(e)));
    api<{ packages: Package[] }>("/packages")
      .then((r) => setPackages(r.packages))
      .catch((e) => setError(errMsg(e)));
  }, []);
  useEffect(load, [load]);

  async function impersonate(u: User) {
    try {
      await api("/auth/impersonate", {
        method: "POST",
        body: { user_id: u.id },
      });
      window.location.href = "/dashboard/";
    } catch (e) {
      setError(errMsg(e));
    }
  }

  async function toggle(u: User) {
    try {
      await api(`/users/${u.id}/status`, {
        method: "PATCH",
        body: { status: u.status === "active" ? "suspended" : "active" },
      });
      load();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold">{translate("Tài khoản")}</h1>
        <p className="text-sm text-slate-500">
          {translate(
            isAdmin
              ? translate("Quản lý đại lý và người dùng trên máy chủ.")
              : translate("Quản lý khách hàng của bạn."),
          )}
        </p>
      </div>

      <div className="flex justify-end">
        <button className="btn-primary" onClick={() => setCreating(true)}>
          {translate("+ Tạo tài khoản")}
        </button>
      </div>
      {creating && (
        <Modal
          size="wide"
          title={translate("Tạo tài khoản")}
          onClose={() => setCreating(false)}
        >
          <CreateUserForm
            creatable={creatable}
            packages={packages}
            requirePackage={requirePackage}
            onCreated={() => {
              setCreating(false);
              load();
            }}
          />
        </Modal>
      )}
      <ErrorBox msg={error} />
      {!quotaOn && list.some((u) => u.linux_uid !== null) && (
        <p className="rounded-lg bg-amber-50 px-3 py-2 text-sm text-amber-800">
          {translate(
            "Quota ổ đĩa chưa được bật trên phân vùng chứa /home nên giới hạn dung lượng chưa có hiệu lực và chưa đo được mức sử dụng.",
          )}
        </p>
      )}

      <div className="card overflow-x-auto">
        <table className="w-full text-left text-sm">
          <thead className="border-b border-slate-200 bg-slate-50 text-xs uppercase text-slate-500">
            <tr>
              <th className="px-4 py-3">{translate("Tài khoản")}</th>
              <th className="px-4 py-3">{translate("Loại")}</th>
              {isAdmin && <th className="px-4 py-3">{translate("Thuộc")}</th>}
              <th className="px-4 py-3">{translate("Gói")}</th>
              <th className="px-4 py-3">{translate("Dung lượng")}</th>
              <th className="px-4 py-3">{translate("Hết hạn")}</th>
              <th className="px-4 py-3">{translate("Trạng thái")}</th>
              <th className="px-4 py-3" />
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100">
            {list.map((u) => {
              const manageable = u.id !== me.id && u.role !== "admin";
              const isChild = u.parent_id === me.id;
              return (
                <tr key={u.id} className="align-middle">
                  <td className="px-4 py-3">
                    <div className="font-medium">{u.username}</div>
                    <div className="text-xs text-slate-500">
                      {translate(u.email || "—")}
                    </div>
                  </td>
                  <td className="px-4 py-3">
                    <span
                      className={`rounded-full px-2 py-0.5 text-xs font-medium ${roleBadge[u.role]}`}
                    >
                      {translate(roleLabel[u.role])}
                    </span>
                  </td>
                  {isAdmin && (
                    <td className="px-4 py-3 text-slate-500">
                      {translate(u.parent_name ?? "—")}
                    </td>
                  )}
                  <td className="px-4 py-3">
                    {u.role === "admin"
                      ? "—"
                      : (u.package_name ?? (
                          <span className="text-slate-400">
                            {translate("Không giới hạn")}
                          </span>
                        ))}
                  </td>
                  <td className="px-4 py-3">
                    {u.linux_uid === null ? (
                      <span className="text-slate-400">—</span>
                    ) : (
                      <UsageBar
                        used={u.disk_used_mb ?? 0}
                        total={u.disk_limit_mb ?? 0}
                        fmt={fmtMB}
                      />
                    )}
                  </td>
                  <td className="px-4 py-3">
                    <ExpiryCell u={u} />
                  </td>
                  <td className="px-4 py-3 whitespace-nowrap">
                    {u.status === "active" ? (
                      <span className="text-emerald-600">
                        {translate("● Hoạt động")}
                      </span>
                    ) : (
                      <span className="text-red-600">
                        {u.suspend_reason === "expired"
                          ? translate("● Hết hạn")
                          : translate("● Đã khóa")}
                      </span>
                    )}
                  </td>
                  <td className="px-4 py-3">
                    {manageable && (
                      <div className="flex flex-wrap justify-end gap-1.5">
                        <button
                          onClick={() =>
                            setDialog({ kind: "edit", user: u, canPackage: isChild })
                          }
                          className="btn-ghost px-2.5 py-1 text-xs"
                        >
                          {translate("Sửa")}
                        </button>
                        {u.suspend_reason === "expired" && (
                          <button
                            onClick={() =>
                              setDialog({ kind: "expiry", user: u })
                            }
                            className="btn-ghost px-2.5 py-1 text-xs text-red-600"
                          >
                            {translate("Gia hạn")}
                          </button>
                        )}
                        {u.status === "active" && !me.impersonator && (
                          <button
                            onClick={() => impersonate(u)}
                            className="btn-ghost px-2.5 py-1 text-xs"
                            title={translate(
                              "Mở panel với quyền của tài khoản này",
                            )}
                          >
                            {translate("Đăng nhập thay")}
                          </button>
                        )}
                        <button
                          onClick={() => toggle(u)}
                          className="btn-ghost px-2.5 py-1 text-xs"
                        >
                          {translate(
                            u.status === "active"
                              ? translate("Khóa")
                              : translate("Mở khóa"),
                          )}
                        </button>
                        <button
                          onClick={() => setDialog({ kind: "delete", user: u })}
                          className="btn-ghost px-2.5 py-1 text-xs text-red-600"
                        >
                          {translate("Xóa")}
                        </button>
                      </div>
                    )}
                  </td>
                </tr>
              );
            })}
            {list.length === 0 && (
              <tr>
                <td
                  colSpan={8}
                  className="px-4 py-8 text-center text-slate-500"
                >
                  {translate("Chưa có tài khoản nào.")}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {dialog && (
        <UserDialog
          dialog={dialog}
          packages={packages}
          requirePackage={requirePackage}
          onClose={() => setDialog(null)}
          onDone={() => {
            setDialog(null);
            load();
          }}
        />
      )}
    </div>
  );
}
