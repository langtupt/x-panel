"use client";
import { localeTag } from "@/lib/i18n";
import { t as translate } from "@/lib/i18n";

import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Activity,
  Cpu,
  Database,
  HardDrive,
  Mail,
  MemoryStick,
  Network,
  Search,
  Users,
  X,
} from "lucide-react";
import { api, errMsg, fmtBytes, fmtMB } from "@/lib/api";
import { ErrorBox } from "@/components/ui";

type Point = {
  at: string;
  cpu_pct: number | null;
  mem_mb: number;
  bw_bytes: number;
};
type Latest = Point & {
  tasks: number;
  disk_mb?: number | null;
  db_mb?: number;
  mail_mb?: number;
  requests?: number;
};
type Account = {
  user_id: number;
  username: string;
  parent_name: string | null;
  status: string;
  package: {
    name: string;
    disk_mb: number;
    cpu_pct: number;
    mem_mb: number;
    bandwidth_gb: number;
  } | null;
  latest: Latest | null;
  bandwidth_bytes: number;
  requests: number;
  month_bandwidth_bytes: number;
  points: Point[];
};

const ranges = [
  { id: "24h", label: "24 giờ" },
  { id: "7d", label: "7 ngày" },
  { id: "30d", label: "30 ngày" },
];
const sorts = [
  { id: "cpu", label: "CPU" },
  { id: "mem", label: "RAM" },
  { id: "bw", label: "Băng thông" },
  { id: "disk", label: "Ổ đĩa" },
  { id: "name", label: "Tên" },
];
const COLORS = { cpu: "#3b82f6", mem: "#8b5cf6", bw: "#10b981" };

const pct = (n: number) => `${n.toFixed(n < 10 ? 1 : 0)}%`;
const fmtTime = (iso: string, range: string) => {
  const d = new Date(iso);
  return range === "24h"
    ? d.toLocaleTimeString(localeTag(), { hour: "2-digit", minute: "2-digit" })
    : d.toLocaleDateString(localeTag(), { day: "2-digit", month: "2-digit" }) +
        (range === "7d"
          ? " " +
            d.toLocaleTimeString(localeTag(), {
              hour: "2-digit",
              minute: "2-digit",
            })
          : "");
};

function level(used: number, total: number) {
  if (!total)
    return { ratio: 0, bar: "bg-sky-500", text: "text-muted-foreground" };
  const r = used / total;
  if (r >= 0.9) return { ratio: r, bar: "bg-red-500", text: "text-red-600" };
  if (r >= 0.7)
    return { ratio: r, bar: "bg-amber-500", text: "text-amber-600" };
  return { ratio: r, bar: "bg-emerald-500", text: "text-emerald-600" };
}

/** Labelled usage bar against a package limit (0 = unlimited). */
function Meter({
  icon: Icon,
  label,
  used,
  total,
  fmt,
  empty,
}: {
  icon: typeof Cpu;
  label: string;
  used: number;
  total: number;
  fmt: (n: number) => string;
  empty?: string;
}) {
  const l = level(used, total);
  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between text-xs">
        <span className="flex items-center gap-1.5 text-muted-foreground">
          <Icon className="size-3.5" />
          {translate(label)}
        </span>
        {empty ? (
          <span className="text-muted-foreground">{translate(empty)}</span>
        ) : (
          <span className="tabular-nums">
            <b className="font-semibold text-foreground">
              {translate(fmt(used))}
            </b>
            <span className="text-muted-foreground">
              {" "}
              / {translate(total ? fmt(total) : "∞")}
            </span>
            {total > 0 && (
              <span className={`ml-1.5 ${l.text}`}>
                {translate(Math.round(l.ratio * 100))}%
              </span>
            )}
          </span>
        )}
      </div>
      <div className="h-1.5 overflow-hidden rounded-full bg-muted">
        {!empty && (
          <div
            className={`h-full rounded-full ${total ? l.bar : "bg-sky-500/60"} transition-all`}
            style={{
              width: `${total ? Math.min(100, l.ratio * 100) : used > 0 ? 8 : 0}%`,
            }}
          />
        )}
      </div>
    </div>
  );
}

