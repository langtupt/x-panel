"use client";

import { useEffect, useState } from "react";
import { api, isOpen, type Task } from "./api";

/** Polls a task every 2s until it finishes, then calls onDone once. */
export function useTask(initial: Task | null, onDone?: (t: Task) => void) {
  const [task, setTask] = useState<Task | null>(initial);

  useEffect(() => setTask(initial), [initial]);

  useEffect(() => {
    if (!task || !isOpen(task)) return;
    const id = setTimeout(async () => {
      try {
        const t = await api<Task>(`/tasks/${task.id}`);
        setTask(t);
        if (!isOpen(t)) onDone?.(t);
      } catch {
        // keep polling; transient errors are fine
        setTask({ ...task });
      }
    }, 2000);
    return () => clearTimeout(id);
  }, [task, onDone]);

  return task;
}
