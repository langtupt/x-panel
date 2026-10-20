"use client";
import { LanguageSwitcher } from "@/components/language-provider";
import { t as translate } from "@/lib/i18n";

import { useEffect, useState } from "react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { api, roleLabel, type Me, type Role } from "@/lib/api";
import { SessionContext } from "./session";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Operations } from "@/components/operations";
import {
  Activity,
  BarChart3,
  Blocks,
  ChevronRight,
  Clock3,
  Database,
  FileCode2,
  FolderOpen,
  Globe2,
  HardDriveDownload,
  LayoutDashboard,
  LogOut,
  Mail,
  Menu,
  Package,
  ScrollText,
  Server,
  Settings2,
  ShieldCheck,
  Users,
  type LucideIcon,
} from "lucide-react";

const ALL: Role[] = ["admin", "reseller", "user"];
type NavItem = {
  href: string;
  label: string;
  roles: Role[];
  icon: LucideIcon;
  group: string;
};
const NAV: NavItem[] = [
  {
    href: "/dashboard/",
    label: "Tổng quan",
    roles: ALL,
    icon: LayoutDashboard,
    group: "Workspace",
  },
  {
    href: "/sites/",
    label: "Website",
    roles: ALL,
    icon: Globe2,
    group: "Workspace",
  },
  {
    href: "/ssl/",
    label: "Chứng chỉ SSL",
    roles: ALL,
    icon: ShieldCheck,
    group: "Workspace",
  },
  {
    href: "/mail/",
    label: "Email",
    roles: ALL,
    icon: Mail,
    group: "Workspace",
  },
  {
    href: "/databases/",
    label: "Database",
    roles: ALL,
    icon: Database,
    group: "Workspace",
  },
  {
    href: "/files/",
    label: "Quản lý file",
    roles: ALL,
    icon: FolderOpen,
    group: "Workspace",
  },
  {
    href: "/ftp/",
    label: "FTP / SFTP",
    roles: ALL,
    icon: FileCode2,
    group: "Workspace",
  },
  {
    href: "/cron/",
    label: "Lịch chạy Cron",
    roles: ALL,
    icon: Clock3,
    group: "Workspace",
  },
  {
    href: "/php/", label: "PHP", roles: ["admin"], icon: Blocks, group: "Quản trị",
  },
  {
    href: "/applications/",
    label: "Applications",
    roles: ["admin"],
    icon: Blocks,
    group: "Quản trị",
  },
  {
    href: "/users/",
    label: "Tài khoản",
    roles: ["admin", "reseller"],
    icon: Users,
    group: "Quản trị",
  },
  {
    href: "/packages/",
    label: "Gói dịch vụ",
    roles: ["admin", "reseller"],
    icon: Package,
    group: "Quản trị",
  },
  {
    href: "/backups/",
    label: "Sao lưu",
    roles: ALL,
    icon: HardDriveDownload,
    group: "Hệ thống",
  },
  {
    href: "/stats/",
    label: "Thống kê tài nguyên",
    roles: ALL,
    icon: BarChart3,
    group: "Hệ thống",
  },
  {
    href: "/tasks/",
    label: "Tác vụ",
    roles: ALL,
    icon: Activity,
    group: "Hệ thống",
  },
  {
    href: "/audit/",
    label: "Nhật ký hoạt động",
    roles: ALL,
    icon: ScrollText,
    group: "Hệ thống",
  },
  {
    href: "/account/",
    label: "Cài đặt tài khoản",
    roles: ALL,
    icon: Settings2,
    group: "Hệ thống",
  },
];
export default function PanelLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const [user, setUser] = useState<Me | null>(null);
  const [mobile, setMobile] = useState(false);
  useEffect(() => {
    api<Me>("/me")
      .then(setUser)
      .catch(() => router.replace("/login/"));
  }, [router]);
  useEffect(() => setMobile(false), [pathname]);
  async function logout() {
    await api("/auth/logout", { method: "POST" }).catch(() => {});
    router.replace("/login/");
  }
  async function stopImpersonating() {
    try {
      await api("/auth/unimpersonate", { method: "POST" });
      window.location.href = "/users/";
    } catch {
      router.replace("/login/");
    }
  }
  if (!user)
    return (
      <div className="flex min-h-screen items-center justify-center gap-3 bg-muted/30 text-sm text-muted-foreground">
        <Activity className="size-5 animate-pulse" />
        {translate("Đang tải workspace…")}
      </div>
    );
  const nav = NAV.filter((n) => n.roles.includes(user.role));
  const current = nav.find(
    (n) => pathname.replace(/\/$/, "") === n.href.replace(/\/$/, ""),
  );
  const navigation = (
    <>
      <Link href="/dashboard/" className="flex h-20 items-center gap-3 px-5">
        <div className="flex size-9 items-center justify-center rounded-xl bg-primary text-lg font-bold text-primary-foreground">
          X
        </div>
        <div>
          <span className="font-semibold tracking-tight">X-Panel</span>
          <p className="text-[11px] text-muted-foreground">Hosting workspace</p>
        </div>
      </Link>
      <nav
        className="flex-1 space-y-6 overflow-y-auto px-3 py-3"
        aria-label={translate("Điều hướng chính")}
      >
        {["Workspace", "Quản trị", "Hệ thống"].map((group) => {
          const items = nav.filter((n) => n.group === group);
          return (
            items.length > 0 && (
              <div key={group}>
                <div className="mb-2 px-3 text-[10px] font-semibold tracking-[0.12em] text-muted-foreground uppercase">
                  {translate(group)}
                </div>
                <div className="space-y-1">
                  {items.map((n) => {
                    const selected = current?.href === n.href;
                    return (
                      <Link
                        key={n.href}
                        href={n.href}
                        aria-current={selected ? "page" : undefined}
                        className={`flex items-center gap-3 rounded-lg px-3 py-2.5 text-[13px] transition-colors ${selected ? "bg-primary font-medium text-primary-foreground shadow-xs" : "text-muted-foreground hover:bg-muted hover:text-foreground"}`}
                      >
                        <n.icon className="size-4" strokeWidth={1.6} />
                        {translate(n.label)}
                        {selected && (
                          <ChevronRight className="ml-auto size-3.5 opacity-60" />
                        )}
                      </Link>
                    );
                  })}
                </div>
              </div>
            )
          );
        })}
      </nav>
      <Link
        href="/account/"
        className="flex items-center gap-3 border-t px-5 py-4"
      >
        <div className="flex size-8 items-center justify-center rounded-full bg-muted text-xs font-semibold uppercase">
          {translate(user.username.slice(0, 2))}
        </div>
        <div className="min-w-0 flex-1">
          <p className="truncate text-xs font-semibold">{user.username}</p>
          <p className="text-[11px] text-muted-foreground">
            {translate(roleLabel[user.role])}
          </p>
        </div>
        <Settings2 className="size-4 text-muted-foreground" />
      </Link>
    </>
  );
  return (
    <SessionContext.Provider value={user}>
      <TooltipProvider>
        <div className="flex min-h-screen bg-muted/30">
          <aside className="fixed inset-y-0 left-0 z-30 hidden w-60 flex-col border-r bg-sidebar lg:flex">
            {translate(navigation)}
          </aside>
          <Sheet open={mobile} onOpenChange={setMobile}>
            <SheetContent side="left" className="flex w-64 flex-col gap-0 p-0">
              <SheetTitle className="sr-only">Menu X-Panel</SheetTitle>
              {translate(navigation)}
            </SheetContent>
          </Sheet>
          <div className="flex min-w-0 flex-1 flex-col lg:pl-60">
            {user.impersonator && (
              <div className="flex flex-wrap items-center justify-between gap-2 bg-amber-100 px-6 py-2 text-xs text-amber-950">
                <span>
                  {translate("Đang đăng nhập thay")} <b>{user.username}</b>{" "}
                  {translate("bởi")} {translate(" ")}
                  {translate(user.impersonator.username)}
                </span>
                <button
                  onClick={stopImpersonating}
                  className="font-semibold underline"
                >
                  {translate("Quay lại tài khoản quản trị")}
                </button>
              </div>
            )}
            <header className="sticky top-0 z-20 flex h-16 items-center justify-between gap-3 border-b bg-background/95 px-5 backdrop-blur-md md:px-8">
              <div className="flex items-center gap-2 sm:gap-3">
                <Button
                  size="icon"
                  variant="ghost"
                  className="lg:hidden"
                  aria-label={translate("Mở menu")}
                  onClick={() => setMobile(true)}
                >
                  <Menu />
                </Button>
                <span className="hidden text-xs text-muted-foreground sm:inline">
                  Workspace
                </span>
                <ChevronRight className="hidden size-3 text-muted-foreground sm:block" />
                <span className="min-w-0 truncate text-xs font-medium">
                  {translate(current?.label ?? "X-Panel")}
                </span>
              </div>
              <div className="flex items-center gap-2 sm:gap-3">
                <LanguageSwitcher />
                <span className="hidden items-center gap-2 rounded-full border px-2.5 py-1 text-[10px] font-medium text-muted-foreground sm:flex">
                  <span className="size-1.5 rounded-full bg-emerald-500" />
                  {translate(roleLabel[user.role])}
                </span>
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <Link
                        href="/tasks/"
                        className="flex size-8 items-center justify-center rounded-lg hover:bg-muted"
                      />
                    }
                  >
                    <Activity className="size-4" />
                    <span className="sr-only">{translate("Tác vụ")}</span>
                  </TooltipTrigger>
                  <TooltipContent>
                    {translate("Trung tâm tác vụ")}
                  </TooltipContent>
                </Tooltip>
                <div className="h-5 w-px bg-border" />
                <Button variant="ghost" size="sm" onClick={logout}>
                  <LogOut className="size-3.5" />
                  <span className="hidden sm:inline">
                    {translate("Đăng xuất")}
                  </span>
                </Button>
              </div>
            </header>
            <main className="panel-main flex-1">{children}</main>
            <footer className="flex flex-wrap items-center justify-between gap-2 border-t px-5 py-4 text-[10px] text-muted-foreground md:px-8">
              <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
                <span>X-Panel · Hosting control panel</span>
                <span>· {translate("Một sản phẩm của")} <a href="https://socsmart.com" target="_blank" rel="noopener noreferrer" className="font-medium hover:text-foreground hover:underline">socsmart.com</a></span>
              </span>
              <span className="flex items-center gap-1.5">
                <Server className="size-3" />
                Workspace / {user.username}
              </span>
            </footer>
          </div>
        </div>
        <Operations />
      </TooltipProvider>
    </SessionContext.Provider>
  );
}
