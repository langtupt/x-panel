"use client";
import { localeTag } from "@/lib/i18n";
import { t as translate } from "@/lib/i18n";

import { FormEvent, useCallback, useEffect, useState } from "react";
import {
  api,
  errMsg,
  type PanelSSL,
  type Site,
  type SiteResult,
  type Task,
} from "@/lib/api";
import { useTask } from "@/lib/useTask";
import { ErrorBox, Modal } from "@/components/ui";
import { TaskCard } from "@/components/task";
import { useSession } from "../session";

const typeLabel: Record<string, string> = {
  letsencrypt: "Let's Encrypt (tự gia hạn)",
  custom: "Chứng chỉ tải lên",
  selfsigned: "Tự ký",
  "": "—",
};

function daysLeft(iso: string | null) {
  if (!iso) return null;
  return Math.floor((new Date(iso).getTime() - Date.now()) / 86_400_000);
}

function Expiry({ iso }: { iso: string | null }) {
  const d = daysLeft(iso);
  if (d === null) return <span className="text-slate-400">—</span>;
  const cls =
    d < 0 ? "text-red-600" : d < 15 ? "text-amber-600" : "text-slate-600";
  return (
    <span className={cls}>
      {translate(new Date(iso!).toLocaleDateString(localeTag()))} (
      {translate(
        d < 0 ? translate("đã hết hạn") : translate("còn {0} ngày", [d]),
      )}
      )
    </span>
  );
}

function UploadDialog({
  site,
  onClose,
  onDone,
}: {
  site: Site;
  onClose: () => void;
  onDone: (r: SiteResult) => void;
}) {
  const [cert, setCert] = useState("");
  const [key, setKey] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function readFile(
    e: React.ChangeEvent<HTMLInputElement>,
    set: (v: string) => void,
  ) {
    const f = e.target.files?.[0];
    if (f) set(await f.text());
  }

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      onDone(
        await api<SiteResult>(`/sites/${site.id}/ssl/custom`, {
          method: "POST",
          body: { certificate: cert, private_key: key },
        }),
      );
    } catch (err) {
      setError(errMsg(err));
      setBusy(false);
    }
  }

  return (
    <Modal
      title={translate("Tải lên chứng chỉ cho {0}", [site.domain])}
      onClose={onClose}
    >
      <form onSubmit={submit} className="space-y-4">
        <div>
          <label className="label">
            {translate("Chứng chỉ (CRT/PEM, gồm cả chuỗi CA nếu có)")}
          </label>
          <textarea
            className="input h-32 font-mono text-xs"
            required
            placeholder="-----BEGIN CERTIFICATE-----"
            value={cert}
            onChange={(e) => setCert(e.target.value)}
          />
          <input
            type="file"
            accept=".crt,.pem,.cer,.txt"
            className="mt-1 text-xs"
            onChange={(e) => readFile(e, setCert)}
          />
        </div>
        <div>
          <label className="label">
            {translate("Khóa riêng (KEY, không đặt mật khẩu)")}
          </label>
          <textarea
            className="input h-32 font-mono text-xs"
            required
            placeholder="-----BEGIN PRIVATE KEY-----"
            value={key}
            onChange={(e) => setKey(e.target.value)}
          />
          <input
            type="file"
            accept=".key,.pem,.txt"
            className="mt-1 text-xs"
            onChange={(e) => readFile(e, setKey)}
          />
        </div>
        <p className="text-xs text-slate-500">
          {translate(
            "Hệ thống kiểm tra khóa khớp chứng chỉ, còn hạn và đúng tên miền trước khi cài.",
          )}
        </p>
        <ErrorBox msg={error} />
        <div className="flex justify-end gap-2">
          <button type="button" className="btn-ghost" onClick={onClose}>
            {translate("Hủy")}
          </button>
          <button className="btn-primary" disabled={busy}>
            {translate(
              busy ? translate("Đang kiểm tra…") : translate("Cài chứng chỉ"),
            )}
          </button>
        </div>
      </form>
    </Modal>
  );
}

