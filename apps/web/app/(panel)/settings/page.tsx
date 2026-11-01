"use client";
import { t as translate } from "@/lib/i18n";

import { FormEvent, useEffect, useState } from "react";
import { Mail, Send, Server, Globe } from "lucide-react";
import { api, errMsg } from "@/lib/api";
import { ErrorBox } from "@/components/ui";
import { useSession } from "../session";

type SMTP = {
  mode: "local" | "smtp";
  enabled: boolean;
  host: string;
  port: number;
  security: "starttls" | "tls" | "none";
  username: string;
  from: string;
  from_name: string;
  password_set: boolean;
};

// Common providers: host, port, security.
const PRESETS: { label: string; host: string; port: number; security: SMTP["security"] }[] = [
  { label: "Gmail / Google Workspace", host: "smtp.gmail.com", port: 587, security: "starttls" },
  { label: "Zoho Mail", host: "smtp.zoho.com", port: 587, security: "starttls" },
  { label: "Outlook / Microsoft 365", host: "smtp.office365.com", port: 587, security: "starttls" },
  { label: "Amazon SES (Singapore)", host: "email-smtp.ap-southeast-1.amazonaws.com", port: 587, security: "starttls" },
  { label: "Mailgun", host: "smtp.mailgun.org", port: 587, security: "starttls" },
];

