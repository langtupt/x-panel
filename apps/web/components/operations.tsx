"use client";
import { t as translate } from "@/lib/i18n";

import { useEffect, useState } from "react";
import Link from "next/link";
import { api, isOpen, taskLabel, type Task } from "@/lib/api";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import {
  CheckCircle2,
  ChevronDown,
  CircleAlert,
  ListChecks,
  Loader2,
  Minimize2,
  ScrollText,
  Terminal,
  X,
} from "lucide-react";
import { TaskStatusBadge } from "@/components/task";
import { CopyButton } from "@/components/ui";

type Operation = {
  id: number;
  path: string;
  method: string;
  status: "running" | "done" | "failed";
  result?: unknown;
  error?: string;
  task?: Task;
  progress?: number;
  /** Shown in the corner tracker (slow request, upload or background task). */
  tracked?: boolean;
  /** When it finished (ms), for auto-hiding successful entries. */
  endedAt?: number;
  /** Hidden from the tracker by the user. */
  dismissed?: boolean;
};

/** Successful entries leave the corner tracker after this long. */
const DONE_VISIBLE_MS = 8000;
/** Background tasks started elsewhere (schedules, other tabs) are picked up this often. */
const SERVER_POLL_MS = 20000;

const finished = (op: Operation) =>
  op.task ? !isOpen(op.task) : op.status !== "running";
const failedOp = (op: Operation) =>
  op.task ? op.task.status === "failed" : op.status === "failed";

function title(op: Operation) {
  if (op.task)
    return `${translate(taskLabel[op.task.type] ?? op.task.type)} · ${op.task.target}`;
  if (op.path.includes("/files/upload"))
    return translate("Tải tệp lên máy chủ");
  if (op.path.includes("/install")) return translate("Cài đặt ứng dụng");
  if (op.path.includes("/build"))
    return translate("Build & khởi động lại website");
  if (op.path.includes("/ssl")) return translate("Cập nhật chứng chỉ SSL");
  const resource = op.path.split("/")[1];
  const labels: Record<string, string> = {
    sites: "website",
    users: translate("tài khoản"),
    databases: "database",
    packages: translate("gói dịch vụ"),
    ftp: "FTP",
    cron: "cron",
    files: translate("tệp"),
    me: translate("tài khoản"),
  };
  return `${op.method === "DELETE" ? translate("Xóa") : op.method === "POST" ? translate("Thực hiện thao tác") : translate("Cập nhật")} ${labels[resource] ?? translate("dữ liệu")}`;
}

function StatusIcon({
  op,
  className = "size-4",
}: {
  op: Operation;
  className?: string;
}) {
  if (!finished(op)) return <Loader2 className={`${className} animate-spin`} />;
  if (failedOp(op))
    return <CircleAlert className={`${className} text-destructive`} />;
  return <CheckCircle2 className={`${className} text-emerald-600`} />;
}

function statusText(op: Operation) {
  if (!finished(op))
    return op.progress !== undefined
      ? `${op.progress}%`
      : op.task?.status === "pending"
        ? translate("Đang chờ")
        : translate("Đang chạy");
  return failedOp(op) ? translate("Thất bại") : translate("Hoàn tất");
}

