"use client";
import { localeTag, t as translate } from "@/lib/i18n";

import { useEffect, useMemo, useState } from "react";
import {
  Activity,
  CheckCircle2,
  ChevronDown,
  CircleAlert,
  Clock3,
  ExternalLink,
  Loader2,
  Search,
} from "lucide-react";
import {
  api,
  errMsg,
  isOpen,
  taskLabel,
  type Task,
  type TaskStatus,
} from "@/lib/api";
import { CopyButton, ErrorBox } from "@/components/ui";
import { CancelTaskButton, TaskStatusBadge } from "@/components/task";
import { openTask } from "@/components/operations";
import { Skeleton } from "@/components/ui/skeleton";

type Filter = "all" | "open" | "failed" | "done";

const filters: { id: Filter; label: string }[] = [
  { id: "all", label: "Tất cả" },
  { id: "open", label: "Đang chạy" },
  { id: "failed", label: "Thất bại" },
  { id: "done", label: "Hoàn tất" },
];

function matches(t: Task, f: Filter) {
  if (f === "open") return isOpen(t);
  if (f === "failed") return t.status === "failed";
  if (f === "done") return t.status === "done";
  return true;
}

function duration(t: Task) {
  if (!t.started_at) return "";
  const end = t.finished_at ? new Date(t.finished_at) : new Date();
  const sec = Math.max(
    0,
    Math.round((end.getTime() - new Date(t.started_at).getTime()) / 1000),
  );
  if (sec < 60) return `${sec}s`;
  if (sec < 3600) return `${Math.floor(sec / 60)}m ${sec % 60}s`;
  return `${Math.floor(sec / 3600)}h ${Math.floor((sec % 3600) / 60)}m`;
}

function StatusIcon({ status }: { status: TaskStatus }) {
  if (status === "done")
    return <CheckCircle2 className="size-4 shrink-0 text-emerald-600" />;
  if (status === "failed")
    return <CircleAlert className="size-4 shrink-0 text-destructive" />;
  if (status === "running")
    return <Loader2 className="size-4 shrink-0 animate-spin" />;
  return <Clock3 className="size-4 shrink-0 text-muted-foreground" />;
}

function TaskRow({ task, onChange }: { task: Task; onChange: () => void }) {
  const [open, setOpen] = useState(false);
  const when = new Date(task.created_at).toLocaleString(localeTag(), {
    dateStyle: "short",
    timeStyle: "short",
  });
  return (
    <li className="border-b last:border-b-0">
      <button
        type="button"
        onClick={() => setOpen(!open)}
        aria-expanded={open}
        className="flex w-full items-center gap-3 px-4 py-2.5 text-left text-sm transition-colors hover:bg-muted/40"
      >
        <StatusIcon status={task.status} />
        <span className="min-w-0 flex-1 truncate">
          <span className="font-medium">
            {translate(taskLabel[task.type] ?? task.type)}
          </span>
          {task.target && (
            <span className="text-muted-foreground"> · {task.target}</span>
          )}
        </span>
        <span className="hidden shrink-0 text-xs text-muted-foreground tabular-nums sm:inline">
          #{task.id}
        </span>
        <span className="hidden w-16 shrink-0 text-right text-xs text-muted-foreground tabular-nums md:inline">
          {duration(task)}
        </span>
        <span className="hidden w-32 shrink-0 text-right text-xs text-muted-foreground tabular-nums sm:inline">
          {when}
        </span>
        <span className="flex w-24 shrink-0 justify-end">
          <TaskStatusBadge status={task.status} />
        </span>
        <span className="w-8 shrink-0" onClick={(e) => e.stopPropagation()}>
          <CancelTaskButton task={task} compact onCancelled={onChange} />
        </span>
        <ChevronDown
          className={`size-4 shrink-0 text-muted-foreground transition-transform ${open ? "rotate-180" : ""}`}
        />
      </button>
      {open && (
        <div className="px-4 pb-3">
          <div className="overflow-hidden rounded-lg border">
            <div className="flex items-center justify-between gap-2 border-b bg-muted/40 px-2 py-1">
              <span className="px-1 text-[11px] text-muted-foreground">
                <span className="sm:hidden">
                  #{task.id} · {when}
                </span>
                <span className="hidden sm:inline">
                  {translate("Nhật ký thực hiện")}
                </span>
              </span>
              <span className="flex items-center gap-1">
                <CopyButton text={task.log} />
                <button
                  type="button"
                  onClick={() => openTask(task)}
                  className="inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-[11px] font-medium text-muted-foreground hover:bg-muted hover:text-foreground"
                >
                  <ExternalLink className="size-3.5" />
                  {translate("Mở popup")}
                </button>
              </span>
            </div>
            <pre className="max-h-80 overflow-auto whitespace-pre-wrap break-words bg-background p-3 font-mono text-xs leading-5">
              {task.log || translate("Chưa có log…")}
            </pre>
          </div>
        </div>
      )}
    </li>
  );
}

