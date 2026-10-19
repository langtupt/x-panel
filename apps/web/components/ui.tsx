"use client";

import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

export function Modal({
  title,
  onClose,
  children,
  size = "default",
}: {
  size?: "default" | "wide" | "large";
  title: string;
  onClose: () => void;
  children: React.ReactNode;
}) {
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent
        className={`panel-modal flex max-h-[calc(100dvh-2rem)] flex-col gap-0 overflow-hidden p-0 ${size === "large" ? "sm:max-w-5xl" : size === "wide" ? "sm:max-w-3xl" : "sm:max-w-xl"}`}
        aria-describedby={undefined}
      >
        <DialogHeader className="shrink-0 border-b px-5 py-5 pr-14 sm:px-6 sm:pr-14">
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>
        <div className="modal-body min-h-0 overflow-y-auto overscroll-contain p-5 sm:p-6">{children}</div>
      </DialogContent>
    </Dialog>
  );
}

export function ErrorBox({ msg }: { msg: string }) {
  if (!msg) return null;
  return (
    <p className="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700">{msg}</p>
  );
}

/** Usage bar; total 0 means unlimited. */
export function UsageBar({
  used,
  total,
  fmt,
}: {
  used: number;
  total: number;
  fmt: (n: number) => string;
}) {
  const pct = total > 0 ? Math.min(100, Math.round((used / total) * 100)) : 0;
  const color =
    pct >= 90 ? "bg-red-500" : pct >= 75 ? "bg-amber-500" : "bg-brand-500";
  return (
    <div className="min-w-28">
      <div className="text-xs text-slate-600">
        {fmt(used)} / {total > 0 ? fmt(total) : "∞"}
      </div>
      {total > 0 && (
        <div className="mt-1 h-1.5 rounded-full bg-slate-100">
          <div
            className={`h-1.5 rounded-full ${color}`}
            style={{ width: `${pct}%` }}
          />
        </div>
      )}
    </div>
  );
}
