"use client";
import { useCallback, useEffect, useState, useRef } from "react";
import Link from "next/link";
import { openTask } from "@/components/operations";
import { useUnsavedChanges } from "@/lib/use-unsaved-changes";
import { t } from "@/lib/i18n";
import { api, errMsg, isOpen, type Task } from "@/lib/api";
import { ErrorBox } from "@/components/ui";
import { Button, buttonVariants } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import {
  Download,
  Trash2,
  RotateCcw,
  Save,
  Search,
  Settings2,
  Blocks,
  RefreshCw,
  Loader2,
} from "lucide-react";

type Module = {
  upgradable: boolean;
  enabled: boolean;
  loaded: boolean;
  builtin: boolean;
  candidate: string;
  modules: string[];
  name: string;
  version: string;
  installed: boolean;
  removable: boolean;
};
type Setting = { name: string; value: string; kind: string; custom: boolean };
type Management = { modules: Module[]; settings: Setting[] };
type Change = {
  operation?: string;
  module?: string;
  remove?: boolean;
  settings?: Record<string, string>;
  reset?: string[];
};
export default function PHPPage() {
  const [logs, setLogs] = useState<string | null>(null);
  const generation = useRef(0);
  const [versions, setVersions] = useState<string[]>([]),
    [version, setVersion] = useState("");
  const [data, setData] = useState<Management | null>(null),
    [tab, setTab] = useState("modules"),
    [search, setSearch] = useState("");
  const [error, setError] = useState(""),
    [loading, setLoading] = useState(false),
    [sending, setSending] = useState(false),
    [task, setTask] = useState<Task | null>(null);
  const [edits, setEdits] = useState<Record<string, string>>({}),
    [resets, setResets] = useState<string[]>([]),
    [confirm, setConfirm] = useState<Change | null>(null),
    [page, setPage] = useState(1);
  useEffect(() => {
    api<{ installed: string[] }>("/php")
      .then((x) => {
        const v = x.installed ?? [];
        setVersions(v);
        setVersion(v.at(-1) ?? "");
      })
      .catch((e) => setError(errMsg(e)));
  }, []);
  const load = useCallback(async () => {
    if (!version) return;
    const seq = ++generation.current;
    setLoading(true);
    setError("");
    try {
      const result = await api<Management>(`/php/${version}/manage`);
      if (seq !== generation.current) return;
      setData(result);
      setEdits({});
      setResets([]);
    } catch (e) {
      if (seq !== generation.current) return;
      setError(errMsg(e));
      setData(null);
    } finally {
      if (seq === generation.current) setLoading(false);
    }
  }, [version]);
  useEffect(() => {
    setData(null);
    void load();
  }, [load]);
  useEffect(() => {
    setPage(1);
  }, [version, search, tab]);
  useEffect(() => {
    if (!task || !isOpen(task)) return;
    let stopped = false;
    const timer = setInterval(async () => {
      try {
        const next = await api<Task>(`/tasks/${task.id}`);
        if (stopped) return;
        setTask(next);
        if (!isOpen(next)) {
          if (next.status === "failed")
            setError(next.log || t("Thao tác thất bại"));
          else void load();
        }
      } catch (e) {
        if (!stopped) setError(errMsg(e));
      }
    }, 2500);
    return () => {
      stopped = true;
      clearInterval(timer);
    };
  }, [task, load]);
  useEffect(() => {
    if (!version) return;
    let stopped = false;
    api<Task[]>("/tasks")
      .then((list) => {
        if (!stopped)
          setTask((current) =>
            current && isOpen(current) && current.target === `PHP ${version}`
              ? current
              : (list.find(
                  (x) =>
                    x.type === "php.manage" &&
                    x.target === `PHP ${version}` &&
                    isOpen(x),
                ) ?? null),
          );
      })
      .catch((e) => {
        if (!stopped) setError(errMsg(e));
      });
    return () => {
      stopped = true;
    };
  }, [version]);
  const dirty = Object.keys(edits).length + resets.length > 0;
  const unsaved = useUnsavedChanges(dirty);
  const busy = sending || !!(task && isOpen(task));
  async function apply() {
    if (!confirm) return;
    setSending(true);
    setError("");
    const change = confirm;
    setConfirm(null);
    try {
      setTask(
        await api<Task>(`/php/${version}/manage`, {
          method: "POST",
          body: change,
        }),
      );
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setSending(false);
    }
  }
  const modules = (data?.modules ?? []).filter((m) =>
    m.name.includes(search.toLowerCase()),
  );
  const settings = (data?.settings ?? []).filter((s) =>
    s.name.includes(search.toLowerCase()),
  );
  const rows = tab === "modules" ? modules.length : settings.length,
    pages = Math.max(1, Math.ceil(rows / 15)),
    current = Math.min(page, pages);
  const changed = Object.keys(edits).length + resets.length;
  function edit(s: Setting, value: string) {
    setResets((x) => x.filter((k) => k !== s.name));
    setEdits((x) => {
      const next = { ...x };
      if (value === s.value) delete next[s.name];
      else next[s.name] = value;
      return next;
    });
  }
  function reset(s: Setting) {
    setEdits((x) => {
      const next = { ...x };
      delete next[s.name];
      return next;
    });
    setResets((x) =>
      x.includes(s.name) ? x.filter((k) => k !== s.name) : [...x, s.name],
    );
  }
  return (
    <div className="space-y-6">
      {unsaved.dialog}
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1>PHP</h1>
          <p className="mt-2 text-sm text-muted-foreground">
            {t("Quản lý mô-đun và cấu hình PHP-FPM theo từng phiên bản.")}
          </p>
        </div>
        <Link
          className={buttonVariants({ variant: "outline" })}
          href="/applications/"
        >
          {t("Cài phiên bản PHP")}
        </Link>
      </div>
      <ErrorBox msg={error} />
      <div className="grid gap-4 sm:grid-cols-3">
        {[
          [t("Phiên bản đã cài"), versions.length],
          [
            t("Mô-đun đã cài"),
            (data?.modules ?? []).filter((m) => m.installed).length,
          ],
          [
            t("Thiết lập tùy chỉnh"),
            (data?.settings ?? []).filter((s) => s.custom).length,
          ],
        ].map(([label, value]) => (
          <div className="card p-5" key={label}>
            <p className="text-xs text-muted-foreground">{label}</p>
            <p className="mt-2 text-2xl font-semibold">{value}</p>
          </div>
        ))}
      </div>
      <div className="card overflow-hidden">
        <div className="flex flex-wrap items-center justify-between gap-4 border-b p-4">
          <div className="flex gap-2">
            <Button
              variant={tab === "modules" ? "secondary" : "ghost"}
              onClick={() => setTab("modules")}
            >
              <Blocks className="size-4" />
              {t("Mô-đun PHP")}
            </Button>
            <Button
              variant={tab === "settings" ? "secondary" : "ghost"}
              onClick={() => setTab("settings")}
            >
              <Settings2 className="size-4" />
              {t("Thiết lập PHP")}
            </Button>
          </div>
          <label className="flex items-center gap-3 text-sm">
            <span>{t("Phiên bản PHP")}</span>
            <select
              aria-label={t("Phiên bản PHP")}
              className="input w-28"
              value={version}
              disabled={busy || changed > 0}
              onChange={(e) => setVersion(e.target.value)}
            >
              {versions.map((v) => (
                <option key={v}>{v}</option>
              ))}
            </select>
          </label>
        </div>
        <div className="flex flex-wrap items-center justify-between gap-3 p-4">
          <div className="relative">
            <Search className="absolute top-3 left-3 size-4 text-muted-foreground" />
            <input
              className="input pl-9"
              placeholder={t("Tìm kiếm")}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
          </div>
          <div className="flex gap-2">
            {changed > 0 && (
              <>
                <Button
                  variant="ghost"
                  disabled={busy}
                  onClick={() => {
                    setEdits({});
                    setResets([]);
                  }}
                >
                  {t("Hủy thay đổi")}
                </Button>
                <Button
                  disabled={busy}
                  onClick={() => setConfirm({ settings: edits, reset: resets })}
                >
                  <Save className="size-4" />
                  {t("Lưu thay đổi")} ({changed})
                </Button>
              </>
            )}
            <Button
              variant="outline"
              disabled={busy || loading || changed > 0}
              onClick={() => setConfirm({ operation: "refresh" })}
            >
              {t("Cập nhật kho mô-đun")}
            </Button>
            <Button
              variant="outline"
              disabled={!version}
              onClick={async () => {
                try {
                  const r = await api<{ log: string }>(`/php/${version}/logs`);
                  setLogs(r.log);
                } catch (e) {
                  setError(errMsg(e));
                }
              }}
            >
              {t("Nhật ký PHP")}
            </Button>
            <Button
              variant="outline"
              disabled={loading || busy || changed > 0}
              onClick={() => void load()}
              aria-label={t("Làm mới")}
            >
              <RefreshCw
                className={loading ? "size-4 animate-spin" : "size-4"}
              />
            </Button>
          </div>
        </div>
        {tab === "settings" && (
          <p className="mx-4 mb-4 rounded-lg bg-muted/60 p-3 text-xs leading-5 text-muted-foreground">
            {t(
              "Áp dụng cho PHP-FPM của toàn bộ website dùng phiên bản này. Cấu hình riêng của website có thể ghi đè các giá trị này. Nút khôi phục sẽ dùng lại giá trị từ hệ thống.",
            )}
          </p>
        )}
        {busy && (
          <p className="flex items-center gap-2 border-y bg-muted/50 p-4 text-sm">
            <Loader2 className="size-4 animate-spin" />
            {t(
              "Đang áp dụng thay đổi. Theo dõi tiến trình trong popup tác vụ.",
            )}
            {task && (
              <Button
                size="sm"
                variant="outline"
                onClick={() => openTask(task)}
              >
                {t("Xem tiến trình")}
              </Button>
            )}
          </p>
        )}
        {loading ? (
          <p className="p-8 text-center text-muted-foreground">
            {t("Đang tải…")}
          </p>
        ) : !versions.length ? (
          <div className="p-10 text-center text-muted-foreground">
            {t("Chưa cài phiên bản PHP. Hãy cài PHP trong trang Ứng dụng.")}
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead className="bg-muted/40 text-xs text-muted-foreground">
                <tr>
                  <th className="px-5 py-3">{t("Tên")}</th>
                  <th className="px-5 py-3">
                    {t(tab === "modules" ? "Trạng thái" : "Giá trị")}
                  </th>
                  <th className="px-5 py-3 text-right">{t("Thao tác")}</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {tab === "modules"
                  ? modules.slice((current - 1) * 15, current * 15).map((m) => (
                      <tr key={m.name}>
                        <td className="px-5 py-4">
                          <p className="font-medium">{m.name}</p>
                          {m.version && (
                            <p className="mt-1 text-xs text-muted-foreground">
                              {m.version}
                            </p>
                          )}
                        </td>
                        <td className="px-5 py-4">
                          <Badge
                            variant={m.installed ? "secondary" : "outline"}
                          >
                            {t(
                              m.builtin
                                ? "Tích hợp sẵn"
                                : m.installed
                                  ? "Đã cài"
                                  : "Chưa cài",
                            )}
                          </Badge>
                          {m.installed && (
                            <p className="mt-2 text-xs text-muted-foreground">
                              {t(
                                m.loaded
                                  ? "Đã nạp vào FPM"
                                  : m.enabled
                                    ? "Đã bật nhưng chưa nạp"
                                    : "Đã tắt cho FPM",
                              )}
                            </p>
                          )}
                          {m.upgradable && (
                            <p className="mt-1 text-xs text-primary">
                              {t("Có bản cập nhật")}: {m.candidate}
                            </p>
                          )}
                        </td>
                        <td className="px-5 py-4 text-right">
                          {!m.builtin && m.installed && (
                            <span className="mr-2 inline-flex gap-2">
                              {m.modules?.length > 0 && (
                                <Button
                                  size="sm"
                                  variant="outline"
                                  disabled={busy || changed > 0}
                                  onClick={() =>
                                    setConfirm({
                                      module: m.name,
                                      operation: m.enabled
                                        ? "disable"
                                        : "enable",
                                    })
                                  }
                                >
                                  {t(m.enabled ? "Tắt mô-đun" : "Bật mô-đun")}
                                </Button>
                              )}
                              {m.upgradable && (
                                <Button
                                  size="sm"
                                  variant="outline"
                                  disabled={busy || changed > 0}
                                  onClick={() =>
                                    setConfirm({
                                      module: m.name,
                                      operation: "update",
                                    })
                                  }
                                >
                                  {t("Cập nhật")}
                                </Button>
                              )}
                            </span>
                          )}
                          <Button
                            size="sm"
                            variant="outline"
                            disabled={
                              busy ||
                              loading ||
                              changed > 0 ||
                              (m.installed && !m.removable) ||
                              m.builtin
                            }
                            onClick={() =>
                              setConfirm({
                                module: m.name,
                                remove: m.installed,
                              })
                            }
                          >
                            {m.installed ? (
                              <Trash2 className="size-4" />
                            ) : (
                              <Download className="size-4" />
                            )}
                            {t(m.installed ? "Gỡ cài đặt" : "Cài mô-đun")}
                          </Button>
                        </td>
                      </tr>
                    ))
                  : settings
                      .slice((current - 1) * 15, current * 15)
                      .map((s) => (
                        <tr key={s.name}>
                          <td className="px-5 py-4">
                            <label
                              htmlFor={s.name}
                              className="font-mono text-xs"
                            >
                              {s.name}
                            </label>
                            <div className="mt-1 text-xs text-muted-foreground">
                              {t(
                                resets.includes(s.name)
                                  ? "Sẽ khôi phục mặc định"
                                  : s.custom
                                    ? "Tùy chỉnh"
                                    : "Hệ thống",
                              )}
                            </div>
                          </td>
                          <td className="px-5 py-4">
                            {s.kind === "bool" ? (
                              <select
                                id={s.name}
                                className="input max-w-xs"
                                value={
                                  edits[s.name] ?? (s.value === "1" ? "1" : "0")
                                }
                                disabled={busy || resets.includes(s.name)}
                                onChange={(e) => edit(s, e.target.value)}
                              >
                                <option value="1">On (1)</option>
                                <option value="0">Off (0)</option>
                              </select>
                            ) : (
                              <input
                                id={s.name}
                                className="input max-w-xs font-mono text-xs"
                                value={edits[s.name] ?? s.value}
                                disabled={busy || resets.includes(s.name)}
                                onChange={(e) => edit(s, e.target.value)}
                              />
                            )}
                          </td>
                          <td className="px-5 py-4 text-right">
                            <Button
                              size="sm"
                              variant={
                                resets.includes(s.name) ? "secondary" : "ghost"
                              }
                              disabled={
                                busy ||
                                (!s.custom && edits[s.name] === undefined)
                              }
                              onClick={() => reset(s)}
                              aria-label={`${t("Khôi phục")} ${s.name}`}
                            >
                              <RotateCcw className="size-4" />
                              {t("Khôi phục")}
                            </Button>
                          </td>
                        </tr>
                      ))}
              </tbody>
            </table>
            {rows === 0 && (
              <p className="p-8 text-center text-muted-foreground">
                {t("Không có kết quả")}
              </p>
            )}
          </div>
        )}
        <div className="flex items-center justify-between gap-4 border-t p-4 text-xs text-muted-foreground">
          <span>
            {rows} {t("kết quả")} · {current}/{pages}
          </span>
          <div className="flex gap-2">
            <Button
              size="sm"
              variant="outline"
              disabled={current <= 1}
              onClick={() => setPage(current - 1)}
            >
              {t("Trước")}
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={current >= pages}
              onClick={() => setPage(current + 1)}
            >
              {t("Tiếp")}
            </Button>
          </div>
        </div>
      </div>
      <Dialog
        open={logs !== null}
        onOpenChange={(open) => {
          if (!open) setLogs(null);
        }}
      >
        <DialogContent className="sm:max-w-4xl">
          <DialogHeader>
            <DialogTitle>
              {t("Nhật ký PHP")} {version}
            </DialogTitle>
            <DialogDescription>
              {t("200 dòng gần nhất từ các service PHP-FPM của phiên bản này.")}
            </DialogDescription>
          </DialogHeader>
          <pre className="max-h-[60vh] overflow-auto rounded-lg bg-muted p-4 text-xs whitespace-pre-wrap">
            {logs || t("Chưa có nhật ký")}
          </pre>
        </DialogContent>
      </Dialog>
      <Dialog
        open={!!confirm}
        onOpenChange={(open) => {
          if (!open) setConfirm(null);
        }}
      >
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>
              {t(
                confirm?.operation
                  ? (
                      {
                        refresh: "Cập nhật kho mô-đun",
                        enable: "Bật mô-đun",
                        disable: "Tắt mô-đun",
                        update: "Cập nhật mô-đun",
                      } as Record<string, string>
                    )[confirm.operation]
                  : confirm?.module
                    ? confirm.remove
                      ? "Gỡ mô-đun PHP"
                      : "Cài mô-đun PHP"
                    : "Lưu thiết lập PHP",
              )}
            </DialogTitle>
            <DialogDescription>
              {confirm?.module ?? `PHP ${version}`} ·{" "}
              {t(
                "PHP-FPM sẽ được reload. Thay đổi có thể ảnh hưởng các website đang dùng phiên bản này.",
              )}
            </DialogDescription>
          </DialogHeader>
          <div className="flex justify-end gap-2">
            <Button variant="outline" onClick={() => setConfirm(null)}>
              {t("Hủy")}
            </Button>
            <Button onClick={() => void apply()}>{t("Xác nhận")}</Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
