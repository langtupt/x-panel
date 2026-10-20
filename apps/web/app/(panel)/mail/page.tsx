"use client";
import { localeTag } from "@/lib/i18n";
import { t as translate } from "@/lib/i18n";

import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";
import {
  AtSign,
  Check,
  Copy,
  ExternalLink,
  Forward,
  Globe,
  Inbox,
  KeyRound,
  Mail,
  Pencil,
  Plus,
  RefreshCw,
  Search,
  Server,
  Settings2,
  Trash2,
} from "lucide-react";
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
type Mailbox = {
  id: number;
  address: string;
  local_part: string;
  quota_mb: number;
  used_mb?: number;
  created_at: string;
};
type Alias = {
  id: number;
  address: string;
  local_part: string;
  destinations: string[];
};
type DNSRecord = {
  type: string;
  name: string;
  value: string;
  priority?: number;
};
type Detail = MailDomain & {
  mailbox_list: Mailbox[];
  alias_list: Alias[];
  dns: DNSRecord[];
  mail_free_mb: number | null;
};
type Status = { installed: boolean; webmail: boolean; hostname: string };
type Tab = "boxes" | "aliases" | "dns" | "clients";

// Roundcube runs on its own port (separate browser origin from the panel).
const webmailURL = () => `https://${window.location.hostname}:2096/webmail/`;
const quotaLabel = (mb: number) =>
  mb === 0
    ? translate("Không giới hạn")
    : mb >= 1024
      ? `${+(mb / 1024).toFixed(1)} GB`
      : `${mb} MB`;
const quotaPresets = [500, 1024, 2048, 5120, 10240, 0];

function CopyButton({ text, label }: { text: string; label?: string }) {
  const [done, setDone] = useState(false);
  return (
    <button
      type="button"
      title={translate("Sao chép")}
      className="inline-flex items-center gap-1 rounded-md px-1.5 py-1 text-xs text-muted-foreground hover:bg-muted hover:text-foreground"
      onClick={() =>
        navigator.clipboard.writeText(text).then(() => {
          setDone(true);
          setTimeout(() => setDone(false), 1500);
        })
      }
    >
      {done ? (
        <Check className="size-3.5 text-emerald-600" />
      ) : (
        <Copy className="size-3.5" />
      )}
      {label && <span>{translate(done ? translate("Đã chép") : label)}</span>}
    </button>
  );
}

function PasswordField({
  value,
  onChange,
}: {
  value: string;
  onChange: (v: string) => void;
}) {
  return (
    <div className="flex gap-2">
      <input
        className="input font-mono"
        required
        minLength={10}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
      <button
        type="button"
        className="btn-ghost shrink-0 px-2.5"
        title={translate("Tạo mật khẩu ngẫu nhiên")}
        onClick={() => onChange(genPassword(16))}
      >
        <RefreshCw className="size-4" />
      </button>
      <CopyButton text={value} />
    </div>
  );
}

function QuotaField({
  value,
  onChange,
}: {
  value: number;
  onChange: (v: number) => void;
}) {
  return (
    <div className="flex flex-wrap gap-1.5">
      {quotaPresets.map((q) => (
        <button
          type="button"
          key={q}
          onClick={() => onChange(q)}
          className={`rounded-lg border px-3 py-1.5 text-xs ${value === q ? "border-foreground bg-foreground text-background" : "hover:bg-muted"}`}
        >
          {translate(quotaLabel(q))}
        </button>
      ))}
      <input
        className="input h-8 w-28 text-xs"
        type="number"
        min={0}
        value={value}
        onChange={(e) => onChange(Number(e.target.value) || 0)}
        title={translate("MB, 0 = không giới hạn")}
      />
    </div>
  );
}

