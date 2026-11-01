"use client";
import { localeTag, t as translate } from "@/lib/i18n";

import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";
import {
  Ban,
  BrickWall,
  CheckCircle2,
  CircleAlert,
  KeyRound,
  ListChecks,
  Loader2,
  Search,
  ShieldCheck,
  ShieldOff,
  Stethoscope,
  Unlock,
  UserLock,
  Zap,
  type LucideIcon,
} from "lucide-react";
import { api, errMsg } from "@/lib/api";
import { ErrorBox } from "@/components/ui";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";

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
type BanEntry = { ip: string; reason: string; expires_at: string | null };
type State = {
  config: Config;
  active: boolean;
  bans: BanEntry[];
  locked_accounts: { key: string; until: string }[];
  your_ip: string;
};
type IPCheck = {
  ip: string;
  firewall_active: boolean;
  banned: boolean;
  ban_reason: string;
  deny_match: string;
  allow_match: string;
  fail2ban: string[];
  other_tables: string[];
  panel_locked: boolean;
};

type Tab = "bans" | "check" | "flood" | "login" | "lists" | "accounts";

function Num({
  label,
  hint,
  value,
  onChange,
}: {
  label: string;
  hint?: string;
  value: number;
  onChange: (v: number) => void;
}) {
  return (
    <div>
      <label className="label">{translate(label)}</label>
      <input
        className="input"
        type="number"
        min={0}
        value={value}
        onChange={(e) => onChange(Number(e.target.value) || 0)}
      />
      {hint && (
        <p className="mt-1 text-[11px] text-muted-foreground">
          {translate(hint)}
        </p>
      )}
    </div>
  );
}

function CheckBox({
  checked,
  onChange,
  children,
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
  children: React.ReactNode;
}) {
  return (
    <label className="flex items-center gap-2 text-sm">
      <input
        type="checkbox"
        checked={checked}
        onChange={(e) => onChange(e.target.checked)}
      />
      {children}
    </label>
  );
}

function reasonLabel(r: string) {
  if (r === "panel-login") return translate("Đăng nhập panel sai nhiều lần");
  if (r === "f2b-sshd") return translate("SSH/SFTP sai mật khẩu");
  if (r === "f2b-nginx-limit-req")
    return translate("Gửi quá nhiều request (HTTP flood)");
  if (r.startsWith("f2b-")) return "fail2ban: " + r.slice(4);
  if (r.startsWith("admin: ")) return translate("Quản trị: {0}", [r.slice(7)]);
  return r || "—";
}

/** What still blocks an address, as readable lines (empty = reachable). */
function blockers(c: IPCheck) {
  const out: string[] = [];
  // Allowed addresses skip every rule of the X-Panel firewall.
  if (c.allow_match) return out;
  if (c.banned)
    out.push(
      translate("Đang trong danh sách bị chặn ({0})", [
        reasonLabel(c.ban_reason),
      ]),
    );
  if (c.deny_match)
    out.push(translate("Nằm trong danh sách “Luôn chặn”: {0}", [c.deny_match]));
  for (const j of c.fail2ban ?? [])
    out.push(translate("fail2ban jail “{0}” vẫn đang chặn", [j]));
  for (const t of c.other_tables ?? [])
    out.push(translate("Có trong bảng tường lửa khác: {0}", [t]));
  if (c.panel_locked)
    out.push(translate("Bị khóa đăng nhập panel (sai mật khẩu nhiều lần)"));
  return out;
}

