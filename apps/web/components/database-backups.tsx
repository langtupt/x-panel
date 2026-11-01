"use client";
import { localeTag, t as translate } from "@/lib/i18n";

import { useCallback, useEffect, useRef, useState } from "react";
import {
  DatabaseBackup,
  Download,
  History,
  Loader2,
  RotateCcw,
  Trash2,
  Upload,
} from "lucide-react";
import {
  api,
  errMsg,
  fmtBytes,
  isOpen,
  type Database,
  type Task,
} from "@/lib/api";
import { useTask } from "@/lib/useTask";
import { TaskCard } from "@/components/task";
import { ErrorBox, Modal } from "@/components/ui";

type DumpFile = { name: string; path: string; size: number; modified: string };
type DumpList = { dir: string; files: DumpFile[] };

const engineInfo = {
  mariadb: {
    label: "MariaDB",
    tool: "mysqldump",
    import: "mariadb",
  },
  postgresql: {
    label: "PostgreSQL",
    tool: "pg_dump",
    import: "psql",
  },
} as const;

function DumpTask({ task, onDone }: { task: Task; onDone: () => void }) {
  const t = useTask(task, onDone);
  if (!t) return null;
  return <TaskCard task={t} defaultOpen={t.status !== "done"} />;
}

/** Backup / restore of one database: dump files kept in ~/backups/databases. */
export function DatabaseBackups({
  db,
  onClose,
}: {
  db: Database;
  onClose: () => void;
}) {
  const engine = engineInfo[db.engine] ?? engineInfo.mariadb;
  const [list, setList] = useState<DumpList | null>(null);
  const [task, setTask] = useState<Task | null>(null);
  const [confirm, setConfirm] = useState<DumpFile | null>(null);
  const [uploading, setUploading] = useState<number | null>(null);
  const [error, setError] = useState("");
  const fileInput = useRef<HTMLInputElement>(null);
  const busy = (!!task && isOpen(task)) || uploading !== null;

  const load = useCallback(() => {
    api<DumpList>(`/databases/${db.id}/backups`)
      .then((l) => {
        setList(l);
        setError("");
      })
      .catch((e) => setError(errMsg(e)));
  }, [db.id]);
  useEffect(load, [load]);

  async function start(path: string, body?: unknown) {
    setError("");
    setConfirm(null);
    try {
      setTask(await api<Task>(path, { method: "POST", body }));
    } catch (e) {
      setError(errMsg(e));
    }
  }

  async function remove(f: DumpFile) {
    setError("");
    try {
      await api(`/databases/${db.id}/backups/${encodeURIComponent(f.name)}`, {
        method: "DELETE",
      });
      load();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  function upload(file: File) {
    const gz = file.name.toLowerCase().endsWith(".gz");
    if (!gz && !file.name.toLowerCase().endsWith(".sql")) {
      setError(translate("Chỉ nhận file .sql hoặc .sql.gz"));
      return;
    }
    const stamp = new Date()
      .toISOString()
      .replace(/[-:]/g, "")
      .replace("T", "-")
      .slice(0, 15);
    const name = `${db.name}-upload-${stamp}.sql${gz ? ".gz" : ""}`;
    const dir = list?.dir ?? "/backups/databases";
    setError("");
    setUploading(0);
    const xhr = new XMLHttpRequest();
    xhr.open(
      "POST",
      `/api/files/upload?user_id=${db.user_id}&path=${encodeURIComponent(`${dir}/${name}`)}`,
    );
    xhr.setRequestHeader("X-XPanel", "1");
    xhr.upload.onprogress = (ev) =>
      setUploading(
        ev.lengthComputable ? Math.round((ev.loaded / ev.total) * 100) : 0,
      );
    xhr.onloadend = () => {
      setUploading(null);
      if (xhr.status !== 200) {
        let msg = translate("Tải lên thất bại");
        try {
          msg = translate(JSON.parse(xhr.responseText).error ?? msg);
        } catch {}
        setError(msg);
      }
      load();
    };
    xhr.send(file);
  }

  return (
    <Modal
      size="wide"
      title={translate("Sao lưu & khôi phục {0}", [db.name])}
      onClose={onClose}
    >
      <div className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border bg-muted/40 p-3 text-xs text-muted-foreground">
          <span>
            <b className="text-foreground">{engine.label}</b> ·{" "}
            {translate("sao lưu bằng {0}, khôi phục bằng {1}", [
              engine.tool,
              engine.import,
            ])}
            <br />
            {translate("File lưu trong thư mục {0} của tài khoản {1}.", [
              `~${list?.dir ?? "/backups/databases"}`,
              db.username,
            ])}
          </span>
          <div className="flex gap-2">
            <input
              ref={fileInput}
              type="file"
              accept=".sql,.gz"
              className="hidden"
              onChange={(e) => {
                const f = e.target.files?.[0];
                e.target.value = "";
                if (f) upload(f);
              }}
            />
            <button
              type="button"
              className="btn-ghost inline-flex items-center gap-1.5 px-3 py-1.5 text-xs"
              disabled={busy}
              onClick={() => fileInput.current?.click()}
            >
              {uploading !== null ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : (
                <Upload className="size-3.5" />
              )}
              {uploading !== null ? `${uploading}%` : translate("Tải lên .sql")}
            </button>
            <button
              type="button"
              className="btn-primary inline-flex items-center gap-1.5 px-3 py-1.5 text-xs"
              disabled={busy}
              onClick={() => start(`/databases/${db.id}/backups`)}
            >
              <DatabaseBackup className="size-3.5" />
              {translate("Sao lưu ngay")}
            </button>
          </div>
        </div>

        <ErrorBox msg={error} />
        {task && <DumpTask task={task} onDone={load} />}

        {confirm && (
          <div className="rounded-lg border border-amber-300 bg-amber-50 p-3 text-sm text-amber-900">
            <p>
              {translate(
                "Khôi phục {0} từ {1}? Dữ liệu hiện tại sẽ bị ghi đè; panel tự sao lưu dữ liệu hiện tại trước (file …-pre-restore).",
                [db.name, confirm.name],
              )}
            </p>
            <div className="mt-3 flex justify-end gap-2">
              <button
                type="button"
                className="btn-ghost px-3 py-1.5 text-xs"
                onClick={() => setConfirm(null)}
              >
                {translate("Hủy")}
              </button>
              <button
                type="button"
                className="btn bg-amber-600 px-3 py-1.5 text-xs text-white hover:bg-amber-700"
                onClick={() =>
                  start(`/databases/${db.id}/restore`, { file: confirm.name })
                }
              >
                {translate("Khôi phục")}
              </button>
            </div>
          </div>
        )}

        <div className="overflow-hidden rounded-lg border">
          {list === null ? (
            <p className="p-6 text-center text-sm text-muted-foreground">
              {translate("Đang tải…")}
            </p>
          ) : list.files.length === 0 ? (
            <div className="flex flex-col items-center gap-2 p-8 text-center text-sm text-muted-foreground">
              <History className="size-5" />
              {translate("Chưa có bản sao lưu nào cho database này.")}
            </div>
          ) : (
            <ul className="divide-y">
              {list.files.map((f) => (
                <li
                  key={f.name}
                  className="flex flex-wrap items-center gap-3 px-3 py-2 text-sm"
                >
                  <div className="min-w-0 flex-1">
                    <p className="truncate font-mono text-xs">{f.name}</p>
                    <p className="text-[11px] text-muted-foreground">
                      {new Date(f.modified).toLocaleString(localeTag())} ·{" "}
                      {fmtBytes(f.size)}
                    </p>
                  </div>
                  <div className="flex gap-1">
                    <a
                      className="btn-ghost inline-flex items-center gap-1 px-2 py-1 text-xs"
                      href={`/api/files/download?user_id=${db.user_id}&path=${encodeURIComponent(f.path)}`}
                      title={translate("Tải về")}
                    >
                      <Download className="size-3.5" />
                      {translate("Tải về")}
                    </a>
                    <button
                      type="button"
                      className="btn-ghost inline-flex items-center gap-1 px-2 py-1 text-xs"
                      disabled={busy}
                      onClick={() => setConfirm(f)}
                    >
                      <RotateCcw className="size-3.5" />
                      {translate("Khôi phục")}
                    </button>
                    <button
                      type="button"
                      className="btn-ghost px-2 py-1 text-xs text-red-600"
                      disabled={busy}
                      aria-label={translate("Xóa {0}", [f.name])}
                      title={translate("Xóa")}
                      onClick={() => remove(f)}
                    >
                      <Trash2 className="size-3.5" />
                    </button>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </div>
        <p className="text-[11px] text-muted-foreground">
          {translate(
            "File tải lên được đổi tên theo database. Có thể chép file .sql/.sql.gz vào thư mục trên bằng Quản lý file hoặc FTP, đặt tên bắt đầu bằng “{0}-”.",
            [db.name],
          )}
        </p>
      </div>
    </Modal>
  );
}
