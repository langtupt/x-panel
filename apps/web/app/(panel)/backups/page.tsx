"use client";
import { localeTag } from "@/lib/i18n";
import { t as translate } from "@/lib/i18n";

import { FormEvent, useCallback, useEffect, useState } from "react";
import { api, errMsg, fmtBytes, type Task } from "@/lib/api";
import { useTask } from "@/lib/useTask";
import { ErrorBox, Modal } from "@/components/ui";
import { TaskCard } from "@/components/task";
import { OwnerSelect, useHostingAccounts } from "@/components/owner";
import { useSession } from "../session";

type Settings = {
  connected: boolean;
  email: string;
  client_id: string;
  schedule: "off" | "daily" | "weekly";
  hour: number;
  keep: number;
  passphrase_set: boolean;
  pending?: {
    user_code: string;
    verification_url: string;
    status: string;
    error?: string;
    expires_at: string;
  };
};
type Backup = {
  id: number;
  user_id: number | null;
  username: string;
  file_name: string;
  size_bytes: number;
  kind: "manual" | "scheduled";
  contents: {
    databases: { name: string }[] | null;
    mail_domains: string[] | null;
  };
  created_at: string;
};

function DriveCard({ st, reload }: { st: Settings; reload: () => void }) {
  const [clientId, setClientId] = useState(st.client_id);
  const [secret, setSecret] = useState("");
  const [error, setError] = useState("");
  const waiting = st.pending?.status === "waiting";

  useEffect(() => {
    if (!waiting) return;
    const t = setInterval(reload, 4000);
    return () => clearInterval(t);
  }, [waiting, reload]);

  async function connect(e: FormEvent) {
    e.preventDefault();
    setError("");
    try {
      await api("/backups/drive", {
        method: "POST",
        body: { client_id: clientId, client_secret: secret },
      });
      reload();
    } catch (err) {
      setError(errMsg(err));
    }
  }

  return (
    <section className="card space-y-4 p-5">
      <div>
        <h2 className="font-semibold">{translate("Google Drive của bạn")}</h2>
        <p className="text-xs text-slate-500">
          {translate(
            "Tài khoản này dùng OAuth client và Google Drive riêng; không ai khác (kể cả quản trị viên) dùng chung.",
          )}
        </p>
      </div>
      {st.connected ? (
        <div className="flex flex-wrap items-center justify-between gap-3">
          <p className="text-sm">
            {translate("Đã kết nối:")} <b>{st.email}</b>{" "}
            {translate("— backup lưu trong thư mục")} <i>X-Panel Backups</i>.
          </p>
          <button
            className="btn-ghost text-red-600"
            onClick={() => {
              if (
                confirm(
                  translate(
                    "Ngắt kết nối Google Drive? Các bản backup đã lưu vẫn còn trên Drive.",
                  ),
                )
              )
                api("/backups/drive", { method: "DELETE" })
                  .then(reload)
                  .catch((e) => setError(errMsg(e)));
            }}
          >
            {translate("Ngắt kết nối")}
          </button>
        </div>
      ) : waiting && st.pending ? (
        <div className="rounded-lg bg-sky-50 p-4 text-sm text-sky-900">
          {translate("Mở")}{" "}
          <a
            className="font-medium underline"
            href={st.pending.verification_url}
            target="_blank"
            rel="noreferrer"
          >
            {translate(st.pending.verification_url)}
          </a>{" "}
          {translate(", đăng nhập Google và nhập mã:")}{" "}
          <div className="my-2 font-mono text-2xl font-bold tracking-widest">
            {translate(st.pending.user_code)}
          </div>
          {translate("Trang này tự cập nhật khi bạn xác nhận xong.")}
        </div>
      ) : (
        <form onSubmit={connect} className="space-y-3">
          <ol className="list-inside list-decimal space-y-1 text-sm text-slate-600">
            <li>
              {translate("Đăng nhập")}{" "}
              <a
                className="underline"
                href="https://console.cloud.google.com/apis/credentials"
                target="_blank"
                rel="noreferrer"
              >
                Google Cloud Console
              </a>{" "}
              {translate("bằng tài khoản Google")} <b>{translate("của bạn")}</b>{" "}
              {translate(", tạo project và bật")} <b>Google Drive API</b>.
            </li>
            <li>
              {translate("Tạo")} <b>OAuth client ID</b> {translate("loại")}{" "}
              <b>TVs and Limited Input devices</b>{" "}
              {translate(
                "(màn hình đồng ý: thêm email của bạn vào Test users).",
              )}
            </li>
            <li>
              {translate(
                "Dán Client ID và Client secret vào đây rồi bấm Kết nối.",
              )}
            </li>
          </ol>
          <div className="modal-grid">
            <div>
              <label className="label">Client ID</label>
              <input
                className="input font-mono text-xs"
                required
                value={clientId}
                onChange={(e) => setClientId(e.target.value)}
                placeholder="1234-abc.apps.googleusercontent.com"
              />
            </div>
            <div>
              <label className="label">Client secret</label>
              <input
                className="input font-mono text-xs"
                type="password"
                required
                value={secret}
                onChange={(e) => setSecret(e.target.value)}
              />
            </div>
          </div>
          {st.pending?.status === "failed" && (
            <ErrorBox
              msg={translate("Kết nối thất bại: {0}", [st.pending.error])}
            />
          )}
          <button className="btn-primary">
            {translate("Kết nối Google Drive")}
          </button>
        </form>
      )}
      <ErrorBox msg={error} />
    </section>
  );
}