function SMTPCard() {
  const me = useSession();
  const [c, setC] = useState<SMTP | null>(null);
  const [password, setPassword] = useState("");
  const [changePw, setChangePw] = useState(false);
  const [testTo, setTestTo] = useState(me.email ?? "");
  const [domains, setDomains] = useState<string[]>([]);
  const [mailInstalled, setMailInstalled] = useState(true);
  useEffect(() => {
    api<{ domain: string }[]>("/mail/domains")
      .then((l) => setDomains(l.map((d) => d.domain)))
      .catch(() => setDomains([]));
    api<{ installed: boolean }>("/mail/status")
      .then((st) => setMailInstalled(st.installed))
      .catch(() => {});
  }, []);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  useEffect(() => {
    api<SMTP>("/settings/smtp")
      .then((s) => {
        setC(s);
        setChangePw(!s.password_set);
      })
      .catch((e) => setError(errMsg(e)));
  }, []);

  if (!c)
    return (
      <div className="card p-6 text-sm text-slate-500">
        {error ? <ErrorBox msg={error} /> : translate("Đang tải…")}
      </div>
    );
  const set = (p: Partial<SMTP>) => setC({ ...c, ...p });

  async function save(e: FormEvent) {
    e.preventDefault();
    if (!c) return;
    setBusy("save");
    setError("");
    setNotice("");
    try {
      const s = await api<SMTP>("/settings/smtp", {
        method: "PUT",
        body: { ...c, ...(changePw ? { password } : {}) },
      });
      setC(s);
      setPassword("");
      setChangePw(!s.password_set);
      setNotice(translate("Đã lưu cấu hình gửi email."));
    } catch (err) {
      setError(errMsg(err));
    } finally {
      setBusy("");
    }
  }

  async function test() {
    setBusy("test");
    setError("");
    setNotice("");
    try {
      await api("/settings/smtp/test", { method: "POST", body: { to: testTo } });
      setNotice(translate("Đã gửi email thử tới {0}. Kiểm tra hộp thư (cả mục Spam).", [testTo]));
    } catch (err) {
      setError(errMsg(err));
    } finally {
      setBusy("");
    }
  }

  return (
    <form onSubmit={save} className="card space-y-5 p-6">
      <div className="flex items-start justify-between gap-3">
        <div className="flex items-center gap-3">
          <span className="flex size-10 items-center justify-center rounded-xl bg-slate-100">
            <Mail className="size-5" />
          </span>
          <div>
            <h2 className="font-semibold">{translate("Email gửi cho khách")}</h2>
            <p className="text-sm text-slate-500">
              {translate(
                "Dùng để gửi thông báo hết hạn hosting, cảnh báo và báo cáo cho khách hàng.",
              )}
            </p>
          </div>
        </div>
      </div>

      <div className="grid gap-3 md:grid-cols-2">
        {(
          [
            {
              id: "local",
              icon: Server,
              title: translate("Mail server của máy chủ này"),
              text: translate(
                "Gửi bằng địa chỉ thuộc một tên miền ở menu Email (ví dụ noreply@congty.vn), ký DKIM tự động. Không cần tài khoản ngoài.",
              ),
            },
            {
              id: "smtp",
              icon: Globe,
              title: translate("SMTP bên ngoài"),
              text: translate(
                "Gmail, Zoho, Microsoft 365, Amazon SES… Dùng khi máy chủ không gửi thư trực tiếp được (cổng 25 bị chặn).",
              ),
            },
          ] as const
        ).map((m) => (
          <button
            type="button"
            key={m.id}
            onClick={() => set({ mode: m.id })}
            className={`rounded-xl border p-4 text-left ${c.mode === m.id ? "border-slate-900 ring-1 ring-slate-900" : "hover:bg-slate-50"}`}
          >
            <div className="mb-1 flex items-center gap-2 font-medium">
              <m.icon className="size-4" /> {m.title}
            </div>
            <p className="text-xs text-slate-500">{m.text}</p>
          </button>
        ))}
      </div>

      {c.mode === "local" && (
        <div className="space-y-4">
          {!mailInstalled && (
            <p className="rounded-lg bg-amber-50 px-3 py-2 text-sm text-amber-800">
              {translate(
                "Máy chủ chưa cài Mail server: vào trang Ứng dụng cài \"Mail server\" trước, hoặc dùng SMTP bên ngoài.",
              )}
            </p>
          )}
          <div className="grid gap-4 md:grid-cols-2">
            <div>
              <label className="label">{translate("Địa chỉ gửi")}</label>
              <div className="flex items-center overflow-hidden rounded-lg border border-slate-300">
                <input
                  className="min-w-0 flex-1 px-3 py-2 text-sm outline-none"
                  placeholder="noreply"
                  value={c.from.split("@")[0] ?? ""}
                  onChange={(e) =>
                    set({
                      from:
                        e.target.value.toLowerCase().replace(/[^a-z0-9._-]/g, "") +
                        "@" +
                        (c.from.split("@")[1] || domains[0] || ""),
                    })
                  }
                />
                <select
                  className="max-w-[60%] border-l bg-slate-50 px-2 py-2 text-sm outline-none"
                  value={c.from.split("@")[1] ?? ""}
                  onChange={(e) =>
                    set({ from: (c.from.split("@")[0] || "noreply") + "@" + e.target.value })
                  }
                >
                  <option value="">{translate("— chọn tên miền —")}</option>
                  {domains.map((d) => (
                    <option key={d} value={d}>
                      @{d}
                    </option>
                  ))}
                </select>
              </div>
              <p className="mt-1 text-xs text-slate-500">
                {domains.length === 0
                  ? translate(
                      "Chưa có tên miền email: thêm tên miền ở menu Email (và tạo bản ghi DNS SPF/DKIM) để thư không vào Spam. Để trống sẽ gửi từ noreply@<tên máy chủ thư>.",
                    )
                  : translate(
                      "Không cần tạo hộp thư cho địa chỉ này. Nhớ tạo đủ bản ghi DNS (SPF, DKIM, DMARC) ở tab DNS của tên miền để thư vào hộp thư đến.",
                    )}
              </p>
            </div>
            <div>
              <label className="label">{translate("Tên người gửi")}</label>
              <input
                className="input"
                placeholder={translate("Hosting Công ty ABC")}
                value={c.from_name}
                onChange={(e) => set({ from_name: e.target.value })}
              />
            </div>
          </div>
        </div>
      )}

      {c.mode === "smtp" && (
      <>
      <div>
        <label className="label">{translate("Chọn nhanh")}</label>
        <div className="flex flex-wrap gap-1.5">
          {PRESETS.map((p) => (
            <button
              type="button"
              key={p.host}
              className={`rounded-md border px-2 py-0.5 text-xs ${c.host === p.host ? "border-brand-500 bg-brand-50 text-brand-700" : "border-slate-300 hover:bg-slate-50"}`}
              onClick={() => set({ host: p.host, port: p.port, security: p.security })}
            >
              {p.label}
            </button>
          ))}
        </div>
      </div>

      <div className="grid gap-4 md:grid-cols-[1fr_120px_180px]">
        <div>
          <label className="label">{translate("Máy chủ SMTP")}</label>
          <input
            className="input font-mono"
            required
            placeholder="smtp.gmail.com"
            value={c.host}
            onChange={(e) => set({ host: e.target.value.trim() })}
          />
        </div>
        <div>
          <label className="label">{translate("Cổng")}</label>
          <input
            className="input"
            type="number"
            min={1}
            max={65535}
            value={c.port}
            onChange={(e) => set({ port: Number(e.target.value) })}
          />
        </div>
        <div>
          <label className="label">{translate("Bảo mật")}</label>
          <select
            className="input"
            value={c.security}
            onChange={(e) => {
              const security = e.target.value as SMTP["security"];
              set({ security, port: security === "tls" ? 465 : security === "starttls" ? 587 : 25 });
            }}
          >
            <option value="starttls">STARTTLS (587)</option>
            <option value="tls">SSL/TLS (465)</option>
            <option value="none">{translate("Không mã hóa (25)")}</option>
          </select>
        </div>
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <div>
          <label className="label">{translate("Tên đăng nhập")}</label>
          <input
            className="input"
            autoComplete="off"
            placeholder="noreply@congty.vn"
            value={c.username}
            onChange={(e) => set({ username: e.target.value })}
          />
        </div>
        <div>
          <div className="mb-1 flex items-center justify-between">
            <label className="label mb-0">{translate("Mật khẩu")}</label>
            {c.password_set && (
              <label className="flex items-center gap-1.5 text-xs">
                <input
                  type="checkbox"
                  checked={changePw}
                  onChange={(e) => setChangePw(e.target.checked)}
                />
                {translate("Đổi mật khẩu")}
              </label>
            )}
          </div>
          {changePw ? (
            <input
              className="input"
              type="password"
              autoComplete="new-password"
              placeholder={translate("Mật khẩu ứng dụng (App password)")}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          ) : (
            <p className="py-2 text-sm text-slate-500">{translate("Đã lưu (giữ nguyên).")}</p>
          )}
        </div>
        <div>
          <label className="label">{translate("Địa chỉ gửi (From)")}</label>
          <input
            className="input"
            type="email"
            required
            placeholder="noreply@congty.vn"
            value={c.from}
            onChange={(e) => set({ from: e.target.value.trim() })}
          />
        </div>
        <div>
          <label className="label">{translate("Tên người gửi")}</label>
          <input
            className="input"
            placeholder={translate("Hosting Công ty ABC")}
            value={c.from_name}
            onChange={(e) => set({ from_name: e.target.value })}
          />
        </div>
      </div>
      <p className="text-xs text-slate-500">
        {translate(
          "Gmail/Google Workspace: bật xác minh 2 bước rồi tạo \"Mật khẩu ứng dụng\" để dùng ở đây; địa chỉ gửi phải là tài khoản Gmail đó.",
        )}
      </p>
      </>
      )}

      <ErrorBox msg={error} />
      {notice && (
        <div className="rounded-lg border border-emerald-200 bg-emerald-50 px-4 py-2 text-sm text-emerald-800">
          {notice}
        </div>
      )}

      <div className="flex flex-wrap items-end justify-between gap-3 border-t pt-4">
        <div className="flex items-end gap-2">
          <div>
            <label className="label">{translate("Gửi email thử tới")}</label>
            <input
              className="input w-64"
              type="email"
              value={testTo}
              onChange={(e) => setTestTo(e.target.value)}
            />
          </div>
          <button
            type="button"
            className="btn-ghost"
            disabled={busy !== "" || !testTo}
            onClick={test}
            title={translate("Lưu trước khi gửi thử")}
          >
            <Send className="mr-1 inline size-4" />
            {busy === "test" ? translate("Đang gửi…") : translate("Gửi thử")}
          </button>
        </div>
        <button className="btn-primary" disabled={busy !== ""}>
          {busy === "save" ? translate("Đang lưu…") : translate("Lưu cấu hình")}
        </button>
      </div>
    </form>
  );
}

