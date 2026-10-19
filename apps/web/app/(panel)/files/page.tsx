"use client";

import { FormEvent, useCallback, useEffect, useRef, useState } from "react";
import { api, errMsg, fmtBytes, joinPath, type FSEntry } from "@/lib/api";
import { ErrorBox, Modal } from "@/components/ui";
import { OwnerSelect, useHostingAccounts } from "@/components/owner";
import { useSession } from "../session";

const archiveRe = /\.(zip|tar|tar\.gz|tgz)$/i;
const editableRe =
  /\.(php|html?|css|js|mjs|ts|json|xml|txt|md|ini|conf|env|htaccess|yml|yaml|sql|log|csv|svg|twig|blade\.php)$|^\.(env|htaccess|user\.ini)$/i;

type Dialog =
  | { kind: "mkdir" | "newfile" }
  | { kind: "rename"; entry: FSEntry }
  | { kind: "chmod"; entry: FSEntry }
  | { kind: "delete"; names: string[] }
  | { kind: "compress"; names: string[] }
  | { kind: "edit"; path: string; content: string };

function Icon({ e }: { e: FSEntry }) {
  if (e.type === "dir") return <span className="text-amber-500">📁</span>;
  if (e.type === "link") return <span className="text-slate-400">🔗</span>;
  return <span className="text-slate-400">📄</span>;
}