function ScheduleCard({ st, reload }: { st: Settings; reload: () => void }) {
  const [schedule, setSchedule] = useState(st.schedule);
  const [hour, setHour] = useState(st.hour);
  const [keep, setKeep] = useState(st.keep);
  const [pass, setPass] = useState("");
  const [msg, setMsg] = useState("");
  const [error, setError] = useState("");

  async function save(e: FormEvent) {
    e.preventDefault();
    setError("");
    setMsg("");
    try {
      await api("/backups/settings", {
        method: "PUT",
        body: { schedule, hour, keep, ...(pass ? { passphrase: pass } : {}) },
      });
      setPass("");
      setMsg(translate("Đã lưu"));
      reload();
    } catch (err) {
      setError(errMsg(err));
    }
  }

  return (
    <form onSubmit={save} className="card space-y-4 p-5">
      <h2 className="font-semibold">{translate("Lịch backup và mã hóa")}</h2>
      <div className="modal-grid">
        <div>
          <label className="label">{translate("Tự động")}</label>
          <select
            className="input"
            value={schedule}
            onChange={(e) =>
              setSchedule(e.target.value as Settings["schedule"])
            }
          >
            <option value="off">{translate("Tắt")}</option>
            <option value="daily">{translate("Hằng ngày")}</option>
            <option value="weekly">{translate("Hằng tuần (Chủ nhật)")}</option>
          </select>
        </div>
        <div>
          <label className="label">{translate("Giờ chạy")}</label>
          <select
            className="input"
            value={hour}
            onChange={(e) => setHour(Number(e.target.value))}
          >
            {Array.from({ length: 24 }, (_, h) => (
              <option key={h} value={h}>
                {translate(String(h).padStart(2, "0"))}:00
              </option>
            ))}
          </select>
        </div>
        <div>
          <label className="label">
            {translate("Giữ lại (bản / tài khoản)")}
          </label>
          <input
            className="input"
            type="number"
            min={1}
            max={365}
            value={keep}
            onChange={(e) => setKeep(Number(e.target.value))}
          />
        </div>
        <div>
          <label className="label">
            {translate("Passphrase mã hóa")}{" "}
            {st.passphrase_set && (
              <span className="text-emerald-600">{translate("(đã đặt)")}</span>
            )}
          </label>
          <input
            className="input"
            type="password"
            minLength={12}
            value={pass}
            onChange={(e) => setPass(e.target.value)}
            placeholder={translate(
              st.passphrase_set
                ? translate("Để trống = giữ nguyên")
                : translate("Ít nhất 12 ký tự"),
            )}
          />
        </div>
      </div>
      <p className="text-xs text-amber-700">
        {translate(
          "Mọi bản backup được mã hóa AES-256 bằng passphrase này trước khi tải lên Google Drive. Hãy lưu passphrase ở nơi an toàn: mất passphrase thì không khôi phục được (kể cả khi cài lại máy chủ). Đổi passphrase chỉ áp dụng cho bản backup mới.",
        )}
      </p>
      <ErrorBox msg={error} />
      <div className="flex items-center gap-3">
        <button className="btn-primary">{translate("Lưu")}</button>
        {msg && (
          <span className="text-sm text-emerald-600">{translate(msg)}</span>
        )}
      </div>
    </form>
  );
}

