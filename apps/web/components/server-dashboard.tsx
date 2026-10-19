"use client";

import { useEffect, useRef, useState } from "react";
import { api, errMsg, fmtBytes, fmtMB, type SystemInfo } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Cpu,
  MemoryStick,
  HardDrive,
  Network,
  RefreshCw,
  Power,
  Loader2,
  Server,
  Clock3,
  ArrowDown,
  ArrowUp,
} from "lucide-react";
import { ErrorBox } from "@/components/ui";
import { Skeleton } from "@/components/ui/skeleton";

function uptime(sec: number) {
  const days = Math.floor(sec / 86400),
    hours = Math.floor((sec % 86400) / 3600);
  return `${days ? `${days} ngày ` : ""}${hours} giờ ${Math.floor((sec % 3600) / 60)} phút`;
}
function Metric({
  label,
  value,
  detail,
  percent,
  icon: Icon,
}: {
  label: string;
  value: string;
  detail: string;
  percent?: number;
  icon: typeof Cpu;
}) {
  const pct = Math.max(0, Math.min(100, percent ?? 0));
  return (
    <div className="card p-5">
      <div className="mb-5 flex items-center justify-between">
        <span className="text-xs font-medium text-muted-foreground">
          {label}
        </span>
        <Icon className="size-4 text-muted-foreground" strokeWidth={1.6} />
      </div>
      <div className="text-3xl font-semibold tracking-tight tabular-nums">
        {value}
      </div>
      {percent !== undefined && (
        <div className="mt-4 h-1.5 overflow-hidden rounded-full bg-muted">
          <div
            className={`h-full rounded-full transition-[width] duration-700 ${pct > 90 ? "bg-destructive" : pct > 75 ? "bg-amber-500" : "bg-primary"}`}
            style={{ width: `${pct}%` }}
          />
        </div>
      )}
      <p className="mt-3 text-[11px] leading-5 text-muted-foreground">
        {detail}
      </p>
    </div>
  );
}

