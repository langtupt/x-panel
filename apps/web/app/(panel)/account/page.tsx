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

/** Recovery codes are shown exactly once, right after they are issued. */
function RecoveryCodes({ codes, user, onClose }: { codes: string[]; user: string; onClose: () => void }) {
  const [copied, setCopied] = useState(false);
  const text = `X-Panel — ${translate("Mã dự phòng 2FA")} (${user})\n${codes.join("\n")}\n`;
  function download() {
    const a = document.createElement("a");
    a.href = URL.createObjectURL(new Blob([text], { type: "text/plain" }));
    a.download = `xpanel-recovery-codes-${user}.txt`;
    a.click();
    URL.revokeObjectURL(a.href);
  }
  return (
    <div className="space-y-3 rounded-lg border border-amber-200 bg-amber-50 p-4">
      <div className="text-sm font-medium text-amber-900">{translate("Lưu các mã dự phòng này ngay")}</div>
      <p className="text-xs text-amber-800">
        {translate("Khi mất điện thoại, nhập một mã thay cho mã 6 số lúc đăng nhập. Mỗi mã dùng được một lần; mã chỉ hiển thị lần này.")}
      </p>
      <div className="grid grid-cols-2 gap-2 rounded-lg bg-white p-3 font-mono text-sm sm:grid-cols-5">
        {codes.map((c) => <span key={c}>{c}</span>)}
      </div>
      <div className="flex flex-wrap gap-2">
        <button type="button" className="btn-ghost"
          onClick={() => navigator.clipboard.writeText(text).then(() => setCopied(true))}>
          {translate(copied ? "Đã chép" : "Sao chép")}
        </button>
        <button type="button" className="btn-ghost" onClick={download}>{translate("Tải file .txt")}</button>
        <button type="button" className="btn-primary" onClick={onClose}>{translate("Tôi đã lưu")}</button>
      </div>
    </div>
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
  const [codes, setCodes] = useState<string[] | null>(null);
  const [remaining, setRemaining] = useState<number | null>(null);
  const [regen, setRegen] = useState({ password: "", code: "" });
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
      const r = await api<{ recovery_codes: string[] }>("/me/2fa/enable", {
        method: "POST",
        body: { secret: setup!.secret, code },
      });
      setCodes(r.recovery_codes);
      setRemaining(r.recovery_codes.length);
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

  useEffect(() => {
    if (enabled)
      api<{ remaining: number }>("/me/2fa/recovery-codes")
        .then((r) => setRemaining(r.remaining))
        .catch(() => setRemaining(null));
  }, [enabled]);

  async function regenerate(e: FormEvent) {
    e.preventDefault();
    setError("");
    try {
      const r = await api<{ recovery_codes: string[] }>("/me/2fa/recovery-codes", {
        method: "POST",
        body: regen,
      });
      setCodes(r.recovery_codes);
      setRemaining(r.recovery_codes.length);
      setRegen({ password: "", code: "" });
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

      {codes && <RecoveryCodes codes={codes} user={me.username} onClose={() => setCodes(null)} />}

      {enabled && !codes && (
        <form onSubmit={regenerate} className="space-y-3 rounded-lg border border-slate-200 p-4">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div>
              <div className="text-sm font-medium">{translate("Mã dự phòng")}</div>
              <div className="text-xs text-slate-500">
                {translate("Dùng để đăng nhập khi mất điện thoại. Mỗi mã dùng được một lần.")}
                {remaining !== null && (
                  <> <b className={remaining <= 2 ? "text-red-600" : ""}>{translate("Còn {0} mã", [remaining])}</b></>
                )}
              </div>
            </div>
          </div>
          <div className="grid gap-3 md:grid-cols-3 md:items-end">
            <div>
              <label className="label">{translate("Mật khẩu")}</label>
              <input className="input" type="password" required value={regen.password}
                onChange={(e) => setRegen({ ...regen, password: e.target.value })} />
            </div>
            <div>
              <label className="label">{translate("Mã 6 số hiện tại")}</label>
              <input className="input font-mono" required inputMode="numeric" pattern="[0-9]{6}" maxLength={6}
                value={regen.code} onChange={(e) => setRegen({ ...regen, code: e.target.value.replace(/D/g, "") })} />
            </div>
            <button className="btn-ghost">{translate("Tạo bộ mã mới")}</button>
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