function Sparkline({ values, color }: { values: number[]; color: string }) {
  if (values.length < 2) return <div className="h-10" />;
  const max = Math.max(1, ...values);
  const w = 120,
    h = 40;
  const pts = values.map(
    (v, i) => `${(i * w) / (values.length - 1)},${h - 2 - (v / max) * (h - 4)}`,
  );
  const id = `sp-${color.slice(1)}`;
  return (
    <svg
      viewBox={`0 0 ${w} ${h}`}
      className="h-10 w-full"
      preserveAspectRatio="none"
    >
      <defs>
        <linearGradient id={id} x1="0" x2="0" y1="0" y2="1">
          <stop offset="0" stopColor={color} stopOpacity="0.35" />
          <stop offset="1" stopColor={color} stopOpacity="0" />
        </linearGradient>
      </defs>
      <path
        d={`M${pts.join(" L")} L${w},${h} L0,${h} Z`}
        fill={`url(#${id})`}
      />
      <polyline
        points={pts.join(" ")}
        fill="none"
        stroke={color}
        strokeWidth="1.5"
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  );
}

/** Larger chart with grid, axis labels and a hover readout. */
function Chart({
  title,
  values,
  labels,
  color,
  fmt,
}: {
  title: string;
  values: (number | null)[];
  labels: string[];
  color: string;
  fmt: (n: number) => string;
}) {
  const [hover, setHover] = useState<number | null>(null);
  const nums = values.map((v) => v ?? 0);
  const max = Math.max(1, ...nums) * 1.1;
  const W = 600,
    H = 180,
    L = 0,
    B = 4;
  const x = (i: number) => L + (i * (W - L)) / Math.max(1, nums.length - 1);
  const y = (v: number) => H - B - (v / max) * (H - B - 8);
  const line = nums
    .map((v, i) => `${i ? "L" : "M"}${x(i).toFixed(1)},${y(v).toFixed(1)}`)
    .join(" ");
  const rawAvg = nums.length
    ? nums.reduce((a, b) => a + b, 0) / nums.length
    : 0;
  const avg = rawAvg >= 10 ? Math.round(rawAvg) : Math.round(rawAvg * 10) / 10;
  const peak = Math.max(0, ...nums);
  const shown = hover ?? nums.length - 1;
  return (
    <div className="rounded-xl border bg-card p-4">
      <div className="mb-3 flex items-start justify-between gap-2">
        <div>
          <div className="text-xs text-muted-foreground">
            {translate(title)}
          </div>
          <div className="text-xl font-semibold tabular-nums">
            {translate(nums.length ? fmt(nums[shown]) : "—")}
          </div>
          <div className="text-[11px] text-muted-foreground">
            {translate(
              nums.length ? labels[shown] : translate("chưa có dữ liệu"),
            )}
          </div>
        </div>
        <div className="text-right text-[11px] text-muted-foreground">
          <div>
            {translate("cao nhất")}{" "}
            <b className="text-foreground">{translate(fmt(peak))}</b>
          </div>
          <div>
            {translate("trung bình")}{" "}
            <b className="text-foreground">{translate(fmt(avg))}</b>
          </div>
        </div>
      </div>
      {nums.length < 2 ? (
        <div className="flex h-40 items-center justify-center text-xs text-muted-foreground">
          {translate("Chưa đủ dữ liệu (đo mỗi 5 phút)")}
        </div>
      ) : (
        <svg
          viewBox={`0 0 ${W} ${H}`}
          className="h-40 w-full"
          preserveAspectRatio="none"
          onMouseLeave={() => setHover(null)}
          onMouseMove={(e) => {
            const r = e.currentTarget.getBoundingClientRect();
            const i = Math.round(
              ((e.clientX - r.left) / r.width) * (nums.length - 1),
            );
            setHover(Math.max(0, Math.min(nums.length - 1, i)));
          }}
        >
          {[0.25, 0.5, 0.75, 1].map((g) => (
            <line
              key={g}
              x1="0"
              x2={W}
              y1={y((max * g) / 1.1)}
              y2={y((max * g) / 1.1)}
              stroke="currentColor"
              className="text-border"
              strokeDasharray="3 4"
              vectorEffect="non-scaling-stroke"
            />
          ))}
          <path
            d={`${line} L${x(nums.length - 1)},${H - B} L${x(0)},${H - B} Z`}
            fill={color}
            opacity="0.12"
          />
          <path
            d={line}
            fill="none"
            stroke={color}
            strokeWidth="2"
            vectorEffect="non-scaling-stroke"
          />
          {hover !== null && (
            <>
              <line
                x1={x(hover)}
                x2={x(hover)}
                y1="0"
                y2={H}
                stroke={color}
                strokeOpacity="0.4"
                vectorEffect="non-scaling-stroke"
              />
              <circle
                cx={x(hover)}
                cy={y(nums[hover])}
                r="4"
                fill={color}
                vectorEffect="non-scaling-stroke"
              />
            </>
          )}
        </svg>
      )}
      {nums.length >= 2 && (
        <div className="mt-1 flex justify-between text-[10px] text-muted-foreground">
          <span>{translate(labels[0])}</span>
          <span>{translate(labels[Math.floor(labels.length / 2)])}</span>
          <span>{translate(labels[labels.length - 1])}</span>
        </div>
      )}
    </div>
  );
}

