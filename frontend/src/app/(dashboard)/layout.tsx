"use client";

import Link from "next/link";
import { LayoutDashboard, Network, Settings, Server, Cpu, Link2, LogOut } from "lucide-react";
import { usePathname, useRouter } from "next/navigation";
import { useState, useEffect } from "react";
import { ChevronRight, ChevronLeft } from "lucide-react";
import { useAuth, auth } from "@/lib/auth";
import { Logo } from "@/components/Logo";
import { ActivationBanner } from "@/components/ActivationBanner";

export default function DashboardLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const pathname = usePathname();
  const router = useRouter();
  const [isCollapsed, setIsCollapsed] = useState(true);
  const { isAuthenticated, logout } = useAuth();
  const [orgName, setOrgName] = useState<string | null>("");

  useEffect(() => {
    if (isAuthenticated === false) {
      router.push("/login");
    }
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setOrgName(auth.getOrgName());
  }, [isAuthenticated, router]);

  // Don't render until we confirm authentication
  if (isAuthenticated === null || isAuthenticated === false) {
    return null; 
  }

  const navItems = [
    { name: "Command Center", href: "/", icon: LayoutDashboard },
    { name: "Topology", href: "/topology", icon: Network },
    { name: "Fleet", href: "/fleet", icon: Server },
    { name: "Models", href: "/models", icon: Cpu },
    { name: "Integrations", href: "/integrations", icon: Link2 },
    { name: "Settings", href: "/settings", icon: Settings },
  ];

  return (
    <div className="flex h-screen overflow-hidden">
      {/* Sidebar */}
      <aside className={`shrink-0 flex flex-col p-3 z-10 bg-[#0B141D]/80 backdrop-blur-xl border-r border-white/[0.06] transition-[width] duration-300 ${isCollapsed ? "w-[76px]" : "w-64"}`}>
        <div className="flex items-center gap-2.5 px-2 py-4 mb-4">
          <div className="w-9 h-9 shrink-0 rounded-xl bg-[#0E1A24] border border-[#93C645]/20 shadow-[0_0_18px_rgba(147,198,69,0.30)] flex items-center justify-center">
            <Logo className="w-5 h-5 text-[#93C645]" />
          </div>
          {!isCollapsed && <span className="text-lg font-semibold tracking-tight text-white whitespace-nowrap overflow-hidden">Kiwi</span>}
        </div>

        <nav className="flex-1 space-y-1">
          {navItems.map((item) => {
            const isActive = pathname === item.href;
            return (
              <Link
                key={item.name}
                href={item.href}
                className={`group relative flex items-center gap-3 px-3 py-2.5 rounded-xl transition-all duration-150 ${
                  isActive
                    ? "bg-[#93C645]/[0.10] text-white"
                    : "text-zinc-400 hover:text-white hover:bg-white/[0.04]"
                } ${isCollapsed ? "justify-center px-0" : ""}`}
                title={isCollapsed ? item.name : undefined}
              >
                {isActive && <span className="absolute left-0 top-1/2 -translate-y-1/2 h-5 w-[3px] rounded-r-full bg-[#93C645] shadow-[0_0_10px_rgba(147,198,69,0.6)]" />}
                <item.icon className={`w-[18px] h-[18px] shrink-0 transition-colors ${isActive ? "text-[#93C645]" : "text-zinc-500 group-hover:text-zinc-300"}`} />
                {!isCollapsed && <span className="text-sm font-medium whitespace-nowrap">{item.name}</span>}
              </Link>
            );
          })}
        </nav>

        <div className="pt-3 mt-2 border-t border-white/[0.06] flex flex-col gap-1">
          <button
            onClick={() => setIsCollapsed(!isCollapsed)}
            className="w-full flex items-center justify-center gap-2 p-2 text-zinc-500 hover:text-white hover:bg-white/[0.04] rounded-xl transition-colors"
          >
            {isCollapsed ? <ChevronRight className="w-[18px] h-[18px]" /> : <><ChevronLeft className="w-[18px] h-[18px] shrink-0" /><span className="text-sm">Collapse</span></>}
          </button>

          <button
            onClick={logout}
            title={isCollapsed ? "Log out" : undefined}
            className="w-full flex items-center justify-center gap-2 p-2 text-zinc-500 hover:text-red-300 hover:bg-red-500/10 rounded-xl transition-colors"
          >
            {isCollapsed ? <LogOut className="w-[18px] h-[18px]" /> : <><LogOut className="w-[18px] h-[18px] shrink-0" /><span className="text-sm">Log out</span></>}
          </button>

          <div className={`mt-1 flex items-center w-full gap-3 px-1.5 py-1.5 ${isCollapsed ? "justify-center" : ""}`}>
            <div className="w-8 h-8 shrink-0 rounded-lg bg-[#0E1A24] border border-[#93C645]/25 flex items-center justify-center text-xs font-semibold text-[#93C645] uppercase">
              {orgName ? orgName.charAt(0) : "A"}
            </div>
            {!isCollapsed && (
              <div className="flex flex-col whitespace-nowrap overflow-hidden">
                <span className="text-sm font-medium text-white truncate">{orgName || "Unknown Org"}</span>
                <span className="text-[11px] text-zinc-500">API key session</span>
              </div>
            )}
          </div>
        </div>
      </aside>

      {/* Main Content */}
      <main className="flex-1 flex flex-col overflow-hidden relative">
        <ActivationBanner />
        <div className="flex-1 overflow-y-auto">{children}</div>
      </main>
    </div>
  );
}