function RestoreDialog({
  b,
  onClose,
  onTask,
}: {
  b: Backup;
  onClose: () => void;
  onTask: (t: Task) => void;
}) {
  const [files, setFiles] = useState(true);
  const [dbs, setDbs] = useState(true);
  const [mail, setMail] = useState(true);
  const [pass, setPass] = useState("");
  const [confirmText, setConfirmText] = useState("");
  const [error, setError] = useState("");

  async function submit(e: FormEvent) {
    e.preventDefault();
    try {
      onTask(
        await api<Task>(`/backups/${b.id}/restore`, {
          method: "POST",
          body: {
            files,
            databases: dbs,
            mail,
            ...(pass ? { passphrase: pass } : {}),
          },
        }),
      );
    } catch (err) {
      setError(errMsg(err));
    }
  }

  return (
    <Modal
      title={translate("Khôi phục {0} — {1}", [
        b.username,
        new Date(b.created_at).toLocaleString(localeTag()),
      ])}
      onClose={onClose}
    >
      <form onSubmit={submit} className="space-y-4">
        <div className="space-y-2 text-sm">
          <label className="flex items-center gap-2">
            <input
              type="checkbox"
              checked={files}
              onChange={(e) => setFiles(e.target.checked)}
            />{" "}
            {translate("File trong thư mục home (website, mã nguồn…)")}
          </label>
          <label className="flex items-center gap-2">
            <input
              type="checkbox"
              checked={dbs}
              onChange={(e) => setDbs(e.target.checked)}
            />{" "}
            Database (
            {translate(
              b.contents.databases?.map((d) => d.name).join(", ") ||
                translate("không có"),
            )}
            )
          </label>
          <label className="flex items-center gap-2">
            <input
              type="checkbox"
              checked={mail}
              onChange={(e) => setMail(e.target.checked)}
            />{" "}
            Email (
            {translate(
              b.contents.mail_domains?.join(", ") || translate("không có"),
            )}
            )
          </label>
        </div>
        <p className="rounded-lg bg-amber-50 px-3 py-2 text-xs text-amber-800">
          {translate(
            "File cùng tên bị ghi đè bằng bản trong backup (file mới hơn không có trong backup được giữ nguyên). Database được thay bằng dữ liệu trong backup; database phải còn tồn tại (tạo lại cùng tên nếu đã xóa).",
          )}
        </p>
        {
          <div>
            <label className="label">
              {translate(
                "Passphrase khác (chỉ khi backup được tạo trước lần đổi passphrase)",
              )}
            </label>
            <input
              className="input"
              type="password"
              value={pass}
              onChange={(e) => setPass(e.target.value)}
            />
          </div>
        }
        <div>
          <label className="label">
            {translate("Gõ")} <b>{b.username}</b> {translate("để xác nhận")}
          </label>
          <input
            className="input"
            value={confirmText}
            onChange={(e) => setConfirmText(e.target.value)}
          />
        </div>
        <ErrorBox msg={error} />
        <div className="flex justify-end gap-2">
          <button type="button" className="btn-ghost" onClick={onClose}>
            {translate("Hủy")}
          </button>
          <button
            className="btn-primary"
            disabled={confirmText !== b.username || (!files && !dbs && !mail)}
          >
            {translate("Khôi phục")}
          </button>
        </div>
      </form>
    </Modal>
  );
}

function RunningTask({ task, onDone }: { task: Task; onDone: () => void }) {
  const t = useTask(task, onDone);
  return t ? <TaskCard task={t} defaultOpen /> : null;
}