function Kpi({
  icon: Icon,
  label,
  value,
  sub,
}: {
  icon: typeof Cpu;
  label: string;
  value: string;
  sub?: string;
}) {
  return (
    <div className="rounded-xl border bg-card p-4">
      <div className="flex items-center justify-between">
        <span className="text-xs text-muted-foreground">
          {translate(label)}
        </span>
        <span className="flex size-8 items-center justify-center rounded-lg bg-muted">
          <Icon className="size-4" />
        </span>
      </div>
      <div className="mt-2 text-2xl font-semibold tabular-nums">{value}</div>
      {sub && (
        <div className="text-xs text-muted-foreground">{translate(sub)}</div>
      )}
    </div>
  );
}

function AccountCard({ a, onOpen }: { a: Account; onOpen: () => void }) {
  const l = a.latest,
    p = a.package;
  return (
    <button
      onClick={onOpen}
      className="group flex flex-col gap-4 rounded-xl border bg-card p-4 text-left transition hover:-translate-y-0.5 hover:shadow-md"
    >
      <div className="flex items-start justify-between gap-2">
        <div className="flex items-center gap-3">
          <span className="flex size-9 items-center justify-center rounded-full bg-muted text-sm font-semibold uppercase">
            {translate(a.username[0])}
          </span>
          <div>
            <div className="font-semibold">{a.username}</div>
            <div className="text-xs text-muted-foreground">
              {translate(p?.name ?? translate("Không gói"))}
              {translate(a.parent_name && ` · ${a.parent_name}`)}
            </div>
          </div>
        </div>
        {a.status === "suspended" ? (
          <span className="rounded-full bg-red-500/10 px-2 py-0.5 text-[11px] font-medium text-red-600">
            {translate("tạm ngưng")}
          </span>
        ) : (
          <span className="rounded-full bg-emerald-500/10 px-2 py-0.5 text-[11px] font-medium text-emerald-600">
            {translate(
              l ? translate("đang hoạt động") : translate("chưa có số liệu"),
            )}
          </span>
        )}
      </div>
      <div className="space-y-3">
        <Meter
          icon={Cpu}
          label="CPU"
          used={l?.cpu_pct ?? 0}
          total={p?.cpu_pct ?? 0}
          fmt={pct}
        />
        <Meter
          icon={MemoryStick}
          label="RAM"
          used={l?.mem_mb ?? 0}
          total={p?.mem_mb ?? 0}
          fmt={fmtMB}
        />
        <Meter
          icon={HardDrive}
          label={translate("Ổ đĩa")}
          used={l?.disk_mb ?? 0}
          total={p?.disk_mb ?? 0}
          fmt={fmtMB}
          empty={
            l && l.disk_mb == null ? translate("quota chưa bật") : undefined
          }
        />
        <Meter
          icon={Network}
          label={translate("Băng thông tháng")}
          used={a.month_bandwidth_bytes / 1048576}
          total={(p?.bandwidth_gb ?? 0) * 1024}
          fmt={(n) => fmtBytes(n * 1048576)}
        />
      </div>
      <div className="grid grid-cols-2 gap-3 border-t pt-3">
        <div>
          <div className="text-[11px] text-muted-foreground">CPU</div>
          <Sparkline
            values={a.points.map((x) => x.cpu_pct ?? 0)}
            color={COLORS.cpu}
          />
        </div>
        <div>
          <div className="text-[11px] text-muted-foreground">
            {translate("Băng thông ·")} {translate(fmtBytes(a.bandwidth_bytes))}
          </div>
          <Sparkline
            values={a.points.map((x) => x.bw_bytes)}
            color={COLORS.bw}
          />
        </div>
      </div>
    </button>
  );
}

