"use client";
import { t as translate } from "@/lib/i18n";

import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useEffect, useState } from "react";
import { Check, Copy } from "lucide-react";

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
          <DialogTitle>{translate(title)}</DialogTitle>
        </DialogHeader>
        <div className="modal-body min-h-0 overflow-y-auto overscroll-contain p-5 sm:p-6">
          {children}
        </div>
      </DialogContent>
    </Dialog>
  );
}

export function ErrorBox({ msg }: { msg: string }) {
  if (!msg) return null;
  return (
    <p className="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700">
      {translate(msg)}
    </p>
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
        {translate(fmt(used))} / {translate(total > 0 ? fmt(total) : "∞")}
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

async function writeClipboard(text: string) {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    // Older browsers / non-secure origins: fall back to a hidden textarea.
    const area = document.createElement("textarea");
    area.value = text;
    area.setAttribute("readonly", "");
    area.style.position = "fixed";
    area.style.opacity = "0";
    document.body.appendChild(area);
    area.select();
    const ok = document.execCommand("copy");
    area.remove();
    return ok;
  }
}

/** Small button that copies text (logs, commands…) to the clipboard. */
export function CopyButton({
  text,
  label = "Sao chép",
  className = "",
  tone = "light",
}: {
  text: string;
  label?: string;
  className?: string;
  tone?: "light" | "dark";
}) {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const id = setTimeout(() => setCopied(false), 1500);
    return () => clearTimeout(id);
  }, [copied]);
  const colors =
    tone === "dark"
      ? "text-neutral-400 hover:bg-white/10 hover:text-white"
      : "text-muted-foreground hover:bg-muted hover:text-foreground";
  return (
    <button
      type="button"
      disabled={!text}
      onClick={async () => setCopied(await writeClipboard(text))}
      title={translate(label)}
      aria-label={translate(label)}
      className={`inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-[11px] font-medium transition-colors disabled:pointer-events-none disabled:opacity-40 ${colors} ${className}`}
    >
      {copied ? (
        <Check className="size-3.5 text-emerald-500" />
      ) : (
        <Copy className="size-3.5" />
      )}
      {translate(copied ? "Đã sao chép" : label)}
    </button>
  );
}
