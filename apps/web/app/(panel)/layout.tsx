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
import { NotificationBell } from "@/components/notifications";
import {
  Activity,
  BarChart3,
  Braces,
  BrickWall,
  ChevronRight,
  Clock3,
  Database,
  FileCode2,
  FolderOpen,
  Globe2,
  HardDriveDownload,
  LayoutDashboard,
  LayoutGrid,
  LogOut,
  Mail,
  Menu,
  Package,
  PanelLeftClose,
  PanelLeftOpen,
  RefreshCw,
  ScrollText,
  Server,
  Settings2,
  ShieldAlert,
  ShieldCheck,
  Users,
  type LucideIcon,
} from "lucide-react";

const ALL: Role[] = ["admin", "reseller", "user"];

type Group = "Dịch vụ" | "Quản lý" | "Hệ thống";
const GROUPS: Group[] = ["Dịch vụ", "Quản lý", "Hệ thống"];

type NavItem = {
  href: string;
  label: string; // Vietnamese source text, translated at render time
  roles: Role[];
  icon: LucideIcon;
  group: Group;
};

const NAV: NavItem[] = [
  {
    href: "/dashboard/",
    label: "Tổng quan",
    roles: ALL,
    icon: LayoutDashboard,
    group: "Dịch vụ",
  },
  {
    href: "/sites/",
    label: "Website",
    roles: ALL,
    icon: Globe2,
    group: "Dịch vụ",
  },
  {
    href: "/ssl/",
    label: "Chứng chỉ SSL",
    roles: ALL,
    icon: ShieldCheck,
    group: "Dịch vụ",
  },
  { href: "/mail/", label: "Email", roles: ALL, icon: Mail, group: "Dịch vụ" },
  {
    href: "/databases/",
    label: "Database",
    roles: ALL,
    icon: Database,
    group: "Dịch vụ",
  },
  {
    href: "/files/",
    label: "Quản lý file",
    roles: ALL,
    icon: FolderOpen,
    group: "Dịch vụ",
  },
  {
    href: "/ftp/",
    label: "FTP / SFTP",
    roles: ALL,
    icon: FileCode2,
    group: "Dịch vụ",
  },
  {
    href: "/cron/",
    label: "Lịch chạy Cron",
    roles: ALL,
    icon: Clock3,
    group: "Dịch vụ",
  },
  {
    href: "/applications/",
    label: "Ứng dụng",
    roles: ["admin"],
    icon: LayoutGrid,
    group: "Quản lý",
  },
  {
    href: "/php/",
    label: "PHP",
    roles: ["admin"],
    icon: Braces,
    group: "Quản lý",
  },
  {
    href: "/users/",
    label: "Tài khoản",
    roles: ["admin", "reseller"],
    icon: Users,
    group: "Quản lý",
  },
  {
    href: "/packages/",
    label: "Gói dịch vụ",
    roles: ["admin", "reseller"],
    icon: Package,
    group: "Quản lý",
  },
  {
    href: "/firewall/",
    label: "Tường lửa",
    roles: ["admin"],
    icon: BrickWall,
    group: "Quản lý",
  },
  {
    href: "/malware/",
    label: "Quét mã độc",
    roles: ALL,
    icon: ShieldAlert,
    group: "Hệ thống",
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
    href: "/update/",
    label: "Cập nhật",
    roles: ["admin"],
    icon: RefreshCw,
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

const COLLAPSE_KEY = "xpanel.sidebar.collapsed";

function samePath(a: string, b: string) {
  return a.replace(/\/$/, "") === b.replace(/\/$/, "");
}

/** One menu entry; icon-only with a tooltip when the sidebar is collapsed. */
function NavLink({
  item,
  selected,
  collapsed,
}: {
  item: NavItem;
  selected: boolean;
  collapsed: boolean;
}) {
  const label = translate(item.label);
  const cls = `group relative flex h-9 items-center gap-3 rounded-md text-[13px] transition-colors ${collapsed ? "justify-center px-0" : "px-3"} ${selected ? "bg-foreground/[.06] font-medium text-foreground" : "text-muted-foreground hover:bg-muted hover:text-foreground"}`;
  const inner = (
    <>
      {selected && (
        <span className="absolute inset-y-1.5 left-0 w-0.5 rounded-full bg-foreground" />
      )}
      <item.icon className="size-4 shrink-0" strokeWidth={1.7} />
      {!collapsed && <span className="truncate">{label}</span>}
    </>
  );
  if (!collapsed)
    return (
      <Link
        href={item.href}
        aria-current={selected ? "page" : undefined}
        className={cls}
      >
        {inner}
      </Link>
    );
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Link
            href={item.href}
            aria-current={selected ? "page" : undefined}
            aria-label={label}
            className={cls}
          />
        }
      >
        {inner}
      </TooltipTrigger>
      <TooltipContent side="right">{label}</TooltipContent>
    </Tooltip>
  );
}

