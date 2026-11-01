"use client";
import { t as translate } from "@/lib/i18n";

import { FormEvent, useCallback, useEffect, useState } from "react";
import { api, errMsg, type CronJob, type CronStatus } from "@/lib/api";
import { localeTag } from "@/lib/i18n";
import { ErrorBox, Modal } from "@/components/ui";
import { OwnerSelect, useHostingAccounts } from "@/components/owner";
import { useSession } from "../session";

const PRESETS: { label: string; value: string }[] = [
  { label: "Mỗi phút", value: "* * * * *" },
  { label: "Mỗi 5 phút", value: "*/5 * * * *" },
  { label: "Mỗi giờ", value: "0 * * * *" },
  { label: "Hằng ngày 3:00", value: "0 3 * * *" },
  { label: "Hằng tuần (CN 3:00)", value: "0 3 * * 0" },
  { label: "Hằng tháng", value: "0 3 1 * *" },
];

function describe(s: string) {
  return PRESETS.find((p) => p.value === s)?.label ?? s;
}

const when = (iso: string) =>
  iso ? new Date(iso).toLocaleString(localeTag()) : "";

// LastRun shows the job's latest run; it refreshes while the job runs.
function LastRun({
  job,
  tick,
  onLog,
}: {
  job: CronJob;
  tick: number;
  onLog: (st: CronStatus) => void;
}) {
  const [st, setSt] = useState<CronStatus | null>(null);
  useEffect(() => {
    if (!job.enabled) {
      setSt(null);
      return;
    }
    let stop = false;
    let timer: ReturnType<typeof setTimeout>;
    const poll = () =>
      api<CronStatus>(`/cron/${job.id}/status`)
        .then((s) => {
          if (stop) return;
          setSt(s);
          if (s.running) timer = setTimeout(poll, 3000);
        })
        .catch(() => !stop && setSt(null));
    poll();
    return () => {
      stop = true;
      clearTimeout(timer);
    };
  }, [job.id, job.enabled, job.schedule, job.command, tick]);

  if (!job.enabled) return <span className="text-xs text-slate-400">—</span>;
  if (!st) return <span className="text-xs text-slate-400">…</span>;
  return (
    <button
      type="button"
      className="text-left text-xs hover:underline"
      onClick={() => onLog(st)}
      title={translate("Xem nhật ký")}
    >
      {st.running ? (
        <span className="text-blue-600">{translate("● Đang chạy…")}</span>
      ) : !st.last_run ? (
        <span className="text-slate-500">{translate("Chưa chạy lần nào")}</span>
      ) : st.result === "success" ? (
        <span className="text-emerald-600">{translate("✓ Thành công")}</span>
      ) : (
        <span className="text-red-600">
          {translate("✗ Lỗi (mã {0})", [st.exit_code])}
        </span>
      )}
      {st.last_run && (
        <span className="block text-slate-500">{when(st.last_run)}</span>
      )}
    </button>
  );
}