export function ServerDashboard() {
  const [info, setInfo] = useState<SystemInfo | null>(null),
    [error, setError] = useState("");
  const [updated, setUpdated] = useState<Date | null>(null),
    [loading, setLoading] = useState(false);
  const [history, setHistory] = useState<number[]>([]);
  const [dialog, setDialog] = useState(false),
    [confirmation, setConfirmation] = useState("");
  const [rebooting, setRebooting] = useState(false),
    [rebootError, setRebootError] = useState("");
  const [rebootState, setRebootState] = useState("");
  const rebootStarted = useRef(0),
    oldBoot = useRef("");
  useEffect(() => {
    let disposed = false;
    let timer: ReturnType<typeof setTimeout>;
    const load = async () => {
      try {
        const value = await api<SystemInfo>("/system/info");
        if (disposed) return;
        setInfo(value);
        setUpdated(new Date());
        setError("");
        setHistory((h) => [...h.slice(-29), value.cpu_percent]);
        if (
          rebootStarted.current &&
          value.boot_id &&
          value.boot_id !== oldBoot.current
        ) {
          setRebootState("Máy chủ đã khởi động lại và kết nối thành công.");
          setRebooting(false);
          rebootStarted.current = 0;
        } else if (
          rebootStarted.current &&
          Date.now() - rebootStarted.current > 180000
        ) {
          setRebootError(
            "Chưa xác nhận được máy chủ đã reboot. Kiểm tra máy chủ trước khi thử lại.",
          );
          setRebooting(false);
          rebootStarted.current = 0;
        }
      } catch (e) {
        if (!disposed)
          setError(
            rebootStarted.current
              ? "Máy chủ đang khởi động lại. Đang chờ kết nối…"
              : errMsg(e),
          );
      } finally {
        if (!disposed) {
          if (
            rebootStarted.current &&
            Date.now() - rebootStarted.current > 180000
          ) {
            setRebootError(
              "Quá thời gian chờ kết nối lại. Kiểm tra máy chủ trước khi thử lại.",
            );
            setRebooting(false);
            rebootStarted.current = 0;
          }
          setLoading(false);
          timer = setTimeout(load, 5000);
        }
      }
    };
    load();
    return () => {
      disposed = true;
      clearTimeout(timer);
    };
  }, []);
  async function refresh() {
    setLoading(true);
    try {
      const value = await api<SystemInfo>("/system/info");
      setInfo(value);
      setUpdated(new Date());
      setError("");
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setLoading(false);
    }
  }
  async function reboot() {
    setRebooting(true);
    setRebootError("");
    try {
      await api("/system/reboot", { method: "POST", body: { confirmation } });
      oldBoot.current = info?.boot_id ?? "";
      rebootStarted.current = Date.now();
      setRebootState(
        "Đã lên lịch reboot sau 10 giây. Đang chờ máy chủ khởi động lại…",
      );
      setConfirmation("");
    } catch (e) {
      setRebootError(errMsg(e));
      setRebooting(false);
    }
  }
  const mem = info ? (info.mem_used_mb / info.mem_total_mb) * 100 : 0,
    disk = info ? (info.disk_used_mb / info.disk_total_mb) * 100 : 0;
  const points = history
    .map((value, i) => `${(i * 600) / 29},${100 - value}`)
    .join(" ");
  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <div className="mb-2 flex items-center gap-2 text-xs text-muted-foreground">
            <span
              className={`size-1.5 rounded-full ${error ? "bg-amber-500" : "bg-emerald-500"}`}
            />
            {updated
              ? `Cập nhật ${updated.toLocaleTimeString("vi-VN")} · mỗi 5 giây`
              : "Đang kết nối máy chủ"}
          </div>
          <h1>Tổng quan máy chủ</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            Theo dõi tài nguyên và vận hành hạ tầng của bạn.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            onClick={refresh}
            disabled={loading || rebooting}
          >
            <RefreshCw className={loading ? "animate-spin" : ""} />
            Làm mới
          </Button>
          <Button
            variant="outline"
            onClick={() => {
              setDialog(true);
              setRebootError("");
            }}
            disabled={!info}
          >
            <Power />
            Khởi động lại
          </Button>
        </div>
      </div>
      {error && (
        <div
          role="status"
          className="rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800"
        >
          {error}
        </div>
      )}
      {rebootState && (
        <div
          role="status"
          className="rounded-lg border bg-background p-4 text-sm"
        >
          {rebootState}
        </div>
      )}
      {!info ? (
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          {[1, 2, 3, 4].map((i) => (
            <Skeleton key={i} className="h-44 rounded-xl" />
          ))}
        </div>
      ) : (
        <>
          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
            <Metric
              label="CPU sử dụng"
              value={`${info.cpu_percent.toFixed(1)}%`}
              detail={`${info.cpu_count} vCPU · ${info.architecture}`}
              percent={info.cpu_percent}
              icon={Cpu}
            />
            <Metric
              label="Bộ nhớ RAM"
              value={`${mem.toFixed(1)}%`}
              detail={`${fmtMB(info.mem_used_mb)} / ${fmtMB(info.mem_total_mb)}`}
              percent={mem}
              icon={MemoryStick}
            />
            <Metric
              label="Dung lượng ổ đĩa"
              value={`${disk.toFixed(1)}%`}
              detail={`${fmtMB(info.disk_used_mb)} / ${fmtMB(info.disk_total_mb)} · còn ${fmtMB(info.disk_available_mb)}`}
              percent={disk}
              icon={HardDrive}
            />
            <Metric
              label="Thời gian hoạt động"
              value={
                info.uptime_sec >= 86400
                  ? `${Math.floor(info.uptime_sec / 86400)} ngày`
                  : `${Math.floor(info.uptime_sec / 3600)} giờ`
              }
              detail={uptime(info.uptime_sec)}
              icon={Clock3}
            />
          </div>
          <div className="grid gap-5 xl:grid-cols-5">
            <section className="card overflow-hidden xl:col-span-3">
              <div className="flex items-center justify-between border-b px-5 py-4">
                <div>
                  <h2 className="text-sm font-semibold">Hoạt động CPU</h2>
                  <p className="mt-1 text-xs text-muted-foreground">
                    30 mẫu gần nhất trong phiên · phần trăm
                  </p>
                </div>
                <Badge variant="outline">Live</Badge>
              </div>
              <div className="p-5">
                <div className="mb-5 flex gap-5 text-xs text-muted-foreground">
                  {info.load.map((v, i) => (
                    <span key={i}>
                      Load {[1, 5, 15][i]}m{" "}
                      <b className="ml-1 font-medium text-foreground">
                        {v.toFixed(2)}
                      </b>
                    </span>
                  ))}
                </div>
                <div className="relative h-36 border-b border-l">
                  <div className="absolute inset-0 flex flex-col justify-between text-[9px] text-muted-foreground">
                    {[100, 50, 0].map((n) => (
                      <div key={n} className="border-t border-dashed">
                        <span className="relative -top-2 bg-card pr-2">
                          {n}%
                        </span>
                      </div>
                    ))}
                  </div>
                  <svg
                    viewBox="0 0 600 100"
                    preserveAspectRatio="none"
                    className="absolute inset-0 h-full w-full"
                    role="img"
                    aria-label="Biểu đồ CPU"
                  >
                    <polyline
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="1.5"
                      vectorEffect="non-scaling-stroke"
                      points={points}
                    />
                    {history.length === 1 && (
                      <circle
                        cx="0"
                        cy={100 - history[0]}
                        r="2"
                        fill="currentColor"
                      />
                    )}
                  </svg>
                </div>
                <p className="mt-3 text-[10px] text-muted-foreground">
                  Số liệu thực từ máy chủ · biểu đồ bắt đầu thu thập khi mở
                  trang.
                </p>
              </div>
            </section>
            <section className="card xl:col-span-2">
              <div className="flex items-center gap-2 border-b px-5 py-4">
                <Server className="size-4" />
                <h2 className="text-sm font-semibold">Thông tin hệ thống</h2>
              </div>
              <dl className="divide-y px-5 text-xs">
                {[
                  ["Hostname", info.hostname],
                  ["IP VPS", info.ip_addresses?.join(" · ") || "—"],
                  ["Hệ điều hành", info.os_name],
                  ["Kernel", info.kernel],
                  ["Bộ xử lý", info.cpu_model || `${info.cpu_count} vCPU`],
                  [
                    "Swap",
                    `${fmtMB(info.swap_used_mb)} / ${fmtMB(info.swap_total_mb)}`,
                  ],
                  [
                    "Disk quota",
                    info.quota_enabled
                      ? `Đang bật · ${info.quota_mount}`
                      : "Chưa bật",
                  ],
                ].map(([k, v]) => (
                  <div key={k} className="flex justify-between gap-5 py-3">
                    <dt className="shrink-0 text-muted-foreground">{k}</dt>
                    <dd className="break-all text-right font-medium">{v}</dd>
                  </div>
                ))}
              </dl>
            </section>
          </div>
          <section className="card flex flex-wrap items-center justify-between gap-5 px-5 py-4">
            <div className="flex items-center gap-3">
              <div className="rounded-lg border p-2">
                <Network className="size-4" />
              </div>
              <div>
                <h2 className="text-sm font-medium">Lưu lượng mạng</h2>
                <p className="mt-1 text-[11px] text-muted-foreground">
                  Tổng từ lúc khởi động · không gồm loopback
                </p>
              </div>
            </div>
            <div className="flex gap-8 text-sm">
              <span className="flex items-center gap-2">
                <ArrowDown className="size-4 text-emerald-600" />
                {fmtBytes(info.network_rx_bytes)}
              </span>
              <span className="flex items-center gap-2">
                <ArrowUp className="size-4 text-blue-600" />
                {fmtBytes(info.network_tx_bytes)}
              </span>
            </div>
          </section>
          {!info.quota_enabled && (
            <p className="text-xs text-amber-700">
              Disk quota chưa bật trên phân vùng chứa /home; giới hạn dung lượng
              chưa có hiệu lực.
            </p>
          )}
        </>
      )}
      <Dialog
        open={dialog}
        onOpenChange={(open) => {
          if (!rebooting) setDialog(open);
        }}
      >
        <DialogContent className="sm:max-w-md" showCloseButton={!rebooting}>
          <DialogHeader>
            <div className="mb-2 flex size-11 items-center justify-center rounded-xl bg-destructive/10 text-destructive">
              <Power className="size-5" />
            </div>
            <DialogTitle>Khởi động lại máy chủ?</DialogTitle>
            <DialogDescription>
              Toàn bộ website, database và panel sẽ tạm ngắt kết nối. Máy chủ sẽ
              reboot sau 10 giây; chỉ thực hiện khi các tác vụ đã hoàn tất.
            </DialogDescription>
          </DialogHeader>
          {!rebooting && (
            <label className="label">
              Nhập REBOOT để xác nhận
              <input
                className="input mt-2 font-mono"
                value={confirmation}
                onChange={(e) => setConfirmation(e.target.value)}
                autoComplete="off"
                placeholder="REBOOT"
              />
            </label>
          )}
          <ErrorBox msg={rebootError} />
          {rebootState && (
            <p className="text-sm text-muted-foreground">{rebootState}</p>
          )}
          <div className="flex justify-end gap-2">
            <Button
              variant="outline"
              disabled={rebooting}
              onClick={() => setDialog(false)}
            >
              Hủy
            </Button>
            <Button
              variant="destructive"
              disabled={rebooting || confirmation !== "REBOOT"}
              onClick={reboot}
            >
              {rebooting ? <Loader2 className="animate-spin" /> : <Power />}
              {rebooting ? "Đang chờ máy chủ…" : "Khởi động lại"}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