export default function FilesPage() {
  const me = useSession();
  const manager = me.role !== "user";
  const owners = useHostingAccounts(manager);
  const [ownerId, setOwnerId] = useState<number | null>(null);
  const [cwd, setCwd] = useState("/www");
  const [entries, setEntries] = useState<FSEntry[] | null>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const [uploads, setUploads] = useState<{ name: string; pct: number }[]>([]);
  const [error, setError] = useState("");
  const fileInput = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (manager && ownerId === null && owners.length > 0)
      setOwnerId(owners[0].id);
  }, [manager, owners, ownerId]);

  const ready = !manager || ownerId !== null;
  const q = manager && ownerId ? `user_id=${ownerId}&` : "";
  const userField = manager ? { user_id: ownerId } : {};

  const load = useCallback(
    (path: string) => {
      if (!ready) return;
      setError("");
      api<{ entries: FSEntry[] }>(`/files?${q}path=${encodeURIComponent(path)}`)
        .then((r) => {
          setEntries(r.entries ?? []);
          setCwd(path);
          setSelected(new Set());
        })
        .catch((e) => {
          setError(errMsg(e));
          if (path !== "/") setEntries([]);
        });
    },
    [q, ready],
  );

  useEffect(() => load("/www"), [load]);

  async function op(request: Record<string, unknown>) {
    return api<{ content?: string }>("/files/op", {
      method: "POST",
      body: { ...userField, request },
    });
  }

  // run executes an operation and returns its error message ("" on success).
  async function run(fn: () => Promise<unknown>): Promise<string> {
    setError("");
    try {
      await fn();
      setDialog(null);
      load(cwd);
      return "";
    } catch (e) {
      setError(errMsg(e));
      return errMsg(e);
    }
  }

  async function openEntry(e: FSEntry) {
    const p = joinPath(cwd, e.name);
    if (e.type === "dir") return load(p);
    if (e.type === "file") {
      try {
        const r = await op({ op: "read", path: p });
        setDialog({ kind: "edit", path: p, content: r.content ?? "" });
      } catch (err) {
        setError(errMsg(err));
      }
    }
  }

  function upload(files: FileList) {
    for (const f of Array.from(files)) {
      const target = joinPath(cwd, f.name);
      const operationID = Date.now() + Math.random();
      const notify = (status: string, progress?: number, error?: string) =>
        window.dispatchEvent(
          new CustomEvent("xpanel:operation", {
            detail: {
              id: operationID,
              path: "/files/upload",
              method: "POST",
              status,
              progress,
              error,
            },
          }),
        );
      notify("running");
      setUploads((u) => [...u, { name: f.name, pct: 0 }]);
      const xhr = new XMLHttpRequest();
      xhr.open(
        "POST",
        `/api/files/upload?${q}path=${encodeURIComponent(target)}`,
      );
      xhr.setRequestHeader("X-XPanel", "1");
      xhr.upload.onprogress = (ev) => {
        const pct = ev.lengthComputable
          ? Math.round((ev.loaded / ev.total) * 100)
          : 0;
        notify("running", pct);
        setUploads((u) =>
          u.map((x) => (x.name === f.name ? { ...x, pct } : x)),
        );
      };
      xhr.onloadend = () => {
        setUploads((u) => u.filter((x) => x.name !== f.name));
        if (xhr.status !== 200) {
          notify(
            "failed",
            undefined,
            `Tải lên ${f.name} thất bại (HTTP ${xhr.status || "mất kết nối"})`,
          );
          try {
            setError(JSON.parse(xhr.responseText).error);
          } catch {
            setError(`Tải lên ${f.name} thất bại`);
          }
        } else {
          notify("done", 100);
        }
        load(cwd);
      };
      xhr.send(f);
    }
  }

  const toggle = (name: string) =>
    setSelected((s) => {
      const n = new Set(s);
      if (n.has(name)) n.delete(name);
      else n.add(name);
      return n;
    });

  const crumbs = cwd.split("/").filter(Boolean);
  const sel = Array.from(selected);

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">Quản lý file</h1>
          <p className="text-sm text-slate-500">
            Thư mục home của tài khoản; thao tác chạy với quyền của chính user
            đó.
          </p>
        </div>
        {manager && (
          <OwnerSelect
            owners={owners}
            value={ownerId}
            onChange={(id) => {
              setOwnerId(id);
              setCwd("/www");
            }}
          />
        )}
      </div>

      <div className="card flex flex-wrap items-center gap-2 p-3">
        <nav className="mr-auto flex flex-wrap items-center gap-1 text-sm">
          <button
            className="rounded px-1.5 py-0.5 hover:bg-slate-100"
            onClick={() => load("/")}
          >
            ~
          </button>
          {crumbs.map((c, i) => (
            <span key={i} className="flex items-center gap-1">
              <span className="text-slate-300">/</span>
              <button
                className="rounded px-1.5 py-0.5 hover:bg-slate-100"
                onClick={() => load("/" + crumbs.slice(0, i + 1).join("/"))}
              >
                {c}
              </button>
            </span>
          ))}
        </nav>
        <button
          className="btn-ghost px-2.5 py-1 text-xs"
          disabled={cwd === "/"}
          onClick={() => load(joinPath(cwd, ".."))}
        >
          ↑ Lên
        </button>
        <button
          className="btn-ghost px-2.5 py-1 text-xs"
          onClick={() => setDialog({ kind: "mkdir" })}
        >
          + Thư mục
        </button>
        <button
          className="btn-ghost px-2.5 py-1 text-xs"
          onClick={() => setDialog({ kind: "newfile" })}
        >
          + File
        </button>
        <button
          className="btn-primary px-2.5 py-1 text-xs"
          onClick={() => fileInput.current?.click()}
        >
          Tải lên
        </button>
        <input
          ref={fileInput}
          type="file"
          multiple
          className="hidden"
          onChange={(e) => {
            if (e.target.files) upload(e.target.files);
            e.target.value = "";
          }}
        />
        {sel.length > 0 && (
          <>
            <button
              className="btn-ghost px-2.5 py-1 text-xs"
              onClick={() => setDialog({ kind: "compress", names: sel })}
            >
              Nén ({sel.length})
            </button>
            <button
              className="btn-ghost px-2.5 py-1 text-xs text-red-600"
              onClick={() => setDialog({ kind: "delete", names: sel })}
            >
              Xóa ({sel.length})
            </button>
          </>
        )}
      </div>

      {uploads.map((u) => (
        <div key={u.name} className="card px-3 py-2 text-sm">
          <div className="flex justify-between">
            <span>{u.name}</span>
            <span>{u.pct}%</span>
          </div>
          <div className="mt-1 h-1.5 rounded-full bg-slate-100">
            <div
              className="h-1.5 rounded-full bg-brand-500"
              style={{ width: `${u.pct}%` }}
            />
          </div>
        </div>
      ))}
      <ErrorBox msg={error} />

      <div className="card overflow-x-auto">
        <table className="w-full text-left text-sm">
          <thead className="border-b border-slate-200 bg-slate-50 text-xs uppercase text-slate-500">
            <tr>
              <th className="w-8 px-3 py-2" />
              <th className="px-3 py-2">Tên</th>
              <th className="px-3 py-2">Kích thước</th>
              <th className="px-3 py-2">Quyền</th>
              <th className="px-3 py-2">Sửa đổi</th>
              <th className="px-3 py-2" />
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100">
            {entries?.map((e) => {
              const p = joinPath(cwd, e.name);
              return (
                <tr
                  key={e.name}
                  className={
                    selected.has(e.name) ? "bg-brand-50" : "hover:bg-slate-50"
                  }
                >
                  <td className="px-3 py-1.5">
                    <input
                      type="checkbox"
                      checked={selected.has(e.name)}
                      onChange={() => toggle(e.name)}
                    />
                  </td>
                  <td className="px-3 py-1.5">
                    <button
                      className="flex items-center gap-2 text-left hover:underline"
                      onClick={() => openEntry(e)}
                      title={
                        e.type === "file" && !editableRe.test(e.name)
                          ? "Mở bằng trình soạn thảo"
                          : undefined
                      }
                    >
                      <Icon e={e} /> <span>{e.name}</span>
                      {e.target && (
                        <span className="text-xs text-slate-400">
                          → {e.target}
                        </span>
                      )}
                    </button>
                  </td>
                  <td className="px-3 py-1.5 text-slate-500">
                    {e.type === "dir" ? "—" : fmtBytes(e.size)}
                  </td>
                  <td className="px-3 py-1.5 font-mono text-xs text-slate-500">
                    {e.mode}
                  </td>
                  <td className="px-3 py-1.5 text-xs text-slate-500">
                    {new Date(e.mtime).toLocaleString("vi-VN")}
                  </td>
                  <td className="px-3 py-1.5">
                    <div className="flex justify-end gap-2 text-xs">
                      {e.type === "file" && (
                        <a
                          className="text-brand-600 hover:underline"
                          href={`/api/files/download?${q}path=${encodeURIComponent(p)}`}
                        >
                          Tải về
                        </a>
                      )}
                      {e.type === "file" && archiveRe.test(e.name) && (
                        <button
                          className="text-brand-600 hover:underline"
                          onClick={() =>
                            run(() => op({ op: "extract", path: p, to: cwd }))
                          }
                        >
                          Giải nén
                        </button>
                      )}
                      <button
                        className="text-slate-600 hover:underline"
                        onClick={() => setDialog({ kind: "rename", entry: e })}
                      >
                        Đổi tên
                      </button>
                      <button
                        className="text-slate-600 hover:underline"
                        onClick={() => setDialog({ kind: "chmod", entry: e })}
                      >
                        Quyền
                      </button>
                    </div>
                  </td>
                </tr>
              );
            })}
            {entries?.length === 0 && (
              <tr>
                <td
                  colSpan={6}
                  className="px-3 py-8 text-center text-slate-500"
                >
                  Thư mục trống.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {dialog && (
        <FileDialog
          dialog={dialog}
          cwd={cwd}
          onClose={() => setDialog(null)}
          run={run}
          op={op}
        />
      )}
    </div>
  );
}

