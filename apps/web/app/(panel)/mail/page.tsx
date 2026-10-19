"use client";

import { FormEvent, useCallback, useEffect, useState } from "react";
import { api, errMsg, genPassword } from "@/lib/api";
import { ErrorBox, Modal } from "@/components/ui";
import { OwnerSelect, useHostingAccounts } from "@/components/owner";
import { useSession } from "../session";

type MailDomain = {
  id: number;
  user_id: number;
  username: string;
  domain: string;
  mailboxes: number;
  aliases: number;
};
type Mailbox = { id: number; address: string; local_part: string; quota_mb: number };
type Alias = { id: number; address: string; local_part: string; destinations: string[] };
type DNSRecord = { type: string; name: string; value: string; priority?: number };
type Detail = MailDomain & { mailbox_list: Mailbox[]; alias_list: Alias[]; dns: DNSRecord[] };
type Status = { installed: boolean; webmail: boolean; hostname: string };

// Roundcube has its own port (separate browser origin from the panel).
const webmailURL = () => `https://${window.location.hostname}:2096/webmail/`;

function Copy({ text }: { text: string }) {
  const [done, setDone] = useState(false);
  return (
    <button type="button" className="text-xs text-brand-600 hover:underline"
      onClick={() => navigator.clipboard.writeText(text).then(() => { setDone(true); setTimeout(() => setDone(false), 1500); })}>
      {done ? "Đã chép" : "Chép"}
    </button>
  );
}

function AddDomain({ manager, onDone, onClose }: { manager: boolean; onDone: () => void; onClose: () => void }) {
  const owners = useHostingAccounts(manager);
  const [ownerId, setOwnerId] = useState<number | null>(null);
  const [domain, setDomain] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (manager && ownerId === null && owners.length) setOwnerId(owners[0].id);
  }, [manager, owners, ownerId]);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api("/mail/domains", { method: "POST", body: { user_id: manager ? ownerId : null, domain } });
      onDone();
    } catch (err) {
      setError(errMsg(err));
      setBusy(false);
    }
  }
  return (
    <Modal title="Thêm tên miền email" onClose={onClose}>
      <form onSubmit={submit} className="space-y-4">
        {manager && (
          <div>
            <label className="label">Tài khoản sở hữu</label>
            <OwnerSelect owners={owners} value={ownerId} onChange={setOwnerId} />
          </div>
        )}
        <div>
          <label className="label">Tên miền</label>
          <input className="input" required placeholder="example.com" value={domain}
            onChange={(e) => setDomain(e.target.value.toLowerCase().trim())} />
        </div>
        <ErrorBox msg={error} />
        <div className="flex justify-end gap-2">
          <button type="button" className="btn-ghost" onClick={onClose}>Hủy</button>
          <button className="btn-primary" disabled={busy}>Thêm</button>
        </div>
      </form>
    </Modal>
  );
}