function CheckResult({ c }: { c: IPCheck }) {
  const list = blockers(c);
  return (
    <div
      className={`rounded-lg border p-4 text-sm ${list.length ? "border-amber-300 bg-amber-50 text-amber-900" : "border-emerald-200 bg-emerald-50 text-emerald-800"}`}
    >
      <p className="flex flex-wrap items-center gap-2 font-medium">
        {list.length ? (
          <CircleAlert className="size-4" />
        ) : (
          <CheckCircle2 className="size-4" />
        )}
        <span className="font-mono">{c.ip}</span>
        {list.length
          ? translate("vẫn bị chặn")
          : c.allow_match
            ? translate("luôn được cho phép ({0})", [c.allow_match])
            : translate("không bị chặn trên máy chủ")}
      </p>
      {list.length > 0 && (
        <ul className="mt-2 list-disc space-y-1 pl-6 text-xs">
          {list.map((l) => (
            <li key={l}>{l}</li>
          ))}
        </ul>
      )}
      {!list.length && !c.allow_match && (
        <p className="mt-1 text-xs opacity-80">
          {translate(
            "Nếu máy đó vẫn không vào được: kiểm tra tường lửa của nhà cung cấp VPS, IPv6 của máy đó (có thể là địa chỉ khác), hoặc giới hạn request tạm thời (HTTP 429).",
          )}
        </p>
      )}
    </div>
  );
}

