"use client";
import { localeTag } from "@/lib/i18n";
import { t as translate } from "@/lib/i18n";

import { useCallback, useEffect, useState } from "react";
import { api, errMsg, type AuditRow } from "@/lib/api";
import { ErrorBox } from "@/components/ui";

const actionLabel: Record<string, string> = {
  "auth.login": "Đăng nhập",
  "auth.login_failed": "Đăng nhập thất bại",
  "auth.impersonate": "Đăng nhập thay",
  "auth.password_change": "Đổi mật khẩu",
  "auth.2fa_enable": "Bật 2FA",
  "auth.2fa_disable": "Tắt 2FA",
  "user.create": "Tạo tài khoản",
  "user.delete": "Xóa tài khoản",
  "user.status": "Đổi trạng thái tài khoản",
  "user.package": "Đổi gói",
  "user.password": "Đặt lại mật khẩu",
  "package.create": "Tạo gói",
  "package.update": "Sửa gói",
  "package.delete": "Xóa gói",
  "site.create": "Tạo website",
  "site.update": "Sửa website",
  "site.delete": "Xóa website",
  "site.ssl_issue": "Cấp SSL",
  "site.ssl_disable": "Tắt SSL",
  "database.create": "Tạo database",
  "database.password": "Đổi mật khẩu database",
  "database.delete": "Xóa database",
  "ftp.create": "Tạo FTP",
  "ftp.password": "Đổi mật khẩu FTP",
  "ftp.delete": "Xóa FTP",
  "cron.create": "Tạo cron",
  "cron.update": "Sửa cron",
  "cron.delete": "Xóa cron",
  "php.install": "Cài PHP",
  "pma.install": "Cài phpMyAdmin",
};

function details(meta: Record<string, unknown>) {
  return Object.entries(meta)
    .filter(
      ([, v]) =>
        v !== null && v !== "" && !(Array.isArray(v) && v.length === 0),
    )
    .map(([k, v]) => `${k}=${typeof v === "string" ? v : JSON.stringify(v)}`)
    .join("  ");
}

export default function AuditPage() {
  const [rows, setRows] = useState<AuditRow[]>([]);
  const [more, setMore] = useState(true);
  const [error, setError] = useState("");

  const load = useCallback((before?: number) => {
    api<AuditRow[]>(`/audit${before ? `?before=${before}` : ""}`)
      .then((r) => {
        setRows((cur) => (before ? [...cur, ...r] : r));
        setMore(r.length === 100);
      })
      .catch((e) => setError(errMsg(e)));
  }, []);
  useEffect(() => load(), [load]);

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold">
          {translate("Nhật ký hoạt động")}
        </h1>
        <p className="text-sm text-slate-500">
          {translate("Mọi thao tác thay đổi dữ liệu trên panel.")}
        </p>
      </div>
      <ErrorBox msg={error} />
      <div className="card overflow-x-auto">
        <table className="w-full text-left text-sm">
          <thead className="border-b border-slate-200 bg-slate-50 text-xs uppercase text-slate-500">
            <tr>
              <th className="px-4 py-3">{translate("Thời gian")}</th>
              <th className="px-4 py-3">{translate("Người thực hiện")}</th>
              <th className="px-4 py-3">{translate("Hành động")}</th>
              <th className="px-4 py-3">{translate("Chi tiết")}</th>
              <th className="px-4 py-3">IP</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100">
            {rows.map((a) => (
              <tr
                key={a.id}
                className={a.action.endsWith("_failed") ? "bg-red-50/50" : ""}
              >
                <td className="px-4 py-2 text-xs whitespace-nowrap text-slate-500">
                  {translate(
                    new Date(a.created_at).toLocaleString(localeTag()),
                  )}
                </td>
                <td className="px-4 py-2">{translate(a.actor ?? "—")}</td>
                <td className="px-4 py-2 whitespace-nowrap">
                  {translate(actionLabel[a.action] ?? a.action)}
                </td>
                <td className="max-w-lg px-4 py-2 font-mono text-xs break-all text-slate-600">
                  {translate(details(a.meta))}
                </td>
                <td className="px-4 py-2 font-mono text-xs text-slate-500">
                  {translate(a.ip)}
                </td>
              </tr>
            ))}
            {rows.length === 0 && (
              <tr>
                <td
                  colSpan={5}
                  className="px-4 py-8 text-center text-slate-500"
                >
                  {translate("Chưa có hoạt động nào.")}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      {more && rows.length > 0 && (
        <button
          className="btn-ghost"
          onClick={() => load(rows[rows.length - 1].id)}
        >
          {translate("Xem thêm")}
        </button>
      )}
    </div>
  );
}