export function Operations() {
  const [operations, setOperations] = useState<Operation[]>([]);
  const [open, setOpen] = useState(false);
  const [selected, setSelected] = useState<number | null>(null);
  const [panel, setPanel] = useState(false);
  const [now, setNow] = useState(() => Date.now());
  const [warning, setWarning] = useState("");

  /** Inserts or updates an operation, stamping when it finished. */
  function upsert(patch: Operation) {
    setOperations((items) => {
      const prev = items.find((item) => item.id === patch.id);
      const merged: Operation = { ...prev, ...patch };
      if (!merged.endedAt && finished(merged)) merged.endedAt = Date.now();
      return [...items.filter((item) => item.id !== patch.id), merged].slice(
        -30,
      );
    });
  }
  function show(id: number) {
    setSelected(id);
    setOpen(true);
    setPanel(false);
    setWarning("");
  }

  useEffect(() => {
    const pending = new Map<number, ReturnType<typeof setTimeout>>();
    const listener = (event: Event) => {
      const op = { ...(event as CustomEvent<Operation>).detail };
      const value = op.result as
        | { task?: Task; id?: number; status?: string; type?: string }
        | undefined;
      if (value?.task) op.task = value.task;
      else if (value?.id && value?.type && value?.status)
        op.task = value as Task;
      // Uploads report progress; follow them in the tracker from the start.
      if (op.progress !== undefined) op.tracked = true;
      upsert(op);
      // Quick requests (save, delete…) finish in a few hundred ms: they never
      // open the popup or the tracker. A request still running after a short
      // delay, or one that started a background task, does.
      if (op.status === "running") {
        if (op.progress === undefined && !pending.has(op.id))
          pending.set(
            op.id,
            setTimeout(() => {
              pending.delete(op.id);
              upsert({ ...op, tracked: true });
              show(op.id);
            }, 700),
          );
        return;
      }
      const timer = pending.get(op.id);
      if (timer !== undefined) {
        clearTimeout(timer);
        pending.delete(op.id);
        if (op.task && isOpen(op.task)) {
          upsert({ ...op, tracked: true });
          show(op.id);
        }
      }
    };
    const restore = (event: Event) => {
      const task = (event as CustomEvent<Task>).detail;
      setOperations((items) =>
        items.filter((item) => item.task?.id !== task.id),
      );
      upsert({
        id: -task.id,
        path: "/tasks",
        method: "GET",
        status: "done",
        task,
        tracked: isOpen(task),
      });
      show(-task.id);
    };
    window.addEventListener("xpanel:operation", listener);
    window.addEventListener("xpanel:task", restore);
    return () => {
      window.removeEventListener("xpanel:operation", listener);
      window.removeEventListener("xpanel:task", restore);
      pending.forEach(clearTimeout);
    };
  }, []);

  // Pick up background tasks started elsewhere (schedules, other tabs).
  useEffect(() => {
    let stopped = false;
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      if (document.visibilityState === "visible") {
        try {
          const list = await api<Task[]>("/tasks");
          if (stopped) return;
          setOperations((items) => {
            const known = new Set(items.map((i) => i.task?.id));
            const fresh = list
              .filter((t) => isOpen(t) && !known.has(t.id))
              .map((t): Operation => ({
                id: -t.id,
                path: "/tasks",
                method: "GET",
                status: "done",
                task: t,
                tracked: true,
              }));
            return fresh.length ? [...items, ...fresh].slice(-30) : items;
          });
        } catch {}
      }
      if (!stopped) timer = setTimeout(poll, SERVER_POLL_MS);
    };
    timer = setTimeout(poll, 3000);
    return () => {
      stopped = true;
      clearTimeout(timer);
    };
  }, []);

  // Follow running tasks until they finish.
  const active = operations.filter((op) => !finished(op));
  const activeTasks = active.filter((op) => op.task);
  useEffect(() => {
    if (!activeTasks.length) return;
    const timer = setTimeout(async () => {
      for (const op of activeTasks) {
        try {
          const task = await api<Task>(`/tasks/${op.task!.id}`);
          upsert({ ...op, task });
          setWarning("");
        } catch {
          setWarning(
            translate("Tạm mất kết nối. Đang thử lấy lại trạng thái tác vụ…"),
          );
          setOperations((items) => [...items]);
        }
      }
    }, 2000);
    return () => clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [operations]);

  const visible = operations.filter(
    (op) =>
      op.tracked &&
      !op.dismissed &&
      (!finished(op) ||
        failedOp(op) ||
        now - (op.endedAt ?? 0) < DONE_VISIBLE_MS),
  );

  // Re-render when the next successful entry should leave the tracker.
  useEffect(() => {
    const expiries = operations
      .filter(
        (op) => op.tracked && !op.dismissed && finished(op) && !failedOp(op),
      )
      .map((op) => (op.endedAt ?? 0) + DONE_VISIBLE_MS)
      .filter((t) => t > Date.now());
    if (!expiries.length) return;
    const id = setTimeout(
      () => setNow(Date.now()),
      Math.min(...expiries) - Date.now() + 50,
    );
    return () => clearTimeout(id);
  }, [operations, now]);
  useEffect(() => setNow(Date.now()), [operations]);
  useEffect(() => {
    if (!visible.length) setPanel(false);
  }, [visible.length]);

  function dismiss(ids: number[]) {
    setOperations((items) =>
      items.map((item) =>
        ids.includes(item.id) ? { ...item, dismissed: true } : item,
      ),
    );
  }

  const current =
    operations.find((op) => op.id === selected) ?? operations.at(-1);
  const running = current ? !finished(current) : false;
  const failed = current ? failedOp(current) : false;
  const failures = visible.filter((op) => finished(op) && failedOp(op));
  const inProgress = visible.filter((op) => !finished(op));

  function closeDialog() {
    setOpen(false);
    // A finished task that was just looked at needs no reminder in the corner.
    if (current && finished(current) && !failedOp(current))
      dismiss([current.id]);
  }

  return (
    <>
      {!open && visible.length > 0 && (
        <div className="fixed right-4 bottom-4 z-40 flex flex-col items-end gap-2">
          {panel && (
            <div className="w-80 overflow-hidden rounded-xl border bg-background shadow-lg">
              <div className="flex items-center justify-between border-b px-3 py-2 text-xs font-medium">
                {translate("Tác vụ gần đây")}
                <button
                  type="button"
                  className="rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
                  aria-label={translate("Thu gọn")}
                  onClick={() => setPanel(false)}
                >
                  <ChevronDown className="size-3.5" />
                </button>
              </div>
              <ul className="max-h-72 divide-y overflow-y-auto">
                {[...visible].reverse().map((op) => (
                  <li key={op.id} className="flex items-center gap-2 px-3 py-2">
                    <StatusIcon op={op} className="size-3.5 shrink-0" />
                    <button
                      type="button"
                      onClick={() => show(op.id)}
                      className="min-w-0 flex-1 text-left"
                      title={translate("Xem nhật ký")}
                    >
                      <span className="block truncate text-xs font-medium">
                        {title(op)}
                      </span>
                      <span
                        className={`block text-[11px] ${failedOp(op) && finished(op) ? "text-destructive" : "text-muted-foreground"}`}
                      >
                        {statusText(op)}
                      </span>
                      {!finished(op) && op.progress !== undefined && (
                        <span className="mt-1 block h-1 overflow-hidden rounded-full bg-muted">
                          <span
                            className="block h-full rounded-full bg-foreground"
                            style={{ width: `${op.progress}%` }}
                          />
                        </span>
                      )}
                    </button>
                    <button
                      type="button"
                      className="rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
                      aria-label={translate("Xem nhật ký")}
                      title={translate("Xem nhật ký")}
                      onClick={() => show(op.id)}
                    >
                      <ScrollText className="size-3.5" />
                    </button>
                    {finished(op) && (
                      <button
                        type="button"
                        className="rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
                        aria-label={translate("Ẩn")}
                        title={translate("Ẩn")}
                        onClick={() => dismiss([op.id])}
                      >
                        <X className="size-3.5" />
                      </button>
                    )}
                  </li>
                ))}
              </ul>
              <div className="flex items-center justify-between border-t px-3 py-2 text-[11px]">
                <button
                  type="button"
                  className="text-muted-foreground hover:text-foreground disabled:opacity-40"
                  disabled={!visible.some(finished)}
                  onClick={() =>
                    dismiss(visible.filter(finished).map((o) => o.id))
                  }
                >
                  {translate("Ẩn mục đã xong")}
                </button>
                <Link
                  href="/tasks/"
                  className="font-medium hover:underline"
                  onClick={() => setPanel(false)}
                >
                  {translate("Tất cả tác vụ →")}
                </Link>
              </div>
            </div>
          )}
          <button
            type="button"
            onClick={() => setPanel(!panel)}
            aria-expanded={panel}
            className={`inline-flex h-9 items-center gap-2 rounded-full border px-3 text-xs font-medium shadow-md backdrop-blur transition ${failures.length && !inProgress.length ? "border-destructive/30 bg-red-50/95 text-red-700 hover:bg-red-50" : "bg-background/95 hover:bg-muted"}`}
          >
            {inProgress.length ? (
              <Loader2 className="size-3.5 animate-spin" />
            ) : failures.length ? (
              <CircleAlert className="size-3.5" />
            ) : (
              <CheckCircle2 className="size-3.5 text-emerald-600" />
            )}
            {inProgress.length
              ? translate("{0} tác vụ đang chạy", [inProgress.length])
              : failures.length
                ? translate("{0} tác vụ thất bại", [failures.length])
                : translate("Đã hoàn tất")}
            <ListChecks className="size-3.5 text-muted-foreground" />
          </button>
        </div>
      )}
      {current && (
        <Dialog
          open={open}
          onOpenChange={(o) => (o ? setOpen(true) : closeDialog())}
        >
          <DialogContent className="sm:max-w-2xl" showCloseButton={false}>
            <DialogHeader>
              <div className="flex items-start gap-3">
                <div className="flex size-9 shrink-0 items-center justify-center rounded-lg border bg-muted">
                  <StatusIcon op={current} />
                </div>
                <DialogTitle className="min-w-0 flex-1 pt-1.5 leading-snug">
                  {title(current)}
                </DialogTitle>
                <Button
                  size="icon"
                  variant="ghost"
                  aria-label={translate("Thu nhỏ tác vụ")}
                  onClick={closeDialog}
                >
                  <Minimize2 />
                </Button>
              </div>
              <DialogDescription>
                {running
                  ? translate(
                      "Đang xử lý. Bạn có thể thu nhỏ và tiếp tục làm việc; tác vụ vẫn chạy trên máy chủ.",
                    )
                  : failed
                    ? translate(
                        "Thao tác chưa hoàn tất. Xem thông tin lỗi bên dưới.",
                      )
                    : translate("Thao tác đã hoàn tất.")}
              </DialogDescription>
            </DialogHeader>
            {current.task && (
              <div className="flex items-center justify-between text-xs text-muted-foreground">
                <span>
                  {translate("Tác vụ #")} {current.task.id}
                </span>
                <TaskStatusBadge status={current.task.status} />
              </div>
            )}
            {running && (
              <div
                role="progressbar"
                aria-label={translate("Đang xử lý")}
                aria-valuenow={current.progress}
                className="h-1.5 overflow-hidden rounded-full bg-muted"
              >
                <div
                  className={`h-full rounded-full bg-primary ${current.progress === undefined ? "w-1/3 animate-pulse" : ""}`}
                  style={
                    current.progress === undefined
                      ? undefined
                      : { width: `${current.progress}%` }
                  }
                />
              </div>
            )}
            <div className="overflow-hidden rounded-lg border bg-neutral-950 text-neutral-200">
              <div className="flex items-center gap-2 border-b border-white/10 py-1.5 pr-2 pl-4 text-xs text-neutral-400">
                <Terminal className="size-3.5" />
                <span className="flex-1">{translate("Nhật ký thực hiện")}</span>
                <CopyButton
                  tone="dark"
                  text={current.error || current.task?.log || ""}
                />
              </div>
              <pre
                aria-live="polite"
                className="max-h-[45dvh] min-h-24 overflow-auto whitespace-pre-wrap break-words p-4 font-mono text-xs leading-6"
              >
                {current.error ||
                  current.task?.log ||
                  (running
                    ? translate("Đã gửi yêu cầu đến máy chủ. Đang chờ nhật ký…")
                    : translate("Hoàn tất thành công."))}
              </pre>
            </div>
            {warning && (
              <p role="alert" className="text-xs text-amber-700">
                {warning}
              </p>
            )}
            <div className="flex justify-end">
              <Button
                variant={running ? "outline" : "default"}
                onClick={closeDialog}
              >
                {running ? translate("Chạy nền") : translate("Đóng")}
              </Button>
            </div>
          </DialogContent>
        </Dialog>
      )}
    </>
  );
}

export function openTask(task: Task) {
  window.dispatchEvent(new CustomEvent("xpanel:task", { detail: task }));
}