function PanelCard() {
  const [st, setSt] = useState<PanelSSL | null>(null);
  const [domain, setDomain] = useState("");
  const [task, setTask] = useState<Task | null>(null);
  const [error, setError] = useState("");
  const refresh = useCallback(() => {
    api<PanelSSL>("/system/panel-ssl")
      .then((s) => {
        setSt(s);
        setDomain((d) => d || s.domain);
      })
      .catch((e) => setError(errMsg(e)));
  }, []);
  useEffect(refresh, [refresh]);
  const t = useTask(task, refresh);

  async function issue(e: FormEvent) {
    e.preventDefault();
    setError("");
    try {
      setTask(
        await api<Task>("/system/panel-ssl", {
          method: "POST",
          body: { domain },
        }),
      );
    } catch (err) {
      setError(errMsg(err));
    }
  }

  return (
    <section className="card space-y-4 p-5">
      <div>
        <h2 className="font-semibold">
          {translate("Chứng chỉ trang quản trị (cổng 8888) và FTPS")}
        </h2>
        {st && (
          <p className="mt-1 text-sm text-slate-600">
            {translate("Hiện tại:")}{" "}
            <b>
              {translate(
                st.info.self_signed
                  ? translate("tự ký (trình duyệt cảnh báo)")
                  : st.info.issuer,
              )}
            </b>
            {st.info.subject && <> · {translate(st.info.subject)}</>}{" "}
            {translate("· hết hạn")} <Expiry iso={st.info.not_after} />
          </p>
        )}
      </div>
      <form onSubmit={issue} className="flex flex-wrap items-end gap-3">
        <div className="min-w-64 flex-1">
          <label className="label">{translate("Tên miền của máy chủ")}</label>
          <input
            className="input"
            required
            placeholder="panel.example.com"
            value={domain}
            onChange={(e) => setDomain(e.target.value.toLowerCase())}
          />
        </div>
        <button
          className="btn-primary"
          disabled={!!t && (t.status === "pending" || t.status === "running")}
        >
          {translate("Cấp Let's Encrypt")}
        </button>
      </form>
      <p className="text-xs text-slate-500">
        {translate(
          "Tên miền phải trỏ (bản ghi A) về IP máy chủ và mở cổng 80. Chứng chỉ được tự gia hạn trước 30 ngày.",
        )}
      </p>
      {t && <TaskCard task={t} defaultOpen={t.status === "failed"} />}
      <ErrorBox msg={error} />
    </section>
  );
}

function SiteTask({ task, onDone }: { task: Task; onDone: () => void }) {
  const t = useTask(task, onDone);
  return t ? <TaskCard task={t} defaultOpen={t.status === "failed"} /> : null;
}

