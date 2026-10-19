"use client";

import { useState } from "react";
import { taskLabel, type Task, type TaskStatus } from "@/lib/api";

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
      {s.label}
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
            {taskLabel[task.type] ?? task.type}
          </span>
          <span className="text-slate-500"> · {task.target}</span>
        </span>
        <span className="flex shrink-0 items-center gap-2">
          <TaskStatusBadge status={task.status} />
          <span className="text-xs text-slate-400">{open ? "▲" : "▼"}</span>
        </span>
      </button>
      {open && (
        <pre className="max-h-72 overflow-auto border-t border-slate-100 bg-slate-950 px-3 py-2 text-xs leading-relaxed text-slate-200">
          {task.log || "Chưa có log…"}
        </pre>
      )}
      <div className="flex justify-end border-t px-3 py-1.5">
        <button
          className="text-[11px] text-muted-foreground hover:text-foreground"
          onClick={() =>
            window.dispatchEvent(
              new CustomEvent("xpanel:task", { detail: task }),
            )
          }
        >
          Mở trong popup →
        </button>
      </div>
    </div>
  );
}
