"use client";

import { FormEvent, useCallback, useEffect, useState } from "react";
import { api, errMsg, genPassword, type FtpAccount } from "@/lib/api";
import { ErrorBox, Modal } from "@/components/ui";
import { OwnerSelect, useHostingAccounts } from "@/components/owner";
import { useSession } from "../session";

function CreateForm({
  manager,
  onDone,
  onCancel,
}: {
  manager: boolean;
  onDone: (a: FtpAccount, password: string) => void;
  onCancel: () => void;
}) {
  const me = useSession();
  const owners = useHostingAccounts(manager);
  const [ownerId, setOwnerId] = useState<number | null>(null);
  const [name, setName] = useState("");
  const [dir, setDir] = useState("www");
  const [password, setPassword] = useState(genPassword);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (manager && ownerId === null && owners.length) setOwnerId(owners[0].id);
  }, [manager, owners, ownerId]);
  const prefix =
    (manager ? owners.find((o) => o.id === ownerId)?.username : me.username) ??
    "user";

  async function submit(e: FormEvent) {
    e.preventDefault();
    setSaving(true);
    setError("");
    try {
      const a = await api<FtpAccount>("/ftp", {
        method: "POST",
        body: {
          user_id: manager ? ownerId : null,
          name,
          dir: dir.replace(/^\/+|\/+$/g, ""),
          password,
        },
      });
      onDone(a, password);
    } catch (err) {
      setError(errMsg(err));
      setSaving(false);
    }
  }

  return (
    <form onSubmit={submit} className="card space-y-4 p-5">
      <h2 className="font-semibold">Tạo tài khoản FTP</h2>
      <div className="modal-grid">
        {manager && (
          <div>
            <label className="label">Tài khoản sở hữu</label>
            <OwnerSelect
              owners={owners}
              value={ownerId}
              onChange={setOwnerId}
            />
          </div>
        )}
        <div>
          <label className="label">Tên đăng nhập</label>
          <div className="flex items-center rounded-lg border border-slate-300 bg-slate-50 text-sm">
            <span className="px-3 text-slate-500">{prefix}_</span>
            <input
              className="input rounded-l-none border-0 border-l"
              required
              pattern="[a-z0-9_]{1,24}"
              value={name}
              onChange={(e) => setName(e.target.value.toLowerCase())}
            />
          </div>
        </div>
        <div>
          <label className="label">Thư mục được phép</label>
          <div className="flex items-center rounded-lg border border-slate-300 bg-slate-50 text-sm">
            <span className="px-3 text-slate-500">~/</span>
            <input
              className="input rounded-l-none border-0 border-l"
              placeholder="www/example.com"
              value={dir}
              onChange={(e) => setDir(e.target.value)}
            />
          </div>
          <p className="mt-1 text-xs text-slate-400">
            Tài khoản FTP chỉ thấy thư mục này.
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
          {saving ? "Đang tạo…" : "Tạo"}
        </button>
        <button type="button" className="btn-ghost" onClick={onCancel}>
          Hủy
        </button>
      </div>
    </form>
  );
}

