"use client";
import { t as translate } from "@/lib/i18n";

import { FormEvent, useEffect, useState } from "react";
import QRCode from "qrcode";
import { api, errMsg, roleLabel } from "@/lib/api";
import { ErrorBox } from "@/components/ui";
import { useSession } from "../session";

function PasswordCard() {
  const me = useSession();
  const [f, setF] = useState({ current: "", next: "", confirm: "" });
  const [msg, setMsg] = useState("");
  const [error, setError] = useState("");

  async function submit(e: FormEvent) {
    e.preventDefault();
    setMsg("");
    setError("");
    if (f.next !== f.confirm)
      return setError(translate("Mật khẩu nhập lại không khớp"));
    try {
      await api("/me/password", {
        method: "POST",
        body: { current: f.current, new: f.next },
      });
      setF({ current: "", next: "", confirm: "" });
      setMsg(
        me.role === "user"
          ? translate(
              "Đã đổi mật khẩu. Các phiên khác đã bị đăng xuất; SFTP dùng mật khẩu mới.",
            )
          : translate("Đã đổi mật khẩu. Các phiên khác đã bị đăng xuất."),
      );
    } catch (err) {
      setError(errMsg(err));
    }
  }

  return (
    <form onSubmit={submit} className="card space-y-4 p-5">
      <h2 className="font-semibold">{translate("Đổi mật khẩu")}</h2>
      <div className="grid gap-4 md:grid-cols-3">
        <div>
          <label className="label">{translate("Mật khẩu hiện tại")}</label>
          <input
            className="input"
            type="password"
            required
            autoComplete="current-password"
            value={f.current}
            onChange={(e) => setF({ ...f, current: e.target.value })}
          />
        </div>
        <div>
          <label className="label">{translate("Mật khẩu mới")}</label>
          <input
            className="input"
            type="password"
            required
            minLength={10}
            autoComplete="new-password"
            value={f.next}
            onChange={(e) => setF({ ...f, next: e.target.value })}
          />
        </div>
        <div>
          <label className="label">{translate("Nhập lại")}</label>
          <input
            className="input"
            type="password"
            required
            minLength={10}
            autoComplete="new-password"
            value={f.confirm}
            onChange={(e) => setF({ ...f, confirm: e.target.value })}
          />
        </div>
      </div>
      {msg && (
        <p className="rounded-lg bg-emerald-50 px-3 py-2 text-sm text-emerald-800">
          {translate(msg)}
        </p>
      )}
      <ErrorBox msg={error} />
      <button className="btn-primary">{translate("Đổi mật khẩu")}</button>
    </form>
  );
}