/** Shown once after a mailbox is created or its password changed. */
function Credentials({
  address,
  password,
  host,
  onClose,
}: {
  address: string;
  password: string;
  host: string;
  onClose: () => void;
}) {
  const all = translate(
    "Email: {0}\nMật khẩu: {1}\nIMAP: {2}:993 (SSL)\nSMTP: {3}:465 (SSL) hoặc 587 (STARTTLS)\nWebmail: {4}",
    [address, password, host, host, webmailURL()],
  );
  return (
    <Modal title={translate("Thông tin đăng nhập hộp thư")} onClose={onClose}>
      <div className="space-y-4">
        <div className="rounded-xl border bg-muted/40 p-4 font-mono text-sm leading-7">
          <div className="flex items-center justify-between">
            <span>{translate(address)}</span>
            <CopyButton text={address} />
          </div>
          <div className="flex items-center justify-between">
            <span>{translate(password)}</span>
            <CopyButton text={password} />
          </div>
        </div>
        <p className="text-xs text-amber-700">
          {translate(
            "Mật khẩu chỉ hiển thị lần này. Hãy lưu lại hoặc gửi cho người dùng hộp thư.",
          )}
        </p>
        <div className="flex justify-end gap-2">
          <CopyButton
            text={all}
            label={translate("Chép tất cả (kèm cấu hình)")}
          />
          <button className="btn-primary" onClick={onClose}>
            Xong
          </button>
        </div>
      </div>
    </Modal>
  );
}

function AddDomain({
  manager,
  onDone,
  onClose,
}: {
  manager: boolean;
  onDone: (id: number) => void;
  onClose: () => void;
}) {
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
      const d = await api<MailDomain>("/mail/domains", {
        method: "POST",
        body: { user_id: manager ? ownerId : null, domain },
      });
      onDone(d.id);
    } catch (err) {
      setError(errMsg(err));
      setBusy(false);
    }
  }
  return (
    <Modal title={translate("Thêm tên miền email")} onClose={onClose}>
      <form onSubmit={submit} className="space-y-4">
        {manager && (
          <div>
            <label className="label">{translate("Tài khoản sở hữu")}</label>
            <OwnerSelect
              owners={owners}
              value={ownerId}
              onChange={setOwnerId}
            />
          </div>
        )}
        <div>
          <label className="label">{translate("Tên miền")}</label>
          <input
            className="input"
            autoFocus
            required
            placeholder="example.com"
            value={domain}
            onChange={(e) => setDomain(e.target.value.toLowerCase().trim())}
          />
          <p className="mt-1 text-xs text-muted-foreground">
            {translate(
              'Sau khi thêm, tạo các bản ghi DNS ở tab "DNS" để nhận và gửi thư.',
            )}
          </p>
        </div>
        <ErrorBox msg={error} />
        <div className="flex justify-end gap-2">
          <button type="button" className="btn-ghost" onClick={onClose}>
            {translate("Hủy")}
          </button>
          <button className="btn-primary" disabled={busy}>
            {translate(
              busy ? translate("Đang thêm…") : translate("Thêm tên miền"),
            )}
          </button>
        </div>
      </form>
    </Modal>
  );
}

function MailboxDialog({
  d,
  box,
  onClose,
  onSaved,
}: {
  d: Detail;
  box: Mailbox | null;
  onClose: () => void;
  onSaved: (address: string, password: string | null) => void;
}) {
  const [local, setLocal] = useState("");
  const [password, setPassword] = useState(() => genPassword(16));
  const [changePw, setChangePw] = useState(!box);
  const [quota, setQuota] = useState(box?.quota_mb ?? 1024);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      if (box) {
        await api(`/mail/mailboxes/${box.id}`, {
          method: "PATCH",
          body: { quota_mb: quota, ...(changePw ? { password } : {}) },
        });
        onSaved(box.address, changePw ? password : null);
      } else {
        await api(`/mail/domains/${d.id}/mailboxes`, {
          method: "POST",
          body: { local_part: local, password, quota_mb: quota },
        });
        onSaved(`${local}@${d.domain}`, password);
      }
    } catch (err) {
      setError(errMsg(err));
      setBusy(false);
    }
  }
  return (
    <Modal
      title={translate(
        box ? translate("Sửa {0}", [box.address]) : translate("Tạo hộp thư"),
      )}
      onClose={onClose}
    >
      <form onSubmit={submit} className="space-y-4">
        {!box && (
          <div>
            <label className="label">{translate("Địa chỉ email")}</label>
            <div className="flex items-center overflow-hidden rounded-lg border focus-within:ring-2 focus-within:ring-ring/40">
              <input
                className="min-w-0 flex-1 bg-transparent px-3 py-2 text-sm outline-none"
                autoFocus
                required
                placeholder="info"
                value={local}
                onChange={(e) =>
                  setLocal(e.target.value.toLowerCase().replace(/\s/g, ""))
                }
              />
              <span className="border-l bg-muted px-3 py-2 text-sm text-muted-foreground">
                @{d.domain}
              </span>
            </div>
          </div>
        )}
        <div>
          <div className="mb-1 flex items-center justify-between">
            <label className="label mb-0">{translate("Mật khẩu")}</label>
            {box && (
              <label className="flex items-center gap-1.5 text-xs">
                <input
                  type="checkbox"
                  checked={changePw}
                  onChange={(e) => setChangePw(e.target.checked)}
                />{" "}
                {translate("Đổi mật khẩu")}
              </label>
            )}
          </div>
          {changePw ? (
            <PasswordField value={password} onChange={setPassword} />
          ) : (
            <p className="text-sm text-muted-foreground">
              {translate("Giữ nguyên mật khẩu hiện tại.")}
            </p>
          )}
        </div>
        <div>
          <label className="label">{translate("Dung lượng hộp thư")}</label>
          <QuotaField value={quota} onChange={setQuota} />
          {d.mail_free_mb !== null && (
            <p className="mt-1 text-xs text-muted-foreground">
              {translate("Dung lượng email được trừ vào dung lượng gói. Có thể cấp thêm tối đa {0} MB.", [
                d.mail_free_mb + (box?.quota_mb ?? 0),
              ])}
            </p>
          )}
        </div>
        <ErrorBox msg={error} />
        <div className="flex justify-end gap-2">
          <button type="button" className="btn-ghost" onClick={onClose}>
            {translate("Hủy")}
          </button>
          <button className="btn-primary" disabled={busy}>
            {translate(
              busy
                ? translate("Đang lưu…")
                : box
                  ? translate("Lưu thay đổi")
                  : translate("Tạo hộp thư"),
            )}
          </button>
        </div>
      </form>
    </Modal>
  );
}