function CronForm({
  editing,
  manager,
  onDone,
  onCancel,
}: {
  editing: CronJob | null;
  manager: boolean;
  onDone: () => void;
  onCancel: () => void;
}) {
  const me = useSession();
  const owners = useHostingAccounts(manager && !editing);
  const [ownerId, setOwnerId] = useState<number | null>(null);
  const [schedule, setSchedule] = useState(editing?.schedule ?? "*/5 * * * *");
  const [command, setCommand] = useState(editing?.command ?? "");
  const [enabled, setEnabled] = useState(editing?.enabled ?? true);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (manager && !editing && ownerId === null && owners.length)
      setOwnerId(owners[0].id);
  }, [manager, editing, owners, ownerId]);

  const home = `/home/${editing?.username ?? (manager ? (owners.find((o) => o.id === ownerId)?.username ?? "user") : me.username)}`;

  async function submit(e: FormEvent) {
    e.preventDefault();
    setSaving(true);
    setError("");
    try {
      const body = {
        user_id: manager ? ownerId : null,
        schedule,
        command,
        enabled,
      };
      if (editing) await api(`/cron/${editing.id}`, { method: "PUT", body });
      else await api("/cron", { method: "POST", body });
      onDone();
    } catch (err) {
      setError(errMsg(err));
      setSaving(false);
    }
  }

  return (
    <form onSubmit={submit} className="card space-y-4 p-5">
      <h2 className="font-semibold">
        {translate(
          editing ? translate("Sửa cron job") : translate("Thêm cron job"),
        )}
      </h2>
      <div className="modal-grid">
        {manager && !editing && (
          <div className="md:col-span-2">
            <label className="label">{translate("Tài khoản")}</label>
            <OwnerSelect
              owners={owners}
              value={ownerId}
              onChange={setOwnerId}
            />
          </div>
        )}
        <div>
          <label className="label">{translate("Lịch chạy")}</label>
          <input
            className="input font-mono"
            required
            value={schedule}
            onChange={(e) => setSchedule(e.target.value)}
            placeholder={translate("phút giờ ngày tháng thứ")}
          />
          <div className="mt-2 flex flex-wrap gap-1.5">
            {PRESETS.map((p) => (
              <button
                key={p.value}
                type="button"
                onClick={() => setSchedule(p.value)}
                className={`rounded-md border px-2 py-0.5 text-xs ${schedule === p.value ? "border-brand-500 bg-brand-50 text-brand-700" : "border-slate-300"}`}
              >
                {translate(p.label)}
              </button>
            ))}
          </div>
        </div>
        <div>
          <label className="label">{translate("Lệnh")}</label>
          <input
            className="input font-mono"
            required
            maxLength={1000}
            value={command}
            onChange={(e) => setCommand(e.target.value)}
            placeholder={translate(
              `/usr/bin/php8.3 ${home}/www/example.com/artisan schedule:run`,
            )}
          />
          <p className="mt-1 text-xs text-slate-400">
            {translate("Chạy bằng user của tài khoản, thư mục home")}{" "}
            {translate(home)}.
          </p>
        </div>
      </div>
      <label className="flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          checked={enabled}
          onChange={(e) => setEnabled(e.target.checked)}
        />
        {translate(" ")}
        {translate("Đang bật")}
      </label>
      <ErrorBox msg={error} />
      <div className="modal-actions">
        <button className="btn-primary" disabled={saving}>
          {translate(saving ? translate("Đang lưu…") : translate("Lưu"))}
        </button>
        <button type="button" className="btn-ghost" onClick={onCancel}>
          {translate("Hủy")}
        </button>
      </div>
    </form>
  );
}