export default function SettingsPage() {
  const me = useSession();
  if (me.role !== "admin")
    return <p className="text-sm text-slate-500">{translate("Chỉ quản trị viên.")}</p>;
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold">{translate("Cài đặt hệ thống")}</h1>
        <p className="text-sm text-slate-500">
          {translate("Thiết lập chung của máy chủ.")}
        </p>
      </div>
      <SMTPCard />
      <div className="card p-6 text-sm text-slate-600">
        <h2 className="mb-2 font-semibold text-slate-900">
          {translate("Thông báo hết hạn hosting")}
        </h2>
        <ul className="list-disc space-y-1 pl-5">
          <li>{translate("Đặt ngày hết hạn ở trang Tài khoản (nút \"Hạn dùng\").")}</li>
          <li>{translate("Khách nhận email nhắc trước 7, 3 và 1 ngày (tới địa chỉ email của tài khoản).")}</li>
          <li>
            {translate(
              "Đến hạn: tài khoản tự tạm ngưng, website hiện trang \"đã hết hạn, vui lòng liên hệ quản trị viên để gia hạn\", khách nhận email báo hết hạn.",
            )}
          </li>
          <li>{translate("Gia hạn (đổi sang ngày mới) sẽ tự mở lại tài khoản và báo cho khách.")}</li>
        </ul>
      </div>
    </div>
  );
}
