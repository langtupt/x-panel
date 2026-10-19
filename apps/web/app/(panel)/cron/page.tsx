"use client";

import { FormEvent, useCallback, useEffect, useState } from "react";
import { api, errMsg, type CronJob } from "@/lib/api";
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
        {editing ? "Sửa cron job" : "Thêm cron job"}
      </h2>
      <div className="modal-grid">
        {manager && !editing && (
          <div className="md:col-span-2">
            <label className="label">Tài khoản</label>
            <OwnerSelect
              owners={owners}
              value={ownerId}
              onChange={setOwnerId}
            />
          </div>
        )}
        <div>
          <label className="label">Lịch chạy</label>
          <input
            className="input font-mono"
            required
            value={schedule}
            onChange={(e) => setSchedule(e.target.value)}
            placeholder="phút giờ ngày tháng thứ"
          />
          <div className="mt-2 flex flex-wrap gap-1.5">
            {PRESETS.map((p) => (
              <button
                key={p.value}
                type="button"
                onClick={() => setSchedule(p.value)}
                className={`rounded-md border px-2 py-0.5 text-xs ${schedule === p.value ? "border-brand-500 bg-brand-50 text-brand-700" : "border-slate-300"}`}
              >
                {p.label}
              </button>
            ))}
          </div>
        </div>
        <div>
          <label className="label">Lệnh</label>
          <input
            className="input font-mono"
            required
            maxLength={1000}
            value={command}
            onChange={(e) => setCommand(e.target.value)}
            placeholder={`/usr/bin/php8.3 ${home}/www/example.com/artisan schedule:run`}
          />
          <p className="mt-1 text-xs text-slate-400">
            Chạy bằng user của tài khoản, thư mục home {home}.
          </p>
        </div>
      </div>
      <label className="flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          checked={enabled}
          onChange={(e) => setEnabled(e.target.checked)}
        />{" "}
        Đang bật
      </label>
      <ErrorBox msg={error} />
      <div className="modal-actions">
        <button className="btn-primary" disabled={saving}>
          {saving ? "Đang lưu…" : "Lưu"}
        </button>
        <button type="button" className="btn-ghost" onClick={onCancel}>
          Hủy
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
            Lệnh chạy định kỳ; tự dừng khi tài khoản bị khóa.
          </p>
        </div>
        {!form && (
          <button
            className="btn-primary"
            onClick={() => setForm({ editing: null })}
          >
            + Thêm cron
          </button>
        )}
      </div>
      {form && (
        <Modal size="wide" title="Lịch chạy Cron" onClose={() => setForm(null)}>
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

      <div className="card overflow-x-auto">
        <table className="w-full text-left text-sm">
          <thead className="border-b border-slate-200 bg-slate-50 text-xs uppercase text-slate-500">
            <tr>
              <th className="px-4 py-3">Lịch</th>
              <th className="px-4 py-3">Lệnh</th>
              {manager && <th className="px-4 py-3">Tài khoản</th>}
              <th className="px-4 py-3">Trạng thái</th>
              <th className="px-4 py-3" />
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100">
            {list.map((j) => (
              <tr key={j.id} className={j.enabled ? "" : "text-slate-400"}>
                <td className="px-4 py-3 whitespace-nowrap">
                  <div className="font-mono text-xs">{j.schedule}</div>
                  <div className="text-xs text-slate-500">
                    {describe(j.schedule) !== j.schedule &&
                      describe(j.schedule)}
                  </div>
                </td>
                <td className="max-w-md px-4 py-3 font-mono text-xs break-all">
                  {j.command}
                </td>
                {manager && <td className="px-4 py-3">{j.username}</td>}
                <td className="px-4 py-3">
                  <button
                    onClick={() => toggle(j)}
                    className={`text-xs ${j.enabled ? "text-emerald-600" : "text-slate-500"}`}
                  >
                    {j.enabled ? "● Bật" : "○ Tắt"}
                  </button>
                </td>
                <td className="px-4 py-3">
                  <div className="flex justify-end gap-1.5">
                    <button
                      className="btn-ghost px-2.5 py-1 text-xs"
                      onClick={() => setForm({ editing: j })}
                    >
                      Sửa
                    </button>
                    <button
                      className="btn-ghost px-2.5 py-1 text-xs text-red-600"
                      onClick={() => setDeleting(j)}
                    >
                      Xóa
                    </button>
                  </div>
                </td>
              </tr>
            ))}
            {list.length === 0 && (
              <tr>
                <td
                  colSpan={5}
                  className="px-4 py-8 text-center text-slate-500"
                >
                  Chưa có cron job nào.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {deleting && (
        <Modal title="Xóa cron job" onClose={() => setDeleting(null)}>
          <p className="break-all font-mono text-sm text-slate-600">
            {deleting.schedule} {deleting.command}
          </p>
          <div className="mt-5 flex justify-end gap-2">
            <button className="btn-ghost" onClick={() => setDeleting(null)}>
              Hủy
            </button>
            <button
              className="btn bg-red-600 text-white hover:bg-red-700"
              onClick={confirmDelete}
            >
              Xóa
            </button>
          </div>
        </Modal>
      )}
    </div>
  );
}
