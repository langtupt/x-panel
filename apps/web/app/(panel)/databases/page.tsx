"use client";

import { FormEvent, useCallback, useEffect, useState } from "react";
import {
  api,
  errMsg,
  fmtBytes,
  genPassword,
  type Database,
  type PMAStatus,
  type Task,
  type User,
} from "@/lib/api";
import { useTask } from "@/lib/useTask";
import { TaskCard } from "@/components/task";
import { ErrorBox, Modal } from "@/components/ui";
import { useSession } from "../session";

type Created = { db: Database; password: string };

function Copy({ value }: { value: string }) {
  const [done, setDone] = useState(false);
  return (
    <button
      type="button"
      className="text-xs text-brand-600 hover:underline"
      onClick={() =>
        navigator.clipboard.writeText(value).then(() => {
          setDone(true);
          setTimeout(() => setDone(false), 1500);
        })
      }
    >
      {done ? "Đã chép" : "Chép"}
    </button>
  );
}

function CreateForm({
  owners,
  me,
  onCreated,
  onCancel,
}: {
  owners: User[] | null;
  me: User;
  onCreated: (c: Created) => void;
  onCancel: () => void;
}) {
  const [userId, setUserId] = useState<number | null>(owners?.[0]?.id ?? null);
  const [engine, setEngine] = useState("mariadb");
  const [name, setName] = useState("");
  const [password, setPassword] = useState(genPassword);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const prefix =
    (owners ? owners.find((o) => o.id === userId)?.username : me.username) ??
    "user";

  async function submit(e: FormEvent) {
    e.preventDefault();
    setSaving(true);
    setError("");
    try {
      const db = await api<Database>("/databases", {
        method: "POST",
        body: { user_id: userId, name, password, engine },
      });
      onCreated({ db, password });
    } catch (err) {
      setError(errMsg(err));
      setSaving(false);
    }
  }

  return (
    <form onSubmit={submit} className="card space-y-4 p-5">
      <h2 className="font-semibold">Tạo database</h2>
      <label className="label">Engine</label>
      <select
        className="input"
        value={engine}
        onChange={(e) => setEngine(e.target.value)}
      >
        <option value="mariadb">MariaDB</option>
        <option value="postgresql">PostgreSQL (cần cài từ Applications)</option>
      </select>
      <div className="modal-grid">
        {owners && (
          <div>
            <label className="label">Tài khoản sở hữu</label>
            <select
              className="input"
              required
              value={userId ?? ""}
              onChange={(e) => setUserId(Number(e.target.value) || null)}
            >
              {owners.length === 0 && (
                <option value="">Chưa có tài khoản người dùng nào</option>
              )}
              {owners.map((u) => (
                <option key={u.id} value={u.id}>
                  {u.username}
                </option>
              ))}
            </select>
          </div>
        )}
        <div>
          <label className="label">Tên database</label>
          <div className="flex items-center rounded-lg border border-slate-300 bg-slate-50 text-sm">
            <span className="px-3 text-slate-500">{prefix}_</span>
            <input
              className="input rounded-l-none border-0 border-l"
              required
              pattern="[a-z0-9_]{1,40}"
              title="Chỉ a-z, 0-9, _"
              value={name}
              onChange={(e) => setName(e.target.value.toLowerCase())}
            />
          </div>
          <p className="mt-1 text-xs text-slate-400">
            User database trùng tên với database.
          </p>
        </div>
        <div>
          <label className="label">Mật khẩu</label>
          <div className="flex items-center gap-2 [&>input]:flex-1 [&>button]:shrink-0">
            <input
              className="input font-mono"
              required
              minLength={10}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
            <button
              type="button"
              className="btn-ghost px-3"
              title="Tạo mật khẩu mới"
              onClick={() => setPassword(genPassword())}
            >
              ↻
            </button>
          </div>
        </div>
      </div>
      <ErrorBox msg={error} />
      <div className="modal-actions">
        <button className="btn-primary" disabled={saving}>
          {saving ? "Đang tạo…" : "Tạo database"}
        </button>
        <button type="button" className="btn-ghost" onClick={onCancel}>
          Hủy
        </button>
      </div>
    </form>
  );
}

