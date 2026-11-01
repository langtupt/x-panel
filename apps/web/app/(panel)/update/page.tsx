"use client";
import { localeTag, t as translate } from "@/lib/i18n";

import { useCallback, useEffect, useRef, useState } from "react";
import {
  ArrowRight,
  CheckCircle2,
  CircleAlert,
  Clock3,
  Download,
  ExternalLink,
  Loader2,
  RefreshCw,
  ShieldCheck,
  Sparkles,
  Terminal,
} from "lucide-react";
import { api, errMsg } from "@/lib/api";
import { CopyButton, ErrorBox } from "@/components/ui";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

type State = {
  current: string;
  latest: string;
  available: boolean;
  auto: boolean;
  checked_at: string;
  check_error?: string;
  notes?: string;
  published_at?: string;
  url?: string;
  running: boolean;
  log: string;
};

// The installer restarts the panel: requests fail for a while, then the
// new version answers. Exit marker written by the agent's update unit.
const exitCode = (log: string) => log.match(/XPANEL_UPDATE_EXIT=(\d+)/)?.[1];

/** Release notes are GitHub markdown; show them as plain text lines. */
function Notes({ text }: { text: string }) {
  return (
    <div className="space-y-1.5 text-sm leading-6">
      {text
        .replace(/\r/g, "")
        .split("\n")
        .map((line, i) => {
          const clean = line.replace(/\*\*/g, "").replace(/`/g, "");
          if (!clean.trim()) return <div key={i} className="h-1" />;
          const bullet = /^\s*[-*]\s+/.test(clean);
          return bullet ? (
            <p key={i} className="flex gap-2 pl-1 text-muted-foreground">
              <span className="mt-2.5 size-1 shrink-0 rounded-full bg-foreground/50" />
              <span>{clean.replace(/^\s*[-*]\s+/, "")}</span>
            </p>
          ) : (
            <p key={i}>{clean.replace(/^#+\s*/, "")}</p>
          );
        })}
    </div>
  );
}

function Toggle({
  checked,
  onChange,
  label,
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
  label: string;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      onClick={() => onChange(!checked)}
      className={`relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition-colors ${checked ? "bg-foreground" : "bg-muted-foreground/30"}`}
    >
      <span
        className={`inline-block size-5 rounded-full bg-background shadow transition-transform ${checked ? "translate-x-5" : "translate-x-0.5"}`}
      />
    </button>
  );
}

export default function UpdatePage() {
  const [st, setSt] = useState<State | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [updating, setUpdating] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const logRef = useRef<HTMLPreElement>(null);

  const load = useCallback(
    async (check = false) => {
      try {
        setSt(await api<State>(`/system/update${check ? "?check=1" : ""}`));
        setError("");
      } catch (e) {
        // Expected while the panel restarts during an update.
        if (!updating) setError(errMsg(e));
      }
    },
    [updating],
  );
  useEffect(() => {
    load();
  }, [load]);

  const active = updating || !!st?.running;
  useEffect(() => {
    if (!active) return;
    const id = setInterval(() => load(), 3000);
    return () => clearInterval(id);
  }, [active, load]);
  useEffect(() => {
    if (updating && st && !st.running && exitCode(st.log) !== undefined)
      setUpdating(false);
  }, [updating, st]);
  // Keep the newest log lines in view while updating.
  useEffect(() => {
    if (active && logRef.current)
      logRef.current.scrollTop = logRef.current.scrollHeight;
  }, [active, st?.log]);

  async function check() {
    setBusy(true);
    await load(true);
    setBusy(false);
  }
  async function update() {
    setConfirm(false);
    setError("");
    try {
      await api("/system/update", { method: "POST" });
      setUpdating(true);
      load();
    } catch (e) {
      setError(errMsg(e));
    }
  }
  async function setAuto(auto: boolean) {
    try {
      await api("/system/update", { method: "PUT", body: { auto } });
      load();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  const header = (
    <div>
      <div className="mb-2 flex items-center gap-2 text-xs text-muted-foreground">
        <RefreshCw className="size-3.5" />
        {translate("Phiên bản panel")}
      </div>
      <h1>{translate("Cập nhật X-Panel")}</h1>
      <p className="mt-1 text-sm text-muted-foreground">
        {translate(
          "Cài bản phát hành mới nhất từ GitHub (kiểm tra SHA-256). Dữ liệu, website và cấu hình được giữ nguyên.",
        )}
      </p>
    </div>
  );

  if (!st)
    return (
      <div className="space-y-6">
        {header}
        <ErrorBox msg={error} />
        {!error && <Skeleton className="h-48 rounded-xl" />}
      </div>
    );

  const code = exitCode(st.log);
  const failed = !active && code !== undefined && code !== "0";
  const succeeded = !active && code === "0";

  return (
    <div className="space-y-6">
      {header}
      <ErrorBox msg={error} />

      <div className="grid gap-5 xl:grid-cols-3">
        <section className="card overflow-hidden xl:col-span-2">
          <div className="flex flex-wrap items-center gap-6 p-6">
            <div>
              <p className="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">
                {translate("Đang dùng")}
              </p>
              <p className="mt-1 font-mono text-3xl font-semibold tracking-tight">
                {st.current}
              </p>
            </div>
            <ArrowRight className="size-5 text-muted-foreground" />
            <div>
              <p className="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">
                {translate("Bản mới nhất")}
              </p>
              <p className="mt-1 font-mono text-3xl font-semibold tracking-tight">
                {st.latest || "—"}
              </p>
            </div>
            <div className="ml-auto">
              {active ? (
                <span className="inline-flex items-center gap-2 rounded-full border px-3 py-1.5 text-xs font-medium">
                  <Loader2 className="size-3.5 animate-spin" />
                  {translate("Đang cập nhật…")}
                </span>
              ) : st.available ? (
                <span className="inline-flex items-center gap-2 rounded-full border border-amber-300 bg-amber-50 px-3 py-1.5 text-xs font-medium text-amber-800">
                  <Sparkles className="size-3.5" />
                  {translate("Có bản mới")}
                </span>
              ) : (
                <span className="inline-flex items-center gap-2 rounded-full border border-emerald-200 bg-emerald-50 px-3 py-1.5 text-xs font-medium text-emerald-700">
                  <CheckCircle2 className="size-3.5" />
                  {translate("Đang dùng bản mới nhất")}
                </span>
              )}
            </div>
          </div>
          <div className="flex flex-wrap items-center justify-between gap-3 border-t bg-muted/30 px-6 py-3">
            <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <Clock3 className="size-3.5" />
              {st.check_error
                ? translate("Không kiểm tra được: {0}", [st.check_error])
                : translate("Kiểm tra lúc {0}", [
                    new Date(st.checked_at).toLocaleString(localeTag()),
                  ])}
            </span>
            <div className="flex gap-2">
              <Button
                variant="outline"
                size="sm"
                onClick={check}
                disabled={busy || active}
              >
                <RefreshCw className={busy ? "animate-spin" : ""} />
                {translate("Kiểm tra bản mới")}
              </Button>
              {st.available && (
                <Button
                  size="sm"
                  onClick={() => setConfirm(true)}
                  disabled={active}
                >
                  <Download />
                  {translate("Cập nhật lên {0}", [st.latest])}
                </Button>
              )}
            </div>
          </div>
        </section>

        <section className="card flex flex-col gap-4 p-6">
          <div className="flex items-start justify-between gap-4">
            <div>
              <h2 className="text-sm font-semibold">
                {translate("Tự động cập nhật")}
              </h2>
              <p className="mt-1 text-xs leading-5 text-muted-foreground">
                {translate(
                  "Mỗi đêm từ 3:00 đến 5:00 (giờ máy chủ), nếu có bản mới thì tự cài. Panel khởi động lại vài phút; website và email không bị gián đoạn.",
                )}
              </p>
            </div>
            <Toggle
              checked={st.auto}
              onChange={setAuto}
              label={translate("Tự động cập nhật")}
            />
          </div>
          <div className="mt-auto flex items-start gap-2 rounded-lg bg-muted/50 p-3 text-xs leading-5 text-muted-foreground">
            <ShieldCheck className="mt-0.5 size-4 shrink-0" />
            {translate(
              "Bản cài được kiểm tra SHA-256. Nếu cập nhật lỗi, panel giữ nguyên bản đang chạy.",
            )}
          </div>
        </section>
      </div>

      {(active || st.log) && (
        <section className="card overflow-hidden">
          <div className="flex flex-wrap items-center justify-between gap-2 border-b px-5 py-3">
            <h2 className="flex items-center gap-2 text-sm font-semibold">
              {active ? (
                <Loader2 className="size-4 animate-spin" />
              ) : failed ? (
                <CircleAlert className="size-4 text-destructive" />
              ) : (
                <CheckCircle2 className="size-4 text-emerald-600" />
              )}
              {active
                ? translate("Đang cập nhật…")
                : succeeded
                  ? translate("Cập nhật xong")
                  : translate("Lần cập nhật gần nhất")}
            </h2>
            <CopyButton text={st.log} />
          </div>
          {active && (
            <div className="h-1 overflow-hidden bg-muted">
              <div className="h-full w-1/3 animate-pulse bg-foreground/60" />
            </div>
          )}
          {failed && (
            <p className="border-b bg-red-50 px-5 py-2 text-sm text-red-700">
              {translate(
                "Cập nhật lỗi (mã {0}); panel vẫn chạy bản cũ. Xem nhật ký bên dưới.",
                [code],
              )}
            </p>
          )}
          <div className="bg-neutral-950 text-neutral-200">
            <div className="flex items-center gap-2 border-b border-white/10 px-5 py-2 text-xs text-neutral-400">
              <Terminal className="size-3.5" />
              {translate("Nhật ký cập nhật")}
            </div>
            <pre
              ref={logRef}
              className="max-h-96 overflow-auto p-5 font-mono text-xs leading-6 whitespace-pre-wrap"
            >
              {st.log || "…"}
            </pre>
          </div>
        </section>
      )}

      {st.notes && (
        <section className="card overflow-hidden">
          <div className="flex flex-wrap items-center justify-between gap-2 border-b px-5 py-3">
            <h2 className="flex items-center gap-2 text-sm font-semibold">
              <Sparkles className="size-4" />
              {translate("Có gì mới trong {0}", [st.latest])}
              {st.published_at && (
                <span className="text-xs font-normal text-muted-foreground">
                  · {new Date(st.published_at).toLocaleDateString(localeTag())}
                </span>
              )}
            </h2>
            {st.url && (
              <a
                href={st.url}
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
              >
                GitHub <ExternalLink className="size-3" />
              </a>
            )}
          </div>
          <div className="max-h-[28rem] overflow-y-auto px-5 py-4">
            <Notes text={st.notes} />
          </div>
        </section>
      )}

      <Dialog open={confirm} onOpenChange={setConfirm}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>
              {translate("Cập nhật X-Panel lên {0}?", [st.latest])}
            </DialogTitle>
            <DialogDescription>
              {translate(
                "Panel sẽ khởi động lại trong vài phút; website và email vẫn chạy.",
              )}
            </DialogDescription>
          </DialogHeader>
          <div className="flex justify-end gap-2">
            <Button variant="outline" onClick={() => setConfirm(false)}>
              {translate("Hủy")}
            </Button>
            <Button onClick={update}>
              <Download />
              {translate("Cập nhật ngay")}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
