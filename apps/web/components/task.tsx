"use client";
import { t as translate } from "@/lib/i18n";

import { useEffect, useState } from "react";
import { Ban, Loader2 } from "lucide-react";
import {
  canCancel,
  cancelTask,
  errMsg,
  taskLabel,
  type Task,
  type TaskStatus,
} from "@/lib/api";
import { CopyButton } from "@/components/ui";

/**
 * Stop button for a running task or upload. The first click asks for
 * confirmation (an accidental click must not kill a build); errors show in
 * the tooltip and next to the button.
 */
export function CancelButton({
  onCancel,
  compact = false,
  tone = "light",
}: {
  onCancel: () => Promise<void> | void;
  compact?: boolean;
  tone?: "light" | "dark";
}) {
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    if (!confirm) return;
    const id = setTimeout(() => setConfirm(false), 4000);
    return () => clearTimeout(id);
  }, [confirm]);
  const base =
    tone === "dark"
      ? "text-neutral-300 hover:bg-white/10 hover:text-white"
      : "text-muted-foreground hover:bg-red-50 hover:text-red-700";
  return (
    <span className="inline-flex items-center gap-1">
      {error && !compact && (
        <span className="max-w-48 truncate text-[11px] text-destructive">
          {error}
        </span>
      )}
      <button
        type="button"
        disabled={busy}
        title={
          error || translate(confirm ? "Bấm lần nữa để hủy" : "Hủy tác vụ")
        }
        aria-label={translate("Hủy tác vụ")}
        onClick={async (e) => {
          e.stopPropagation();
          if (!confirm) return setConfirm(true);
          setBusy(true);
          setError("");
          try {
            await onCancel();
          } catch (err) {
            setError(errMsg(err));
          } finally {
            setBusy(false);
            setConfirm(false);
          }
        }}
        className={`inline-flex items-center gap-1 rounded-md px-1.5 py-1 text-[11px] font-medium transition-colors disabled:opacity-50 ${confirm ? "bg-red-600 text-white hover:bg-red-700" : base}`}
      >
        {busy ? (
          <Loader2 className="size-3.5 animate-spin" />
        ) : (
          <Ban className="size-3.5" />
        )}
        {confirm ? translate("Hủy?") : !compact && translate("Hủy")}
      </button>
    </span>
  );
}

/** CancelButton for a server task (shown only when it can be cancelled). */
export function CancelTaskButton({
  task,
  compact,
  tone,
  onCancelled,
}: {
  task: Task;
  compact?: boolean;
  tone?: "light" | "dark";
  onCancelled?: () => void;
}) {
  if (!canCancel(task)) return null;
  return (
    <CancelButton
      compact={compact}
      tone={tone}
      onCancel={async () => {
        await cancelTask(task.id);
        onCancelled?.();
      }}
    />
  );
}

const statusStyle: Record<TaskStatus, { label: string; cls: string }> = {
  pending: { label: "Đang chờ", cls: "bg-slate-100 text-slate-600" },
  running: { label: "Đang chạy", cls: "bg-brand-50 text-brand-700" },
  done: { label: "Hoàn tất", cls: "bg-emerald-50 text-emerald-700" },
  failed: { label: "Thất bại", cls: "bg-red-50 text-red-700" },
};

export function TaskStatusBadge({ status }: { status: TaskStatus }) {
  const s = statusStyle[status];
  return (
    <span
      className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium ${s.cls}`}
    >
      {(status === "running" || status === "pending") && (
        <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-current" />
      )}
      {translate(s.label)}
    </span>
  );
}

/** Compact task card with a collapsible log. */
export function TaskCard({
  task,
  defaultOpen = false,
}: {
  task: Task;
  defaultOpen?: boolean;
}) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <div className="rounded-lg border border-slate-200 bg-white">
      <button
        type="button"
        onClick={() => setOpen(!open)}
        className="flex w-full items-center justify-between gap-3 px-3 py-2 text-left text-sm"
      >
        <span className="min-w-0 truncate">
          <span className="font-medium">
            {translate(taskLabel[task.type] ?? task.type)}
          </span>
          <span className="text-slate-500"> · {translate(task.target)}</span>
        </span>
        <span className="flex shrink-0 items-center gap-2">
          <TaskStatusBadge status={task.status} />
          <span className="text-xs text-slate-400">
            {translate(open ? "▲" : "▼")}
          </span>
        </span>
      </button>
      {open && (
        <pre className="max-h-72 overflow-auto border-t border-slate-100 bg-slate-950 px-3 py-2 text-xs leading-relaxed text-slate-200">
          {task.log || translate("Chưa có log…")}
        </pre>
      )}
      <div className="flex items-center justify-between gap-2 border-t px-2 py-1">
        <span className="flex items-center gap-1">
          <CopyButton text={task.log} />
          <CancelTaskButton task={task} />
        </span>
        <button
          className="text-[11px] text-muted-foreground hover:text-foreground"
          onClick={() =>
            window.dispatchEvent(
              new CustomEvent("xpanel:task", { detail: task }),
            )
          }
        >
          {translate("Mở trong popup →")}
        </button>
      </div>
    </div>
  );
}
