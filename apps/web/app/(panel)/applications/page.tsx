"use client";
import { localeTag } from "@/lib/i18n";
import { t as translate } from "@/lib/i18n";

import { useCallback, useEffect, useState } from "react";
import { api, errMsg, isOpen, type AppInfo, type Task } from "@/lib/api";
import { ErrorBox } from "@/components/ui";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { openTask } from "@/components/operations";
import {
  Blocks,
  Code2,
  Database,
  Download,
  HardDrive,
  Loader2,
  Mail,
  RefreshCw,
  Search,
  ShieldCheck,
  Terminal,
  Wrench,
  Zap,
  Trash2,
} from "lucide-react";

const categories = [
  { id: "all", label: "Tất cả" },
  { id: "runtime", label: "Runtime" },
  { id: "database", label: "Database" },
  { id: "cache", label: "Cache" },
  { id: "tool", label: "Công cụ" },
  { id: "mail", label: "Email" },
];
function displayVersion(value: string) {
  return value.replace(/(?:\d+:)?(\d+(?:\.\d+)+)[+~-][^\s,→]*/g, "$1");
}

function AppCard({
  app,
  busy,
  select,
}: {
  app: AppInfo;
  busy: boolean;
  select: (app: AppInfo, version: string, remove?: boolean) => void;
}) {
  const available = app.available ?? [],
    installed = app.installed ?? [];
  const [version, setVersion] = useState(available[0] ?? "");
  const selected = available.includes(version) ? version : (available[0] ?? "");
  const update =
    installed.includes(selected) && ["php", "nodejs"].includes(app.id);
  const Icon =
    app.category === "database"
      ? Database
      : app.category === "cache"
        ? Zap
        : app.category === "mail"
          ? Mail
          : app.category === "tool"
            ? Wrench
            : Code2;
  return (
    <article className="card flex flex-col overflow-hidden">
      <div className="flex-1 p-5">
        <div className="mb-5 flex items-start justify-between gap-2">
          <div className="flex size-11 items-center justify-center rounded-xl border bg-muted/50">
            <Icon className="size-5" strokeWidth={1.5} />
          </div>
          <Badge variant={installed.length ? "secondary" : "outline"}>
            {translate(
              app.builtin
                ? translate("Tích hợp")
                : installed.length
                  ? translate("Đã cài")
                  : translate("Chưa cài"),
            )}
          </Badge>
        </div>
        <h2 className="text-base font-semibold tracking-tight">{app.name}</h2>
        <p className="mt-2 min-h-14 text-xs leading-5 text-muted-foreground">
          {translate(
            app.id === "php"
              ? translate("PHP-FPM, hỗ trợ chạy song song nhiều phiên bản.")
              : app.description,
          )}
        </p>
        <div className="mt-4 rounded-lg bg-muted/60 px-3 py-2.5">
          <span className="text-[10px] font-medium tracking-wide text-muted-foreground uppercase">
            {translate("Phiên bản đã cài")}
          </span>
          <div className="mt-2 flex flex-wrap gap-2">
            {installed.length ? (
              installed.map((v) => (
                <div
                  key={v}
                  className="flex items-center gap-1 rounded-md border bg-background pl-2 text-xs"
                >
                  <span className="font-mono">
                    {translate(displayVersion(app.installed_labels?.[v] ?? v))}
                  </span>
                  <Button
                    size="icon"
                    variant="ghost"
                    className="size-7 text-destructive"
                    disabled={busy || app.builtin || app.id === "postgresql"}
                    title={translate(
                      app.builtin || app.id === "postgresql"
                        ? translate("Database được bảo vệ")
                        : translate("Gỡ {0} {1}", [app.name, v]),
                    )}
                    aria-label={translate("Gỡ {0} {1}", [app.name, v])}
                    onClick={() => select(app, v, true)}
                  >
                    <Trash2 className="size-3.5" />
                  </Button>
                </div>
              ))
            ) : (
              <span className="text-xs text-muted-foreground">
                {translate("Chưa cài đặt")}
              </span>
            )}
          </div>
        </div>
        <ErrorBox msg={app.error ?? ""} />
      </div>
      <div className="space-y-3 border-t bg-muted/15 p-4">
        {!app.builtin ? (
          <>
            <label className="sr-only" htmlFor={`version-${app.id}`}>
              {translate("Phiên bản")} {app.name}
            </label>
            <select
              id={`version-${app.id}`}
              className="input h-9 text-xs"
              value={selected}
              onChange={(e) => setVersion(e.target.value)}
              disabled={busy || !selected}
            >
              {!available.length && (
                <option value="">{translate("Chưa có phiên bản")}</option>
              )}
              {available.map((v) => (
                <option key={v} value={v}>
                  {translate(displayVersion(app.version_labels?.[v] ?? v))}
                </option>
              ))}
            </select>
            <Button
              className="w-full"
              disabled={
                busy ||
                !selected ||
                !!app.error ||
                (installed.includes(selected) && !update)
              }
              onClick={() => select(app, selected)}
            >
              {update ? <RefreshCw /> : <Download />}
              {translate(
                update
                  ? translate("Cập nhật bản vá")
                  : installed.includes(selected)
                    ? translate("Đã cài phiên bản này")
                    : translate("Cài đặt ứng dụng"),
              )}
            </Button>
          </>
        ) : (
          <div className="flex h-[84px] items-center justify-center gap-2 text-xs text-muted-foreground">
            <ShieldCheck className="size-4" />
            {translate("Được quản lý cùng hệ thống")}
          </div>
        )}
        <details className="text-[10px] leading-5 text-muted-foreground">
          <summary className="cursor-pointer">
            {translate("Nguồn & thời gian kiểm tra")}
          </summary>
          <p className="mt-1 break-words">{translate(app.source ?? "—")}</p>
          {app.checked_at && (
            <p>
              {translate(new Date(app.checked_at).toLocaleString(localeTag()))}
            </p>
          )}
        </details>
      </div>
    </article>
  );
}
export default function ApplicationsPage() {
  const [apps, setApps] = useState<AppInfo[]>([]),
    [tasks, setTasks] = useState<Task[]>([]),
    [error, setError] = useState("");
  const [loading, setLoading] = useState(true),
    [refreshing, setRefreshing] = useState(false),
    [sending, setSending] = useState(false);
  const [category, setCategory] = useState("all"),
    [search, setSearch] = useState("");
  const [selection, setSelection] = useState<{
    app: AppInfo;
    version: string;
    remove?: boolean;
  } | null>(null);
  const load = useCallback(async (refresh = false) => {
    try {
      const r = await api<{ apps: AppInfo[] }>(
        `/apps${refresh ? "?refresh=1" : ""}`,
      );
      setApps(r.apps);
      setError("");
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  }, []);
  useEffect(() => {
    void load();
  }, [load]);
  useEffect(() => {
    let stopped = false;
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      try {
        const list = await api<Task[]>("/tasks");
        if (stopped) return;
        setTasks(
          list.filter(
            (t) =>
              [
                "app.install",
                "app.remove",
                "php.install",
                "pma.install",
              ].includes(t.type) && isOpen(t),
          ),
        );
        if (
          tasks.length &&
          !list.some(
            (t) =>
              [
                "app.install",
                "app.remove",
                "php.install",
                "pma.install",
              ].includes(t.type) && isOpen(t),
          )
        )
          void load();
      } catch {
      } finally {
        if (!stopped) timer = setTimeout(poll, 3000);
      }
    };
    poll();
    return () => {
      stopped = true;
      clearTimeout(timer);
    };
  }, [load, tasks.length]);
  async function install() {
    if (!selection) return;
    const { app, version, remove } = selection;
    setSelection(null);
    setSending(true);
    try {
      const task = await api<Task>(
        `/apps/${app.id}/${remove ? "remove" : "install"}`,
        {
          method: "POST",
          body: { version },
        },
      );
      setTasks((cur) => [task, ...cur]);
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setSending(false);
    }
  }
  const visible = apps.filter(
    (a) =>
      (category === "all" || a.category === category) &&
      `${a.name} ${a.description}`.toLowerCase().includes(search.toLowerCase()),
  );
  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <div className="mb-2 flex items-center gap-2 text-xs text-muted-foreground">
            <Blocks className="size-3.5" />
            Application catalog
          </div>
          <h1>{translate("Ứng dụng")}</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            {translate("Cài runtime và dịch vụ cần thiết cho website của bạn.")}
          </p>
        </div>
        <Button
          variant="outline"
          disabled={refreshing || sending || tasks.some(isOpen)}
          onClick={() => {
            setRefreshing(true);
            void load(true);
          }}
        >
          <RefreshCw className={refreshing ? "animate-spin" : ""} />
          {translate(
            refreshing
              ? translate("Đang kiểm tra…")
              : translate("Làm mới phiên bản"),
          )}
        </Button>
      </div>
      <div className="grid gap-4 sm:grid-cols-3">
        {[
          ["Trong catalog", apps.length, Blocks],
          [
            translate("Ứng dụng đã cài"),
            apps.filter((a) => a.installed.length).length,
            HardDrive,
          ],
          [translate("Tác vụ đang chạy"), tasks.length, Terminal],
        ].map(([label, value, Icon]) => {
          const I = Icon as typeof Blocks;
          return (
            <div
              className="card flex items-center justify-between p-4"
              key={String(label)}
            >
              <div>
                <p className="text-xs text-muted-foreground">
                  {translate(String(label))}
                </p>
                <p className="mt-1 text-2xl font-semibold tabular-nums">
                  {translate(loading ? "—" : String(value))}
                </p>
              </div>
              <I className="size-5 text-muted-foreground" strokeWidth={1.5} />
            </div>
          );
        })}
      </div>
      <ErrorBox msg={error} />
      {tasks.map((t) => (
        <button
          key={t.id}
          onClick={() => openTask(t)}
          className="flex w-full items-center gap-3 rounded-xl border bg-background px-4 py-3 text-left text-sm"
        >
          <Loader2 className="size-4 animate-spin" />
          <span className="flex-1">
            {translate(
              t.type === "app.remove"
                ? translate("Đang gỡ")
                : translate("Đang cài đặt"),
            )}{" "}
            {translate(t.target)}
          </span>
          <span className="text-xs text-muted-foreground">
            {translate("Xem tiến trình →")}
          </span>
        </button>
      ))}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex max-w-full gap-1 overflow-x-auto rounded-lg border bg-muted/40 p-1">
          {categories.map((c) => (
            <button
              key={c.id}
              onClick={() => setCategory(c.id)}
              className={`shrink-0 rounded-md px-3 py-1.5 text-xs font-medium transition ${category === c.id ? "bg-background shadow-xs" : "text-muted-foreground hover:text-foreground"}`}
            >
              {translate(c.label)}
            </button>
          ))}
        </div>
        <div className="relative w-full sm:w-60">
          <Search className="absolute top-2.5 left-3 size-4 text-muted-foreground" />
          <input
            className="input pl-9"
            placeholder={translate("Tìm ứng dụng…")}
            aria-label={translate("Tìm ứng dụng")}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
        </div>
      </div>
      {loading ? (
        <div className="grid gap-5 md:grid-cols-2 2xl:grid-cols-3">
          {[1, 2, 3, 4, 5, 6].map((i) => (
            <Skeleton key={i} className="h-80 rounded-xl" />
          ))}
        </div>
      ) : (
        <div className="grid gap-5 md:grid-cols-2 2xl:grid-cols-3">
          {visible.map((app) => (
            <AppCard
              key={app.id}
              app={app}
              busy={sending || tasks.some(isOpen)}
              select={(app, version, remove) =>
                setSelection({ app, version, remove })
              }
            />
          ))}
        </div>
      )}
      {!loading && !visible.length && (
        <div className="card py-16 text-center text-sm text-muted-foreground">
          {translate("Không tìm thấy ứng dụng phù hợp.")}
        </div>
      )}
      <p className="text-[11px] text-muted-foreground">
        {translate(
          "Danh sách lưu đệm 5 phút. Chọn “Làm mới phiên bản” để kiểm tra nguồn phát hành ngay.",
        )}
      </p>
      <Dialog
        open={!!selection}
        onOpenChange={(open) => {
          if (!open) setSelection(null);
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <div className="mb-2 flex size-11 items-center justify-center rounded-xl border bg-muted">
              <Download className="size-5" />
            </div>
            <DialogTitle>
              {translate(
                selection?.remove
                  ? translate("Gỡ cài đặt")
                  : translate("Cài đặt"),
              )}{" "}
              {translate(selection?.app.name)}?
            </DialogTitle>
            <DialogDescription>
              {translate(
                selection?.remove
                  ? translate(
                      "Phiên bản đã chọn sẽ được gỡ khỏi máy chủ. Website đang sử dụng phiên bản này phải được chuyển sang phiên bản khác trước.",
                    )
                  : translate(
                      "Ứng dụng sẽ được cài trên máy chủ. Bạn có thể theo dõi trạng thái và nhật ký trong popup tác vụ.",
                    ),
              )}
            </DialogDescription>
          </DialogHeader>
          <div className="rounded-lg border bg-muted/40 p-3 text-sm">
            <span className="text-xs text-muted-foreground">
              {translate("Phiên bản đã chọn")}
            </span>
            <p className="mt-1 break-all font-medium">
              {translate(
                displayVersion(
                  (selection?.remove
                    ? selection.app.installed_labels?.[selection.version]
                    : selection?.app.version_labels?.[selection.version]) ??
                    selection?.version ??
                    "",
                ),
              )}
            </p>
          </div>
          <div className="flex justify-end gap-2">
            <Button variant="outline" onClick={() => setSelection(null)}>
              {translate("Hủy")}
            </Button>
            <Button
              variant={selection?.remove ? "destructive" : "default"}
              onClick={install}
            >
              <Download />
              {translate(
                selection?.remove
                  ? translate("Xác nhận gỡ cài đặt")
                  : translate("Xác nhận cài đặt"),
              )}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
