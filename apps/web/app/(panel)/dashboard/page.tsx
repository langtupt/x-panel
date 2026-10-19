"use client";

import { useEffect, useState } from "react";
import { api, fmtMB, limit, type Overview } from "@/lib/api";
import { ServerDashboard } from "@/components/server-dashboard";
import { useSession } from "../session";

function Meter({
  label,
  used,
  total,
}: {
  label: string;
  used: number;
  total: number;
}) {
  const pct = total > 0 ? Math.round((used / total) * 100) : 0;
  const color =
    pct >= 90 ? "bg-red-500" : pct >= 75 ? "bg-amber-500" : "bg-brand-500";
  return (
    <div className="card p-5">
      <div className="flex items-baseline justify-between">
        <span className="text-sm text-slate-500">{label}</span>
        <span className="text-2xl font-semibold">{pct}%</span>
      </div>
      <div className="mt-3 h-2 rounded-full bg-slate-100">
        <div
          className={`h-2 rounded-full ${color}`}
          style={{ width: `${pct}%` }}
        />
      </div>
      <p className="mt-2 text-xs text-slate-500">
        {fmtMB(used)} / {fmtMB(total)}
      </p>
    </div>
  );
}

function Stat({
  label,
  value,
  hint,
}: {
  label: string;
  value: string;
  hint?: string;
}) {
  return (
    <div className="card p-5">
      <span className="text-sm text-slate-500">{label}</span>
      <div className="mt-1 text-2xl font-semibold">{value}</div>
      {hint && <p className="mt-1 text-xs text-slate-500">{hint}</p>}
    </div>
  );
}

function MyPlan() {
  const me = useSession();
  const [ov, setOv] = useState<Overview | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    api<Overview>("/me/overview")
      .then(setOv)
      .catch((e) => setError(e.message));
  }, []);

  if (error)
    return (
      <p className="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700">
        {error}
      </p>
    );
  if (!ov) return null;
  const p = ov.package;

  return (
    <section className="space-y-4">
      <h2 className="text-sm font-semibold uppercase tracking-wide text-slate-500">
        Gói dịch vụ: {p ? p.name : "Không giới hạn"}
      </h2>
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        {me.role === "user" &&
          (ov.quota_enabled && ov.disk_used_mb !== null ? (
            <Meter
              label="Dung lượng ổ cứng"
              used={ov.disk_used_mb}
              total={p?.disk_mb ?? 0}
            />
          ) : (
            <Stat
              label="Dung lượng ổ cứng"
              value={p ? limit(p.disk_mb, fmtMB) : "∞"}
              hint="Chưa đo được mức sử dụng"
            />
          ))}
        {me.role === "reseller" && (
          <>
            <Meter
              label="Dung lượng đã cấp"
              used={ov.allocated_disk_mb ?? 0}
              total={p?.disk_mb ?? 0}
            />
            <Stat
              label="Tài khoản khách"
              value={`${ov.allocated_users ?? 0} / ${p ? limit(p.max_users) : "∞"}`}
              hint={
                p?.oversell ? "Gói cho phép oversell dung lượng" : undefined
              }
            />
          </>
        )}
        {p && (
          <>
            <Stat
              label="Website"
              value={limit(p.max_sites)}
              hint={`Database: ${limit(p.max_dbs)} · FTP: ${limit(p.max_ftp)}`}
            />
            <Stat
              label="PHP"
              value={
                p.php_versions.length ? p.php_versions.join(", ") : "Tất cả"
              }
              hint={`CPU ${limit(p.cpu_pct, (n) => n + "%")} · RAM ${limit(p.mem_mb, fmtMB)}`}
            />
          </>
        )}
      </div>
    </section>
  );
}

export default function DashboardPage() {
  const me = useSession();
  if (me.role === "admin") return <ServerDashboard />;
  return (
    <div className="space-y-7">
      <div>
        <h1>Xin chào, {me.username}</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          Tổng quan workspace và gói dịch vụ của bạn.
        </p>
      </div>
      <MyPlan />
    </div>
  );
}
