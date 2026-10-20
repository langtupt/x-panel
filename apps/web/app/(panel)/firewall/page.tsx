"use client";
import { localeTag } from "@/lib/i18n";
import { t as translate } from "@/lib/i18n";

import { FormEvent, useCallback, useEffect, useState } from "react";
import { Ban, ShieldCheck, ShieldOff, Unlock } from "lucide-react";
import { api, errMsg } from "@/lib/api";
import { ErrorBox } from "@/components/ui";

type Config = {
  enabled: boolean;
  syn_rate: number;
  conn_limit: number;
  http_rate: number;
  http_burst: number;
  http_conn: number;
  max_retry: number;
  find_min: number;
  ban_min: number;
  increment: boolean;
  flood_ban: boolean;
  panel_ip_fails: number;
  panel_ban_min: number;
  account_fails: number;
  account_lock_min: number;
  allow: string[];
  block: string[];
};
type State = {
  config: Config;
  active: boolean;
  bans: { ip: string; reason: string; expires_at: string | null }[];
  locked_accounts: { key: string; until: string }[];
  your_ip: string;
};

function Num({ label, hint, value, onChange }: { label: string; hint?: string; value: number; onChange: (v: number) => void }) {
  return (
    <div>
      <label className="label">{translate(label)}</label>
      <input className="input" type="number" min={0} value={value} onChange={(e) => onChange(Number(e.target.value) || 0)} />
      {hint && <p className="mt-1 text-[11px] text-muted-foreground">{translate(hint)}</p>}
    </div>
  );
}

function reasonLabel(r: string) {
  if (r === "panel-login") return translate("Đăng nhập panel sai nhiều lần");
  if (r === "f2b-sshd") return translate("SSH/SFTP sai mật khẩu");
  if (r === "f2b-nginx-limit-req") return translate("Gửi quá nhiều request (HTTP flood)");
  if (r.startsWith("f2b-")) return "fail2ban: " + r.slice(4);
  return r || "—";
}