function FileDialog({
  dialog,
  cwd,
  onClose,
  run,
  op,
}: {
  dialog: Dialog;
  cwd: string;
  onClose: () => void;
  run: (fn: () => Promise<unknown>) => Promise<string>;
  op: (req: Record<string, unknown>) => Promise<unknown>;
}) {
  const initial =
    dialog.kind === "rename"
      ? dialog.entry.name
      : dialog.kind === "chmod"
        ? dialog.entry.mode
        : dialog.kind === "compress"
          ? "archive.zip"
          : dialog.kind === "edit"
            ? dialog.content
            : "";
  const [value, setValue] = useState(initial);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setErr("");
    const p = (name: string) => joinPath(cwd, name);
    let msg = "";
    switch (dialog.kind) {
      case "mkdir":
        msg = await run(() => op({ op: "mkdir", path: p(value) }));
        break;
      case "newfile":
        msg = await run(() => op({ op: "write", path: p(value), content: "" }));
        break;
      case "rename":
        msg = await run(() =>
          op({ op: "rename", path: p(dialog.entry.name), to: p(value) }),
        );
        break;
      case "chmod":
        msg = await run(() =>
          op({ op: "chmod", path: p(dialog.entry.name), mode: value }),
        );
        break;
      case "delete":
        msg = await run(() => op({ op: "delete", paths: dialog.names.map(p) }));
        break;
      case "compress":
        msg = await run(() =>
          op({ op: "compress", paths: dialog.names.map(p), to: p(value) }),
        );
        break;
      case "edit":
        msg = await run(() =>
          op({ op: "write", path: dialog.path, content: value }),
        );
        break;
    }
    setErr(msg);
    setBusy(false);
  }

  const titles: Record<Dialog["kind"], string> = {
    mkdir: "Tạo thư mục",
    newfile: "Tạo file",
    rename: "Đổi tên",
    chmod: "Đổi quyền",
    delete: "Xóa",
    compress: "Nén thành .zip",
    edit: dialog.kind === "edit" ? dialog.path : "",
  };

  if (dialog.kind === "edit") {
    return (
      <div className="fixed inset-0 z-50 flex flex-col bg-slate-900/60 p-2 sm:p-6">
        <form
          onSubmit={submit}
          className="card flex min-h-0 flex-1 flex-col overflow-hidden"
        >
          <div className="flex items-center justify-between gap-2 border-b border-slate-200 px-4 py-2">
            <span className="truncate font-mono text-sm">{dialog.path}</span>
            <div className="flex gap-2">
              <button
                type="button"
                className="btn-ghost px-3 py-1 text-xs"
                onClick={onClose}
              >
                Đóng
              </button>
              <button className="btn-primary px-3 py-1 text-xs" disabled={busy}>
                {busy ? "Đang lưu…" : "Lưu"}
              </button>
            </div>
          </div>
          {err && (
            <p className="bg-red-50 px-4 py-2 text-sm text-red-700">{err}</p>
          )}
          <textarea
            className="min-h-0 flex-1 resize-none bg-slate-950 p-4 font-mono text-sm text-slate-100 outline-none"
            spellCheck={false}
            value={value}
            onChange={(e) => setValue(e.target.value)}
            onKeyDown={(e) => {
              if ((e.ctrlKey || e.metaKey) && e.key === "s") {
                e.preventDefault();
                e.currentTarget.form?.requestSubmit();
              }
            }}
          />
        </form>
      </div>
    );
  }

  return (
    <Modal title={titles[dialog.kind]} onClose={onClose}>
      <form onSubmit={submit} className="space-y-4">
        {dialog.kind === "delete" ? (
          <p className="text-sm text-slate-600">
            Xóa vĩnh viễn {dialog.names.length} mục:{" "}
            <b>{dialog.names.join(", ")}</b>?
          </p>
        ) : (
          <input
            className="input font-mono"
            autoFocus
            required
            value={value}
            onChange={(e) => setValue(e.target.value)}
            placeholder={dialog.kind === "chmod" ? "644" : undefined}
            pattern={dialog.kind === "chmod" ? "[0-7]{3}" : undefined}
          />
        )}
        <ErrorBox msg={err} />
        <div className="flex justify-end gap-2">
          <button type="button" className="btn-ghost" onClick={onClose}>
            Hủy
          </button>
          <button
            className={
              dialog.kind === "delete"
                ? "btn bg-red-600 text-white hover:bg-red-700"
                : "btn-primary"
            }
            disabled={busy}
          >
            {dialog.kind === "delete" ? "Xóa" : "OK"}
          </button>
        </div>
      </form>
    </Modal>
  );
}
