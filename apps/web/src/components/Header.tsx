"use client";

import React, { useState, useEffect, useRef } from "react";
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
  Menu,
  X,
} from "lucide-react";
import { useAuth } from "@/lib/auth";

export default function Header() {
  const { user, organization, logout } = useAuth();
  const pathname = usePathname();
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);

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

  // Close mobile menu on route change
  useEffect(() => {
    setMobileMenuOpen(false);
  }, [pathname]);

  // Close mobile menu on Escape key
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape" && mobileMenuOpen) {
        setMobileMenuOpen(false);
        triggerRef.current?.focus();
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [mobileMenuOpen]);

  return (
    <header className="border-b border-slate-800/80 bg-slate-900/60 backdrop-blur-md sticky top-0 z-40">
      <a href="#main-content" className="skip-link">
        Skip to main content
      </a>
      <div className="max-w-7xl mx-auto px-4 sm:px-6 h-16 flex items-center justify-between">
        <div className="flex items-center gap-4 sm:gap-6">
          <Link
            href="/app"
            className="flex items-center gap-2.5 focus-visible:ring-2 focus-visible:ring-blue-500 rounded-lg p-1"
          >
            <span className="p-1.5 rounded-lg bg-blue-600/20 text-blue-400 border border-blue-500/30">
              <ShieldCheck className="w-5 h-5" aria-hidden="true" />
            </span>
            <span className="font-bold text-base text-white tracking-tight">ClaimBridge</span>
          </Link>

          <span className="hidden sm:inline-block h-4 w-px bg-slate-800" aria-hidden="true" />

          {/* Desktop Navigation Links */}
          <nav aria-label="Primary Desktop Navigation" className="hidden md:flex items-center gap-1">
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
                  aria-current={isActive ? "page" : undefined}
                  className={`px-3 py-1.5 rounded-lg text-xs font-medium flex items-center gap-1.5 transition focus-visible:ring-2 focus-visible:ring-blue-500 ${
                    isActive
                      ? "bg-blue-600/15 text-blue-400 border border-blue-500/30 font-semibold"
                      : "text-slate-400 hover:text-slate-200 hover:bg-slate-800/60"
                  }`}
                >
                  <Icon className="w-3.5 h-3.5" aria-hidden="true" />
                  <span>{item.label}</span>
                </Link>
              );
            })}
          </nav>
        </div>

        <div className="flex items-center gap-2 sm:gap-3">
          {/* Tenant Badge */}
          <div className="hidden sm:flex items-center gap-2 px-2.5 py-1 rounded-md bg-slate-800/60 border border-slate-700/60 text-xs">
            <Building2 className="w-3.5 h-3.5 text-blue-400" aria-hidden="true" />
            <span className="font-medium text-slate-200 truncate max-w-[140px]">{primaryOrg.name}</span>
            <span className="px-1.5 py-0.2 rounded text-[10px] font-mono bg-blue-500/20 text-blue-300">
              {primaryOrg.role}
            </span>
          </div>

          {user && (
            <div className="hidden lg:flex items-center gap-1.5 text-xs text-slate-400 font-mono">
              <User className="w-3.5 h-3.5 text-slate-500" aria-hidden="true" />
              <span className="truncate max-w-[160px]">{user.email}</span>
            </div>
          )}

          <Link
            href="/settings/account"
            className="p-2 min-w-[40px] min-h-[40px] rounded-lg bg-slate-800/80 hover:bg-slate-700 text-slate-300 hover:text-white transition border border-slate-700 text-xs flex items-center justify-center focus-visible:ring-2 focus-visible:ring-blue-500"
            aria-label="Account and Workspace Settings"
            title="Account Settings"
          >
            <Settings className="w-4 h-4" aria-hidden="true" />
          </Link>

          <button
            onClick={logout}
            className="p-2 min-w-[40px] min-h-[40px] rounded-lg bg-slate-800/80 hover:bg-rose-500/20 hover:text-rose-400 text-slate-400 transition border border-slate-700 text-xs flex items-center justify-center focus-visible:ring-2 focus-visible:ring-rose-500"
            aria-label="Sign Out of ClaimBridge"
            title="Sign Out"
          >
            <LogOut className="w-4 h-4" aria-hidden="true" />
          </button>

          {/* Mobile Menu Trigger Button */}
          <button
            ref={triggerRef}
            type="button"
            onClick={() => setMobileMenuOpen(!mobileMenuOpen)}
            aria-expanded={mobileMenuOpen}
            aria-controls="mobile-navigation"
            aria-label={mobileMenuOpen ? "Close navigation menu" : "Open navigation menu"}
            className="md:hidden p-2 min-w-[44px] min-h-[44px] rounded-lg bg-slate-800/80 hover:bg-slate-700 text-slate-300 hover:text-white transition border border-slate-700 flex items-center justify-center focus-visible:ring-2 focus-visible:ring-blue-500"
          >
            {mobileMenuOpen ? (
              <X className="w-5 h-5 text-blue-400" aria-hidden="true" />
            ) : (
              <Menu className="w-5 h-5 text-slate-300" aria-hidden="true" />
            )}
          </button>
        </div>
      </div>

      {/* Mobile Navigation Drawer / Panel */}
      {mobileMenuOpen && (
        <div
          ref={menuRef}
          id="mobile-navigation"
          role="region"
          aria-label="Mobile Navigation"
          className="md:hidden border-t border-slate-800 bg-slate-900/95 backdrop-blur-xl px-4 py-4 space-y-3 shadow-2xl animate-in slide-in-from-top-2"
        >
          {/* Organization & User Info in Mobile View */}
          <div className="p-3 rounded-lg bg-slate-950/80 border border-slate-800/80 space-y-1.5 text-xs">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <Building2 className="w-4 h-4 text-blue-400" aria-hidden="true" />
                <span className="font-semibold text-white truncate max-w-[200px]">
                  {primaryOrg.name}
                </span>
              </div>
              <span className="px-1.5 py-0.5 rounded text-[10px] font-mono bg-blue-500/20 text-blue-300">
                {primaryOrg.role}
              </span>
            </div>
            {user && (
              <div className="flex items-center gap-1.5 text-slate-400 font-mono text-[11px] truncate">
                <User className="w-3.5 h-3.5 text-slate-500 flex-shrink-0" aria-hidden="true" />
                <span className="truncate">{user.email}</span>
              </div>
            )}
          </div>

          {/* Navigation Links for Mobile */}
          <nav aria-label="Primary Mobile Navigation" className="space-y-1">
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
                  onClick={() => setMobileMenuOpen(false)}
                  aria-current={isActive ? "page" : undefined}
                  className={`w-full min-h-[44px] px-3.5 py-2.5 rounded-lg text-sm font-medium flex items-center gap-3 transition focus-visible:ring-2 focus-visible:ring-blue-500 ${
                    isActive
                      ? "bg-blue-600/20 text-blue-400 border border-blue-500/30 font-semibold"
                      : "text-slate-300 hover:text-white hover:bg-slate-800/70"
                  }`}
                >
                  <Icon className="w-4 h-4 flex-shrink-0 text-blue-400" aria-hidden="true" />
                  <span>{item.label}</span>
                </Link>
              );
            })}
          </nav>

          {/* Mobile Footer Actions */}
          <div className="pt-2 border-t border-slate-800/80 flex items-center justify-between gap-2">
            <Link
              href="/settings/account"
              onClick={() => setMobileMenuOpen(false)}
              className="flex-1 min-h-[44px] px-3 py-2 rounded-lg bg-slate-800/60 hover:bg-slate-800 text-slate-300 hover:text-white text-xs font-medium flex items-center justify-center gap-2 border border-slate-700/80 transition focus-visible:ring-2 focus-visible:ring-blue-500"
            >
              <Settings className="w-4 h-4 text-slate-400" aria-hidden="true" />
              <span>Account Settings</span>
            </Link>

            <button
              type="button"
              onClick={() => {
                setMobileMenuOpen(false);
                logout();
              }}
              className="flex-1 min-h-[44px] px-3 py-2 rounded-lg bg-rose-500/10 hover:bg-rose-500/20 text-rose-400 text-xs font-medium flex items-center justify-center gap-2 border border-rose-500/20 transition focus-visible:ring-2 focus-visible:ring-rose-500"
            >
              <LogOut className="w-4 h-4 text-rose-400" aria-hidden="true" />
              <span>Sign Out</span>
            </button>
          </div>
        </div>
      )}
    </header>
  );
}
