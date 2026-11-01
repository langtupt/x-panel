"use client";
import { LanguageSwitcher } from "@/components/language-provider";
import { t as translate } from "@/lib/i18n";

import { FormEvent, useState } from "react";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/button";
import {
  ArrowRight,
  Database,
  Globe,
  LockKeyhole,
  Mail,
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
      <section className="relative hidden flex-col justify-between overflow-hidden bg-neutral-950 p-12 text-neutral-100 lg:flex xl:p-16">
        {/* Background: fine grid fading out, soft light at the top. */}
        <div
          aria-hidden="true"
          className="pointer-events-none absolute inset-0 opacity-[0.07]"
          style={{
            backgroundImage:
              "linear-gradient(to right, #fff 1px, transparent 1px), linear-gradient(to bottom, #fff 1px, transparent 1px)",
            backgroundSize: "44px 44px",
            maskImage:
              "radial-gradient(ellipse 80% 70% at 30% 20%, #000 30%, transparent 80%)",
            WebkitMaskImage:
              "radial-gradient(ellipse 80% 70% at 30% 20%, #000 30%, transparent 80%)",
          }}
        />
        <div
          aria-hidden="true"
          className="pointer-events-none absolute -left-32 -top-40 size-[520px] rounded-full bg-white/[0.06] blur-3xl"
        />

        <div className="relative flex items-center gap-3">
          <div className="flex size-10 items-center justify-center rounded-xl bg-white text-lg font-bold text-neutral-950 shadow-[0_0_40px_rgba(255,255,255,0.15)]">
            X
          </div>
          <span className="text-lg font-semibold tracking-tight">X-Panel</span>
        </div>

        <div className="relative max-w-lg py-12">
          <span className="inline-flex items-center gap-2 rounded-full border border-white/10 bg-white/[0.04] px-3 py-1 text-[11px] text-neutral-300">
            <span className="size-1.5 rounded-full bg-white" />
            {translate("Hosting control panel")}
          </span>
          <h2 className="mt-6 text-4xl font-semibold leading-[1.1] tracking-tight xl:text-[3.25rem]">
            {translate("Quản lý hosting")}
            <br />
            <span className="bg-gradient-to-r from-white to-neutral-500 bg-clip-text text-transparent">
              {translate("đơn giản, an toàn.")}
            </span>
          </h2>
          <p className="mt-5 max-w-md text-sm leading-7 text-neutral-400">
            {translate(
              "Website, database, email, SSL, sao lưu và bảo mật máy chủ trong một giao diện duy nhất.",
            )}
          </p>

          <div className="mt-10 grid grid-cols-2 gap-3">
            {[
              { icon: Globe, t: translate("Website & SSL"), d: translate("PHP, Node.js, Go, chứng chỉ tự động") },
              { icon: Database, t: "Database", d: "MariaDB · PostgreSQL" },
              { icon: Mail, t: "Email", d: translate("Hộp thư, DKIM, webmail") },
              { icon: ShieldCheck, t: translate("Bảo mật"), d: translate("Tường lửa, quét mã độc, 2FA") },
            ].map((f) => (
              <div
                key={f.t}
                className="rounded-xl border border-white/10 bg-white/[0.03] p-4 backdrop-blur-sm transition-colors hover:bg-white/[0.06]"
              >
                <f.icon className="mb-3 size-5 text-white" strokeWidth={1.5} />
                <div className="text-sm font-medium">{f.t}</div>
                <div className="mt-0.5 text-xs text-neutral-500">{f.d}</div>
              </div>
            ))}
          </div>
        </div>

        <div className="relative flex items-center justify-between text-[11px] text-neutral-500">
          <span>© {new Date().getFullYear()} SOC MART</span>
          <span className="flex items-center gap-1.5">
            <LockKeyhole className="size-3" />
            {translate("Kết nối được mã hóa")}
          </span>
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
                  {translate(
                    recovery
                      ? "Dùng mã 6 số từ ứng dụng"
                      : "Mất điện thoại? Dùng mã dự phòng",
                  )}
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