function AliasDialog({
  d,
  alias,
  onClose,
  onSaved,
}: {
  d: Detail;
  alias: Alias | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [local, setLocal] = useState("");
  const [dest, setDest] = useState(alias?.destinations.join("\n") ?? "");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const list = dest.split(/[\s,;]+/).filter(Boolean);
  const add = (a: string) =>
    !list.includes(a) &&
    setDest((list.length ? dest.trimEnd() + "\n" : "") + a);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      if (alias)
        await api(`/mail/aliases/${alias.id}`, {
          method: "PATCH",
          body: { destinations: list },
        });
      else
        await api(`/mail/domains/${d.id}/aliases`, {
          method: "POST",
          body: { local_part: local, destinations: list },
        });
      onSaved();
    } catch (err) {
      setError(errMsg(err));
      setBusy(false);
    }
  }
  return (
    <Modal
      title={translate(
        alias
          ? translate("Sửa chuyển tiếp {0}", [alias.address])
          : translate("Tạo chuyển tiếp"),
      )}
      onClose={onClose}
    >
      <form onSubmit={submit} className="space-y-4">
        {!alias && (
          <div>
            <label className="label">{translate("Thư gửi tới")}</label>
            <div className="flex items-center overflow-hidden rounded-lg border focus-within:ring-2 focus-within:ring-ring/40">
              <input
                className="min-w-0 flex-1 bg-transparent px-3 py-2 text-sm outline-none"
                autoFocus
                required
                placeholder="sales"
                value={local}
                onChange={(e) =>
                  setLocal(e.target.value.toLowerCase().replace(/\s/g, ""))
                }
              />
              <span className="border-l bg-muted px-3 py-2 text-sm text-muted-foreground">
                @{d.domain}
              </span>
            </div>
          </div>
        )}
        <div>
          <label className="label">
            {translate(
              "Chuyển tới (mỗi dòng một địa chỉ, có thể là email ngoài như Gmail)",
            )}
          </label>
          <textarea
            className="input h-28 font-mono text-sm"
            required
            value={dest}
            onChange={(e) => setDest(e.target.value)}
            placeholder={translate("info@" + d.domain + "\nban@gmail.com")}
          />
          {d.mailbox_list.length > 0 && (
            <div className="mt-2 flex flex-wrap gap-1.5">
              {d.mailbox_list.map((m) => (
                <button
                  type="button"
                  key={m.id}
                  onClick={() => add(m.address)}
                  disabled={list.includes(m.address)}
                  className="rounded-full border px-2.5 py-0.5 text-xs hover:bg-muted disabled:opacity-40"
                >
                  + {translate(m.address)}
                </button>
              ))}
            </div>
          )}
        </div>
        <ErrorBox msg={error} />
        <div className="flex justify-end gap-2">
          <button type="button" className="btn-ghost" onClick={onClose}>
            {translate("Hủy")}
          </button>
          <button className="btn-primary" disabled={busy || list.length === 0}>
            {translate(busy ? translate("Đang lưu…") : translate("Lưu"))}
          </button>
        </div>
      </form>
    </Modal>
  );
}

