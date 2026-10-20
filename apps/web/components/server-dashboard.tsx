"use client";
import { localeTag } from "@/lib/i18n";
import { t as translate } from "@/lib/i18n";

import { useEffect, useRef, useState } from "react";
import { api, errMsg, fmtBytes, fmtMB, type SystemInfo } from "@/lib/api";
import { Button } from "@/components/ui/button";
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
  ArrowDown,
  ArrowUp,
  Copy,
  Check,
  Globe2,
} from "lucide-react";
import { ErrorBox } from "@/components/ui";
import { Skeleton } from "@/components/ui/skeleton";

function uptime(sec: number) {
  const days = Math.floor(sec / 86400),
    hours = Math.floor((sec % 86400) / 3600);
  return translate("{0}{1} giờ {2} phút", [
    days ? translate("{0} ngày", [days]) + " " : "",
    hours,
    Math.floor((sec % 3600) / 60),
  ]);
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
    <div className="card min-w-0 p-5">
      <div className="mb-5 flex items-center justify-between">
        <span className="text-xs font-medium text-muted-foreground">
          {translate(label)}
        </span>
        <Icon className="size-4 text-muted-foreground" strokeWidth={1.6} />
      </div>
      <div className="break-words text-2xl font-semibold tracking-tight tabular-nums sm:text-3xl">
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
        {translate(detail)}
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
  const [ramHistory, setRamHistory] = useState<number[]>([]);
  const [rates, setRates] = useState<{ rx: number; tx: number } | null>(null);
  const previousNetwork = useRef<{ rx: number; tx: number; at: number } | null>(
    null,
  );
  const [copied, setCopied] = useState("");
  const copyTimer = useRef<ReturnType<typeof setTimeout> | undefined>(
    undefined,
  );
  useEffect(() => () => clearTimeout(copyTimer.current), []);
  async function copyIP(value: string) {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(value);
      clearTimeout(copyTimer.current);
      copyTimer.current = setTimeout(() => setCopied(""), 1800);
    } catch {
      setError(
        translate("Không thể sao chép. Hãy chọn và sao chép địa chỉ IP."),
      );
    }
  }
  function snapshot(value: SystemInfo) {
    const at = Date.now(),
      previous = previousNetwork.current;
    if (previous && at > previous.at) {
      const seconds = (at - previous.at) / 1000;
      setRates({
        rx: Math.max(0, value.network_rx_bytes - previous.rx) / seconds,
        tx: Math.max(0, value.network_tx_bytes - previous.tx) / seconds,
      });
    }
    previousNetwork.current = {
      rx: value.network_rx_bytes,
      tx: value.network_tx_bytes,
      at,
    };
    setInfo(value);
    setUpdated(new Date(at));
    setError("");
    setHistory((h) => [...h.slice(-29), value.cpu_percent]);
    setRamHistory((h) => [
      ...h.slice(-29),
      value.mem_total_mb ? (value.mem_used_mb / value.mem_total_mb) * 100 : 0,
    ]);
  }
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
        snapshot(value);
        if (
          rebootStarted.current &&
          value.boot_id &&
          value.boot_id !== oldBoot.current
        ) {
          setRebootState(
            translate("Máy chủ đã khởi động lại và kết nối thành công."),
          );
          setRebooting(false);
          rebootStarted.current = 0;
        } else if (
          rebootStarted.current &&
          Date.now() - rebootStarted.current > 180000
        ) {
          setRebootError(
            translate(
              "Chưa xác nhận được máy chủ đã reboot. Kiểm tra máy chủ trước khi thử lại.",
            ),
          );
          setRebooting(false);
          rebootStarted.current = 0;
        }
      } catch (e) {
        if (!disposed)
          setError(
            rebootStarted.current
              ? translate("Máy chủ đang khởi động lại. Đang chờ kết nối…")
              : errMsg(e),
          );
      } finally {
        if (!disposed) {
          if (
            rebootStarted.current &&
            Date.now() - rebootStarted.current > 180000
          ) {
            setRebootError(
              translate(
                "Quá thời gian chờ kết nối lại. Kiểm tra máy chủ trước khi thử lại.",
              ),
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
      snapshot(value);
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
        translate(
          "Đã lên lịch reboot sau 10 giây. Đang chờ máy chủ khởi động lại…",
        ),
      );
      setConfirmation("");
    } catch (e) {
      setRebootError(errMsg(e));
      setRebooting(false);
    }
  }
  const mem = info?.mem_total_mb
      ? (info.mem_used_mb / info.mem_total_mb) * 100
      : 0,
    disk = info?.disk_total_mb
      ? (info.disk_used_mb / info.disk_total_mb) * 100
      : 0;
  const chartPoints = (values: number[]) =>
    values
      .map(
        (v, i) =>
          `${values.length === 1 ? 600 : (i * 600) / (values.length - 1)},${100 - Math.max(0, Math.min(100, v))}`,
      )
      .join(" ");
  const points = chartPoints(history);
  const bootTime =
    info && updated
      ? new Date(updated.getTime() - info.uptime_sec * 1000).toLocaleString(
          localeTag(),
        )
      : "—";
  function Detail({ rows }: { rows: [string, string][] }) {
    return (
      <dl className="divide-y divide-border/60">
        {rows.map(([key, value]) => (
          <div
            key={key}
            className="flex flex-wrap items-start justify-between gap-x-5 gap-y-1 py-3 text-xs"
          >
            <dt className="text-muted-foreground">{translate(key)}</dt>
            <dd className="min-w-0 max-w-full break-words text-right font-medium [overflow-wrap:anywhere]">
              {value}
            </dd>
          </div>
        ))}
      </dl>
    );
  }
  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <div className="mb-2 flex items-center gap-2 text-xs text-muted-foreground">
            <span
              className={`size-1.5 rounded-full ${error ? "bg-amber-500" : "bg-emerald-500"}`}
            />
            {translate(
              updated
                ? translate("Cập nhật {0} · mỗi 5 giây", [
                    updated.toLocaleTimeString(localeTag()),
                  ])
                : translate("Đang kết nối máy chủ"),
            )}
          </div>
          <h1>{translate("Tổng quan máy chủ")}</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            {translate("Theo dõi tài nguyên và vận hành hạ tầng của bạn.")}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            onClick={refresh}
            disabled={loading || rebooting}
          >
            <RefreshCw className={loading ? "animate-spin" : ""} />
            {translate("Làm mới")}
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
            {translate("Khởi động lại")}
          </Button>
        </div>
      </div>
      {error && (
        <div
          role="status"
          className="rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800"
        >
          {translate(error)}
        </div>
      )}
      {rebootState && (
        <div
          role="status"
          className="rounded-lg border bg-background p-4 text-sm"
        >
          {translate(rebootState)}
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
          <section className="relative overflow-hidden rounded-2xl border border-neutral-800 bg-neutral-950 p-6 text-white sm:p-7">
            <div
              aria-hidden="true"
              className="pointer-events-none absolute -right-12 -top-32 size-96 rounded-full bg-emerald-400/10 blur-3xl"
            />
            <div className="relative flex flex-wrap items-start justify-between gap-6">
              <div className="flex min-w-0 items-center gap-4">
                <div className="flex size-14 shrink-0 items-center justify-center rounded-2xl border border-white/15 bg-white/5">
                  <Server
                    className="size-6 text-emerald-300"
                    strokeWidth={1.5}
                  />
                </div>
                <div className="min-w-0">
                  <p className="mb-1 text-[10px] font-medium uppercase tracking-[.18em] text-neutral-400">
                    {translate("Máy chủ của bạn")}
                  </p>
                  <h2 className="break-all text-xl font-semibold tracking-tight sm:text-2xl">
                    {info.hostname || "—"}
                  </h2>
                  <p className="mt-1.5 text-xs text-neutral-400">
                    {info.os_name} · {info.architecture} · {info.cpu_count} vCPU
                  </p>
                </div>
              </div>
              <span
                className={`inline-flex items-center gap-2 rounded-full border px-3 py-1.5 text-[11px] ${error ? "border-amber-400/30 bg-amber-400/10 text-amber-200" : "border-emerald-400/25 bg-emerald-400/10 text-emerald-300"}`}
              >
                <span
                  className={`size-1.5 rounded-full ${error ? "bg-amber-300" : "bg-emerald-300"}`}
                />
                {translate(error ? "Dữ liệu chưa cập nhật" : "Đã kết nối")}
              </span>
            </div>
            <div className="relative mt-6 grid gap-5 border-t border-white/10 pt-5 sm:grid-cols-3">
              {[
                ["Hệ điều hành", info.os_name],
                ["Thời gian hoạt động", uptime(info.uptime_sec)],
                ["Khởi động gần nhất", bootTime],
              ].map(([label, value]) => (
                <div key={label}>
                  <p className="text-[10px] text-neutral-400">
                    {translate(label)}
                  </p>
                  <p className="mt-1.5 text-xs font-medium">{value}</p>
                </div>
              ))}
            </div>
          </section>
          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
            <Metric
              label={translate("CPU sử dụng")}
              value={`${info.cpu_percent.toFixed(1)}%`}
              detail={`${info.cpu_count} vCPU · ${info.architecture}`}
              percent={info.cpu_percent}
              icon={Cpu}
            />
            <Metric
              label={translate("Bộ nhớ RAM")}
              value={`${mem.toFixed(1)}%`}
              detail={`${fmtMB(info.mem_used_mb)} / ${fmtMB(info.mem_total_mb)}`}
              percent={mem}
              icon={MemoryStick}
            />
            <Metric
              label={translate("Dung lượng ổ đĩa")}
              value={`${disk.toFixed(1)}%`}
              detail={translate("{0} / {1} · còn {2}", [
                fmtMB(info.disk_used_mb),
                fmtMB(info.disk_total_mb),
                fmtMB(info.disk_available_mb),
              ])}
              percent={disk}
              icon={HardDrive}
            />
            <Metric
              label={translate("Lưu lượng mạng")}
              value={rates ? `${fmtBytes(rates.rx + rates.tx)}/s` : "—"}
              detail={translate("Tốc độ nhận và gửi hiện tại")}
              icon={Network}
            />
          </div>
          <div className="grid gap-5 xl:grid-cols-3">
            <section className="card min-w-0 overflow-hidden xl:col-span-2">
              <div className="flex flex-wrap items-center justify-between gap-3 border-b px-5 py-4">
                <div>
                  <h2 className="text-sm font-semibold">
                    {translate("Hiệu suất tài nguyên")}
                  </h2>
                  <p className="mt-1 text-[11px] text-muted-foreground">
                    {translate("30 mẫu gần nhất · cập nhật mỗi 5 giây")}
                  </p>
                </div>
                <div className="flex gap-4 text-[10px]">
                  <span className="flex items-center gap-1.5">
                    <span className="size-2 rounded-full bg-blue-500" />
                    CPU
                  </span>
                  <span className="flex items-center gap-1.5">
                    <span className="size-2 rounded-full bg-violet-500" />
                    RAM
                  </span>
                </div>
              </div>
              <div className="p-5">
                <div className="mb-6 grid grid-cols-3 divide-x rounded-xl border bg-muted/30 py-3">
                  {info.load.map((value, i) => (
                    <div key={i} className="px-3">
                      <p className="text-[9px] text-muted-foreground">
                        Load · {[1, 5, 15][i]} {translate("phút")}
                      </p>
                      <p className="mt-1 font-mono text-base font-medium">
                        {value.toFixed(2)}
                      </p>
                    </div>
                  ))}
                </div>
                <div className="relative ml-7 h-44 border-b">
                  <div className="pointer-events-none absolute inset-0 flex flex-col justify-between">
                    {[100, 75, 50, 25, 0].map((n) => (
                      <div
                        key={n}
                        className="border-t border-dashed border-border/70"
                      >
                        <span className="absolute -left-8 -translate-y-1/2 text-[9px] text-muted-foreground">
                          {n}%
                        </span>
                      </div>
                    ))}
                  </div>
                  <svg
                    viewBox="0 0 600 100"
                    preserveAspectRatio="none"
                    className="absolute inset-0 h-full w-full overflow-visible"
                    role="img"
                    aria-label={translate("Biểu đồ CPU và RAM")}
                  >
                    <polyline
                      points={chartPoints(ramHistory)}
                      fill="none"
                      stroke="#8b5cf6"
                      strokeWidth="2"
                      strokeLinejoin="round"
                      vectorEffect="non-scaling-stroke"
                    />
                    <polyline
                      points={points}
                      fill="none"
                      stroke="#3b82f6"
                      strokeWidth="2"
                      strokeLinejoin="round"
                      vectorEffect="non-scaling-stroke"
                    />
                  </svg>
                </div>
                <div className="mt-3 flex justify-between text-[9px] text-muted-foreground">
                  <span>{translate("Lịch sử phiên")}</span>
                  <span>{translate("Hiện tại")}</span>
                </div>
                <p className="mt-4 text-[10px] leading-5 text-muted-foreground">
                  {translate(
                    "Số liệu thực từ máy chủ · biểu đồ bắt đầu thu thập khi mở trang.",
                  )}
                </p>
              </div>
            </section>
            <section className="card min-w-0 overflow-hidden">
              <div className="flex items-center gap-2 border-b px-5 py-4">
                <Globe2 className="size-4 text-muted-foreground" />
                <h2 className="text-sm font-semibold">
                  {translate("Địa chỉ mạng")}
                </h2>
              </div>
              <div className="space-y-3 p-5">
                {(info.ip_addresses ?? []).length ? (
                  info.ip_addresses.map((ip) => (
                    <div key={ip} className="rounded-xl border bg-muted/20 p-3">
                      <div className="mb-2 flex items-center justify-between">
                        <span className="text-[9px] font-medium tracking-wider text-muted-foreground">
                          {ip.includes(":") ? "IPv6" : "IPv4"}
                        </span>
                        <button
                          type="button"
                          className="rounded-md p-1.5 text-muted-foreground hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                          aria-label={`${translate("Sao chép")} ${ip}`}
                          onClick={() => copyIP(ip)}
                        >
                          {copied === ip ? (
                            <Check className="size-3.5 text-emerald-600" />
                          ) : (
                            <Copy className="size-3.5" />
                          )}
                        </button>
                      </div>
                      <p className="break-all font-mono text-xs font-medium">
                        {ip}
                      </p>
                    </div>
                  ))
                ) : (
                  <p className="text-xs text-muted-foreground">
                    {translate("Chưa có địa chỉ IP")}
                  </p>
                )}
                <div className="border-t pt-4">
                  <p className="text-[10px] text-muted-foreground">
                    {translate("Tổng từ lúc khởi động · không gồm loopback")}
                  </p>
                  <div className="mt-3 grid grid-cols-2 gap-3">
                    {[
                      ["Nhận", info.network_rx_bytes],
                      ["Gửi", info.network_tx_bytes],
                    ].map(([label, value], i) => (
                      <div key={label}>
                        <div className="mb-1.5 flex items-center gap-1 text-[10px] text-muted-foreground">
                          {i === 0 ? (
                            <ArrowDown className="size-3 text-emerald-600" />
                          ) : (
                            <ArrowUp className="size-3 text-blue-500" />
                          )}
                          {translate(label)}
                        </div>
                        <p className="font-mono text-sm font-medium">
                          {fmtBytes(Number(value))}
                        </p>
                        <p className="mt-1 text-[10px] text-muted-foreground">
                          {rates
                            ? `${fmtBytes(i === 0 ? rates.rx : rates.tx)}/s`
                            : "—"}
                        </p>
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            </section>
          </div>
          <div className="grid gap-5 lg:grid-cols-3">
            <section className="card min-w-0 p-5">
              <div className="mb-3 flex items-center gap-2">
                <Cpu className="size-4 text-muted-foreground" />
                <h2 className="text-sm font-semibold">
                  {translate("Bộ xử lý & hệ thống")}
                </h2>
              </div>
              <Detail
                rows={[
                  [translate("Bộ xử lý"), info.cpu_model || "—"],
                  [translate("Số lõi CPU"), `${info.cpu_count} vCPU`],
                  [translate("Kiến trúc"), info.architecture],
                  ["Kernel", info.kernel],
                  ["Boot ID", info.boot_id || "—"],
                ]}
              />
            </section>
            <section className="card min-w-0 p-5">
              <div className="mb-3 flex items-center gap-2">
                <MemoryStick className="size-4 text-muted-foreground" />
                <h2 className="text-sm font-semibold">
                  {translate("Bộ nhớ & Swap")}
                </h2>
              </div>
              <Detail
                rows={[
                  [translate("Tổng RAM"), fmtMB(info.mem_total_mb)],
                  [translate("Đang sử dụng"), fmtMB(info.mem_used_mb)],
                  [
                    translate("RAM khả dụng"),
                    fmtMB(Math.max(0, info.mem_total_mb - info.mem_used_mb)),
                  ],
                  [translate("Tổng Swap"), fmtMB(info.swap_total_mb)],
                  [translate("Swap đang dùng"), fmtMB(info.swap_used_mb)],
                ]}
              />
            </section>
            <section className="card min-w-0 p-5">
              <div className="mb-3 flex items-center gap-2">
                <HardDrive className="size-4 text-muted-foreground" />
                <h2 className="text-sm font-semibold">
                  {translate("Lưu trữ & hạn mức")}
                </h2>
              </div>
              <Detail
                rows={[
                  [translate("Tổng dung lượng"), fmtMB(info.disk_total_mb)],
                  [translate("Đang sử dụng"), fmtMB(info.disk_used_mb)],
                  [
                    translate("Dung lượng khả dụng"),
                    fmtMB(info.disk_available_mb),
                  ],
                  [
                    "Disk quota",
                    translate(info.quota_enabled ? "Đang bật" : "Chưa bật"),
                  ],
                  [translate("Phân vùng quota"), info.quota_mount || "—"],
                ]}
              />
            </section>
          </div>
          {!info.quota_enabled && (
            <p className="text-xs text-amber-700">
              {translate(
                "Disk quota chưa bật trên phân vùng chứa /home; giới hạn dung lượng chưa có hiệu lực.",
              )}
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
            <DialogTitle>{translate("Khởi động lại máy chủ?")}</DialogTitle>
            <DialogDescription>
              {translate(
                "Toàn bộ website, database và panel sẽ tạm ngắt kết nối. Máy chủ sẽ reboot sau 10 giây; chỉ thực hiện khi các tác vụ đã hoàn tất.",
              )}
            </DialogDescription>
          </DialogHeader>
          {!rebooting && (
            <label className="label">
              {translate("Nhập REBOOT để xác nhận")}{" "}
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
            <p className="text-sm text-muted-foreground">
              {translate(rebootState)}
            </p>
          )}
          <div className="flex justify-end gap-2">
            <Button
              variant="outline"
              disabled={rebooting}
              onClick={() => setDialog(false)}
            >
              {translate("Hủy")}
            </Button>
            <Button
              variant="destructive"
              disabled={rebooting || confirmation !== "REBOOT"}
              onClick={reboot}
            >
              {rebooting ? <Loader2 className="animate-spin" /> : <Power />}
              {translate(
                rebooting
                  ? translate("Đang chờ máy chủ…")
                  : translate("Khởi động lại"),
              )}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
