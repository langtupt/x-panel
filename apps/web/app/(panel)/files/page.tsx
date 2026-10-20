"use client";
import { localeTag } from "@/lib/i18n";
import { t as translate } from "@/lib/i18n";

import {
  DragEvent,
  FormEvent,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import dynamic from "next/dynamic";
import { api, errMsg, fmtBytes, joinPath, type FSEntry } from "@/lib/api";
import { ErrorBox, Modal } from "@/components/ui";
import { OwnerSelect, useHostingAccounts } from "@/components/owner";
import { useSession } from "../session";

// The editor (CodeMirror + language grammars) loads only when a file is opened.
const CodeEditor = dynamic(() => import("@/components/code-editor"), {
  ssr: false,
  loading: () => (
    <div className="p-6 text-sm text-slate-400">
      {translate("Đang tải trình soạn thảo…")}
    </div>
  ),
});

const archiveRe = /\.(zip|tar|tar\.gz|tgz|tar\.bz2|tbz2)$/i;
const binaryRe =
  /\.(png|jpe?g|gif|webp|ico|bmp|pdf|zip|gz|tgz|bz2|tar|rar|7z|mp[34]|mov|avi|woff2?|ttf|otf|eot|exe|so|bin|iso|sqlite|db)$/i;

type OpFn = (
  req: Record<string, unknown>,
) => Promise<{ content?: string; entries?: FSEntry[] }>;

type Dialog =
  | { kind: "mkdir" | "newfile" }
  | { kind: "rename"; entry: FSEntry }
  | { kind: "chmod"; entry: FSEntry }
  | { kind: "delete"; names: string[] }
  | { kind: "compress"; names: string[] }
  | { kind: "move" | "copy"; names: string[] }
  | { kind: "extract"; entry: FSEntry };

function Icon({ e }: { e: FSEntry }) {
  if (e.type === "dir") return <span className="text-amber-500">📁</span>;
  if (e.type === "link") return <span className="text-slate-400">🔗</span>;
  if (archiveRe.test(e.name)) return <span>🗜️</span>;
  return <span className="text-slate-400">📄</span>;
}

type MenuItem =
  | {
      label: string;
      onClick: () => void;
      danger?: boolean;
      hidden?: boolean;
      hint?: string;
    }
  | "sep";
type MenuState = { x: number; y: number; items: MenuItem[] };

/** Floating context menu at the pointer, kept inside the window. */
function ContextMenu({
  x,
  y,
  items,
  onClose,
}: MenuState & { onClose: () => void }) {
  const ref = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState({ left: x, top: y });
  useEffect(() => {
    const el = ref.current;
    if (el) {
      const r = el.getBoundingClientRect();
      setPos({
        left: Math.max(4, Math.min(x, window.innerWidth - r.width - 4)),
        top: Math.max(4, Math.min(y, window.innerHeight - r.height - 4)),
      });
    }
    const close = (ev: Event) => {
      if (ev instanceof KeyboardEvent && ev.key !== "Escape") return;
      if (ev instanceof MouseEvent && ref.current?.contains(ev.target as Node))
        return;
      onClose();
    };
    document.addEventListener("mousedown", close);
    document.addEventListener("keydown", close);
    window.addEventListener("scroll", onClose, true);
    window.addEventListener("resize", onClose);
    return () => {
      document.removeEventListener("mousedown", close);
      document.removeEventListener("keydown", close);
      window.removeEventListener("scroll", onClose, true);
      window.removeEventListener("resize", onClose);
    };
  }, [x, y, onClose]);
  const visible = items.filter((i) => i === "sep" || !i.hidden);
  return (
    <div
      ref={ref}
      style={pos}
      onContextMenu={(e) => e.preventDefault()}
      className="fixed z-[60] min-w-52 rounded-lg border border-slate-200 bg-white py-1 text-sm shadow-xl"
    >
      {visible.map((i, n) =>
        i === "sep" ? (
          n > 0 && n < visible.length - 1 && visible[n - 1] !== "sep" ? (
            <div key={n} className="my-1 border-t border-slate-100" />
          ) : null
        ) : (
          <button
            key={i.label}
            className={`flex w-full items-center justify-between gap-6 px-3 py-1.5 text-left hover:bg-slate-100 ${i.danger ? "text-red-600" : ""}`}
            onClick={() => {
              onClose();
              i.onClick();
            }}
          >
            <span>{translate(i.label)}</span>
            {i.hint && (
              <span className="text-xs text-slate-400">
                {translate(i.hint)}
              </span>
            )}
          </button>
        ),
      )}
    </div>
  );
}

/** Folder browser used to choose a destination. */
function FolderPicker({
  op,
  start,
  value,
  onChange,
}: {
  op: OpFn;
  start: string;
  value: string;
  onChange: (p: string) => void;
}) {
  const [dir, setDir] = useState(start);
  const [dirs, setDirs] = useState<string[]>([]);
  useEffect(() => {
    op({ op: "list", path: dir })
      .then((r) =>
        setDirs(
          (r.entries ?? []).filter((e) => e.type === "dir").map((e) => e.name),
        ),
      )
      .catch(() => setDirs([]));
    onChange(dir);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dir]);
  return (
    <div className="space-y-2">
      <input
        className="input font-mono text-sm"
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
      <div className="max-h-56 overflow-y-auto rounded-lg border border-slate-200 text-sm">
        {dir !== "/" && (
          <button
            type="button"
            className="block w-full px-3 py-1.5 text-left hover:bg-slate-50"
            onClick={() => setDir(joinPath(dir, ".."))}
          >
            ↑ ..
          </button>
        )}
        {dirs.map((d) => (
          <button
            type="button"
            key={d}
            className="block w-full px-3 py-1.5 text-left hover:bg-slate-50"
            onClick={() => setDir(joinPath(dir, d))}
          >
            📁 {translate(d)}
          </button>
        ))}
        {dirs.length === 0 && (
          <div className="px-3 py-2 text-slate-400">
            {translate("Không có thư mục con")}
          </div>
        )}
      </div>
    </div>
  );
}

function Editor({
  path,
  initial,
  op,
  onClose,
}: {
  path: string;
  initial: string;
  op: OpFn;
  onClose: () => void;
}) {
  const [value, setValue] = useState(initial);
  const [saved, setSaved] = useState(initial);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const dirty = value !== saved;

  async function save() {
    setBusy(true);
    setErr("");
    try {
      await op({ op: "write", path, content: value });
      setSaved(value);
    } catch (e) {
      setErr(errMsg(e));
    }
    setBusy(false);
  }
  function close() {
    if (
      !dirty ||
      confirm(translate("Có thay đổi chưa lưu. Đóng mà không lưu?"))
    )
      onClose();
  }

  return (
    <div className="fixed inset-0 z-50 flex flex-col bg-slate-900/60 p-2 sm:p-6">
      <div className="card flex min-h-0 flex-1 flex-col overflow-hidden">
        <div className="flex items-center justify-between gap-2 border-b border-slate-200 px-4 py-2">
          <span className="truncate font-mono text-sm">
            {translate(path)}
            {dirty && (
              <span className="ml-2 text-amber-600">
                {translate("● chưa lưu")}
              </span>
            )}
          </span>
          <div className="flex gap-2">
            <span className="hidden self-center text-xs text-slate-400 sm:block">
              {translate("Ctrl+S lưu · Ctrl+F tìm")}
            </span>
            <button
              type="button"
              className="btn-ghost px-3 py-1 text-xs"
              onClick={close}
            >
              {translate("Đóng")}
            </button>
            <button
              type="button"
              className="btn-primary px-3 py-1 text-xs"
              disabled={busy || !dirty}
              onClick={save}
            >
              {translate(busy ? translate("Đang lưu…") : translate("Lưu"))}
            </button>
          </div>
        </div>
        {err && (
          <p className="bg-red-50 px-4 py-2 text-sm text-red-700">
            {translate(err)}
          </p>
        )}
        <div className="min-h-0 flex-1 overflow-hidden">
          <CodeEditor
            path={path}
            value={value}
            onChange={setValue}
            onSave={save}
          />
        </div>
      </div>
    </div>
  );
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
  const [editing, setEditing] = useState<{
    path: string;
    content: string;
  } | null>(null);
  const [uploads, setUploads] = useState<{ name: string; pct: number }[]>([]);
  const [uploaded, setUploaded] = useState<string[]>([]); // archives just uploaded here
  const [drag, setDrag] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [menu, setMenu] = useState<MenuState | null>(null);
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
          if (path !== cwd) setUploaded([]);
          setCwd(path);
          setSelected(new Set());
        })
        .catch((e) => {
          setError(errMsg(e));
          if (path !== "/") setEntries([]);
        });
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [q, ready],
  );

  useEffect(() => load("/www"), [load]);

  const op: OpFn = (request) =>
    api<{ content?: string; entries?: FSEntry[] }>("/files/op", {
      method: "POST",
      body: { ...userField, request },
    });

  // run executes an operation and returns its error message ("" on success).
  async function run(
    fn: () => Promise<unknown>,
    done?: string,
  ): Promise<string> {
    setError("");
    setNotice("");
    try {
      await fn();
      setDialog(null);
      load(cwd);
      if (done) setNotice(done);
      return "";
    } catch (e) {
      setError(errMsg(e));
      return errMsg(e);
    }
  }

  async function edit(e: FSEntry) {
    const p = joinPath(cwd, e.name);
    try {
      const r = await op({ op: "read", path: p });
      setEditing({ path: p, content: r.content ?? "" });
    } catch (err) {
      setError(errMsg(err));
    }
  }

  function openEntry(e: FSEntry) {
    if (e.type === "dir") return load(joinPath(cwd, e.name));
    if (e.type === "file" && archiveRe.test(e.name))
      return setDialog({ kind: "extract", entry: e });
    if (e.type === "file" && !binaryRe.test(e.name)) return edit(e);
    if (e.type === "file") download(e.name); // images, archives… are downloaded
  }

  function upload(files: FileList | File[]) {
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
            translate("Tải lên {0} thất bại (HTTP {1})", [
              f.name,
              xhr.status || "mất kết nối",
            ]),
          );
          try {
            setError(JSON.parse(xhr.responseText).error);
          } catch {
            setError(translate("Tải lên {0} thất bại", [f.name]));
          }
        } else {
          notify("done", 100);
          if (archiveRe.test(f.name))
            setUploaded((u) => [...u.filter((n) => n !== f.name), f.name]);
        }
        load(cwd);
      };
      xhr.send(f);
    }
  }

  function onDrop(e: DragEvent) {
    e.preventDefault();
    setDrag(false);
    if (e.dataTransfer.files.length) upload(e.dataTransfer.files);
  }

  const toggle = (name: string) =>
    setSelected((s) => {
      const n = new Set(s);
      if (n.has(name)) n.delete(name);
      else n.add(name);
      return n;
    });
  const allSelected = !!entries?.length && selected.size === entries.length;

  const crumbs = cwd.split("/").filter(Boolean);
  const sel = Array.from(selected);
  const download = (name: string) =>
    (window.location.href = `/api/files/download?${q}path=${encodeURIComponent(joinPath(cwd, name))}`);

  // A click opens (folder: enter it, file: edit, archive: extract); only the
  // checkboxes select. Right-click acts on the selection when the row is
  // part of it, otherwise on that row alone, without changing the selection.
  function rowMenu(ev: React.MouseEvent, e: FSEntry) {
    ev.preventDefault();
    const names = selected.has(e.name) ? sel : [e.name];
    const one = names.length === 1;
    const isFile = one && e.type === "file";
    const editable = isFile && !binaryRe.test(e.name);
    const archive = isFile && archiveRe.test(e.name);
    setMenu({
      x: ev.clientX,
      y: ev.clientY,
      items: [
        {
          label: e.type === "dir" ? translate("Mở thư mục") : translate("Sửa"),
          hint: "Enter",
          hidden: !one || (!editable && e.type !== "dir"),
          onClick: () => openEntry(e),
        },
        {
          label: translate("Giải nén…"),
          hidden: !archive,
          onClick: () => setDialog({ kind: "extract", entry: e }),
        },
        {
          label: translate("Tải về"),
          hidden: !isFile,
          onClick: () => download(e.name),
        },
        "sep",
        {
          label: one
            ? translate("Sao chép tới…")
            : translate("Sao chép {0} mục tới…", [names.length]),
          onClick: () => setDialog({ kind: "copy", names }),
        },
        {
          label: one
            ? translate("Di chuyển tới…")
            : translate("Di chuyển {0} mục tới…", [names.length]),
          onClick: () => setDialog({ kind: "move", names }),
        },
        {
          label: one
            ? translate("Nén…")
            : translate("Nén {0} mục…", [names.length]),
          onClick: () => setDialog({ kind: "compress", names }),
        },
        "sep",
        {
          label: translate("Đổi tên"),
          hint: "F2",
          hidden: !one,
          onClick: () => setDialog({ kind: "rename", entry: e }),
        },
        {
          label: translate("Phân quyền (chmod)"),
          hidden: !one,
          onClick: () => setDialog({ kind: "chmod", entry: e }),
        },
        "sep",
        {
          label: one
            ? translate("Xóa")
            : translate("Xóa {0} mục", [names.length]),
          hint: "Del",
          danger: true,
          onClick: () => setDialog({ kind: "delete", names }),
        },
      ],
    });
  }

  function backgroundMenu(ev: React.MouseEvent) {
    ev.preventDefault();
    setMenu({
      x: ev.clientX,
      y: ev.clientY,
      items: [
        {
          label: translate("Tạo thư mục"),
          onClick: () => setDialog({ kind: "mkdir" }),
        },
        {
          label: translate("Tạo file"),
          onClick: () => setDialog({ kind: "newfile" }),
        },
        {
          label: translate("Tải lên…"),
          onClick: () => fileInput.current?.click(),
        },
        "sep",
        { label: translate("Làm mới"), onClick: () => load(cwd) },
        {
          label: translate("Chọn tất cả"),
          hint: "Ctrl+A",
          onClick: () => setSelected(new Set(entries?.map((x) => x.name))),
        },
      ],
    });
  }

  // Keyboard shortcuts while no dialog or editor is open.
  useEffect(() => {
    const onKey = (ev: KeyboardEvent) => {
      if (dialog || editing || menu) return;
      const t = ev.target as HTMLElement;
      if (t.closest("input, textarea, select, [contenteditable]")) return;
      const names = Array.from(selected);
      const only =
        names.length === 1
          ? entries?.find((x) => x.name === names[0])
          : undefined;
      if (ev.key === "Delete" && names.length)
        setDialog({ kind: "delete", names });
      else if (ev.key === "F2" && only)
        setDialog({ kind: "rename", entry: only });
      else if (ev.key === "Enter" && only) openEntry(only);
      else if (ev.key === "Escape") setSelected(new Set());
      else if ((ev.ctrlKey || ev.metaKey) && ev.key.toLowerCase() === "a")
        setSelected(new Set(entries?.map((x) => x.name)));
      else return;
      ev.preventDefault();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">{translate("Quản lý file")}</h1>
          <p className="text-sm text-slate-500">
            {translate(
              "Thư mục home của tài khoản; mọi thao tác chạy với quyền của chính user đó.",
            )}
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
                {translate(c)}
              </button>
            </span>
          ))}
        </nav>
        <button
          className="btn-ghost px-2.5 py-1 text-xs"
          disabled={cwd === "/"}
          onClick={() => load(joinPath(cwd, ".."))}
        >
          {translate("↑ Lên")}
        </button>
        <button
          className="btn-ghost px-2.5 py-1 text-xs"
          onClick={() => load(cwd)}
        >
          ↻
        </button>
        <button
          className="btn-ghost px-2.5 py-1 text-xs"
          onClick={() => setDialog({ kind: "mkdir" })}
        >
          {translate("+ Thư mục")}
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
          {translate("Tải lên")}
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
      </div>

      {sel.length > 0 && (
        <div className="card flex flex-wrap items-center gap-2 bg-brand-50/60 p-2 text-xs">
          <span className="px-2 font-medium">
            {translate("Đã chọn")} {translate(sel.length)}
          </span>
          <button
            className="btn-ghost px-2.5 py-1 text-xs"
            onClick={() => setDialog({ kind: "copy", names: sel })}
          >
            {translate("Sao chép")}
          </button>
          <button
            className="btn-ghost px-2.5 py-1 text-xs"
            onClick={() => setDialog({ kind: "move", names: sel })}
          >
            {translate("Di chuyển")}
          </button>
          <button
            className="btn-ghost px-2.5 py-1 text-xs"
            onClick={() => setDialog({ kind: "compress", names: sel })}
          >
            {translate("Nén")}
          </button>
          <button
            className="btn-ghost px-2.5 py-1 text-xs text-red-600"
            onClick={() => setDialog({ kind: "delete", names: sel })}
          >
            {translate("Xóa")}
          </button>
          <button
            className="ml-auto px-2 text-slate-500 hover:underline"
            onClick={() => setSelected(new Set())}
          >
            {translate("Bỏ chọn")}
          </button>
        </div>
      )}

      {uploads.map((u) => (
        <div key={u.name} className="card px-3 py-2 text-sm">
          <div className="flex justify-between">
            <span>{u.name}</span>
            <span>{translate(u.pct)}%</span>
          </div>
          <div className="mt-1 h-1.5 rounded-full bg-slate-100">
            <div
              className="h-1.5 rounded-full bg-brand-500"
              style={{ width: `${u.pct}%` }}
            />
          </div>
        </div>
      ))}
      {uploaded.map((name) => (
        <div
          key={name}
          className="flex flex-wrap items-center gap-2 rounded-lg bg-sky-50 px-3 py-2 text-sm text-sky-900"
        >
          {translate("Đã tải lên")} <b>{translate(name)}</b>.
          <button
            className="btn-primary px-2.5 py-1 text-xs"
            onClick={() => {
              const e = entries?.find((x) => x.name === name);
              if (e) setDialog({ kind: "extract", entry: e });
            }}
          >
            {translate("Giải nén")}
          </button>
          <button
            className="text-xs text-slate-500 hover:underline"
            onClick={() => setUploaded((u) => u.filter((n) => n !== name))}
          >
            {translate("Bỏ qua")}
          </button>
        </div>
      ))}
      <ErrorBox msg={error} />
      {notice && (
        <p className="rounded-lg bg-emerald-50 px-3 py-2 text-sm text-emerald-800">
          {translate(notice)}
        </p>
      )}

      <div
        className={`card relative overflow-x-auto ${drag ? "ring-2 ring-brand-500" : ""}`}
        onDragOver={(e) => {
          e.preventDefault();
          setDrag(true);
        }}
        onDragLeave={() => setDrag(false)}
        onDrop={onDrop}
      >
        {drag && (
          <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center bg-brand-50/80 text-sm font-medium text-brand-700">
            {translate("Thả file vào đây để tải lên")} {translate(cwd)}
          </div>
        )}
        <table className="w-full text-left text-sm">
          <thead className="border-b border-slate-200 bg-slate-50 text-xs uppercase text-slate-500">
            <tr>
              <th className="w-8 px-3 py-2">
                <input
                  type="checkbox"
                  checked={allSelected}
                  onChange={() =>
                    setSelected(
                      allSelected
                        ? new Set()
                        : new Set(entries?.map((e) => e.name)),
                    )
                  }
                />
              </th>
              <th className="px-3 py-2">{translate("Tên")}</th>
              <th className="px-3 py-2">{translate("Kích thước")}</th>
              <th className="px-3 py-2">{translate("Quyền")}</th>
              <th className="px-3 py-2">{translate("Sửa đổi")}</th>
              <th className="px-3 py-2" />
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100">
            {entries?.map((e) => (
              <tr
                key={e.name}
                className={`cursor-pointer select-none ${selected.has(e.name) ? "bg-brand-50" : "hover:bg-slate-50"}`}
                onClick={() => openEntry(e)}
                onContextMenu={(ev) => rowMenu(ev, e)}
              >
                <td
                  className="px-3 py-1.5"
                  onClick={(ev) => ev.stopPropagation()}
                >
                  <input
                    type="checkbox"
                    checked={selected.has(e.name)}
                    onChange={() => toggle(e.name)}
                  />
                </td>
                <td className="px-3 py-1.5">
                  <span className="flex items-center gap-2">
                    <Icon e={e} /> <span>{e.name}</span>
                    {e.target && (
                      <span className="text-xs text-slate-400">
                        → {translate(e.target)}
                      </span>
                    )}
                  </span>
                </td>
                <td className="px-3 py-1.5 text-slate-500">
                  {translate(e.type === "dir" ? "—" : fmtBytes(e.size))}
                </td>
                <td className="px-3 py-1.5 font-mono text-xs text-slate-500">
                  {translate(e.mode)}
                </td>
                <td className="px-3 py-1.5 text-xs text-slate-500">
                  {translate(new Date(e.mtime).toLocaleString(localeTag()))}
                </td>
                <td className="w-10 px-2 py-1.5 text-right">
                  <button
                    className="rounded px-2 py-0.5 text-slate-400 hover:bg-slate-200 hover:text-slate-700"
                    aria-label={translate("Thao tác")}
                    onClick={(ev) => {
                      ev.stopPropagation();
                      rowMenu(ev, e);
                    }}
                  >
                    ⋯
                  </button>
                </td>
              </tr>
            ))}
            {entries?.length === 0 && (
              <tr>
                <td
                  colSpan={6}
                  className="px-3 py-8 text-center text-slate-500"
                >
                  {translate("Thư mục trống. Kéo thả file vào đây để tải lên.")}
                </td>
              </tr>
            )}
          </tbody>
        </table>
        <div className="min-h-16" onContextMenu={backgroundMenu} />
      </div>
      <p className="text-xs text-slate-400">
        {translate(
          "Bấm để mở thư mục / sửa file · tích ô để chọn nhiều · chuột phải để xem thao tác · Delete xóa · F2 đổi tên · Ctrl+A chọn tất cả",
        )}
      </p>
      {menu && <ContextMenu {...menu} onClose={() => setMenu(null)} />}

      {dialog && (
        <FileDialog
          dialog={dialog}
          cwd={cwd}
          onClose={() => setDialog(null)}
          run={run}
          op={op}
        />
      )}
      {editing && (
        <Editor
          path={editing.path}
          initial={editing.content}
          op={op}
          onClose={() => {
            setEditing(null);
            load(cwd);
          }}
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
  run: (fn: () => Promise<unknown>, done?: string) => Promise<string>;
  op: OpFn;
}) {
  const archiveBase =
    dialog.kind === "extract" ? dialog.entry.name.replace(archiveRe, "") : "";
  const initial =
    dialog.kind === "rename"
      ? dialog.entry.name
      : dialog.kind === "chmod"
        ? dialog.entry.mode
        : dialog.kind === "compress"
          ? dialog.names.length === 1
            ? dialog.names[0].replace(/\/$/, "")
            : "archive"
          : "";
  const [value, setValue] = useState(initial);
  const [format, setFormat] = useState(".zip");
  const [dest, setDest] = useState(cwd);
  const [extractTo, setExtractTo] = useState<"here" | "folder" | "other">(
    "folder",
  );
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
        msg = await run(
          () => op({ op: "delete", paths: dialog.names.map(p) }),
          translate("Đã xóa {0} mục", [dialog.names.length]),
        );
        break;
      case "compress":
        msg = await run(
          () =>
            op({
              op: "compress",
              paths: dialog.names.map(p),
              to: p(value + format),
            }),
          translate("Đã tạo {0}{1}", [value, format]),
        );
        break;
      case "move":
      case "copy":
        msg = await run(
          () => op({ op: dialog.kind, paths: dialog.names.map(p), to: dest }),
          translate("Đã {0} {1} mục tới {2}", [
            dialog.kind === "move"
              ? translate("di chuyển")
              : translate("sao chép"),
            dialog.names.length,
            dest,
          ]),
        );
        break;
      case "extract": {
        const to =
          extractTo === "here"
            ? cwd
            : extractTo === "folder"
              ? p(archiveBase)
              : dest;
        msg = await run(
          () => op({ op: "extract", path: p(dialog.entry.name), to }),
          translate("Đã giải nén vào {0}", [to]),
        );
        break;
      }
    }
    setErr(msg);
    setBusy(false);
  }

  const titles: Record<Dialog["kind"], string> = {
    mkdir: translate("Tạo thư mục"),
    newfile: translate("Tạo file"),
    rename: translate("Đổi tên"),
    chmod: translate("Phân quyền"),
    delete: translate("Xóa"),
    compress: translate("Nén"),
    move: translate("Di chuyển tới"),
    copy: translate("Sao chép tới"),
    extract:
      dialog.kind === "extract"
        ? translate("Giải nén {0}", [dialog.entry.name])
        : "",
  };

  let body: React.ReactNode;
  switch (dialog.kind) {
    case "delete":
      body = (
        <p className="text-sm text-slate-600">
          {translate("Xóa vĩnh viễn")} {translate(dialog.names.length)}{" "}
          {translate("mục:")} <b>{translate(dialog.names.join(", "))}</b>?
        </p>
      );
      break;
    case "move":
    case "copy":
      body = (
        <>
          <p className="text-sm text-slate-600">
            {translate(dialog.names.join(", "))}
          </p>
          <label className="label">{translate("Thư mục đích")}</label>
          <FolderPicker op={op} start={cwd} value={dest} onChange={setDest} />
        </>
      );
      break;
    case "extract":
      body = (
        <div className="space-y-2 text-sm">
          <label className="flex items-center gap-2">
            <input
              type="radio"
              checked={extractTo === "folder"}
              onChange={() => setExtractTo("folder")}
            />{" "}
            {translate("Vào thư mục mới")} <code>{archiveBase}/</code>
          </label>
          <label className="flex items-center gap-2">
            <input
              type="radio"
              checked={extractTo === "here"}
              onChange={() => setExtractTo("here")}
            />{" "}
            {translate("Vào thư mục hiện tại")} <code>{cwd}</code>
          </label>
          <label className="flex items-center gap-2">
            <input
              type="radio"
              checked={extractTo === "other"}
              onChange={() => setExtractTo("other")}
            />{" "}
            {translate("Chọn thư mục khác…")}
          </label>
          {extractTo === "other" && (
            <FolderPicker op={op} start={cwd} value={dest} onChange={setDest} />
          )}
          <p className="text-xs text-slate-500">
            {translate(
              "File trùng tên sẽ bị ghi đè. Hỗ trợ .zip, .tar, .tar.gz, .tgz, .tar.bz2.",
            )}
          </p>
        </div>
      );
      break;
    case "compress":
      body = (
        <div className="flex gap-2">
          <input
            className="input font-mono"
            autoFocus
            required
            value={value}
            onChange={(e) => setValue(e.target.value)}
          />
          <select
            className="input w-32"
            value={format}
            onChange={(e) => setFormat(e.target.value)}
          >
            <option value=".zip">.zip</option>
            <option value=".tar.gz">.tar.gz</option>
          </select>
        </div>
      );
      break;
    default:
      body = (
        <input
          className="input font-mono"
          autoFocus
          required
          value={value}
          onChange={(e) => setValue(e.target.value)}
          placeholder={translate(dialog.kind === "chmod" ? "644" : undefined)}
          pattern={dialog.kind === "chmod" ? "[0-7]{3}" : undefined}
        />
      );
  }

  return (
    <Modal title={translate(titles[dialog.kind])} onClose={onClose}>
      <form onSubmit={submit} className="space-y-4">
        {translate(body)}
        <ErrorBox msg={err} />
        <div className="flex justify-end gap-2">
          <button type="button" className="btn-ghost" onClick={onClose}>
            {translate("Hủy")}
          </button>
          <button
            className={
              dialog.kind === "delete"
                ? "btn bg-red-600 text-white hover:bg-red-700"
                : "btn-primary"
            }
            disabled={busy}
          >
            {translate(
              busy
                ? translate("Đang xử lý…")
                : dialog.kind === "delete"
                  ? translate("Xóa")
                  : "OK",
            )}
          </button>
        </div>
      </form>
    </Modal>
  );
}