export default function CronPage() {
  const me = useSession();
  const manager = me.role !== "user";
  const [list, setList] = useState<CronJob[]>([]);
  const [form, setForm] = useState<{ editing: CronJob | null } | null>(null);
  const [deleting, setDeleting] = useState<CronJob | null>(null);
  const [log, setLog] = useState<{ job: CronJob; st: CronStatus } | null>(
    null,
  );
  const [tick, setTick] = useState(0);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");

  const load = useCallback(() => {
    api<CronJob[]>("/cron")
      .then(setList)
      .catch((e) => setError(errMsg(e)));
  }, []);
  useEffect(load, [load]);

  async function toggle(j: CronJob) {
    try {
      await api(`/cron/${j.id}`, {
        method: "PUT",
        body: { schedule: j.schedule, command: j.command, enabled: !j.enabled },
      });
      load();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  async function runNow(j: CronJob) {
    setError("");
    setNotice("");
    try {
      await api(`/cron/${j.id}/run`, { method: "POST" });
      setNotice(translate("Đã chạy: {0}", [j.command]));
      setTimeout(() => setTick((t) => t + 1), 1000);
    } catch (e) {
      setError(errMsg(e));
    }
  }

  async function confirmDelete() {
    if (!deleting) return;
    try {
      await api(`/cron/${deleting.id}`, { method: "DELETE" });
      load();
    } catch (e) {
      setError(errMsg(e));
    }
    setDeleting(null);
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">Cron</h1>
          <p className="text-sm text-slate-500">
            {translate("Lệnh chạy định kỳ; tự dừng khi tài khoản bị khóa.")}
          </p>
        </div>
        {!form && (
          <button
            className="btn-primary"
            onClick={() => setForm({ editing: null })}
          >
            {translate("+ Thêm cron")}
          </button>
        )}
      </div>
      {form && (
        <Modal
          size="wide"
          title={translate("Lịch chạy Cron")}
          onClose={() => setForm(null)}
        >
          <CronForm
            key={form.editing?.id ?? "new"}
            editing={form.editing}
            manager={manager}
            onCancel={() => setForm(null)}
            onDone={() => {
              setForm(null);
              load();
            }}
          />
        </Modal>
      )}
      <ErrorBox msg={error} />
      {notice && (
        <div className="break-all rounded-lg border border-emerald-200 bg-emerald-50 px-4 py-2 text-sm text-emerald-800">
          {notice}
        </div>
      )}

      <div className="card overflow-x-auto">
        <table className="w-full text-left text-sm">
          <thead className="border-b border-slate-200 bg-slate-50 text-xs uppercase text-slate-500">
            <tr>
              <th className="px-4 py-3">{translate("Lịch")}</th>
              <th className="px-4 py-3">{translate("Lệnh")}</th>
              {manager && (
                <th className="px-4 py-3">{translate("Tài khoản")}</th>
              )}
              <th className="px-4 py-3">{translate("Trạng thái")}</th>
              <th className="px-4 py-3">{translate("Lần chạy cuối")}</th>
              <th className="px-4 py-3" />
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100">
            {list.map((j) => (
              <tr key={j.id} className={j.enabled ? "" : "text-slate-400"}>
                <td className="px-4 py-3 whitespace-nowrap">
                  <div className="font-mono text-xs">
                    {translate(j.schedule)}
                  </div>
                  <div className="text-xs text-slate-500">
                    {translate(
                      describe(j.schedule) !== j.schedule &&
                        describe(j.schedule),
                    )}
                  </div>
                </td>
                <td className="max-w-md px-4 py-3 font-mono text-xs break-all">
                  {translate(j.command)}
                </td>
                {manager && <td className="px-4 py-3">{j.username}</td>}
                <td className="px-4 py-3">
                  <button
                    onClick={() => toggle(j)}
                    className={`text-xs ${j.enabled ? "text-emerald-600" : "text-slate-500"}`}
                  >
                    {translate(
                      j.enabled ? translate("● Bật") : translate("○ Tắt"),
                    )}
                  </button>
                </td>
                <td className="px-4 py-3 whitespace-nowrap">
                  <LastRun
                    job={j}
                    tick={tick}
                    onLog={(st) => setLog({ job: j, st })}
                  />
                </td>
                <td className="px-4 py-3">
                  <div className="flex justify-end gap-1.5">
                    <button
                      className="btn-ghost px-2.5 py-1 text-xs"
                      disabled={!j.enabled}
                      title={
                        j.enabled
                          ? translate("Chạy ngay một lần")
                          : translate("Bật cron job trước khi chạy")
                      }
                      onClick={() => runNow(j)}
                    >
                      {translate("Chạy ngay")}
                    </button>
                    <button
                      className="btn-ghost px-2.5 py-1 text-xs"
                      onClick={() => setForm({ editing: j })}
                    >
                      {translate("Sửa")}
                    </button>
                    <button
                      className="btn-ghost px-2.5 py-1 text-xs text-red-600"
                      onClick={() => setDeleting(j)}
                    >
                      {translate("Xóa")}
                    </button>
                  </div>
                </td>
              </tr>
            ))}
            {list.length === 0 && (
              <tr>
                <td
                  colSpan={6}
                  className="px-4 py-8 text-center text-slate-500"
                >
                  {translate("Chưa có cron job nào.")}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {log && (
        <Modal
          size="wide"
          title={translate("Nhật ký cron job")}
          onClose={() => setLog(null)}
        >
          <p className="mb-2 break-all font-mono text-xs text-slate-600">
            {log.job.schedule} {log.job.command}
          </p>
          <p className="mb-3 text-sm">
            {log.st.running
              ? translate("Đang chạy…")
              : !log.st.last_run
                ? translate("Chưa chạy lần nào.")
                : translate("Lần cuối: {0} → {1}, mã thoát {2}", [
                    when(log.st.last_run),
                    when(log.st.finished),
                    log.st.exit_code,
                  ])}
          </p>
          <pre className="max-h-[60vh] overflow-auto whitespace-pre-wrap break-all rounded-lg bg-slate-950 p-3 font-mono text-xs text-slate-100">
            {log.st.log.trim() || translate("(chưa có đầu ra)")}
          </pre>
          <p className="mt-2 text-xs text-slate-500">
            {translate(
              "Hiện đầu ra (stdout/stderr) của các lần chạy gần nhất. Lệnh chạy trong sandbox: chỉ ghi được trong thư mục home.",
            )}
          </p>
          <div className="mt-4 flex justify-end gap-2">
            <button
              className="btn-ghost"
              onClick={() =>
                api<CronStatus>(`/cron/${log.job.id}/status`)
                  .then((st) => setLog({ job: log.job, st }))
                  .catch((e) => setError(errMsg(e)))
              }
            >
              {translate("Làm mới")}
            </button>
            <button className="btn-primary" onClick={() => setLog(null)}>
              {translate("Đóng")}
            </button>
          </div>
        </Modal>
      )}

      {deleting && (
        <Modal
          title={translate("Xóa cron job")}
          onClose={() => setDeleting(null)}
        >
          <p className="break-all font-mono text-sm text-slate-600">
            {translate(deleting.schedule)} {translate(deleting.command)}
          </p>
          <div className="mt-5 flex justify-end gap-2">
            <button className="btn-ghost" onClick={() => setDeleting(null)}>
              {translate("Hủy")}
            </button>
            <button
              className="btn bg-red-600 text-white hover:bg-red-700"
              onClick={confirmDelete}
            >
              {translate("Xóa")}
            </button>
          </div>
        </Modal>
      )}
    </div>
  );
}
