"use client";
import { useCallback, useEffect, useState, useRef } from "react";
import { t } from "@/lib/i18n";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
export function useUnsavedChanges(dirty: boolean) {
  const leaving = useRef(false);
  const [pending, setPending] = useState<null | (() => void)>(null);
  const confirmLeave = useCallback(
    (action: () => void) => {
      if (dirty) setPending(() => action);
      else action();
    },
    [dirty],
  );
  useEffect(() => {
    leaving.current = false;
    if (!dirty) return;
    const unload = (e: BeforeUnloadEvent) => {
      if (leaving.current) return;
      e.preventDefault();
      e.returnValue = "";
    };
    const language = (e: Event) => {
      e.preventDefault();
      setPending(
        () => (e as CustomEvent<{ proceed: () => void }>).detail.proceed,
      );
    };
    const click = (e: MouseEvent) => {
      const a = (e.target as Element).closest(
        "a[href]",
      ) as HTMLAnchorElement | null;
      if (!a || a.target === "_blank" || a.href === location.href) return;
      e.preventDefault();
      e.stopPropagation();
      setPending(() => () => location.assign(a.href));
    };
    window.addEventListener("beforeunload", unload);
    window.addEventListener("xpanel:language-change", language);
    document.addEventListener("click", click, true);
    return () => {
      window.removeEventListener("beforeunload", unload);
      window.removeEventListener("xpanel:language-change", language);
      document.removeEventListener("click", click, true);
    };
  }, [dirty]);
  return {
    confirmLeave,
    dialog: (
      <Dialog
        open={!!pending}
        onOpenChange={(open) => {
          if (!open) setPending(null);
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("Thay đổi chưa được lưu")}</DialogTitle>
            <DialogDescription>
              {t("Rời trang sẽ bỏ các thay đổi bạn chưa lưu.")}
            </DialogDescription>
          </DialogHeader>
          <div className="flex justify-end gap-2">
            <Button variant="outline" onClick={() => setPending(null)}>
              {t("Tiếp tục chỉnh sửa")}
            </Button>
            <Button
              variant="destructive"
              onClick={() => {
                const action = pending;
                leaving.current = true;
                setPending(null);
                action?.();
              }}
            >
              {t("Bỏ thay đổi")}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    ),
  };
}
