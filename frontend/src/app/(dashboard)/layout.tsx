"use client";

import Link from "next/link";
import { LayoutDashboard, Network, Settings, TerminalSquare } from "lucide-react";
import { usePathname } from "next/navigation";
import { useState } from "react";
import { ChevronRight, ChevronLeft } from "lucide-react";

export default function DashboardLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const pathname = usePathname();
  const [isCollapsed, setIsCollapsed] = useState(true);

  const navItems = [
    { name: "God View", href: "/", icon: LayoutDashboard },
    { name: "Topology", href: "/topology", icon: Network },
    { name: "Onboarding", href: "/onboarding", icon: TerminalSquare },
    { name: "Settings", href: "/settings", icon: Settings },
  ];

  return (
    <div className="flex h-screen overflow-hidden">
      {/* Sidebar */}
      <aside className={`glass border-y-0 border-l-0 rounded-none shrink-0 flex flex-col p-4 z-10 transition-all duration-300 ${isCollapsed ? "w-20" : "w-64"}`}>
        <div className="flex items-center gap-2 px-2 py-4 mb-6">
          <div className="w-8 h-8 shrink-0 rounded-lg bg-white shadow-[0_0_15px_rgba(255,255,255,0.3)] flex items-center justify-center">
            <span className="text-black font-bold text-xl leading-none">K</span>
          </div>
          {!isCollapsed && <span className="text-xl font-medium tracking-tight text-white whitespace-nowrap overflow-hidden">Kiwi Swarm</span>}
        </div>
        
        <nav className="flex-1 space-y-1">
          {navItems.map((item) => {
            const isActive = pathname === item.href;
            return (
              <Link
                key={item.name}
                href={item.href}
                className={`flex items-center gap-3 px-3 py-2 rounded-md transition-colors ${
                  isActive 
                    ? "bg-white/10 text-white shadow-sm" 
                    : "text-zinc-400 hover:text-white hover:bg-white/5"
                } ${isCollapsed ? "justify-center px-0" : ""}`}
                title={isCollapsed ? item.name : undefined}
              >
                <item.icon className="w-5 h-5 shrink-0" />
                {!isCollapsed && <span className="text-sm font-medium whitespace-nowrap">{item.name}</span>}
              </Link>
            );
          })}
        </nav>
        
        <div className="pt-4 border-t border-white/5 flex flex-col items-center">
          <button 
            onClick={() => setIsCollapsed(!isCollapsed)}
            className="w-full flex items-center justify-center p-2 mb-4 text-zinc-400 hover:text-white hover:bg-white/10 rounded-md transition-colors"
          >
            {isCollapsed ? <ChevronRight className="w-5 h-5" /> : <div className="flex items-center gap-2 whitespace-nowrap"><ChevronLeft className="w-5 h-5 shrink-0" /><span className="text-sm">Collapse</span></div>}
          </button>
          
          <div className={`flex items-center w-full gap-3 px-2 ${isCollapsed ? "justify-center" : ""}`}>
            <div className="w-8 h-8 shrink-0 rounded-full bg-zinc-800 border border-zinc-700"></div>
            {!isCollapsed && (
              <div className="flex flex-col whitespace-nowrap overflow-hidden">
                <span className="text-sm font-medium text-white">Acme Corp</span>
                <span className="text-xs text-zinc-500">Startup Tier</span>
              </div>
            )}
          </div>
        </div>
      </aside>

      {/* Main Content */}
      <main className="flex-1 overflow-y-auto relative">
        {children}
      </main>
    </div>
  );
}