export default function TasksPage() {
  const [list, setList] = useState<Task[] | null>(null);
  const [error, setError] = useState("");
  const [filter, setFilter] = useState<Filter>("all");
  const [query, setQuery] = useState("");
  const [reload, setReload] = useState(0);

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout>;
    let stopped = false;
    const load = () =>
      api<Task[]>("/tasks")
        .then((l) => {
          if (stopped) return;
          setList(l);
          setError("");
          // Refresh while something is still running.
          if (l.some(isOpen)) timer = setTimeout(load, 3000);
        })
        .catch((e) => {
          if (!stopped) setError(errMsg(e));
        });
    load();
    return () => {
      stopped = true;
      clearTimeout(timer);
    };
  }, [reload]);

  const counts = useMemo(() => {
    const c: Record<Filter, number> = { all: 0, open: 0, failed: 0, done: 0 };
    for (const t of list ?? [])
      for (const f of filters) if (matches(t, f.id)) c[f.id]++;
    return c;
  }, [list]);

  const q = query.trim().toLowerCase();
  const visible = (list ?? []).filter(
    (t) =>
      matches(t, filter) &&
      (!q ||
        `${translate(taskLabel[t.type] ?? t.type)} ${t.type} ${t.target} #${t.id}`
          .toLowerCase()
          .includes(q)),
  );

  return (
    <div className="space-y-5">
      <div>
        <div className="mb-2 flex items-center gap-2 text-xs text-muted-foreground">
          <Activity className="size-3.5" />
          {translate("Trung tâm tác vụ")}
        </div>
        <h1>{translate("Tác vụ")}</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          {translate(
            "50 công việc chạy nền gần nhất: build website, cài ứng dụng, SSL, sao lưu, quét mã độc…",
          )}
        </p>
      </div>
      <ErrorBox msg={error} />
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex max-w-full gap-1 overflow-x-auto rounded-lg border bg-muted/40 p-1">
          {filters.map((f) => (
            <button
              key={f.id}
              type="button"
              onClick={() => setFilter(f.id)}
              className={`flex shrink-0 items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition ${filter === f.id ? "bg-background shadow-xs" : "text-muted-foreground hover:text-foreground"}`}
            >
              {translate(f.label)}
              <span className="rounded bg-muted px-1.5 text-[10px] tabular-nums">
                {counts[f.id]}
              </span>
            </button>
          ))}
        </div>
        <div className="relative w-full sm:w-60">
          <Search className="absolute top-2.5 left-3 size-4 text-muted-foreground" />
          <input
            className="input pl-9"
            placeholder={translate("Tìm tác vụ…")}
            aria-label={translate("Tìm tác vụ")}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
        </div>
      </div>
      <div className="card overflow-hidden">
        {list === null ? (
          <div className="space-y-2 p-4">
            {[1, 2, 3, 4, 5].map((i) => (
              <Skeleton key={i} className="h-8 rounded-md" />
            ))}
          </div>
        ) : visible.length ? (
          <ul>
            {visible.map((t) => (
              <TaskRow
                key={t.id}
                task={t}
                onChange={() => setReload((n) => n + 1)}
              />
            ))}
          </ul>
        ) : (
          <p className="py-12 text-center text-sm text-muted-foreground">
            {translate(
              list.length ? "Không có tác vụ phù hợp." : "Chưa có tác vụ nào.",
            )}
          </p>
        )}
      </div>
    </div>
  );
}
