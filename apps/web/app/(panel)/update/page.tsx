"use client";
import { localeTag } from "@/lib/i18n";
import { t as translate } from "@/lib/i18n";

import { useCallback, useEffect, useState } from "react";
import { CheckCircle2, Download, RefreshCw } from "lucide-react";
import { api, errMsg } from "@/lib/api";
import { ErrorBox } from "@/components/ui";

type State = {
  current: string;
  latest: string;
  available: boolean;
  auto: boolean;
  checked_at: string;
  check_error?: string;
  running: boolean;
  log: string;
};

// The installer restarts the panel: requests fail for a while, then the
// new version answers. Exit marker written by the agent's update unit.
const exitCode = (log: string) => log.match(/XPANEL_UPDATE_EXIT=(\d+)/)?.[1];

export default function UpdatePage() {
  const [st, setSt] = useState<State | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [updating, setUpdating] = useState(false);

  const load = useCallback(async (check = false) => {
    try {
      setSt(await api<State>(`/system/update${check ? "?check=1" : ""}`));
      setError("");
    } catch (e) {
      // Expected while the panel restarts during an update.
      if (!updating) setError(errMsg(e));
    }
  }, [updating]);
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
    if (updating && st && !st.running && exitCode(st.log) !== undefined) setUpdating(false);
  }, [updating, st]);

  async function check() {
    setBusy(true);
    await load(true);
    setBusy(false);
  }
  async function update() {
    if (!st || !confirm(translate("Cập nhật X-Panel lên {0}? Panel sẽ khởi động lại trong vài phút; website và email vẫn chạy.", [st.latest]))) return;
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

  if (!st) return <ErrorBox msg={error} />;
  const code = exitCode(st.log);
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold">{translate("Cập nhật X-Panel")}</h1>
        <p className="text-sm text-slate-500">
          {translate("Cài bản phát hành mới nhất từ GitHub (kiểm tra SHA-256). Dữ liệu, website và cấu hình được giữ nguyên.")}
        </p>
      </div>

      <section className="card grid gap-4 p-5 sm:grid-cols-2">
        <div>
          <div className="text-xs text-muted-foreground">{translate("Phiên bản đang dùng")}</div>
          <div className="text-2xl font-semibold">{st.current}</div>
        </div>
        <div>
          <div className="text-xs text-muted-foreground">{translate("Bản mới nhất")}</div>
          <div className="text-2xl font-semibold">{st.latest || "—"}</div>
          <div className="text-xs text-muted-foreground">
            {st.check_error
              ? translate("Không kiểm tra được: {0}", [st.check_error])
              : translate("Kiểm tra lúc {0}", [new Date(st.checked_at).toLocaleString(localeTag())])}
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2 sm:col-span-2">
          {st.available ? (
            <button className="btn-primary" onClick={update} disabled={active}>
              <Download className="size-4" /> {translate("Cập nhật lên {0}", [st.latest])}
            </button>
          ) : (
            <span className="flex items-center gap-1.5 text-sm text-emerald-700">
              <CheckCircle2 className="size-4" /> {translate("Đang dùng bản mới nhất")}
            </span>
          )}
          <button className="btn-ghost" onClick={check} disabled={busy || active}>
            <RefreshCw className={`size-4 ${busy ? "animate-spin" : ""}`} /> {translate("Kiểm tra bản mới")}
          </button>
        </div>
      </section>

      <section className="card space-y-2 p-5">
        <label className="flex items-center gap-2 font-semibold">
          <input type="checkbox" checked={st.auto} onChange={(e) => setAuto(e.target.checked)} />
          {translate("Tự động cập nhật")}
        </label>
        <p className="text-xs text-muted-foreground">
          {translate("Mỗi đêm từ 3:00 đến 5:00 (giờ máy chủ), nếu có bản mới thì tự cài. Panel khởi động lại vài phút; website và email không bị gián đoạn.")}
        </p>
      </section>

      {(active || st.log) && (
        <section className="card space-y-2 p-5">
          <h2 className="font-semibold">
            {active
              ? translate("Đang cập nhật…")
              : code === "0"
                ? translate("Cập nhật xong")
                : translate("Lần cập nhật gần nhất")}
          </h2>
          {code !== undefined && code !== "0" && !active && (
            <p className="text-sm text-red-600">{translate("Cập nhật lỗi (mã {0}); panel vẫn chạy bản cũ. Xem nhật ký bên dưới.", [code])}</p>
          )}
          <pre className="max-h-96 overflow-auto rounded-lg bg-slate-950 p-3 text-xs text-slate-100">{st.log || "…"}</pre>
        </section>
      )}
      <ErrorBox msg={error} />
    </div>
  );
}