function DomainView({
  id,
  status,
  manager,
  onChanged,
  onDeleted,
}: {
  id: number;
  status: Status | null;
  manager: boolean;
  onChanged: () => void;
  onDeleted: () => void;
}) {
  const [d, setD] = useState<Detail | null>(null);
  const [tab, setTab] = useState<Tab>("boxes");
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");
  const [boxDialog, setBoxDialog] = useState<{ box: Mailbox | null } | null>(
    null,
  );
  const [aliasDialog, setAliasDialog] = useState<{
    alias: Alias | null;
  } | null>(null);
  const [creds, setCreds] = useState<{
    address: string;
    password: string;
  } | null>(null);

  const load = useCallback(() => {
    setError("");
    api<Detail>(`/mail/domains/${id}`)
      .then(setD)
      .catch((e) => setError(errMsg(e)));
  }, [id]);
  useEffect(() => {
    setD(null);
    load();
  }, [load]);

  async function run(fn: () => Promise<unknown>) {
    setError("");
    try {
      await fn();
      load();
      onChanged();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  const host = status?.hostname || `mail.${d?.domain ?? ""}`;
  const boxes = useMemo(
    () =>
      (d?.mailbox_list ?? []).filter((m) =>
        m.address.includes(query.toLowerCase()),
      ),
    [d, query],
  );

  if (!d)
    return (
      <div className="rounded-xl border bg-card p-10 text-center text-sm text-muted-foreground">
        {error ? <ErrorBox msg={error} /> : translate("Đang tải…")}
      </div>
    );

  const tabs: { id: Tab; label: string; icon: typeof Inbox; count?: number }[] =
    [
      {
        id: "boxes",
        label: translate("Hộp thư"),
        icon: Inbox,
        count: d.mailbox_list.length,
      },
      {
        id: "aliases",
        label: translate("Chuyển tiếp"),
        icon: Forward,
        count: d.alias_list.length,
      },
      { id: "dns", label: "DNS", icon: Globe },
      {
        id: "clients",
        label: translate("Cài đặt ứng dụng mail"),
        icon: Settings2,
      },
    ];

  return (
    <div className="rounded-xl border bg-card">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b p-5">
        <div className="flex items-center gap-3">
          <span className="flex size-10 items-center justify-center rounded-xl bg-muted">
            <AtSign className="size-5" />
          </span>
          <div>
            <h2 className="text-lg font-semibold">{d.domain}</h2>
            <p className="text-xs text-muted-foreground">
              {translate(d.mailbox_list.length)} {translate("hộp thư ·")}{" "}
              {translate(d.alias_list.length)} {translate("chuyển tiếp")}{" "}
              {translate(
                manager && translate(" · tài khoản {0}", [d.username]),
              )}
            </p>
          </div>
        </div>
        <button
          className="btn-ghost text-xs text-red-600"
          onClick={() => {
            if (
              confirm(
                translate(
                  "Xóa tên miền {0} cùng mọi hộp thư và thư đã lưu? Không thể hoàn tác.",
                  [d.domain],
                ),
              )
            )
              api(`/mail/domains/${d.id}`, { method: "DELETE" })
                .then(onDeleted)
                .catch((e) => setError(errMsg(e)));
          }}
        >
          <Trash2 className="mr-1 inline size-3.5" />{" "}
          {translate("Xóa tên miền")}
        </button>
      </div>

      <div className="flex gap-1 overflow-x-auto border-b px-3">
        {tabs.map((t) => (
          <button
            key={t.id}
            onClick={() => setTab(t.id)}
            className={`flex shrink-0 items-center gap-1.5 border-b-2 px-3 py-2.5 text-sm ${tab === t.id ? "border-foreground font-medium" : "border-transparent text-muted-foreground hover:text-foreground"}`}
          >
            <t.icon className="size-4" />
            {translate(t.label)}
            {t.count !== undefined && (
              <span className="rounded-full bg-muted px-1.5 text-[11px]">
                {translate(t.count)}
              </span>
            )}
          </button>
        ))}
      </div>

      <div className="space-y-4 p-5">
        <ErrorBox msg={error} />

        {tab === "boxes" && (
          <>
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div className="relative">
                <Search className="pointer-events-none absolute left-2.5 top-2.5 size-4 text-muted-foreground" />
                <input
                  className="input h-9 w-56 pl-8"
                  placeholder={translate("Tìm hộp thư…")}
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                />
              </div>
              <button
                className="btn-primary"
                onClick={() => setBoxDialog({ box: null })}
              >
                <Plus className="mr-1 inline size-4" />{" "}
                {translate("Tạo hộp thư")}
              </button>
            </div>
            {d.mailbox_list.length === 0 ? (
              <div className="rounded-xl border border-dashed p-10 text-center">
                <Inbox className="mx-auto mb-2 size-8 text-muted-foreground" />
                <p className="font-medium">
                  {translate("Chưa có hộp thư nào")}
                </p>
                <p className="mb-4 text-sm text-muted-foreground">
                  {translate("Tạo hộp thư đầu tiên, ví dụ info@")}{d.domain}
                </p>
                <button
                  className="btn-primary"
                  onClick={() => setBoxDialog({ box: null })}
                >
                  {translate("Tạo hộp thư")}
                </button>
              </div>
            ) : (
              <div className="overflow-x-auto rounded-xl border">
                <table className="w-full text-left text-sm">
                  <thead className="bg-muted/50 text-xs text-muted-foreground">
                    <tr>
                      <th className="px-4 py-2.5 font-medium">
                        {translate("Địa chỉ")}
                      </th>
                      <th className="px-4 py-2.5 font-medium">
                        {translate("Dung lượng")}
                      </th>
                      <th className="px-4 py-2.5 font-medium">
                        {translate("Ngày tạo")}
                      </th>
                      <th />
                    </tr>
                  </thead>
                  <tbody className="divide-y">
                    {boxes.map((m) => (
                      <tr key={m.id} className="hover:bg-muted/30">
                        <td className="px-4 py-2.5">
                          <div className="flex items-center gap-2.5">
                            <span className="flex size-8 items-center justify-center rounded-full bg-muted text-xs font-semibold uppercase">
                              {m.local_part[0]}
                            </span>
                            <span className="font-medium">{m.address}</span>
                            <CopyButton text={m.address} />
                          </div>
                        </td>
                        <td className="px-4 py-2.5 text-muted-foreground">
                          <UsageCell used={m.used_mb} quota={m.quota_mb} />
                        </td>
                        <td className="px-4 py-2.5 text-muted-foreground">
                          {new Date(m.created_at).toLocaleDateString(
                            localeTag(),
                          )}
                        </td>
                        <td className="px-4 py-2.5">
                          <div className="flex justify-end gap-1">
                            {status?.webmail && (
                              <a
                                className="btn-ghost px-2 py-1 text-xs"
                                href={webmailURL()}
                                target="_blank"
                                rel="noreferrer"
                                title={translate("Mở Webmail")}
                              >
                                <ExternalLink className="size-3.5" />
                              </a>
                            )}
                            <button
                              className="btn-ghost px-2 py-1 text-xs"
                              title={translate("Sửa / đổi mật khẩu")}
                              onClick={() => setBoxDialog({ box: m })}
                            >
                              <Pencil className="mr-1 inline size-3.5" />{" "}
                              {translate("Sửa")}
                            </button>
                            <button
                              className="btn-ghost px-2 py-1 text-xs text-red-600"
                              title={translate("Xóa")}
                              onClick={() => {
                                if (
                                  confirm(
                                    translate(
                                      "Xóa {0} và toàn bộ thư trong hộp?",
                                      [m.address],
                                    ),
                                  )
                                )
                                  run(() =>
                                    api(`/mail/mailboxes/${m.id}`, {
                                      method: "DELETE",
                                    }),
                                  );
                              }}
                            >
                              <Trash2 className="size-3.5" />
                            </button>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </>
        )}

        {tab === "aliases" && (
          <>
            <div className="flex flex-wrap items-center justify-between gap-2">
              <p className="text-sm text-muted-foreground">
                {translate(
                  "Thư gửi tới địa chỉ chuyển tiếp được chuyển sang một hoặc nhiều hộp thư khác (kể cả Gmail…).",
                )}
              </p>
              <button
                className="btn-primary"
                onClick={() => setAliasDialog({ alias: null })}
              >
                <Plus className="mr-1 inline size-4" />{" "}
                {translate("Tạo chuyển tiếp")}
              </button>
            </div>
            {d.alias_list.length === 0 ? (
              <div className="rounded-xl border border-dashed p-10 text-center text-sm text-muted-foreground">
                <Forward className="mx-auto mb-2 size-8" />{" "}
                {translate("Chưa có chuyển tiếp nào.")}
              </div>
            ) : (
              <div className="divide-y rounded-xl border">
                {d.alias_list.map((a) => (
                  <div
                    key={a.id}
                    className="flex flex-wrap items-center justify-between gap-3 px-4 py-3"
                  >
                    <div className="min-w-0">
                      <div className="font-medium">{a.address}</div>
                      <div className="mt-1 flex flex-wrap gap-1.5">
                        {a.destinations.map((x) => (
                          <span
                            key={x}
                            className="rounded-full bg-muted px-2.5 py-0.5 text-xs"
                          >
                            → {x}
                          </span>
                        ))}
                      </div>
                    </div>
                    <div className="flex gap-1">
                      <button
                        className="btn-ghost px-2 py-1 text-xs"
                        onClick={() => setAliasDialog({ alias: a })}
                      >
                        <Pencil className="mr-1 inline size-3.5" />{" "}
                        {translate("Sửa")}
                      </button>
                      <button
                        className="btn-ghost px-2 py-1 text-xs text-red-600"
                        onClick={() => {
                          if (
                            confirm(
                              translate("Xóa chuyển tiếp {0}?", [a.address]),
                            )
                          )
                            run(() =>
                              api(`/mail/aliases/${a.id}`, {
                                method: "DELETE",
                              }),
                            );
                        }}
                      >
                        <Trash2 className="size-3.5" />
                      </button>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </>
        )}

        {tab === "dns" && (
          <>
            <p className="text-sm text-muted-foreground">
              {translate(
                "Tạo các bản ghi sau tại nơi quản lý tên miền (Cloudflare, nhà đăng ký…). MX để nhận thư; SPF, DKIM, DMARC để thư gửi đi không vào spam.",
              )}
            </p>
            <div className="space-y-2">
              {d.dns.map((r) => (
                <div key={r.type + r.name} className="rounded-xl border p-3">
                  <div className="mb-1.5 flex flex-wrap items-center gap-2 text-xs">
                    <span className="rounded-md bg-foreground px-2 py-0.5 font-semibold text-background">
                      {translate(r.type)}
                    </span>
                    {r.priority ? (
                      <span className="text-muted-foreground">
                        {translate("ưu tiên")} {r.priority}
                      </span>
                    ) : null}
                    <span className="font-mono">{r.name}</span>
                    <CopyButton text={r.name} />
                    <span className="text-muted-foreground">
                      {translate(
                        r.type === "MX"
                          ? translate("nhận thư")
                          : r.name.startsWith("_dmarc")
                            ? "DMARC"
                            : r.name.includes("_domainkey")
                              ? translate("DKIM (chữ ký)")
                              : r.type === "A"
                                ? translate("máy chủ thư")
                                : "SPF",
                      )}
                    </span>
                  </div>
                  <div className="flex items-start justify-between gap-2 rounded-lg bg-muted/50 px-3 py-2">
                    <code className="break-all text-xs">{r.value}</code>
                    <CopyButton text={r.value} />
                  </div>
                </div>
              ))}
            </div>
          </>
        )}

        {tab === "clients" && (
          <div className="grid gap-3 md:grid-cols-2">
            {[
              {
                t: translate("IMAP (khuyên dùng)"),
                rows: [
                  [translate("Máy chủ"), host],
                  [translate("Cổng"), "993"],
                  [translate("Bảo mật"), "SSL/TLS"],
                ],
              },
              {
                t: translate("SMTP (gửi thư)"),
                rows: [
                  [translate("Máy chủ"), host],
                  [
                    translate("Cổng"),
                    translate("465 (SSL/TLS) hoặc 587 (STARTTLS)"),
                  ],
                  [translate("Xác thực"), translate("Có")],
                ],
              },
              {
                t: "POP3",
                rows: [
                  [translate("Máy chủ"), host],
                  [translate("Cổng"), "995"],
                  [translate("Bảo mật"), "SSL/TLS"],
                ],
              },
              {
                t: translate("Đăng nhập"),
                rows: [
                  [
                    translate("Tên đăng nhập"),
                    translate("địa chỉ email đầy đủ"),
                  ],
                  [translate("Mật khẩu"), translate("mật khẩu hộp thư")],
                  [
                    "Webmail",
                    status?.webmail ? webmailURL() : translate("chưa cài"),
                  ],
                ],
              },
            ].map((c) => (
              <div key={c.t} className="rounded-xl border p-4">
                <div className="mb-2 flex items-center gap-2 font-medium">
                  <Server className="size-4" />
                  {translate(c.t)}
                </div>
                <dl className="space-y-1 text-sm">
                  {c.rows.map(([k, v]) => (
                    <div key={k} className="flex justify-between gap-3">
                      <dt className="text-muted-foreground">{k}</dt>
                      <dd className="text-right font-mono text-xs">{v}</dd>
                    </div>
                  ))}
                </dl>
              </div>
            ))}
            <p className="text-xs text-muted-foreground md:col-span-2">
              {translate(
                "Website (WordPress, form liên hệ…) gửi thư qua SMTP cổng 587 bằng một hộp thư ở trên. Hàm mail()/sendmail của PHP bị tắt để tài khoản này không thể mạo danh địa chỉ của tài khoản khác.",
              )}
            </p>
          </div>
        )}
      </div>

      {boxDialog && (
        <MailboxDialog
          d={d}
          box={boxDialog.box}
          onClose={() => setBoxDialog(null)}
          onSaved={(address, password) => {
            setBoxDialog(null);
            load();
            onChanged();
            if (password) setCreds({ address, password });
          }}
        />
      )}
      {aliasDialog && (
        <AliasDialog
          d={d}
          alias={aliasDialog.alias}
          onClose={() => setAliasDialog(null)}
          onSaved={() => {
            setAliasDialog(null);
            load();
            onChanged();
          }}
        />
      )}
      {creds && (
        <Credentials {...creds} host={host} onClose={() => setCreds(null)} />
      )}
    </div>
  );
}

export default function MailPage() {
  const me = useSession();
  const manager = me.role !== "user";
  const [status, setStatus] = useState<Status | null>(null);
  const [list, setList] = useState<MailDomain[] | null>(null);
  const [current, setCurrent] = useState<number | null>(null);
  const [adding, setAdding] = useState(false);
  const [query, setQuery] = useState("");
  const [error, setError] = useState("");

  const load = useCallback(() => {
    api<MailDomain[]>("/mail/domains")
      .then((l) => {
        setList(l);
        setCurrent((c) =>
          c !== null && l.some((d) => d.id === c) ? c : (l[0]?.id ?? null),
        );
      })
      .catch((e) => setError(errMsg(e)));
  }, []);
  useEffect(() => {
    load();
    api<Status>("/mail/status")
      .then(setStatus)
      .catch(() => setStatus(null));
  }, [load]);

  const shown = (list ?? []).filter(
    (d) =>
      d.domain.includes(query.toLowerCase()) ||
      d.username.includes(query.toLowerCase()),
  );
  const notInstalled = status !== null && !status.installed;

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Email</h1>
          <p className="text-sm text-muted-foreground">
            {translate("Hộp thư theo tên miền, chuyển tiếp và bản ghi DNS")}{" "}
            {status?.hostname && (
              <>
                {" "}
                {translate("· máy chủ thư")} <b>{status.hostname}</b>
              </>
            )}
          </p>
        </div>
        <div className="flex gap-2">
          {status?.webmail && (
            <a
              className="btn-ghost"
              href={webmailURL()}
              target="_blank"
              rel="noreferrer"
            >
              <Mail className="mr-1.5 inline size-4" />{" "}
              {translate("Mở Webmail")}
            </a>
          )}
          <button
            className="btn-primary"
            disabled={notInstalled}
            onClick={() => setAdding(true)}
          >
            <Plus className="mr-1 inline size-4" /> {translate("Thêm tên miền")}
          </button>
        </div>
      </div>
      <ErrorBox msg={error} />

      {notInstalled ? (
        <div className="rounded-xl border bg-card p-10 text-center">
          <Mail className="mx-auto mb-3 size-10 text-muted-foreground" />
          <h2 className="text-lg font-semibold">
            {translate("Mail server chưa được cài")}
          </h2>
          <p className="mx-auto mt-1 max-w-md text-sm text-muted-foreground">
            {translate(
              me.role === "admin"
                ? translate(
                    'Vào trang Applications cài "Mail server" (Postfix, Dovecot, DKIM) và "Roundcube Webmail", sau đó quay lại đây để tạo hộp thư.',
                  )
                : translate(
                    "Quản trị viên máy chủ cần cài Mail server trước. Hãy liên hệ quản trị viên.",
                  ),
            )}
          </p>
          {me.role === "admin" && (
            <a className="btn-primary mt-4 inline-block" href="/applications/">
              {translate("Đến trang Applications")}
            </a>
          )}
        </div>
      ) : list?.length === 0 ? (
        <div className="rounded-xl border bg-card p-10 text-center">
          <AtSign className="mx-auto mb-3 size-10 text-muted-foreground" />
          <h2 className="text-lg font-semibold">
            {translate("Chưa có tên miền email")}
          </h2>
          <p className="mx-auto mt-1 max-w-md text-sm text-muted-foreground">
            {translate(
              "Thêm tên miền (ví dụ congty.vn) để tạo hộp thư như info@congty.vn.",
            )}
          </p>
          <button className="btn-primary mt-4" onClick={() => setAdding(true)}>
            <Plus className="mr-1 inline size-4" /> {translate("Thêm tên miền")}
          </button>
        </div>
      ) : (
        <div className="grid items-start gap-6 lg:grid-cols-[300px_1fr]">
          <div className="rounded-xl border bg-card">
            <div className="border-b p-3">
              <div className="relative">
                <Search className="pointer-events-none absolute left-2.5 top-2.5 size-4 text-muted-foreground" />
                <input
                  className="input h-9 pl-8"
                  placeholder={translate("Tìm tên miền…")}
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                />
              </div>
            </div>
            <div className="max-h-[70vh] overflow-y-auto p-2">
              {list === null && (
                <div className="p-4 text-sm text-muted-foreground">
                  {translate("Đang tải…")}
                </div>
              )}
              {shown.map((d) => (
                <button
                  key={d.id}
                  onClick={() => setCurrent(d.id)}
                  className={`flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-left ${current === d.id ? "bg-foreground text-background" : "hover:bg-muted"}`}
                >
                  <span
                    className={`flex size-9 shrink-0 items-center justify-center rounded-lg text-sm font-semibold uppercase ${current === d.id ? "bg-background/15" : "bg-muted"}`}
                  >
                    {translate(d.domain[0])}
                  </span>
                  <span className="min-w-0">
                    <span className="block truncate font-medium">
                      {d.domain}
                    </span>
                    <span
                      className={`block text-xs ${current === d.id ? "text-background/70" : "text-muted-foreground"}`}
                    >
                      {translate(d.mailboxes)} {translate("hộp thư ·")}{" "}
                      {translate(d.aliases)} {translate("chuyển tiếp")}{" "}
                      {translate(manager && ` · ${d.username}`)}
                    </span>
                  </span>
                </button>
              ))}
              {list && shown.length === 0 && (
                <div className="p-4 text-sm text-muted-foreground">
                  {translate("Không tìm thấy.")}
                </div>
              )}
            </div>
          </div>
          {current !== null && (
            <DomainView
              key={current}
              id={current}
              status={status}
              manager={manager}
              onChanged={load}
              onDeleted={() => {
                setCurrent(null);
                load();
              }}
            />
          )}
        </div>
      )}

      {adding && (
        <AddDomain
          manager={manager}
          onClose={() => setAdding(false)}
          onDone={(id) => {
            setAdding(false);
            setCurrent(id);
            load();
          }}
        />
      )}
    </div>
  );
}

function UsageCell({ used, quota }: { used?: number; quota: number }) {
  const label = quotaLabel(quota);
  if (used === undefined) return <>{label}</>;
  const pct = quota > 0 ? Math.min(100, (used / quota) * 100) : 0;
  return (
    <div className="min-w-32 space-y-1">
      <div className="text-xs">
        {used < 10 ? used.toFixed(1) : Math.round(used)} MB / {label}
      </div>
      {quota > 0 && (
        <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
          <div
            className={`h-full rounded-full ${pct >= 90 ? "bg-red-500" : pct >= 70 ? "bg-amber-500" : "bg-emerald-500"}`}
            style={{ width: `${pct}%` }}
          />
        </div>
      )}
    </div>
  );
}
