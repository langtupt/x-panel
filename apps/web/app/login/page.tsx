"use client";
import { LanguageSwitcher } from "@/components/language-provider";
import { t as translate } from "@/lib/i18n";

import { FormEvent, useState } from "react";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/button";
import {
  ArrowRight,
  Box,
  Check,
  LockKeyhole,
  Server,
  ShieldCheck,
} from "lucide-react";
import { api, ApiError } from "@/lib/api";

export default function LoginPage() {
  const router = useRouter();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [recovery, setRecovery] = useState(false);
  const [needCode, setNeedCode] = useState(false);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError("");
    setLoading(true);
    try {
      await api("/auth/login", {
        method: "POST",
        body: { username, password, code },
      });
      router.replace("/dashboard/");
    } catch (err) {
      if (err instanceof ApiError && err.data.totp_required) {
        setNeedCode(true);
      } else {
        setError(
          err instanceof ApiError
            ? err.message
            : translate("Không kết nối được máy chủ"),
        );
      }
      setLoading(false);
    }
  }

  return (
    <main className="grid min-h-screen lg:grid-cols-2">
      <div className="absolute right-4 top-4 z-10">
        <LanguageSwitcher />
      </div>
      <section className="relative hidden flex-col justify-between overflow-hidden border-r bg-neutral-950 p-12 text-neutral-100 lg:flex">
        <div className="flex items-center gap-3">
          <div className="flex size-9 items-center justify-center rounded-xl bg-white font-bold text-neutral-950">
            X
          </div>
          <span className="font-semibold tracking-tight">X-Panel</span>
          <span className="ml-1 rounded-full border border-white/20 px-2 py-0.5 text-[10px] text-neutral-400">
            Hosting workspace
          </span>
        </div>
        <div className="relative z-10 max-w-md py-16">
          <div className="mb-8 flex size-14 items-center justify-center rounded-2xl border border-white/15 bg-white/5">
            <Server className="size-7" strokeWidth={1.3} />
          </div>
          <p className="mb-4 text-xs tracking-[0.2em] text-neutral-500 uppercase">
            Built for your infrastructure
          </p>
          <h2 className="text-4xl font-medium leading-tight tracking-tight xl:text-5xl">
            {translate("Một workspace.")} <br />
            <span className="text-neutral-500">
              {translate("Toàn bộ hạ tầng.")}
            </span>
          </h2>
          <p className="mt-6 max-w-sm text-sm leading-7 text-neutral-400">
            {translate(
              "Quản lý website, ứng dụng và tài nguyên máy chủ trong một giao diện thống nhất.",
            )}
          </p>
          <div className="mt-10 grid gap-4 text-xs text-neutral-300">
            {[
              translate("Website & runtime đa nền tảng"),
              translate("Database & tài khoản tập trung"),
              translate("Theo dõi và vận hành máy chủ"),
            ].map((label) => (
              <div key={label} className="flex items-center gap-3">
                <Check className="size-3.5 text-neutral-500" />
                {translate(label)}
              </div>
            ))}
          </div>
        </div>
        <p className="text-[10px] text-neutral-500">
          X-Panel · Your hosting, under control.
        </p>
        <div
          aria-hidden="true"
          className="pointer-events-none absolute -right-40 -bottom-40 size-[500px] rounded-full border border-white/5 p-14"
        >
          <div className="size-full rounded-full border border-white/5 p-14">
            <div className="size-full rounded-full border border-white/5" />
          </div>
        </div>
      </section>
      <section className="flex flex-col items-center justify-center bg-background px-6 py-12">
        <div className="w-full max-w-[350px]">
          <div className="mb-10 flex items-center gap-2 text-sm font-semibold lg:hidden">
            <div className="flex size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
              X
            </div>
            X-Panel
          </div>
          <div className="mb-8">
            <div className="mb-5 flex size-10 items-center justify-center rounded-xl border bg-muted/40">
              <LockKeyhole className="size-4" />
            </div>
            <h1 className="text-2xl font-semibold tracking-tight">
              {translate(
                needCode
                  ? translate("Xác thực tài khoản")
                  : translate("Chào mừng trở lại"),
              )}
            </h1>
            <p className="mt-2 text-sm text-muted-foreground">
              {translate(
                needCode
                  ? translate("Nhập mã từ ứng dụng xác thực để tiếp tục.")
                  : translate("Đăng nhập để truy cập workspace của bạn."),
              )}
            </p>
          </div>
          <form onSubmit={onSubmit} className="space-y-5">
            <div className={needCode ? "hidden" : "space-y-5"}>
              <div>
                <label className="label" htmlFor="username">
                  {translate("Tên đăng nhập")}
                </label>
                <input
                  id="username"
                  className="input h-10"
                  autoComplete="username"
                  autoFocus
                  required
                  placeholder={translate("Nhập tên tài khoản")}
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                />
              </div>
              <div>
                <label className="label" htmlFor="password">
                  {translate("Mật khẩu")}
                </label>
                <input
                  id="password"
                  type="password"
                  className="input h-10"
                  autoComplete="current-password"
                  required
                  placeholder={translate("Nhập mật khẩu")}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                />
              </div>
            </div>
            {needCode && (
              <div>
                <label className="label" htmlFor="code">
                  {translate("Mã xác thực 2 lớp")}
                </label>
                {recovery ? (
                  <input
                    id="code"
                    key="recovery"
                    className="input h-12 text-center font-mono text-lg tracking-[0.2em]"
                    autoFocus
                    required
                    autoComplete="off"
                    placeholder="abcde-fghij"
                    pattern="[A-Za-z2-7]{5}-?[A-Za-z2-7]{5}"
                    maxLength={11}
                    value={code}
                    onChange={(e) => setCode(e.target.value.trim())}
                  />
                ) : (
                  <input
                    id="code"
                    key="totp"
                    className="input h-12 text-center font-mono text-lg tracking-[0.4em]"
                    autoFocus
                    required
                    inputMode="numeric"
                    autoComplete="one-time-code"
                    pattern="[0-9]{6}"
                    maxLength={6}
                    value={code}
                    onChange={(e) => setCode(e.target.value.replace(/\D/g, ""))}
                  />
                )}
                <button
                  type="button"
                  className="mt-2 text-xs text-muted-foreground hover:underline"
                  onClick={() => {
                    setRecovery(!recovery);
                    setCode("");
                  }}
                >
                  {translate(recovery ? "Dùng mã 6 số từ ứng dụng" : "Mất điện thoại? Dùng mã dự phòng")}
                </button>
              </div>
            )}
            {error && (
              <p
                role="alert"
                className="rounded-lg border border-destructive/20 bg-destructive/5 px-3 py-2 text-xs text-destructive"
              >
                {translate(error)}
              </p>
            )}
            <Button type="submit" className="h-10 w-full" disabled={loading}>
              {translate(
                loading
                  ? translate("Đang đăng nhập…")
                  : needCode
                    ? translate("Xác nhận")
                    : translate("Đăng nhập"),
              )}
              <ArrowRight />
            </Button>
            {needCode && (
              <button
                type="button"
                className="w-full text-xs text-muted-foreground hover:underline"
                onClick={() => {
                  setNeedCode(false);
                  setCode("");
                  setError("");
                }}
              >
                {translate("Quay lại")}
              </button>
            )}
          </form>
          <div className="mt-8 flex items-center justify-center gap-2 border-t pt-6 text-[10px] text-muted-foreground">
            <ShieldCheck className="size-3.5" />
            {translate("Không chia sẻ thông tin đăng nhập của bạn.")}
          </div>
        </div>
      </section>
    </main>
  );
}