function DomainPanel({ id, webmail, onChanged }: { id: number; webmail: boolean; onChanged: () => void }) {
  const [d, setD] = useState<Detail | null>(null);
  const [error, setError] = useState("");
  const [created, setCreated] = useState<{ address: string; password: string } | null>(null);
  const [box, setBox] = useState({ local: "", password: genPassword(16), quota: "1024" });
  const [alias, setAlias] = useState({ local: "", dest: "" });

  const load = useCallback(() => {
    api<Detail>(`/mail/domains/${id}`).then(setD).catch((e) => setError(errMsg(e)));
  }, [id]);
  useEffect(load, [load]);

  async function run(fn: () => Promise<unknown>) {
    setError("");
    try {
      await fn();
      load();
      onChanged();
      return true;
    } catch (e) {
      setError(errMsg(e));
      return false;
    }
  }

  async function addBox(e: FormEvent) {
    e.preventDefault();
    const pw = box.password;
    const ok = await run(() => api(`/mail/domains/${id}/mailboxes`, {
      method: "POST", body: { local_part: box.local, password: pw, quota_mb: Number(box.quota) || 0 },
    }));
    if (ok && d) {
      setCreated({ address: `${box.local}@${d.domain}`, password: pw });
      setBox({ local: "", password: genPassword(16), quota: box.quota });
    }
  }

  async function addAlias(e: FormEvent) {
    e.preventDefault();
    const ok = await run(() => api(`/mail/domains/${id}/aliases`, {
      method: "POST", body: { local_part: alias.local, destinations: alias.dest.split(/[\s,;]+/).filter(Boolean) },
    }));
    if (ok) setAlias({ local: "", dest: "" });
  }

  async function resetPassword(m: Mailbox) {
    const pw = genPassword(16);
    if (await run(() => api(`/mail/mailboxes/${m.id}`, { method: "PATCH", body: { password: pw } }))) {
      setCreated({ address: m.address, password: pw });
    }
  }

  if (!d) return <div className="p-5"><ErrorBox msg={error} /></div>;
  return (
    <div className="space-y-6 border-t border-slate-100 p-5">
      <ErrorBox msg={error} />
      {created && (
        <div className="rounded-lg bg-emerald-50 px-4 py-3 text-sm text-emerald-900">
          Hộp thư <b>{created.address}</b> — mật khẩu: <code className="rounded bg-white px-1.5">{created.password}</code>{" "}
          <Copy text={created.password} /> (chỉ hiện một lần)
        </div>
      )}

      <section className="space-y-3">
        <h3 className="font-semibold">Hộp thư</h3>
        <table className="w-full text-left text-sm">
          <tbody className="divide-y divide-slate-100">
            {d.mailbox_list.map((m) => (
              <tr key={m.id}>
                <td className="py-2 font-medium">{m.address}</td>
                <td className="py-2 text-slate-500">{m.quota_mb ? `${m.quota_mb} MB` : "Không giới hạn"}</td>
                <td className="py-2 text-right">
                  <div className="flex justify-end gap-1.5">
                    {webmail && (
                      <a className="btn-ghost px-2.5 py-1 text-xs" href={webmailURL()} target="_blank" rel="noreferrer">
                        Webmail
                      </a>
                    )}
                    <button className="btn-ghost px-2.5 py-1 text-xs" onClick={() => resetPassword(m)}>Đổi mật khẩu</button>
                    <button className="btn-ghost px-2.5 py-1 text-xs text-red-600"
                      onClick={() => confirm(`Xóa ${m.address} và toàn bộ thư?`) && run(() => api(`/mail/mailboxes/${m.id}`, { method: "DELETE" }))}>
                      Xóa
                    </button>
                  </div>
                </td>
              </tr>
            ))}
            {d.mailbox_list.length === 0 && <tr><td className="py-2 text-slate-500">Chưa có hộp thư.</td></tr>}
          </tbody>
        </table>
        <form onSubmit={addBox} className="flex flex-wrap items-end gap-2">
          <div>
            <label className="label">Tên hộp thư</label>
            <div className="flex items-center gap-1">
              <input className="input w-40" required placeholder="info" value={box.local}
                onChange={(e) => setBox({ ...box, local: e.target.value.toLowerCase() })} />
              <span className="text-sm text-slate-500">@{d.domain}</span>
            </div>
          </div>
          <div>
            <label className="label">Mật khẩu</label>
            <input className="input w-48 font-mono" required minLength={10} value={box.password}
              onChange={(e) => setBox({ ...box, password: e.target.value })} />
          </div>
          <div>
            <label className="label">Dung lượng (MB, 0 = không giới hạn)</label>
            <input className="input w-32" type="number" min={0} value={box.quota}
              onChange={(e) => setBox({ ...box, quota: e.target.value })} />
          </div>
          <button className="btn-primary">Tạo hộp thư</button>
        </form>
      </section>

      <section className="space-y-3">
        <h3 className="font-semibold">Chuyển tiếp (alias)</h3>
        <table className="w-full text-left text-sm">
          <tbody className="divide-y divide-slate-100">
            {d.alias_list.map((a) => (
              <tr key={a.id}>
                <td className="py-2 font-medium">{a.address}</td>
                <td className="py-2 text-slate-500">→ {a.destinations.join(", ")}</td>
                <td className="py-2 text-right">
                  <button className="btn-ghost px-2.5 py-1 text-xs text-red-600"
                    onClick={() => run(() => api(`/mail/aliases/${a.id}`, { method: "DELETE" }))}>Xóa</button>
                </td>
              </tr>
            ))}
            {d.alias_list.length === 0 && <tr><td className="py-2 text-slate-500">Chưa có chuyển tiếp.</td></tr>}
          </tbody>
        </table>
        <form onSubmit={addAlias} className="flex flex-wrap items-end gap-2">
          <div>
            <label className="label">Địa chỉ</label>
            <div className="flex items-center gap-1">
              <input className="input w-40" required placeholder="sales" value={alias.local}
                onChange={(e) => setAlias({ ...alias, local: e.target.value.toLowerCase() })} />
              <span className="text-sm text-slate-500">@{d.domain}</span>
            </div>
          </div>
          <div className="min-w-64 flex-1">
            <label className="label">Chuyển tới (cách nhau bởi dấu phẩy)</label>
            <input className="input" required placeholder="info@example.com, ban@gmail.com" value={alias.dest}
              onChange={(e) => setAlias({ ...alias, dest: e.target.value })} />
          </div>
          <button className="btn-primary">Thêm chuyển tiếp</button>
        </form>
      </section>

      <section className="space-y-3">
        <h3 className="font-semibold">Bản ghi DNS cần tạo</h3>
        <p className="text-xs text-slate-500">Tạo các bản ghi sau tại nhà cung cấp tên miền để gửi/nhận thư và không bị vào spam.</p>
        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs">
            <thead className="text-slate-500">
              <tr><th className="py-1 pr-3">Loại</th><th className="py-1 pr-3">Tên</th><th className="py-1 pr-3">Giá trị</th><th /></tr>
            </thead>
            <tbody className="divide-y divide-slate-100 font-mono">
              {d.dns.map((r) => (
                <tr key={r.type + r.name} className="align-top">
                  <td className="py-2 pr-3">{r.type}{r.priority ? ` (${r.priority})` : ""}</td>
                  <td className="py-2 pr-3">{r.name}</td>
                  <td className="max-w-xl break-all py-2 pr-3">{r.value}</td>
                  <td className="py-2"><Copy text={r.value} /></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}

export default function MailPage() {
  const me = useSession();
  const manager = me.role !== "user";
  const [status, setStatus] = useState<Status | null>(null);
  const [list, setList] = useState<MailDomain[]>([]);
  const [open, setOpen] = useState<number | null>(null);
  const [adding, setAdding] = useState(false);
  const [error, setError] = useState("");

  const load = useCallback(() => {
    api<MailDomain[]>("/mail/domains").then(setList).catch((e) => setError(errMsg(e)));
  }, []);
  useEffect(() => {
    load();
    api<Status>("/mail/status").then(setStatus).catch(() => setStatus(null));
  }, [load]);

  async function remove(d: MailDomain) {
    if (!confirm(`Xóa tên miền ${d.domain} cùng mọi hộp thư và thư đã lưu?`)) return;
    try {
      await api(`/mail/domains/${d.id}`, { method: "DELETE" });
      if (open === d.id) setOpen(null);
      load();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">Email</h1>
          <p className="text-sm text-slate-500">
            Hộp thư theo tên miền (IMAP/POP3/SMTP){status?.hostname && <> — máy chủ thư: <b>{status.hostname}</b></>}
          </p>
        </div>
        <div className="flex gap-2">
          {status?.webmail && <a className="btn-ghost" href={webmailURL()} target="_blank" rel="noreferrer">Mở Webmail</a>}
          <button className="btn-primary" disabled={status !== null && !status.installed} onClick={() => setAdding(true)}>
            Thêm tên miền
          </button>
        </div>
      </div>

      {status && !status.installed && (
        <p className="rounded-lg bg-amber-50 px-4 py-3 text-sm text-amber-800">
          Mail server chưa được cài. {me.role === "admin" ? "Cài “Mail server” (và “Roundcube Webmail”) tại trang Applications." : "Liên hệ quản trị viên."}
        </p>
      )}
      <ErrorBox msg={error} />

      <div className="card divide-y divide-slate-100">
        {list.map((d) => (
          <div key={d.id}>
            <div className="flex flex-wrap items-center justify-between gap-3 px-5 py-4">
              <button className="text-left" onClick={() => setOpen(open === d.id ? null : d.id)}>
                <div className="font-medium">{d.domain}</div>
                <div className="text-xs text-slate-500">
                  {d.mailboxes} hộp thư · {d.aliases} chuyển tiếp{manager && <> · tài khoản {d.username}</>}
                </div>
              </button>
              <div className="flex gap-1.5">
                <button className="btn-ghost px-2.5 py-1 text-xs" onClick={() => setOpen(open === d.id ? null : d.id)}>
                  {open === d.id ? "Thu gọn" : "Quản lý"}
                </button>
                <button className="btn-ghost px-2.5 py-1 text-xs text-red-600" onClick={() => remove(d)}>Xóa</button>
              </div>
            </div>
            {open === d.id && <DomainPanel id={d.id} webmail={!!status?.webmail} onChanged={load} />}
          </div>
        ))}
        {list.length === 0 && <p className="px-5 py-8 text-center text-sm text-slate-500">Chưa có tên miền email nào.</p>}
      </div>

      {status?.installed && (
        <div className="card p-5 text-sm text-slate-600">
          <h2 className="mb-2 font-semibold text-slate-900">Cấu hình ứng dụng mail (Outlook, Thunderbird, điện thoại)</h2>
          <ul className="list-inside list-disc space-y-1">
            <li>Tên đăng nhập: địa chỉ email đầy đủ</li>
            <li>IMAP: {status.hostname}, cổng 993 (SSL/TLS) — POP3: cổng 995 (SSL/TLS)</li>
            <li>SMTP: {status.hostname}, cổng 465 (SSL/TLS) hoặc 587 (STARTTLS), có xác thực</li>
            <li>
              Website gửi thư (form liên hệ, WordPress…): cấu hình SMTP 587 bằng một hộp thư ở trên. Hàm
              mail()/sendmail bị tắt để tài khoản này không thể mạo danh địa chỉ của tài khoản khác.
            </li>
          </ul>
        </div>
      )}

      {adding && <AddDomain manager={manager} onClose={() => setAdding(false)} onDone={() => { setAdding(false); load(); }} />}
    </div>
  );
}
