"use client";
import { t as translate } from "@/lib/i18n";

import { useEffect, useState } from "react";
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
  Activity,
  CheckCircle2,
  CircleAlert,
  Loader2,
  Terminal,
  Minimize2,
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
};

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

export function Operations() {
  const [operations, setOperations] = useState<Operation[]>([]);
  const [open, setOpen] = useState(false);
  const [warning, setWarning] = useState("");
  useEffect(() => {
    const pending = new Map<number, ReturnType<typeof setTimeout>>();
    const listener = (event: Event) => {
      const op = (event as CustomEvent<Operation>).detail;
      const value = op.result as
        | { task?: Task; id?: number; status?: string; type?: string }
        | undefined;
      if (value?.task) op.task = value.task;
      else if (value?.id && value?.type && value?.status)
        op.task = value as Task;
      setOperations((items) =>
        [...items.filter((item) => item.id !== op.id), op].slice(-20),
      );
      // Quick requests (save, delete…) finish in a few hundred ms: opening
      // the popup for them made it flash over the page's own dialog. Open
      // only when a request is still running after a short delay, or when it
      // started a background task whose progress is worth following.
      if (op.status === "running") {
        if (op.progress === undefined && !pending.has(op.id))
          pending.set(
            op.id,
            setTimeout(() => {
              pending.delete(op.id);
              setOpen(true);
              setWarning("");
            }, 700),
          );
        return;
      }
      const timer = pending.get(op.id);
      if (timer !== undefined) {
        clearTimeout(timer);
        pending.delete(op.id);
        if (op.task && isOpen(op.task)) {
          setOpen(true);
          setWarning("");
        }
      }
    };
    window.addEventListener("xpanel:operation", listener);
    const restore = (event: Event) => {
      const task = (event as CustomEvent<Task>).detail;
      setOperations((items) => [
        ...items.filter((item) => item.task?.id !== task.id),
        { id: -task.id, path: "/tasks", method: "GET", status: "done", task },
      ]);
      setOpen(true);
    };
    window.addEventListener("xpanel:task", restore);
    return () => {
      window.removeEventListener("xpanel:operation", listener);
      pending.forEach(clearTimeout);
      window.removeEventListener("xpanel:task", restore);
    };
  }, []);
  const active = operations.filter((op) =>
    op.task ? isOpen(op.task) : op.status === "running",
  );
  useEffect(() => {
    if (!active.some((op) => op.task)) return;
    const timer = setTimeout(async () => {
      for (const op of active) {
        if (!op.task) continue;
        try {
          const task = await api<Task>(`/tasks/${op.task.id}`);
          setOperations((items) =>
            items.map((item) => (item.id === op.id ? { ...item, task } : item)),
          );
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
  }, [operations]);
  const current = operations.at(-1);
  if (!current) return null;
  const running = current.task
    ? isOpen(current.task)
    : current.status === "running";
  const failed =
    current.task?.status === "failed" || current.status === "failed";
  return (
    <>
      {!open && (
        <button
          type="button"
          onClick={() => setOpen(true)}
          title={translate("Xem tác vụ gần nhất")}
          className="fixed right-4 bottom-4 z-40 inline-flex h-9 items-center gap-2 rounded-full border bg-background/95 px-3 text-xs font-medium shadow-md backdrop-blur transition hover:bg-muted"
        >
          {active.length ? (
            <Loader2 className="size-3.5 animate-spin" />
          ) : failed ? (
            <CircleAlert className="size-3.5 text-destructive" />
          ) : (
            <Activity className="size-3.5" />
          )}
          {active.length
            ? translate("{0} tác vụ đang chạy", [active.length])
            : translate("Tác vụ")}
        </button>
      )}
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="sm:max-w-2xl" showCloseButton={false}>
          <DialogHeader>
            <div className="flex items-start gap-3">
              <div className="flex size-9 shrink-0 items-center justify-center rounded-lg border bg-muted">
                {running ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : failed ? (
                  <CircleAlert className="size-4 text-destructive" />
                ) : (
                  <CheckCircle2 className="size-4 text-emerald-600" />
                )}
              </div>
              <DialogTitle className="min-w-0 flex-1 pt-1.5 leading-snug">
                {title(current)}
              </DialogTitle>
              <Button
                size="icon"
                variant="ghost"
                aria-label={translate("Thu nhỏ tác vụ")}
                onClick={() => setOpen(false)}
              >
                <Minimize2 />
              </Button>
            </div>
            <DialogDescription>
              {translate(
                running
                  ? translate(
                      "Đang xử lý. Bạn có thể thu nhỏ và tiếp tục làm việc; tác vụ vẫn chạy trên máy chủ.",
                    )
                  : failed
                    ? translate(
                        "Thao tác chưa hoàn tất. Xem thông tin lỗi bên dưới.",
                      )
                    : translate("Thao tác đã hoàn tất."),
              )}
            </DialogDescription>
          </DialogHeader>
          {operations.length > 1 && (
            <div className="flex gap-2 overflow-x-auto pb-1">
              {operations.map((op) => (
                <button
                  key={op.id}
                  className={`shrink-0 rounded-md border px-2 py-1 text-xs ${op.id === current.id ? "bg-primary text-primary-foreground" : "bg-muted"}`}
                  onClick={() =>
                    setOperations((items) => [
                      ...items.filter((item) => item.id !== op.id),
                      op,
                    ])
                  }
                >
                  {translate(
                    op.task
                      ? `#${op.task.id}`
                      : translate("Thao tác {0}", [op.id]),
                  )}
                </button>
              ))}
            </div>
          )}
          {current.task && (
            <div className="flex items-center justify-between text-xs text-muted-foreground">
              <span>
                {translate("Tác vụ #")} {translate(current.task.id)}
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
              {translate(warning)}
            </p>
          )}
          <div className="flex justify-end">
            <Button
              variant={running ? "outline" : "default"}
              onClick={() => setOpen(false)}
            >
              {translate(running ? translate("Chạy nền") : translate("Đóng"))}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}

export function openTask(task: Task) {
  window.dispatchEvent(new CustomEvent("xpanel:task", { detail: task }));
}
