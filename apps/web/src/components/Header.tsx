"use client";

import React from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import {
  ShieldCheck,
  Building2,
  User,
  LogOut,
  Settings,
  CreditCard,
  PlusCircle,
  LayoutDashboard,
  PlugZap,
} from "lucide-react";
import { useAuth } from "@/lib/auth";

export default function Header() {
  const { user, organization, logout } = useAuth();
  const pathname = usePathname();

  const primaryOrg = organization || {
    id: "org-personal",
    name: "Personal Workspace",
    slug: "personal-workspace",
    is_personal: true,
    role: "ADMIN",
  };

  const navItems = [
    { label: "Overview", href: "/app", icon: LayoutDashboard },
    { label: "Payout Cases", href: "/app/payouts", icon: CreditCard },
    { label: "New Payout", href: "/app/payouts/new", icon: PlusCircle },
    { label: "PayPal Sandbox", href: "/app/settings/integrations", icon: PlugZap },
  ];

  return (
    <header className="border-b border-slate-800/80 bg-slate-900/60 backdrop-blur-md sticky top-0 z-40">
      <div className="max-w-7xl mx-auto px-6 h-16 flex items-center justify-between">
        <div className="flex items-center gap-6">
          <Link href="/app" className="flex items-center gap-2.5">
            <span className="p-1.5 rounded-lg bg-blue-600/20 text-blue-400 border border-blue-500/30">
              <ShieldCheck className="w-5 h-5" />
            </span>
            <span className="font-bold text-base text-white tracking-tight">ClaimBridge</span>
          </Link>

          <span className="h-4 w-px bg-slate-800" />

          {/* Navigation Links */}
          <nav className="hidden md:flex items-center gap-1">
            {navItems.map((item) => {
              const Icon = item.icon;
              const isActive =
                item.href === "/app"
                  ? pathname === "/app"
                  : pathname.startsWith(item.href);

              return (
                <Link
                  key={item.href}
                  href={item.href}
                  className={`px-3 py-1.5 rounded-lg text-xs font-medium flex items-center gap-1.5 transition ${
                    isActive
                      ? "bg-blue-600/15 text-blue-400 border border-blue-500/30 font-semibold"
                      : "text-slate-400 hover:text-slate-200 hover:bg-slate-800/60"
                  }`}
                >
                  <Icon className="w-3.5 h-3.5" />
                  <span>{item.label}</span>
                </Link>
              );
            })}
          </nav>
        </div>

        <div className="flex items-center gap-3">
          {/* Tenant Badge */}
          <div className="hidden sm:flex items-center gap-2 px-2.5 py-1 rounded-md bg-slate-800/60 border border-slate-700/60 text-xs">
            <Building2 className="w-3.5 h-3.5 text-blue-400" />
            <span className="font-medium text-slate-200">{primaryOrg.name}</span>
            <span className="px-1.5 py-0.2 rounded text-[10px] font-mono bg-blue-500/20 text-blue-300">
              {primaryOrg.role}
            </span>
          </div>

          {user && (
            <div className="hidden lg:flex items-center gap-1.5 text-xs text-slate-400 font-mono">
              <User className="w-3.5 h-3.5 text-slate-500" />
              <span>{user.email}</span>
            </div>
          )}

          <Link
            href="/settings/account"
            className="p-2 rounded-lg bg-slate-800/80 hover:bg-slate-700 text-slate-300 hover:text-white transition border border-slate-700 text-xs flex items-center gap-1.5"
            title="Account Settings"
          >
            <Settings className="w-4 h-4" />
          </Link>

          <button
            onClick={logout}
            className="p-2 rounded-lg bg-slate-800/80 hover:bg-rose-500/20 hover:text-rose-400 text-slate-400 transition border border-slate-700 text-xs flex items-center gap-1.5"
            title="Sign Out"
          >
            <LogOut className="w-4 h-4" />
          </button>
        </div>
      </div>
    </header>
  );
}