function Detail({
  a,
  range,
  onClose,
}: {
  a: Account;
  range: string;
  onClose: () => void;
}) {
  const labels = a.points.map((p) => fmtTime(p.at, range));
  const l = a.latest;
  useEffect(() => {
    const k = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", k);
    return () => window.removeEventListener("keydown", k);
  }, [onClose]);
  return (
    <div
      className="fixed inset-0 z-50 flex justify-end bg-black/40"
      onClick={onClose}
    >
      <div
        className="h-full w-full max-w-3xl overflow-y-auto bg-background p-6 shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-6 flex items-start justify-between">
          <div>
            <h2 className="text-lg font-semibold">{a.username}</h2>
            <p className="text-sm text-muted-foreground">
              {translate(a.package?.name ?? translate("Không gói"))} ·{" "}
              {translate(ranges.find((r) => r.id === range)?.label)}{" "}
              {translate("gần nhất")}{" "}
              {translate(
                l &&
                  translate(" · cập nhật {0}", [
                    new Date(l.at).toLocaleTimeString(localeTag()),
                  ]),
              )}
            </p>
          </div>
          <button
            className="rounded-lg p-2 hover:bg-muted"
            onClick={onClose}
            aria-label={translate("Đóng")}
          >
            <X className="size-4" />
          </button>
        </div>
        <div className="mb-6 grid grid-cols-2 gap-3 md:grid-cols-4">
          <Kpi
            icon={Activity}
            label={translate("Tiến trình")}
            value={String(l?.tasks ?? 0)}
          />
          <Kpi icon={Database} label="Database" value={fmtMB(l?.db_mb ?? 0)} />
          <Kpi
            icon={Mail}
            label={translate("Hộp thư")}
            value={fmtMB(l?.mail_mb ?? 0)}
          />
          <Kpi
            icon={Network}
            label={translate("Lượt truy cập")}
            value={a.requests.toLocaleString(localeTag())}
            sub={fmtBytes(a.bandwidth_bytes)}
          />
        </div>
        <div className="space-y-4">
          <Chart
            title={translate("CPU (trung bình; 100% = 1 nhân)")}
            values={a.points.map((p) => p.cpu_pct)}
            labels={labels}
            color={COLORS.cpu}
            fmt={pct}
          />
          <Chart
            title={translate("RAM (cao nhất)")}
            values={a.points.map((p) => p.mem_mb)}
            labels={labels}
            color={COLORS.mem}
            fmt={fmtMB}
          />
          <Chart
            title={translate("Băng thông")}
            values={a.points.map((p) => p.bw_bytes)}
            labels={labels}
            color={COLORS.bw}
            fmt={fmtBytes}
          />
        </div>
      </div>
    </div>
  );
}