function Sidebar({
  user,
  nav,
  current,
  collapsed,
  onToggle,
}: {
  user: Me;
  nav: NavItem[];
  current?: NavItem;
  collapsed: boolean;
  onToggle?: () => void;
}) {
  return (
    <>
      <div
        className={`flex h-16 shrink-0 items-center border-b ${collapsed ? "justify-center px-2" : "gap-3 px-4"}`}
      >
        <Link
          href="/dashboard/"
          className="flex min-w-0 flex-1 items-center gap-3"
          aria-label="X-Panel"
        >
          <div className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-foreground text-sm font-bold text-background">
            X
          </div>
          {!collapsed && (
            <div className="min-w-0">
              <span className="block text-sm font-semibold tracking-tight">
                X-Panel
              </span>
              <span className="block truncate text-[11px] text-muted-foreground">
                {translate("Bảng điều khiển hosting")}
                {user.panel_version ? ` · ${user.panel_version}` : ""}
              </span>
            </div>
          )}
        </Link>
        {onToggle && !collapsed && (
          <Button
            size="icon"
            variant="ghost"
            className="size-8 shrink-0 text-muted-foreground"
            aria-label={translate("Thu gọn menu")}
            title={translate("Thu gọn menu")}
            onClick={onToggle}
          >
            <PanelLeftClose className="size-4" />
          </Button>
        )}
      </div>
      <nav
        className={`flex-1 overflow-y-auto py-3 ${collapsed ? "px-2" : "px-3"}`}
        aria-label={translate("Điều hướng chính")}
      >
        {GROUPS.map((group, i) => {
          const items = nav.filter((n) => n.group === group);
          if (!items.length) return null;
          return (
            <div key={group} className={i ? "mt-4" : ""}>
              {collapsed ? (
                i > 0 && <div className="mx-2 mb-3 border-t" />
              ) : (
                <div className="mb-1.5 px-3 text-[10px] font-semibold tracking-[0.12em] text-muted-foreground/80 uppercase">
                  {translate(group)}
                </div>
              )}
              <div className="space-y-0.5">
                {items.map((n) => (
                  <NavLink
                    key={n.href}
                    item={n}
                    selected={current?.href === n.href}
                    collapsed={collapsed}
                  />
                ))}
              </div>
            </div>
          );
        })}
      </nav>
      {onToggle && collapsed && (
        <div className="flex justify-center border-t py-2">
          <Button
            size="icon"
            variant="ghost"
            className="size-8 text-muted-foreground"
            aria-label={translate("Mở rộng menu")}
            title={translate("Mở rộng menu")}
            onClick={onToggle}
          >
            <PanelLeftOpen className="size-4" />
          </Button>
        </div>
      )}
      <Link
        href="/account/"
        title={collapsed ? user.username : undefined}
        className={`flex shrink-0 items-center gap-3 border-t py-3 hover:bg-muted/50 ${collapsed ? "justify-center px-2" : "px-4"}`}
      >
        <div className="flex size-8 shrink-0 items-center justify-center rounded-full bg-muted text-xs font-semibold uppercase">
          {user.username.slice(0, 2)}
        </div>
        {!collapsed && (
          <>
            <div className="min-w-0 flex-1">
              <p className="truncate text-xs font-semibold">{user.username}</p>
              <p className="text-[11px] text-muted-foreground">
                {translate(roleLabel[user.role])}
              </p>
            </div>
            <Settings2 className="size-4 text-muted-foreground" />
          </>
        )}
      </Link>
    </>
  );
}

