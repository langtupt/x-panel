"use client";
import { useEffect, useState } from "react";
import { api, errMsg, type Site } from "@/lib/api";
import { useUnsavedChanges } from "@/lib/use-unsaved-changes";
import { t } from "@/lib/i18n";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { ErrorBox } from "@/components/ui";
type Config = {
  settings: Record<string, string>;
  workers: number;
  request_timeout: number;
  slow_timeout: number;
};
type Setting = { name: string; value: string; kind: string };
export function SitePHP({
  site,
  admin,
  onClose,
}: {
  site: Site;
  admin: boolean;
  onClose: () => void;
}) {
  const [config, setConfig] = useState<Config | null>(null),
    [original, setOriginal] = useState(""),
    [defaults, setDefaults] = useState<Setting[]>([]),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [search, setSearch] = useState(""),
    [confirm, setConfirm] = useState(false),
    [log, setLog] = useState<string | null>(null),
    [tab, setTab] = useState("settings");
  useEffect(() => {
    let stop = false;
    api<{ config: Config; defaults: Setting[] }>(`/sites/${site.id}/php`)
      .then((r) => {
        if (stop) return;
        const c = { ...r.config, settings: r.config.settings ?? {} };
        setConfig(c);
        setOriginal(JSON.stringify(c));
        setDefaults(r.defaults);
      })
      .catch((e) => setError(errMsg(e)));
    return () => {
      stop = true;
    };
  }, [site.id]);
  const dirty = !!config && JSON.stringify(config) !== original;
  const unsaved = useUnsavedChanges(dirty);
  function close() {
    if (!busy) unsaved.confirmLeave(onClose);
  }
  async function save() {
    if (!config) return;
    setBusy(true);
    setError("");
    setConfirm(false);
    try {
      await api(`/sites/${site.id}/php`, { method: "PUT", body: config });
      setOriginal(JSON.stringify(config));
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setBusy(false);
    }
  }
  async function logs() {
    setTab("logs");
    try {
      setLog((await api<{ log: string }>(`/sites/${site.id}/logs`)).log);
    } catch (e) {
      setError(errMsg(e));
    }
  }
  function value(name: string, value: string) {
    setConfig((c) =>
      c ? { ...c, settings: { ...c.settings, [name]: value } } : c,
    );
  }
  function reset(name: string) {
    setConfig((c) => {
      if (!c) return c;
      const settings = { ...c.settings };
      delete settings[name];
      return { ...c, settings };
    });
  }
  return (
    <>
      {unsaved.dialog}
      <Dialog
        open
        onOpenChange={(open) => {
          if (!open) close();
        }}
      >
        <DialogContent className="sm:max-w-4xl">
          <DialogHeader>
            <DialogTitle>PHP · {site.domain}</DialogTitle>
            <DialogDescription>
              {t(
                "Thiết lập riêng cho website này. Để trống ghi đè để kế thừa cấu hình PHP toàn phiên bản.",
              )}
            </DialogDescription>
          </DialogHeader>
          <ErrorBox msg={error} />
          <div className="flex flex-wrap gap-2">
            <Button
              variant={tab === "settings" ? "secondary" : "ghost"}
              onClick={() => setTab("settings")}
            >
              {t("Thiết lập PHP")}
            </Button>
            <Button
              variant={tab === "fpm" ? "secondary" : "ghost"}
              onClick={() => setTab("fpm")}
            >
              PHP-FPM
            </Button>
            <Button
              variant={tab === "logs" ? "secondary" : "ghost"}
              onClick={() => void logs()}
            >
              {t("Nhật ký PHP / slow log")}
            </Button>
          </div>
          {tab === "logs" ? (
            <pre className="max-h-[55vh] overflow-auto rounded-lg bg-muted p-4 text-xs whitespace-pre-wrap">
              {log ?? t("Đang tải…")}
            </pre>
          ) : !config ? (
            <p>{t("Đang tải…")}</p>
          ) : tab === "fpm" ? (
            <div className="space-y-4">
              <p className="text-sm text-muted-foreground">
                {t(
                  "0 sử dụng mặc định hoặc tắt timeout. Workers thực tế bị giới hạn bởi gói hosting.",
                )}
              </p>
              {(
                [
                  ["workers", "Số worker tối đa", 50],
                  ["request_timeout", "Giới hạn request (giây)", 3600],
                  ["slow_timeout", "Ghi slow log sau (giây)", 3600],
                ] as const
              ).map(([key, label, max]) => (
                <label key={key} className="grid gap-2 text-sm">
                  {t(label)}
                  <input
                    type="number"
                    min={0}
                    max={max}
                    disabled={busy || (key === "workers" && !admin)}
                    className="input"
                    value={config[key] ?? 0}
                    onChange={(e) =>
                      setConfig({ ...config, [key]: Number(e.target.value) })
                    }
                  />
                </label>
              ))}
            </div>
          ) : (
            <>
              <input
                className="input"
                placeholder={t("Tìm kiếm")}
                value={search}
                onChange={(e) => setSearch(e.target.value)}
              />
              <div className="max-h-[50vh] overflow-auto rounded-lg border">
                <table className="w-full text-sm">
                  <thead className="sticky top-0 bg-muted">
                    <tr>
                      <th className="p-3 text-left">{t("Tên")}</th>
                      <th className="p-3 text-left">{t("Giá trị")}</th>
                      <th />
                    </tr>
                  </thead>
                  <tbody className="divide-y">
                    {defaults
                      .filter((s) => s.name.includes(search.toLowerCase()))
                      .map((s) => {
                        const custom = Object.hasOwn(config.settings, s.name);
                        return (
                          <tr key={s.name}>
                            <td className="p-3">
                              <label
                                htmlFor={`site-${s.name}`}
                                className="font-mono text-xs"
                              >
                                {s.name}
                              </label>
                              <p className="mt-1 text-xs text-muted-foreground">
                                {t(custom ? "Tùy chỉnh" : "Kế thừa")}
                              </p>
                            </td>
                            <td className="p-3">
                              {s.kind === "bool" ? (
                                <select
                                  id={`site-${s.name}`}
                                  className="input"
                                  disabled={busy}
                                  value={config.settings[s.name] ?? s.value}
                                  onChange={(e) =>
                                    value(s.name, e.target.value)
                                  }
                                >
                                  <option value="1">On (1)</option>
                                  <option value="0">Off (0)</option>
                                </select>
                              ) : (
                                <input
                                  id={`site-${s.name}`}
                                  className="input"
                                  disabled={busy}
                                  value={config.settings[s.name] ?? s.value}
                                  onChange={(e) =>
                                    value(s.name, e.target.value)
                                  }
                                />
                              )}
                            </td>
                            <td className="p-3">
                              <Button
                                variant="ghost"
                                size="sm"
                                disabled={busy || !custom}
                                onClick={() => reset(s.name)}
                              >
                                {t("Kế thừa")}
                              </Button>
                            </td>
                          </tr>
                        );
                      })}
                  </tbody>
                </table>
              </div>
            </>
          )}
          {confirm && (
            <p className="rounded-lg border p-3 text-sm">
              {t("Lưu và khởi động lại PHP của website này?")}
            </p>
          )}
          <div className="flex justify-end gap-2">
            <Button variant="outline" disabled={busy} onClick={close}>
              {t("Đóng")}
            </Button>
            {confirm ? (
              <>
                <Button variant="ghost" onClick={() => setConfirm(false)}>
                  {t("Hủy")}
                </Button>
                <Button disabled={busy} onClick={() => void save()}>
                  {t("Xác nhận")}
                </Button>
              </>
            ) : (
              <Button
                disabled={!dirty || busy}
                onClick={() => setConfirm(true)}
              >
                {t("Lưu thay đổi")}
              </Button>
            )}
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}
