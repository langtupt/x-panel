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

/** Bell with unread count; refreshes every 30 s, on focus and when opened. */
export function NotificationBell() {
  const [list, setList] = useState<Notification[] | null>(null);
  const [unread, setUnread] = useState(0);
  const [open, setOpen] = useState(false);
  const [expanded, setExpanded] = useState<number | null>(null);
  const box = useRef<HTMLDivElement>(null);

  const load = useCallback(
    () =>
      api<{ notifications: Notification[]; unread: number }>("/notifications")
        .then((r) => {
          setList(r.notifications);
          setUnread(r.unread);
          return r;
        })
        .catch(() => null),
    [],
  );
  useEffect(() => {
    load();
    const t = setInterval(load, 30_000);
    const onFocus = () => load();
    window.addEventListener("focus", onFocus);
    return () => {
      clearInterval(t);
      window.removeEventListener("focus", onFocus);
    };
  }, [load]);

  // Items shown as unread lose the highlight once the panel closes.
  const close = useCallback(() => {
    setOpen(false);
    setList(
      (l) =>
        l?.map((n) =>
          n.read_at ? n : { ...n, read_at: new Date().toISOString() },
        ) ?? l,
    );
  }, []);

  useEffect(() => {
    if (!open) return;
    const outside = (e: MouseEvent) => {
      if (box.current && !box.current.contains(e.target as Node)) close();
    };
    const esc = (e: KeyboardEvent) => e.key === "Escape" && close();
    document.addEventListener("mousedown", outside);
    document.addEventListener("keydown", esc);
    return () => {
      document.removeEventListener("mousedown", outside);
      document.removeEventListener("keydown", esc);
    };
  }, [open, close]);

  async function toggle() {
    if (open) return close();
    setOpen(true);
    setExpanded(null);
    // Show the latest list; what is shown now counts as read.
    const r = await load();
    if (r && r.unread > 0)
      api("/notifications/read", { method: "POST" })
        .then(() => setUnread(0))
        .catch(() => {});
  }

  return (
    <div className="relative" ref={box}>
      <button
        className="relative flex size-8 items-center justify-center rounded-lg hover:bg-muted"
        aria-label={translate("Thông báo")}
        aria-expanded={open}
        title={translate("Thông báo")}
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
        <div className="absolute right-0 top-10 z-30 w-[22rem] max-w-[calc(100vw-2rem)] overflow-hidden rounded-xl border bg-background shadow-lg">
          <div className="flex items-center justify-between border-b px-4 py-2.5">
            <span className="text-sm font-medium">{translate("Thông báo")}</span>
            <button
              className="text-xs text-muted-foreground hover:text-foreground"
              onClick={() => load()}
            >
              {translate("Làm mới")}
            </button>
          </div>
          {list === null ? (
            <p className="px-4 py-6 text-center text-sm text-muted-foreground">
              {translate("Đang tải…")}
            </p>
          ) : list.length === 0 ? (
            <p className="px-4 py-6 text-center text-sm text-muted-foreground">
              {translate("Chưa có thông báo")}
            </p>
          ) : (
            <ul className="max-h-96 divide-y overflow-auto">
              {list.map((n) => {
                const body = (
                  <div className="flex items-start gap-2">
                    {!n.read_at && (
                      <span className="mt-1.5 size-1.5 shrink-0 rounded-full bg-red-600" />
                    )}
                    <div className="min-w-0 flex-1">
                      <div className="text-sm font-medium">{translate(n.title)}</div>
                      {n.body && (
                        <div
                          className={`whitespace-pre-line text-xs text-muted-foreground ${expanded === n.id ? "" : "line-clamp-3"}`}
                        >
                          {n.body.trim()}
                        </div>
                      )}
                      <div className="mt-1 text-[10px] text-muted-foreground">
                        {new Date(n.created_at).toLocaleString(localeTag())}
                      </div>
                    </div>
                  </div>
                );
                return (
                  <li key={n.id} className={n.read_at ? "" : "bg-muted/40"}>
                    {n.link ? (
                      <Link
                        href={n.link}
                        className="block px-4 py-3 hover:bg-muted"
                        onClick={close}
                      >
                        {body}
                      </Link>
                    ) : (
                      <button
                        type="button"
                        className="block w-full px-4 py-3 text-left hover:bg-muted"
                        title={translate("Bấm để xem đầy đủ")}
                        onClick={() => setExpanded(expanded === n.id ? null : n.id)}
                      >
                        {body}
                      </button>
                    )}
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      )}
    </div>
  );
}