export default function PanelLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const [user, setUser] = useState<Me | null>(null);
  const [mobile, setMobile] = useState(false);
  const [collapsed, setCollapsed] = useState(false);
  useEffect(() => {
    api<Me>("/me")
      .then(setUser)
      .catch(() => router.replace("/login/"));
  }, [router]);
  useEffect(() => {
    try {
      setCollapsed(localStorage.getItem(COLLAPSE_KEY) === "1");
    } catch {}
  }, []);
  useEffect(() => setMobile(false), [pathname]);
  function toggleCollapsed() {
    setCollapsed((c) => {
      try {
        localStorage.setItem(COLLAPSE_KEY, c ? "0" : "1");
      } catch {}
      return !c;
    });
  }
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
  const current = nav.find((n) => samePath(pathname, n.href));
  return (
    <SessionContext.Provider value={user}>
      <TooltipProvider>
        <div className="flex min-h-screen bg-muted/30">
          <aside
            className={`fixed inset-y-0 left-0 z-30 hidden flex-col border-r bg-sidebar transition-[width] duration-200 lg:flex ${collapsed ? "w-16" : "w-60"}`}
          >
            <Sidebar
              user={user}
              nav={nav}
              current={current}
              collapsed={collapsed}
              onToggle={toggleCollapsed}
            />
          </aside>
          <Sheet open={mobile} onOpenChange={setMobile}>
            <SheetContent side="left" className="flex w-64 flex-col gap-0 p-0">
              <SheetTitle className="sr-only">Menu X-Panel</SheetTitle>
              <Sidebar
                user={user}
                nav={nav}
                current={current}
                collapsed={false}
              />
            </SheetContent>
          </Sheet>
          <div
            className={`flex min-w-0 flex-1 flex-col transition-[padding] duration-200 ${collapsed ? "lg:pl-16" : "lg:pl-60"}`}
          >
            {user.impersonator && (
              <div className="flex flex-wrap items-center justify-between gap-2 bg-amber-100 px-6 py-2 text-xs text-amber-950">
                <span>
                  {translate("Đang đăng nhập thay")} <b>{user.username}</b>{" "}
                  {translate("bởi")} <b>{user.impersonator.username}</b>
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
              <div className="flex min-w-0 items-center gap-2 sm:gap-3">
                <Button
                  size="icon"
                  variant="ghost"
                  className="lg:hidden"
                  aria-label={translate("Mở menu")}
                  onClick={() => setMobile(true)}
                >
                  <Menu />
                </Button>
                {current && (
                  <>
                    <span className="hidden text-xs text-muted-foreground sm:inline">
                      {translate(current.group)}
                    </span>
                    <ChevronRight className="hidden size-3 text-muted-foreground sm:block" />
                  </>
                )}
                <span className="min-w-0 truncate text-xs font-medium">
                  {current ? translate(current.label) : "X-Panel"}
                </span>
              </div>
              <div className="flex items-center gap-2 sm:gap-3">
                <LanguageSwitcher />
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
                <NotificationBell />
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
                <span>X-Panel</span>
                <span>
                  · {translate("Một sản phẩm của")}{" "}
                  <a
                    href="https://socsmart.com"
                    target="_blank"
                    rel="noopener noreferrer"
                    className="font-medium hover:text-foreground hover:underline"
                  >
                    socsmart.com
                  </a>
                </span>
              </span>
              <span className="flex items-center gap-1.5">
                <Server className="size-3" />
                {user.username}
              </span>
            </footer>
          </div>
        </div>
        <Operations />
      </TooltipProvider>
    </SessionContext.Provider>
  );
}