export default function SSLPage() {
  const me = useSession();
  const [list, setList] = useState<Site[]>([]);
  const [tasks, setTasks] = useState<Record<number, Task>>({});
  const [upload, setUpload] = useState<Site | null>(null);
  const [notes, setNotes] = useState<string[]>([]);
  const [error, setError] = useState("");

  const load = useCallback(() => {
    api<Site[]>("/sites")
      .then(setList)
      .catch((e) => setError(errMsg(e)));
  }, []);
  useEffect(load, [load]);

  async function act(s: Site, fn: () => Promise<unknown>) {
    setError("");
    setNotes([]);
    try {
      const r = await fn();
      if (r && typeof r === "object" && "type" in r && "status" in r)
        setTasks((t) => ({ ...t, [s.id]: r as Task }));
      if (r && typeof r === "object" && "warnings" in r)
        setNotes((r as SiteResult).warnings ?? []);
      load();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold">{translate("Chứng chỉ SSL")}</h1>
        <p className="text-sm text-slate-500">
          {translate(
            "Cấp Let's Encrypt miễn phí (tự gia hạn), tải lên chứng chỉ mua ngoài, hoặc tạo chứng chỉ tự ký để thử nghiệm.",
          )}
        </p>
      </div>

      {me.role === "admin" && !me.impersonator && <PanelCard />}
      <ErrorBox msg={error} />
      {notes.map((n) => (
        <p
          key={n}
          className="rounded-lg bg-amber-50 px-3 py-2 text-sm text-amber-800"
        >
          {translate(n)}
        </p>
      ))}

      <div className="card overflow-x-auto">
        <table className="w-full text-left text-sm">
          <thead className="border-b border-slate-200 bg-slate-50 text-xs uppercase text-slate-500">
            <tr>
              <th className="px-4 py-3">Website</th>
              <th className="px-4 py-3">{translate("Loại")}</th>
              <th className="px-4 py-3">{translate("Nhà cấp")}</th>
              <th className="px-4 py-3">{translate("Hết hạn")}</th>
              <th className="px-4 py-3" />
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100">
            {list.map((s) => {
              const active = s.ssl_status === "active";
              return (
                <tr key={s.id} className="align-top">
                  <td className="px-4 py-3">
                    <div className="font-medium">{s.domain}</div>
                    {s.aliases.length > 0 && (
                      <div className="text-xs text-slate-500">
                        {translate(s.aliases.join(", "))}
                      </div>
                    )}
                    {s.ssl_status === "pending" && (
                      <div className="text-xs text-brand-600">
                        {translate("Đang cấp chứng chỉ…")}
                      </div>
                    )}
                    {s.ssl_status === "failed" && (
                      <div className="text-xs text-red-600">
                        {translate("Cấp Let's Encrypt lỗi, xem Tác vụ")}
                      </div>
                    )}
                    {tasks[s.id] && (
                      <div className="mt-2 max-w-xl">
                        <SiteTask task={tasks[s.id]} onDone={load} />
                      </div>
                    )}
                  </td>
                  <td className="px-4 py-3">
                    {active ? (
                      typeLabel[s.ssl_type]
                    ) : (
                      <span className="text-slate-400">
                        {translate("Chưa có SSL")}
                      </span>
                    )}
                  </td>
                  <td className="px-4 py-3 text-slate-600">
                    {translate(active ? s.ssl_issuer || "—" : "—")}
                  </td>
                  <td className="px-4 py-3">
                    {active ? <Expiry iso={s.ssl_expires_at} /> : "—"}
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex flex-wrap justify-end gap-1.5">
                      <button
                        className="btn-ghost px-2.5 py-1 text-xs"
                        disabled={s.ssl_status === "pending"}
                        onClick={() =>
                          act(s, () =>
                            api<Task>(`/sites/${s.id}/ssl`, { method: "POST" }),
                          )
                        }
                      >
                        {translate(
                          active && s.ssl_type === "letsencrypt"
                            ? translate("Cấp lại")
                            : "Let's Encrypt",
                        )}
                      </button>
                      <button
                        className="btn-ghost px-2.5 py-1 text-xs"
                        onClick={() => setUpload(s)}
                      >
                        {translate("Tải lên")}
                      </button>
                      <button
                        className="btn-ghost px-2.5 py-1 text-xs"
                        onClick={() =>
                          act(s, () =>
                            api<SiteResult>(`/sites/${s.id}/ssl/selfsigned`, {
                              method: "POST",
                            }),
                          )
                        }
                      >
                        {translate("Tự ký")}
                      </button>
                      {active && (
                        <button
                          className="btn-ghost px-2.5 py-1 text-xs text-red-600"
                          onClick={() =>
                            act(s, () =>
                              api(`/sites/${s.id}/ssl`, { method: "DELETE" }),
                            )
                          }
                        >
                          {translate("Tắt HTTPS")}
                        </button>
                      )}
                    </div>
                  </td>
                </tr>
              );
            })}
            {list.length === 0 && (
              <tr>
                <td
                  colSpan={5}
                  className="px-4 py-8 text-center text-slate-500"
                >
                  {translate("Chưa có website nào.")}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {upload && (
        <UploadDialog
          site={upload}
          onClose={() => setUpload(null)}
          onDone={(r) => {
            setUpload(null);
            setNotes(r.warnings ?? []);
            load();
          }}
        />
      )}
    </div>
  );
}