export default function StatsPage() {
  const [range, setRange] = useState("24h");
  const [sort, setSort] = useState("cpu");
  const [query, setQuery] = useState("");
  const [list, setList] = useState<Account[] | null>(null);
  const [open, setOpen] = useState<number | null>(null);
  const [error, setError] = useState("");

  const load = useCallback(() => {
    api<Account[]>(`/stats/accounts?range=${range}`)
      .then(setList)
      .catch((e) => setError(errMsg(e)));
  }, [range]);
  useEffect(() => {
    load();
    const t = setInterval(load, 60_000);
    return () => clearInterval(t);
  }, [load]);

  const shown = useMemo(() => {
    const key: Record<string, (a: Account) => number | string> = {
      cpu: (a) => -(a.latest?.cpu_pct ?? 0),
      mem: (a) => -(a.latest?.mem_mb ?? 0),
      bw: (a) => -a.bandwidth_bytes,
      disk: (a) => -(a.latest?.disk_mb ?? 0),
      name: (a) => a.username,
    };
    return (list ?? [])
      .filter((a) => a.username.includes(query.trim().toLowerCase()))
      .sort((a, b) =>
        key[sort](a) < key[sort](b) ? -1 : key[sort](a) > key[sort](b) ? 1 : 0,
      );
  }, [list, sort, query]);

  const total = useMemo(() => {
    const l = list ?? [];
    return {
      cpu: l.reduce((s, a) => s + (a.latest?.cpu_pct ?? 0), 0),
      mem: l.reduce((s, a) => s + (a.latest?.mem_mb ?? 0), 0),
      bw: l.reduce((s, a) => s + a.bandwidth_bytes, 0),
      req: l.reduce((s, a) => s + a.requests, 0),
      active: l.filter((a) => a.status !== "suspended").length,
    };
  }, [list]);
  const current = list?.find((a) => a.user_id === open);
  const rangeLabel = ranges.find((r) => r.id === range)?.label;

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">
            {translate("Thống kê tài nguyên")}
          </h1>
          <p className="text-sm text-muted-foreground">
            {translate(
              "CPU, RAM, dung lượng và băng thông của từng tài khoản · đo mỗi 5 phút",
            )}
          </p>
        </div>
        <div className="flex rounded-lg border bg-card p-0.5">
          {ranges.map((r) => (
            <button
              key={r.id}
              onClick={() => setRange(r.id)}
              className={`rounded-md px-3 py-1.5 text-xs font-medium transition ${range === r.id ? "bg-foreground text-background shadow-sm" : "text-muted-foreground hover:text-foreground"}`}
            >
              {translate(r.label)}
            </button>
          ))}
        </div>
      </div>
      <ErrorBox msg={error} />

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <Kpi
          icon={Users}
          label={translate("Tài khoản")}
          value={String(list?.length ?? "—")}
          sub={translate("{0} đang hoạt động", [total.active])}
        />
        <Kpi
          icon={Cpu}
          label={translate("CPU hiện tại (tổng)")}
          value={pct(total.cpu)}
          sub={translate("100% = 1 nhân")}
        />
        <Kpi
          icon={MemoryStick}
          label={translate("RAM hiện tại (tổng)")}
          value={fmtMB(total.mem)}
        />
        <Kpi
          icon={Network}
          label={translate("Băng thông {0}", [rangeLabel])}
          value={fmtBytes(total.bw)}
          sub={translate("{0} lượt truy cập", [
            total.req.toLocaleString(localeTag()),
          ])}
        />
      </div>

      {list && list.length > 1 && (
        <div className="flex flex-wrap items-center gap-3">
          <div className="relative">
            <Search className="pointer-events-none absolute left-2.5 top-2.5 size-4 text-muted-foreground" />
            <input
              className="input h-9 w-56 pl-8"
              placeholder={translate("Tìm tài khoản…")}
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
          </div>
          <div className="flex items-center gap-1 text-xs text-muted-foreground">
            {translate("Sắp xếp:")}{" "}
            {sorts.map((s) => (
              <button
                key={s.id}
                onClick={() => setSort(s.id)}
                className={`rounded-md px-2 py-1 ${sort === s.id ? "bg-muted font-medium text-foreground" : "hover:text-foreground"}`}
              >
                {translate(s.label)}
              </button>
            ))}
          </div>
        </div>
      )}

      {list === null && !error && (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {[0, 1, 2].map((i) => (
            <div
              key={i}
              className="h-72 animate-pulse rounded-xl border bg-muted/40"
            />
          ))}
        </div>
      )}
      {list?.length === 0 && (
        <div className="rounded-xl border bg-card p-10 text-center text-sm text-muted-foreground">
          {translate("Chưa có tài khoản hosting nào.")}
        </div>
      )}
      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        {shown.map((a) => (
          <AccountCard
            key={a.user_id}
            a={a}
            onOpen={() => setOpen(a.user_id)}
          />
        ))}
      </div>

      {current && (
        <Detail a={current} range={range} onClose={() => setOpen(null)} />
      )}
    </div>
  );
}