export default function FirewallPage() {
  const [st, setSt] = useState<State | null>(null);
  const [c, setC] = useState<Config | null>(null);
  const [allow, setAllow] = useState("");
  const [block, setBlock] = useState("");
  const [ban, setBan] = useState({ ip: "", minutes: 60, reason: "" });
  const [msg, setMsg] = useState("");
  const [error, setError] = useState("");

  const apply = useCallback((s: State) => {
    setSt(s);
    setC(s.config);
    setAllow(s.config.allow.join("\n"));
    setBlock(s.config.block.join("\n"));
  }, []);
  const load = useCallback(() => {
    api<State>("/firewall").then(apply).catch((e) => setError(errMsg(e)));
  }, [apply]);
  useEffect(load, [load]);

  async function save(e: FormEvent) {
    e.preventDefault();
    if (!c) return;
    setMsg("");
    setError("");
    const lines = (s: string) => s.split(/[\s,]+/).map((x) => x.trim()).filter(Boolean);
    try {
      apply(await api<State>("/firewall", { method: "PUT", body: { ...c, allow: lines(allow), block: lines(block) } }));
      setMsg(translate("Đã áp dụng tường lửa."));
    } catch (err) {
      setError(errMsg(err));
    }
  }
  async function act(path: string, body: object) {
    setError("");
    try {
      await api(path, { method: "POST", body });
      load();
    } catch (err) {
      setError(errMsg(err));
    }
  }

  if (!st || !c) return <ErrorBox msg={error} />;
  const set = (k: keyof Config) => (v: number) => setC({ ...c, [k]: v });

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">{translate("Tường lửa")}</h1>
          <p className="text-sm text-slate-500">
            {translate("Chống flood/DDoS theo IP, tự chặn IP dò mật khẩu (SSH, FTP, email, panel) và khóa tạm tài khoản đăng nhập sai nhiều lần.")}
          </p>
        </div>
        <span className={`flex items-center gap-1.5 rounded-full px-3 py-1 text-xs font-medium ${st.active ? "bg-emerald-500/10 text-emerald-700" : "bg-slate-500/10 text-slate-600"}`}>
          {st.active ? <ShieldCheck className="size-4" /> : <ShieldOff className="size-4" />}
          {translate(st.active ? "Đang bật" : "Đang tắt")}
        </span>
      </div>

      <form onSubmit={save} className="space-y-6">
        <section className="card space-y-4 p-5">
          <label className="flex items-center gap-2 font-semibold">
            <input type="checkbox" checked={c.enabled} onChange={(e) => setC({ ...c, enabled: e.target.checked })} />
            {translate("Bật tường lửa X-Panel")}
          </label>
          <p className="text-xs text-muted-foreground">
            {translate("IP của bạn: {0}. Thêm IP này vào danh sách cho phép để không bao giờ bị chặn. Lưu ý: tấn công DDoS băng thông lớn phải được chặn ở nhà cung cấp VPS; tường lửa trên máy chủ chỉ giảm tải các đợt flood nhỏ và tấn công tầng ứng dụng.", [st.your_ip])}
          </p>
        </section>

        <section className="card space-y-4 p-5">
          <h2 className="font-semibold">{translate("Chống flood / DDoS (mỗi IP)")}</h2>
          <div className="grid gap-4 md:grid-cols-3">
            <Num label="Kết nối TCP mới mỗi giây" hint="Vượt mức thì bỏ gói SYN (0 = tắt)" value={c.syn_rate} onChange={set("syn_rate")} />
            <Num label="Kết nối đồng thời tới web (80/443)" hint="0 = tắt" value={c.conn_limit} onChange={set("conn_limit")} />
            <Num label="Request HTTP mỗi giây" hint="Vượt mức trả 429 (0 = tắt)" value={c.http_rate} onChange={set("http_rate")} />
            <Num label="Request vượt cho phép (burst)" value={c.http_burst} onChange={set("http_burst")} />
            <Num label="Request HTTP đồng thời" hint="0 = tắt" value={c.http_conn} onChange={set("http_conn")} />
          </div>
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={c.flood_ban} onChange={(e) => setC({ ...c, flood_ban: e.target.checked })} />
            {translate("Chặn 30 phút IP liên tục vượt giới hạn request")}
          </label>
        </section>

        <section className="card space-y-4 p-5">
          <h2 className="font-semibold">{translate("Chống dò mật khẩu")}</h2>
          <div className="grid gap-4 md:grid-cols-3">
            <Num label="SSH/FTP/Email: số lần sai" value={c.max_retry} onChange={set("max_retry")} />
            <Num label="… trong (phút)" value={c.find_min} onChange={set("find_min")} />
            <Num label="… thì chặn IP (phút)" value={c.ban_min} onChange={set("ban_min")} />
            <Num label="Panel: số lần sai mỗi IP" hint="Trong 15 phút; 0 = không chặn IP" value={c.panel_ip_fails} onChange={set("panel_ip_fails")} />
            <Num label="… thì chặn IP (phút)" value={c.panel_ban_min} onChange={set("panel_ban_min")} />
            <div />
            <Num label="Panel: số lần sai mỗi tài khoản" hint="Trong 15 phút, từ mọi IP; 0 = không khóa" value={c.account_fails} onChange={set("account_fails")} />
            <Num label="… thì khóa đăng nhập (phút)" value={c.account_lock_min} onChange={set("account_lock_min")} />
          </div>
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={c.increment} onChange={(e) => setC({ ...c, increment: e.target.checked })} />
            {translate("Tái phạm: thời gian chặn tăng gấp đôi mỗi lần (tối đa 1 tuần)")}
          </label>
        </section>

        <section className="card grid gap-4 p-5 md:grid-cols-2">
          <div>
            <label className="label">{translate("Luôn cho phép (không giới hạn, không bao giờ chặn)")}</label>
            <textarea className="input h-32 font-mono text-xs" placeholder={"203.0.113.7\n10.0.0.0/8"} value={allow} onChange={(e) => setAllow(e.target.value)} />
          </div>
          <div>
            <label className="label">{translate("Luôn chặn")}</label>
            <textarea className="input h-32 font-mono text-xs" placeholder={"198.51.100.0/24"} value={block} onChange={(e) => setBlock(e.target.value)} />
          </div>
          <p className="text-[11px] text-muted-foreground md:col-span-2">{translate("Mỗi dòng một IP hoặc dải mạng (CIDR), IPv4 hoặc IPv6.")}</p>
        </section>

        {msg && <p className="rounded-lg bg-emerald-50 px-3 py-2 text-sm text-emerald-800">{msg}</p>}
        <ErrorBox msg={error} />
        <button className="btn-primary">{translate("Lưu và áp dụng")}</button>
      </form>

      <section className="card space-y-4 p-5">
        <h2 className="font-semibold">{translate("IP đang bị chặn")} ({st.bans.length})</h2>
        <form className="flex flex-wrap items-end gap-2" onSubmit={(e) => { e.preventDefault(); act("/firewall/ban", ban); }}>
          <div>
            <label className="label">IP</label>
            <input className="input w-48 font-mono" required value={ban.ip} onChange={(e) => setBan({ ...ban, ip: e.target.value.trim() })} />
          </div>
          <div>
            <label className="label">{translate("Phút (0 = vĩnh viễn)")}</label>
            <input className="input w-32" type="number" min={0} value={ban.minutes} onChange={(e) => setBan({ ...ban, minutes: Number(e.target.value) || 0 })} />
          </div>
          <div>
            <label className="label">{translate("Lý do")}</label>
            <input className="input w-56" value={ban.reason} onChange={(e) => setBan({ ...ban, reason: e.target.value })} />
          </div>
          <button className="btn-ghost text-red-600"><Ban className="size-4" /> {translate("Chặn IP")}</button>
        </form>
        {st.bans.length === 0 ? (
          <p className="text-sm text-muted-foreground">{translate("Không có IP nào bị chặn.")}</p>
        ) : (
          <table className="w-full text-left text-sm">
            <thead className="border-b text-xs uppercase text-slate-500">
              <tr>
                <th className="py-2">IP</th>
                <th className="py-2">{translate("Lý do")}</th>
                <th className="py-2">{translate("Hết hạn")}</th>
                <th />
              </tr>
            </thead>
            <tbody className="divide-y">
              {st.bans.map((b) => (
                <tr key={b.ip}>
                  <td className="py-2 font-mono">{b.ip}</td>
                  <td className="py-2 text-xs">{reasonLabel(b.reason)}</td>
                  <td className="py-2 text-xs">{b.expires_at ? new Date(b.expires_at).toLocaleString(localeTag()) : translate("Vĩnh viễn")}</td>
                  <td className="py-2 text-right">
                    <button className="btn-ghost px-2.5 py-1 text-xs" onClick={() => act("/firewall/unban", { ip: b.ip })}>
                      <Unlock className="size-3.5" /> {translate("Gỡ chặn")}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>

      <section className="card space-y-3 p-5">
        <h2 className="font-semibold">{translate("Tài khoản đang bị khóa đăng nhập")} ({st.locked_accounts.length})</h2>
        {st.locked_accounts.length === 0 ? (
          <p className="text-sm text-muted-foreground">{translate("Không có tài khoản nào bị khóa.")}</p>
        ) : (
          <ul className="divide-y">
            {st.locked_accounts.map((l) => (
              <li key={l.key} className="flex items-center justify-between py-2 text-sm">
                <span>
                  <b>{l.key}</b> · {translate("đến")} {new Date(l.until).toLocaleString(localeTag())}
                </span>
                <button className="btn-ghost px-2.5 py-1 text-xs" onClick={() => act("/firewall/unlock", { username: l.key })}>
                  <Unlock className="size-3.5" /> {translate("Mở khóa")}
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}