function PasswordDialog({
  db,
  onClose,
}: {
  db: Database;
  onClose: () => void;
}) {
  const [password, setPassword] = useState(genPassword);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setError("");
    try {
      await api(`/databases/${db.id}/password`, {
        method: "PATCH",
        body: { password },
      });
      setSaved(true);
    } catch (err) {
      setError(errMsg(err));
    }
  }

  return (
    <Modal title={`Đổi mật khẩu ${db.db_user}`} onClose={onClose}>
      <form onSubmit={submit} className="space-y-4">
        <div className="flex items-center gap-2 [&>input]:flex-1 [&>button]:shrink-0">
          <input
            className="input font-mono"
            required
            minLength={10}
            value={password}
            readOnly={saved}
            onChange={(e) => setPassword(e.target.value)}
          />
          {!saved && (
            <button
              type="button"
              className="btn-ghost px-3"
              onClick={() => setPassword(genPassword())}
            >
              ↻
            </button>
          )}
        </div>
        {saved && (
          <p className="text-sm text-emerald-700">
            Đã đổi mật khẩu. Nhớ cập nhật cấu hình website.{" "}
            <Copy value={password} />
          </p>
        )}
        <ErrorBox msg={error} />
        <div className="modal-actions">
          <button type="button" className="btn-ghost" onClick={onClose}>
            {saved ? "Đóng" : "Hủy"}
          </button>
          {!saved && <button className="btn-primary">Lưu</button>}
        </div>
      </form>
    </Modal>
  );
}

function PhpMyAdmin({ admin }: { admin: boolean }) {
  const [st, setSt] = useState<PMAStatus | null>(null);
  const [task, setTask] = useState<Task | null>(null);
  const [error, setError] = useState("");
  const refresh = useCallback(() => {
    api<PMAStatus>("/phpmyadmin")
      .then(setSt)
      .catch(() => setSt(null));
  }, []);
  useEffect(refresh, [refresh]);
  const t = useTask(task, refresh);

  async function install() {
    setError("");
    try {
      setTask(await api<Task>("/phpmyadmin", { method: "POST" }));
    } catch (e) {
      setError(errMsg(e));
    }
  }

  if (!st) return null;
  return (
    <div className="card flex flex-wrap items-center justify-between gap-3 p-4 text-sm">
      <div>
        <b>phpMyAdmin</b>{" "}
        {st.installed ? (
          <span className="text-slate-500">
            đăng nhập bằng user + mật khẩu database (PHP {st.php_version})
          </span>
        ) : (
          <span className="text-slate-500">
            chưa được cài{admin ? "" : " (liên hệ quản trị viên)"}
          </span>
        )}
      </div>
      {st.installed ? (
        <a
          className="btn-primary px-3 py-1.5 text-xs"
          href="/phpmyadmin/"
          target="_blank"
          rel="noreferrer"
        >
          Mở phpMyAdmin
        </a>
      ) : (
        admin &&
        !t && (
          <button className="btn-primary px-3 py-1.5 text-xs" onClick={install}>
            Cài phpMyAdmin
          </button>
        )
      )}
      {t && (
        <div className="w-full">
          <TaskCard task={t} defaultOpen={t.status === "failed"} />
        </div>
      )}
      {error && (
        <div className="w-full">
          <ErrorBox msg={error} />
        </div>
      )}
    </div>
  );
}

