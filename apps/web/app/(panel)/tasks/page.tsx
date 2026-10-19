"use client";

import { useEffect, useState } from "react";
import { api, errMsg, isOpen, type Task } from "@/lib/api";
import { ErrorBox } from "@/components/ui";
import { TaskCard } from "@/components/task";

export default function TasksPage() {
  const [list, setList] = useState<Task[] | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout>;
    const load = () =>
      api<Task[]>("/tasks")
        .then((l) => {
          setList(l);
          // Refresh while something is still running.
          if (l.some(isOpen)) timer = setTimeout(load, 3000);
        })
        .catch((e) => setError(errMsg(e)));
    load();
    return () => clearTimeout(timer);
  }, []);

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold">Tác vụ</h1>
        <p className="text-sm text-slate-500">
          Các công việc chạy nền gần đây (cài PHP, cấp và gia hạn SSL).
        </p>
      </div>
      <ErrorBox msg={error} />
      <div className="space-y-2">
        {list?.map((t) => (
          <div key={t.id}>
            <div className="mb-1 text-xs text-slate-400">
              #{t.id} · {new Date(t.created_at).toLocaleString("vi-VN")}
            </div>
            <TaskCard task={t} />
          </div>
        ))}
        {list?.length === 0 && (
          <p className="text-sm text-slate-500">Chưa có tác vụ nào.</p>
        )}
      </div>
    </div>
  );
}