export default function FirewallPage() {
  const [st, setSt] = useState<State | null>(null);
  const [c, setC] = useState<Config | null>(null);
  const [allow, setAllow] = useState("");
  const [block, setBlock] = useState("");
  const [tab, setTab] = useState<Tab>("bans");
  const [query, setQuery] = useState("");
  const [ban, setBan] = useState({ ip: "", minutes: 60, reason: "" });
  const [checkIP, setCheckIP] = useState("");
  const [check, setCheck] = useState<IPCheck | null>(null);
  const [busy, setBusy] = useState("");
  const [msg, setMsg] = useState("");
  const [error, setError] = useState("");

  const apply = useCallback((s: State) => {
    setSt(s);
    setC(s.config);
    setAllow(s.config.allow.join("\n"));
    setBlock(s.config.block.join("\n"));
  }, []);
  const load = useCallback(() => {
    api<State>("/firewall")
      .then(apply)
      .catch((e) => setError(errMsg(e)));
  }, [apply]);
  useEffect(load, [load]);

  const bans = useMemo(() => {
    const q = query.trim().toLowerCase();
    const list = st?.bans ?? [];
    return q
      ? list.filter(
          (b) =>
            b.ip.toLowerCase().includes(q) ||
            reasonLabel(b.reason).toLowerCase().includes(q),
        )
      : list;
  }, [st, query]);

  async function save(e?: FormEvent) {
    e?.preventDefault();
    if (!c) return;
    setMsg("");
    setError("");
    setBusy("save");
    const lines = (s: string) =>
      s
        .split(/[\s,]+/)
        .map((x) => x.trim())
        .filter(Boolean);
    try {
      apply(
        await api<State>("/firewall", {
          method: "PUT",
          body: { ...c, allow: lines(allow), block: lines(block) },
        }),
      );
      setMsg(translate("Đã áp dụng tường lửa."));
    } catch (err) {
      setError(errMsg(err));
    } finally {
      setBusy("");
    }
  }
  async function banIP(e: FormEvent) {
    e.preventDefault();
    setError("");
    setMsg("");
    setBusy("ban");
    try {
      await api("/firewall/ban", { method: "POST", body: ban });
      setMsg(translate("Đã chặn {0}.", [ban.ip]));
      setBan({ ip: "", minutes: 60, reason: "" });
      load();
    } catch (err) {
      setError(errMsg(err));
    } finally {
      setBusy("");
    }
  }
  async function unban(ip: string) {
    setError("");
    setMsg("");
    setBusy(ip);
    try {
      const res = await api<IPCheck>("/firewall/unban", {
        method: "POST",
        body: { ip },
      });
      setCheck(res);
      setCheckIP(ip);
      if (blockers(res).length) setTab("check");
      else setMsg(translate("Đã gỡ chặn {0} ở mọi nơi trên máy chủ.", [ip]));
      load();
    } catch (err) {
      setError(errMsg(err));
    } finally {
      setBusy("");
    }
  }
  async function runCheck(e?: FormEvent) {
    e?.preventDefault();
    if (!checkIP.trim()) return;
    setError("");
    setBusy("check");
    try {
      setCheck(
        await api<IPCheck>(
          `/firewall/check?ip=${encodeURIComponent(checkIP.trim())}`,
        ),
      );
    } catch (err) {
      setError(errMsg(err));
      setCheck(null);
    } finally {
      setBusy("");
    }
  }
  async function unlock(username: string) {
    setError("");
    try {
      await api("/firewall/unlock", { method: "POST", body: { username } });
      load();
    } catch (err) {
      setError(errMsg(err));
    }
  }

  const header = (
    <div>
      <div className="mb-2 flex items-center gap-2 text-xs text-muted-foreground">
        <BrickWall className="size-3.5" />
        {translate("Bảo vệ máy chủ")}
      </div>
      <h1>{translate("Tường lửa")}</h1>
      <p className="mt-1 text-sm text-muted-foreground">
        {translate(
          "Chống flood/DDoS theo IP, tự chặn IP dò mật khẩu (SSH, FTP, email, panel) và khóa tạm tài khoản đăng nhập sai nhiều lần.",
        )}
      </p>
    </div>
  );
  if (!st || !c)
    return (
      <div className="space-y-6">
        {header}
        <ErrorBox msg={error} />
        {!error && <Skeleton className="h-64 rounded-xl" />}
      </div>
    );

  const set = (k: keyof Config) => (v: number) => setC({ ...c, [k]: v });
  const yourIPAllowed = allow
    .split(/[\s,]+/)
    .some((x) => x.trim() === st.your_ip);
  const tabs: { id: Tab; label: string; icon: LucideIcon; count?: number }[] = [
    { id: "bans", label: "IP bị chặn", icon: Ban, count: st.bans.length },
    { id: "check", label: "Kiểm tra IP", icon: Stethoscope },
    { id: "flood", label: "Chống flood", icon: Zap },
    { id: "login", label: "Chống dò mật khẩu", icon: KeyRound },
    { id: "lists", label: "Danh sách IP", icon: ListChecks },
    {
      id: "accounts",
      label: "Tài khoản bị khóa",
      icon: UserLock,
      count: st.locked_accounts.length,
    },
  ];
  const configTab = tab === "flood" || tab === "login" || tab === "lists";

  return (
    <div className="space-y-6">
      {header}

      <section className="card flex flex-wrap items-center gap-4 p-5">
        <div
          className={`flex size-11 items-center justify-center rounded-xl border ${st.active ? "bg-emerald-50 text-emerald-700" : "bg-muted text-muted-foreground"}`}
        >
          {st.active ? (
            <ShieldCheck className="size-5" />
          ) : (
            <ShieldOff className="size-5" />
          )}
        </div>
        <div className="min-w-0 flex-1">
          <p className="text-sm font-semibold">
            {translate(st.active ? "Tường lửa đang bật" : "Tường lửa đang tắt")}
          </p>
          <p className="text-xs text-muted-foreground">
            {translate("IP của bạn: {0}", [st.your_ip])}
            {yourIPAllowed && ` · ${translate("đã ở danh sách cho phép")}`}
          </p>
        </div>
        {!yourIPAllowed && (
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              setAllow((a) => (a.trim() ? a.trim() + "\n" : "") + st.your_ip);
              setTab("lists");
            }}
          >
            {translate("Thêm IP của tôi vào danh sách cho phép")}
          </Button>
        )}
        <label className="flex items-center gap-2 text-sm font-medium">
          <input
            type="checkbox"
            checked={c.enabled}
            onChange={(e) => setC({ ...c, enabled: e.target.checked })}
          />
          {translate("Bật tường lửa X-Panel")}
        </label>
        {c.enabled !== st.config.enabled && (
          <Button size="sm" onClick={() => save()} disabled={!!busy}>
            {translate("Lưu và áp dụng")}
          </Button>
        )}
      </section>

      {msg && (
        <p className="rounded-lg bg-emerald-50 px-3 py-2 text-sm text-emerald-800">
          {msg}
        </p>
      )}
      <ErrorBox msg={error} />

      <div className="flex max-w-full gap-1 overflow-x-auto rounded-lg border bg-muted/40 p-1">
        {tabs.map((t) => (
          <button
            key={t.id}
            type="button"
            onClick={() => setTab(t.id)}
            className={`flex shrink-0 items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition ${tab === t.id ? "bg-background shadow-xs" : "text-muted-foreground hover:text-foreground"}`}
          >
            <t.icon className="size-3.5" />
            {translate(t.label)}
            {t.count !== undefined && (
              <span className="rounded bg-muted px-1.5 text-[10px] tabular-nums">
                {t.count}
              </span>
            )}
          </button>
        ))}
      </div>

      {tab === "bans" && (
        <section className="card overflow-hidden">
          <form
            className="flex flex-wrap items-end gap-2 border-b p-4"
            onSubmit={banIP}
          >
            <div>
              <label className="label">IP</label>
              <input
                className="input w-48 font-mono"
                required
                placeholder="203.0.113.7"
                value={ban.ip}
                onChange={(e) => setBan({ ...ban, ip: e.target.value.trim() })}
              />
            </div>
            <div>
              <label className="label">
                {translate("Phút (0 = vĩnh viễn)")}
              </label>
              <input
                className="input w-32"
                type="number"
                min={0}
                value={ban.minutes}
                onChange={(e) =>
                  setBan({ ...ban, minutes: Number(e.target.value) || 0 })
                }
              />
            </div>
            <div className="min-w-48 flex-1">
              <label className="label">{translate("Lý do")}</label>
              <input
                className="input"
                value={ban.reason}
                onChange={(e) => setBan({ ...ban, reason: e.target.value })}
              />
            </div>
            <Button
              type="submit"
              variant="outline"
              className="text-red-600"
              disabled={busy === "ban"}
            >
              <Ban /> {translate("Chặn IP")}
            </Button>
          </form>
          <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
            <p className="text-xs text-muted-foreground">
              {query
                ? translate("{0} / {1} IP", [bans.length, st.bans.length])
                : translate("{0} IP đang bị chặn", [st.bans.length])}
            </p>
            <div className="relative w-full sm:w-72">
              <Search className="absolute top-2.5 left-3 size-4 text-muted-foreground" />
              <input
                className="input pl-9 font-mono"
                placeholder={translate("Tìm IP hoặc lý do…")}
                aria-label={translate("Tìm IP")}
                value={query}
                onChange={(e) => setQuery(e.target.value)}
              />
            </div>
          </div>
          {bans.length === 0 ? (
            <p className="border-t px-4 py-10 text-center text-sm text-muted-foreground">
              {translate(
                st.bans.length
                  ? "Không có IP phù hợp."
                  : "Không có IP nào bị chặn.",
              )}
            </p>
          ) : (
            <div className="overflow-x-auto border-t">
              <table>
                <thead>
                  <tr>
                    <th>IP</th>
                    <th>{translate("Lý do")}</th>
                    <th>{translate("Hết hạn")}</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {bans.slice(0, 500).map((b) => (
                    <tr key={b.ip}>
                      <td className="font-mono">{b.ip}</td>
                      <td className="text-xs">{reasonLabel(b.reason)}</td>
                      <td className="text-xs">
                        {b.expires_at
                          ? new Date(b.expires_at).toLocaleString(localeTag())
                          : translate("Vĩnh viễn")}
                      </td>
                      <td className="text-right">
                        <Button
                          variant="ghost"
                          size="sm"
                          disabled={busy === b.ip}
                          onClick={() => unban(b.ip)}
                        >
                          {busy === b.ip ? (
                            <Loader2 className="animate-spin" />
                          ) : (
                            <Unlock />
                          )}
                          {translate("Gỡ chặn")}
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {bans.length > 500 && (
                <p className="border-t px-4 py-2 text-xs text-muted-foreground">
                  {translate(
                    "Đang hiển thị 500 IP đầu tiên; dùng ô tìm kiếm để lọc.",
                  )}
                </p>
              )}
            </div>
          )}
        </section>
      )}

      {tab === "check" && (
        <section className="card space-y-4 p-5">
          <p className="text-sm text-muted-foreground">
            {translate(
              "Kiểm tra một IP đang bị chặn ở đâu trên máy chủ: danh sách bị chặn, “Luôn chặn” (cả dải mạng), fail2ban, bảng tường lửa khác và khóa đăng nhập panel.",
            )}
          </p>
          <form className="flex flex-wrap gap-2" onSubmit={runCheck}>
            <input
              className="input w-64 font-mono"
              placeholder="203.0.113.7"
              value={checkIP}
              onChange={(e) => setCheckIP(e.target.value)}
            />
            <Button type="submit" disabled={busy === "check"}>
              {busy === "check" ? (
                <Loader2 className="animate-spin" />
              ) : (
                <Stethoscope />
              )}
              {translate("Kiểm tra")}
            </Button>
            {check &&
              blockers(check).length > 0 &&
              check.ip === checkIP.trim() && (
                <Button
                  type="button"
                  variant="outline"
                  disabled={busy === check.ip}
                  onClick={() => unban(check.ip)}
                >
                  <Unlock /> {translate("Gỡ chặn ở mọi nơi")}
                </Button>
              )}
          </form>
          {check && <CheckResult c={check} />}
          {check?.deny_match && !check.allow_match && (
            <p className="text-xs text-muted-foreground">
              {translate(
                "Mục “Luôn chặn” không gỡ được bằng nút Gỡ chặn: xóa {0} ở tab Danh sách IP rồi Lưu và áp dụng.",
                [check.deny_match],
              )}
            </p>
          )}
        </section>
      )}

      {configTab && (
        <form onSubmit={save} className="space-y-4">
          {tab === "flood" && (
            <section className="card space-y-4 p-5">
              <h2 className="text-sm font-semibold">
                {translate("Chống flood / DDoS (mỗi IP)")}
              </h2>
              <div className="grid gap-4 md:grid-cols-3">
                <Num
                  label="Kết nối TCP mới mỗi giây"
                  hint="Vượt mức thì bỏ gói SYN (0 = tắt)"
                  value={c.syn_rate}
                  onChange={set("syn_rate")}
                />
                <Num
                  label="Kết nối đồng thời tới web (80/443)"
                  hint="0 = tắt"
                  value={c.conn_limit}
                  onChange={set("conn_limit")}
                />
                <Num
                  label="Request HTTP mỗi giây"
                  hint="Vượt mức trả 429 (0 = tắt)"
                  value={c.http_rate}
                  onChange={set("http_rate")}
                />
                <Num
                  label="Request vượt cho phép (burst)"
                  value={c.http_burst}
                  onChange={set("http_burst")}
                />
                <Num
                  label="Request HTTP đồng thời"
                  hint="0 = tắt"
                  value={c.http_conn}
                  onChange={set("http_conn")}
                />
              </div>
              <CheckBox
                checked={c.flood_ban}
                onChange={(v) => setC({ ...c, flood_ban: v })}
              >
                {translate("Chặn 30 phút IP liên tục vượt giới hạn request")}
              </CheckBox>
              <p className="text-[11px] text-muted-foreground">
                {translate(
                  "Tấn công DDoS băng thông lớn phải được chặn ở nhà cung cấp VPS; tường lửa trên máy chủ chỉ giảm tải các đợt flood nhỏ và tấn công tầng ứng dụng.",
                )}
              </p>
            </section>
          )}
          {tab === "login" && (
            <section className="card space-y-4 p-5">
              <h2 className="text-sm font-semibold">
                {translate("Chống dò mật khẩu")}
              </h2>
              <div className="grid gap-4 md:grid-cols-3">
                <Num
                  label="SSH/FTP/Email: số lần sai"
                  value={c.max_retry}
                  onChange={set("max_retry")}
                />
                <Num
                  label="… trong (phút)"
                  value={c.find_min}
                  onChange={set("find_min")}
                />
                <Num
                  label="… thì chặn IP (phút)"
                  value={c.ban_min}
                  onChange={set("ban_min")}
                />
                <Num
                  label="Panel: số lần sai mỗi IP"
                  hint="Trong 15 phút; 0 = không chặn IP"
                  value={c.panel_ip_fails}
                  onChange={set("panel_ip_fails")}
                />
                <Num
                  label="… thì chặn IP (phút)"
                  value={c.panel_ban_min}
                  onChange={set("panel_ban_min")}
                />
                <div />
                <Num
                  label="Panel: số lần sai mỗi tài khoản"
                  hint="Trong 15 phút, từ mọi IP; 0 = không khóa"
                  value={c.account_fails}
                  onChange={set("account_fails")}
                />
                <Num
                  label="… thì khóa đăng nhập (phút)"
                  value={c.account_lock_min}
                  onChange={set("account_lock_min")}
                />
              </div>
              <CheckBox
                checked={c.increment}
                onChange={(v) => setC({ ...c, increment: v })}
              >
                {translate(
                  "Tái phạm: thời gian chặn tăng gấp đôi mỗi lần (tối đa 1 tuần)",
                )}
              </CheckBox>
            </section>
          )}
          {tab === "lists" && (
            <section className="card grid gap-4 p-5 md:grid-cols-2">
              <div>
                <label className="label">
                  {translate(
                    "Luôn cho phép (không giới hạn, không bao giờ chặn)",
                  )}
                </label>
                <textarea
                  className="input h-48 font-mono text-xs"
                  placeholder={"203.0.113.7\n10.0.0.0/8"}
                  value={allow}
                  onChange={(e) => setAllow(e.target.value)}
                />
              </div>
              <div>
                <label className="label">{translate("Luôn chặn")}</label>
                <textarea
                  className="input h-48 font-mono text-xs"
                  placeholder={"198.51.100.0/24"}
                  value={block}
                  onChange={(e) => setBlock(e.target.value)}
                />
              </div>
              <p className="text-[11px] text-muted-foreground md:col-span-2">
                {translate(
                  "Mỗi dòng một IP hoặc dải mạng (CIDR), IPv4 hoặc IPv6.",
                )}
              </p>
            </section>
          )}
          <div className="flex justify-end">
            <Button type="submit" disabled={busy === "save"}>
              {busy === "save" && <Loader2 className="animate-spin" />}
              {translate("Lưu và áp dụng")}
            </Button>
          </div>
        </form>
      )}

      {tab === "accounts" && (
        <section className="card overflow-hidden">
          {st.locked_accounts.length === 0 ? (
            <p className="px-4 py-10 text-center text-sm text-muted-foreground">
              {translate("Không có tài khoản nào bị khóa.")}
            </p>
          ) : (
            <ul className="divide-y">
              {st.locked_accounts.map((l) => (
                <li
                  key={l.key}
                  className="flex items-center justify-between px-4 py-3 text-sm"
                >
                  <span>
                    <b>{l.key}</b> · {translate("đến")}{" "}
                    {new Date(l.until).toLocaleString(localeTag())}
                  </span>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => unlock(l.key)}
                  >
                    <Unlock /> {translate("Mở khóa")}
                  </Button>
                </li>
              ))}
            </ul>
          )}
        </section>
      )}
    </div>
  );
}