export default function DatabasesPage() {
  const me = useSession();
  const manager = me.role !== "user";
  const [list, setList] = useState<Database[]>([]);
  const [owners, setOwners] = useState<User[]>([]);
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<Created | null>(null);
  const [pwFor, setPwFor] = useState<Database | null>(null);
  const [deleting, setDeleting] = useState<Database | null>(null);
  const [error, setError] = useState("");

  const load = useCallback(() => {
    api<Database[]>("/databases")
      .then(setList)
      .catch((e) => setError(errMsg(e)));
  }, []);

  useEffect(() => {
    load();
    if (manager) {
      api<{ users: User[] }>("/users")
        .then((r) =>
          setOwners(
            r.users.filter((u) => u.role === "user" && u.linux_uid !== null),
          ),
        )
        .catch((e) => setError(errMsg(e)));
    }
  }, [load, manager]);

  async function confirmDelete() {
    if (!deleting) return;
    try {
      await api(`/databases/${deleting.id}`, { method: "DELETE" });
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
          <h1 className="text-xl font-semibold">Database</h1>
          <p className="text-sm text-slate-500">
            Kết nối nội bộ: MariaDB localhost:3306; PostgreSQL 127.0.0.1:5433.
          </p>
        </div>
        {!creating && (
          <button
            className="btn-primary"
            onClick={() => {
              setCreating(true);
              setCreated(null);
            }}
          >
            + Tạo database
          </button>
        )}
      </div>

      <PhpMyAdmin admin={me.role === "admin"} />

      {creating && (
        <Modal size="wide" title="Tạo database" onClose={() => setCreating(false)}>
          <CreateForm
            owners={manager ? owners : null}
            me={me}
            onCancel={() => setCreating(false)}
            onCreated={(c) => {
              setCreating(false);
              setCreated(c);
              load();
            }}
          />
        </Modal>
      )}

      {created && (
        <div className="card border-emerald-200 bg-emerald-50 p-5 text-sm">
          <div className="mb-3 font-semibold text-emerald-800">
            Đã tạo database. Hãy lưu mật khẩu, nó sẽ không hiển thị lại.
          </div>
          <dl className="grid max-w-xl grid-cols-[8rem_1fr_auto] gap-x-3 gap-y-1.5 font-mono">
            <dt className="font-sans text-slate-500">Host</dt>
            <dd>
              {created.db.engine === "postgresql"
                ? "127.0.0.1:5433"
                : "localhost:3306"}
            </dd>
            <span />
            <dt className="font-sans text-slate-500">Database</dt>
            <dd>{created.db.name}</dd>
            <Copy value={created.db.name} />
            <dt className="font-sans text-slate-500">User</dt>
            <dd>{created.db.db_user}</dd>
            <Copy value={created.db.db_user} />
            <dt className="font-sans text-slate-500">Mật khẩu</dt>
            <dd className="break-all">{created.password}</dd>
            <Copy value={created.password} />
          </dl>
          <button
            className="btn-ghost mt-4 px-3 py-1 text-xs"
            onClick={() => setCreated(null)}
          >
            Đã lưu, đóng
          </button>
        </div>
      )}
      <ErrorBox msg={error} />

      <div className="card overflow-x-auto">
        <table className="w-full text-left text-sm">
          <thead className="border-b border-slate-200 bg-slate-50 text-xs uppercase text-slate-500">
            <tr>
              <th className="px-4 py-3">Database</th>
              <th className="px-4 py-3">User</th>
              {manager && <th className="px-4 py-3">Tài khoản</th>}
              <th className="px-4 py-3">Dung lượng</th>
              <th className="px-4 py-3" />
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100">
            {list.map((d) => (
              <tr key={d.id}>
                <td className="px-4 py-3 font-mono font-medium">
                  {d.name}
                  <div className="text-xs font-sans text-slate-500">
                    {d.engine === "postgresql"
                      ? "PostgreSQL · 127.0.0.1:5433"
                      : "MariaDB · localhost:3306"}
                  </div>
                </td>
                <td className="px-4 py-3 font-mono text-slate-600">
                  {d.db_user}
                </td>
                {manager && (
                  <td className="px-4 py-3 text-slate-600">{d.username}</td>
                )}
                <td className="px-4 py-3 text-slate-600">
                  {d.size_bytes === null ? "—" : fmtBytes(d.size_bytes)}
                </td>
                <td className="px-4 py-3">
                  <div className="flex justify-end gap-1.5">
                    <a
                      className="btn-ghost px-2.5 py-1 text-xs"
                      target="_blank"
                      rel="noreferrer"
                      href={
                        d.engine === "postgresql" ? "/adminer/" : "/phpmyadmin/"
                      }
                    >
                      {d.engine === "postgresql" ? "Adminer" : "phpMyAdmin"}
                    </a>
                    <button
                      className="btn-ghost px-2.5 py-1 text-xs"
                      onClick={() => setPwFor(d)}
                    >
                      Mật khẩu
                    </button>
                    <button
                      className="btn-ghost px-2.5 py-1 text-xs text-red-600"
                      onClick={() => setDeleting(d)}
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
                  Chưa có database nào.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {pwFor && <PasswordDialog db={pwFor} onClose={() => setPwFor(null)} />}
      {deleting && (
        <Modal
          title={`Xóa database ${deleting.name}`}
          onClose={() => setDeleting(null)}
        >
          <p className="text-sm text-slate-600">
            Toàn bộ dữ liệu trong database và user <b>{deleting.db_user}</b> sẽ
            bị xóa vĩnh viễn.
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