export default function BackupsPage() {
  const me = useSession();
  // Each hosting account connects its own Google Drive; managers only see
  // their customers' backups (or configure them via "log in as").
  const hosting = me.role === "user";
  const manager = me.role !== "user";
  const owners = useHostingAccounts(manager);
  const [ownerId, setOwnerId] = useState<number | null>(null);
  const [st, setSt] = useState<Settings | null>(null);
  const [list, setList] = useState<Backup[]>([]);
  const [task, setTask] = useState<Task | null>(null);
  const [restore, setRestore] = useState<Backup | null>(null);
  const [error, setError] = useState("");

  const loadSettings = useCallback(() => {
    if (hosting)
      api<Settings>("/backups/settings")
        .then(setSt)
        .catch((e) => setError(errMsg(e)));
  }, [hosting]);
  const load = useCallback(() => {
    api<Backup[]>("/backups")
      .then(setList)
      .catch((e) => setError(errMsg(e)));
  }, []);
  useEffect(() => {
    loadSettings();
    load();
  }, [loadSettings, load]);
  useEffect(() => {
    if (manager && ownerId === null && owners.length) setOwnerId(owners[0].id);
  }, [manager, owners, ownerId]);

  async function backupNow() {
    setError("");
    try {
      setTask(
        await api<Task>("/backups", {
          method: "POST",
          body: { user_id: manager ? ownerId : me.id },
        }),
      );
    } catch (e) {
      setError(errMsg(e));
    }
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold">{translate("Sao lưu")}</h1>
        <p className="text-sm text-slate-500">
          {translate(
            "Backup mã hóa từng tài khoản (file, database, email) lên Google Drive và khôi phục khi cần.",
          )}
        </p>
      </div>

      {!hosting && (
        <div className="card p-5 text-sm text-slate-600">
          {translate(
            "Mỗi tài khoản hosting tự kết nối Google Drive (OAuth client của chính họ), tự đặt passphrase và lịch backup. Bạn xem và khôi phục được backup của các tài khoản mình quản lý; để cấu hình hộ, dùng",
          )}{" "}
          <b>{translate("Đăng nhập thay")}</b>.
        </div>
      )}
      {hosting && st && <DriveCard st={st} reload={loadSettings} />}
      {hosting && st && <ScheduleCard st={st} reload={loadSettings} />}

      <section className="card space-y-4 p-5">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <h2 className="font-semibold">{translate("Các bản backup")}</h2>
          <div className="flex items-end gap-2">
            {manager && (
              <OwnerSelect
                owners={owners}
                value={ownerId}
                onChange={setOwnerId}
              />
            )}
            <button
              className="btn-primary"
              onClick={backupNow}
              disabled={manager && !ownerId}
            >
              Backup ngay
            </button>
          </div>
        </div>
        <ErrorBox msg={error} />
        {task && <RunningTask task={task} onDone={load} />}
        <table className="w-full text-left text-sm">
          <thead className="border-b border-slate-200 text-xs uppercase text-slate-500">
            <tr>
              <th className="py-2">{translate("Thời gian")}</th>
              {manager && <th className="py-2">{translate("Tài khoản")}</th>}
              <th className="py-2">{translate("Nội dung")}</th>
              <th className="py-2">{translate("Dung lượng")}</th>
              <th className="py-2" />
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100">
            {list.map((b) => (
              <tr key={b.id}>
                <td className="py-2">
                  {translate(
                    new Date(b.created_at).toLocaleString(localeTag()),
                  )}
                  <div className="text-xs text-slate-500">
                    {translate(
                      b.kind === "scheduled"
                        ? translate("tự động")
                        : translate("thủ công"),
                    )}
                  </div>
                </td>
                {manager && (
                  <td className="py-2">
                    {b.username}
                    {b.user_id === null && (
                      <span className="text-xs text-red-600">
                        {" "}
                        {translate("(đã xóa)")}
                      </span>
                    )}
                  </td>
                )}
                <td className="py-2 text-xs text-slate-600">
                  home · {translate(b.contents.databases?.length ?? 0)} database
                  · {translate(b.contents.mail_domains?.length ?? 0)}{" "}
                  {translate("tên miền email")}
                </td>
                <td className="py-2">{translate(fmtBytes(b.size_bytes))}</td>
                <td className="py-2 text-right">
                  <div className="flex justify-end gap-1.5">
                    <button
                      className="btn-ghost px-2.5 py-1 text-xs"
                      onClick={() => setRestore(b)}
                    >
                      {translate("Khôi phục")}
                    </button>
                    <button
                      className="btn-ghost px-2.5 py-1 text-xs text-red-600"
                      onClick={() => {
                        if (
                          confirm(
                            translate("Xóa bản backup {0} khỏi Google Drive?", [
                              b.file_name,
                            ]),
                          )
                        )
                          api(`/backups/${b.id}`, { method: "DELETE" })
                            .then(load)
                            .catch((e) => setError(errMsg(e)));
                      }}
                    >
                      {translate("Xóa")}
                    </button>
                  </div>
                </td>
              </tr>
            ))}
            {list.length === 0 && (
              <tr>
                <td colSpan={5} className="py-8 text-center text-slate-500">
                  {translate("Chưa có bản backup nào.")}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </section>

      {restore && (
        <RestoreDialog
          b={restore}
          onClose={() => setRestore(null)}
          onTask={(t) => {
            setRestore(null);
            setTask(t);
          }}
        />
      )}
    </div>
  );
}