export default function FTPPage() {
  const me = useSession();
  const manager = me.role !== "user";
  const host = typeof window === "undefined" ? "" : window.location.hostname;
  const [list, setList] = useState<FtpAccount[]>([]);
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<{
    a: FtpAccount;
    password: string;
  } | null>(null);
  const [pwFor, setPwFor] = useState<FtpAccount | null>(null);
  const [newPw, setNewPw] = useState("");
  const [deleting, setDeleting] = useState<FtpAccount | null>(null);
  const [error, setError] = useState("");
  const [dlgError, setDlgError] = useState("");

  const load = useCallback(() => {
    api<FtpAccount[]>("/ftp")
      .then(setList)
      .catch((e) => setError(errMsg(e)));
  }, []);
  useEffect(load, [load]);

  async function savePassword(e: FormEvent) {
    e.preventDefault();
    if (!pwFor) return;
    try {
      await api(`/ftp/${pwFor.id}/password`, {
        method: "PATCH",
        body: { password: newPw },
      });
      setCreated({ a: pwFor, password: newPw });
      setPwFor(null);
    } catch (err) {
      setDlgError(errMsg(err));
    }
  }

  async function confirmDelete() {
    if (!deleting) return;
    try {
      await api(`/ftp/${deleting.id}`, { method: "DELETE" });
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
          <h1 className="text-xl font-semibold">FTP / SFTP</h1>
          <p className="text-sm text-slate-500">
            FTP (cổng 21, hỗ trợ TLS) cho từng thư mục; SFTP (cổng 22) cho cả
            thư mục home.
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
            + Tạo FTP
          </button>
        )}
      </div>

      {me.role === "user" && (
        <div className="card p-4 text-sm text-slate-600">
          <b>SFTP</b> toàn bộ thư mục home: host <code>{host}</code>, cổng{" "}
          <code>22</code>, user <code>{me.username}</code>, mật khẩu = mật khẩu
          đăng nhập panel.
        </div>
      )}

      {creating && (
        <Modal size="wide" title="Tạo FTP" onClose={() => setCreating(false)}>
          <CreateForm
            manager={manager}
            onCancel={() => setCreating(false)}
            onDone={(a, password) => {
              setCreating(false);
              setCreated({ a, password });
              load();
            }}
          />
        </Modal>
      )}
      {created && (
        <div className="card border-emerald-200 bg-emerald-50 p-5 text-sm">
          <div className="mb-3 font-semibold text-emerald-800">
            Thông tin đăng nhập FTP. Hãy lưu lại, mật khẩu sẽ không hiển thị
            lại.
          </div>
          <dl className="grid max-w-xl grid-cols-[8rem_1fr] gap-x-3 gap-y-1.5 font-mono">
            <dt className="font-sans text-slate-500">Host</dt>
            <dd>{host}</dd>
            <dt className="font-sans text-slate-500">Cổng</dt>
            <dd>21 (FTP / FTPS explicit)</dd>
            <dt className="font-sans text-slate-500">User</dt>
            <dd>{created.a.login}</dd>
            <dt className="font-sans text-slate-500">Mật khẩu</dt>
            <dd className="break-all">{created.password}</dd>
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
              <th className="px-4 py-3">Đăng nhập</th>
              <th className="px-4 py-3">Thư mục</th>
              {manager && <th className="px-4 py-3">Tài khoản</th>}
              <th className="px-4 py-3" />
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100">
            {list.map((a) => (
              <tr key={a.id}>
                <td className="px-4 py-3 font-mono font-medium">{a.login}</td>
                <td className="px-4 py-3 font-mono text-slate-600">
                  ~/{a.dir}
                </td>
                {manager && (
                  <td className="px-4 py-3 text-slate-600">{a.username}</td>
                )}
                <td className="px-4 py-3">
                  <div className="flex justify-end gap-1.5">
                    <button
                      className="btn-ghost px-2.5 py-1 text-xs"
                      onClick={() => {
                        setPwFor(a);
                        setNewPw(genPassword());
                        setDlgError("");
                      }}
                    >
                      Mật khẩu
                    </button>
                    <button
                      className="btn-ghost px-2.5 py-1 text-xs text-red-600"
                      onClick={() => setDeleting(a)}
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
                  colSpan={4}
                  className="px-4 py-8 text-center text-slate-500"
                >
                  Chưa có tài khoản FTP nào.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {pwFor && (
        <Modal
          title={`Đổi mật khẩu ${pwFor.login}`}
          onClose={() => setPwFor(null)}
        >
          <form onSubmit={savePassword} className="space-y-4">
            <div className="flex gap-2">
              <input
                className="input font-mono"
                required
                minLength={10}
                value={newPw}
                onChange={(e) => setNewPw(e.target.value)}
              />
              <button
                type="button"
                className="btn-ghost px-3"
                onClick={() => setNewPw(genPassword())}
              >
                ↻
              </button>
            </div>
            <ErrorBox msg={dlgError} />
            <div className="flex justify-end gap-2">
              <button
                type="button"
                className="btn-ghost"
                onClick={() => setPwFor(null)}
              >
                Hủy
              </button>
              <button className="btn-primary">Lưu</button>
            </div>
          </form>
        </Modal>
      )}
      {deleting && (
        <Modal
          title={`Xóa FTP ${deleting.login}`}
          onClose={() => setDeleting(null)}
        >
          <p className="text-sm text-slate-600">
            Chỉ xóa tài khoản đăng nhập; file trong ~/{deleting.dir} được giữ
            lại.
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
