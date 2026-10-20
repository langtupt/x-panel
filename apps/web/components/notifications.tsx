"use client";
import { localeTag, t as translate } from "@/lib/i18n";

import { useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { Bell } from "lucide-react";
import { api } from "@/lib/api";

type Notification = {
  id: number;
  kind: string;
  title: string;
  body: string;
  link: string;
  read_at: string | null;
  created_at: string;
};

/** Bell with unread count; polls every minute. */
export function NotificationBell() {
  const [list, setList] = useState<Notification[]>([]);
  const [unread, setUnread] = useState(0);
  const [open, setOpen] = useState(false);
  const box = useRef<HTMLDivElement>(null);

  const load = useCallback(() => {
    api<{ notifications: Notification[]; unread: number }>("/notifications")
      .then((r) => {
        setList(r.notifications);
        setUnread(r.unread);
      })
      .catch(() => {});
  }, []);
  useEffect(() => {
    load();
    const t = setInterval(load, 60_000);
    return () => clearInterval(t);
  }, [load]);
  useEffect(() => {
    if (!open) return;
    const close = (e: MouseEvent) => {
      if (box.current && !box.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", close);
    return () => document.removeEventListener("mousedown", close);
  }, [open]);

  function toggle() {
    setOpen(!open);
    if (!open && unread > 0)
      api("/notifications/read", { method: "POST" })
        .then(() => setUnread(0))
        .catch(() => {});
  }

  return (
    <div className="relative" ref={box}>
      <button
        className="relative flex size-8 items-center justify-center rounded-lg hover:bg-muted"
        aria-label={translate("Thông báo")}
        onClick={toggle}
      >
        <Bell className="size-4" />
        {unread > 0 && (
          <span className="absolute -right-0.5 -top-0.5 flex min-w-4 items-center justify-center rounded-full bg-red-600 px-1 text-[10px] font-semibold text-white">
            {unread > 9 ? "9+" : unread}
          </span>
        )}
      </button>
      {open && (
        <div className="absolute right-0 top-10 z-30 w-80 overflow-hidden rounded-xl border bg-background shadow-lg">
          <div className="border-b px-4 py-2.5 text-sm font-medium">{translate("Thông báo")}</div>
          {list.length === 0 ? (
            <p className="px-4 py-6 text-center text-sm text-muted-foreground">{translate("Chưa có thông báo")}</p>
          ) : (
            <ul className="max-h-96 divide-y overflow-auto">
              {list.map((n) => (
                <li key={n.id} className={n.read_at ? "" : "bg-muted/40"}>
                  <Link href={n.link || "#"} className="block px-4 py-3 hover:bg-muted" onClick={() => setOpen(false)}>
                    <div className="text-sm font-medium">{translate(n.title)}</div>
                    <div className="text-xs text-muted-foreground">{n.body}</div>
                    <div className="mt-1 text-[10px] text-muted-foreground">
                      {new Date(n.created_at).toLocaleString(localeTag())}
                    </div>
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  );
}