function TwoFactorCard() {
  const me = useSession();
  const [enabled, setEnabled] = useState(me.totp_enabled);
  const [setup, setSetup] = useState<{
    secret: string;
    url: string;
    qr: string;
  } | null>(null);
  const [code, setCode] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [msg, setMsg] = useState("");

  async function start() {
    setError("");
    try {
      const s = await api<{ secret: string; url: string }>("/me/2fa/setup", {
        method: "POST",
      });
      setSetup({
        ...s,
        qr: await QRCode.toDataURL(s.url, { margin: 1, width: 200 }),
      });
    } catch (err) {
      setError(errMsg(err));
    }
  }

  async function enable(e: FormEvent) {
    e.preventDefault();
    setError("");
    try {
      await api("/me/2fa/enable", {
        method: "POST",
        body: { secret: setup!.secret, code },
      });
      setEnabled(true);
      setSetup(null);
      setCode("");
      setMsg(
        translate(
          "Đã bật xác thực 2 lớp. Lần đăng nhập sau sẽ cần mã từ ứng dụng.",
        ),
      );
    } catch (err) {
      setError(errMsg(err));
    }
  }

  async function disable(e: FormEvent) {
    e.preventDefault();
    setError("");
    try {
      await api("/me/2fa/disable", {
        method: "POST",
        body: { password, code },
      });
      setEnabled(false);
      setPassword("");
      setCode("");
      setMsg(translate("Đã tắt xác thực 2 lớp."));
    } catch (err) {
      setError(errMsg(err));
    }
  }

  return (
    <div className="card space-y-4 p-5">
      <div className="flex items-center justify-between">
        <h2 className="font-semibold">{translate("Xác thực 2 lớp (TOTP)")}</h2>
        <span
          className={`rounded-full px-2 py-0.5 text-xs font-medium ${enabled ? "bg-emerald-50 text-emerald-700" : "bg-slate-100 text-slate-600"}`}
        >
          {translate(enabled ? translate("Đang bật") : translate("Đang tắt"))}
        </span>
      </div>
      {msg && (
        <p className="rounded-lg bg-emerald-50 px-3 py-2 text-sm text-emerald-800">
          {translate(msg)}
        </p>
      )}

      {!enabled && !setup && (
        <>
          <p className="text-sm text-slate-600">
            {translate(
              "Dùng Google Authenticator, Authy, 1Password… để tạo mã 6 số khi đăng nhập.",
            )}
          </p>
          <button className="btn-primary" onClick={start}>
            {translate("Bật xác thực 2 lớp")}
          </button>
        </>
      )}

      {!enabled && setup && (
        <form onSubmit={enable} className="flex flex-col gap-5 sm:flex-row">
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img
            src={setup.qr}
            alt={translate("Mã QR xác thực 2 lớp")}
            width={200}
            height={200}
            className="rounded-lg border border-slate-200"
          />
          <div className="space-y-3 text-sm">
            <p>
              {translate(
                "1. Quét mã QR bằng ứng dụng xác thực, hoặc nhập khóa thủ công:",
              )}
            </p>
            <code className="block break-all rounded bg-slate-100 px-2 py-1 font-mono">
              {setup.secret}
            </code>
            <p>{translate("2. Nhập mã 6 số ứng dụng hiển thị để xác nhận:")}</p>
            <input
              className="input w-40 text-center font-mono tracking-[0.3em]"
              required
              inputMode="numeric"
              pattern="[0-9]{6}"
              maxLength={6}
              value={code}
              onChange={(e) => setCode(e.target.value.replace(/\D/g, ""))}
            />
            <div className="flex gap-2">
              <button className="btn-primary">
                {translate("Xác nhận và bật")}
              </button>
              <button
                type="button"
                className="btn-ghost"
                onClick={() => setSetup(null)}
              >
                {translate("Hủy")}
              </button>
            </div>
          </div>
        </form>
      )}

      {enabled && (
        <form
          onSubmit={disable}
          className="grid gap-3 md:grid-cols-3 md:items-end"
        >
          <div>
            <label className="label">{translate("Mật khẩu")}</label>
            <input
              className="input"
              type="password"
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </div>
          <div>
            <label className="label">{translate("Mã 6 số hiện tại")}</label>
            <input
              className="input font-mono"
              required
              inputMode="numeric"
              pattern="[0-9]{6}"
              maxLength={6}
              value={code}
              onChange={(e) => setCode(e.target.value.replace(/\D/g, ""))}
            />
          </div>
          <button className="btn-ghost text-red-600">
            {translate("Tắt xác thực 2 lớp")}
          </button>
        </form>
      )}
      <ErrorBox msg={error} />
    </div>
  );
}

export default function AccountPage() {
  const me = useSession();
  const [host, setHost] = useState("");
  useEffect(() => setHost(window.location.hostname), []);

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold">
          {translate("Tài khoản của tôi")}
        </h1>
        <p className="text-sm text-slate-500">
          {me.username} · {translate(roleLabel[me.role])}
          {translate(me.email && ` · ${me.email}`)}
        </p>
      </div>
      {me.impersonator ? (
        <p className="rounded-lg bg-amber-50 px-3 py-2 text-sm text-amber-800">
          {translate(
            "Đang đăng nhập thay: không thể đổi mật khẩu hay xác thực 2 lớp của tài khoản này.",
          )}
        </p>
      ) : (
        <>
          <PasswordCard />
          <TwoFactorCard />
        </>
      )}
      {me.role === "user" && (
        <div className="card p-5 text-sm text-slate-600">
          <h2 className="mb-2 font-semibold text-slate-800">SFTP</h2>
          Host <code>{host}</code> {translate(", cổng")} <code>22</code>, user
          {translate(" ")}
          <code>{me.username}</code> {translate(", mật khẩu = mật khẩu panel.")}
        </div>
      )}
    </div>
  );
}
