"use client";

import React, { useEffect } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import {
  ShieldCheck,
  Building2,
  User,
  LogOut,
  Settings,
  Lock,
  Cpu,
  Server,
  CheckCircle2,
  ExternalLink,
  Mail,
  KeyRound,
  FileCheck,
  AlertTriangle,
} from "lucide-react";
import { useAuth } from "@/lib/auth";

export default function AppDashboardPage() {
  const { user, organization, loading, logout } = useAuth();
  const router = useRouter();

  useEffect(() => {
    if (!loading && !user) {
      router.push("/login?returnTo=/app");
    }
  }, [user, loading, router]);

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-slate-950 text-slate-400">
        <div className="flex flex-col items-center gap-3">
          <div className="w-8 h-8 border-2 border-blue-500 border-t-transparent rounded-full animate-spin" />
          <span className="text-xs font-mono">Authenticating session...</span>
        </div>
      </div>
    );
  }

  if (!user) {
    return null;
  }

  const primaryOrg = organization || {
    id: "org-personal",
    name: "Personal Workspace",
    slug: "personal-workspace",
    is_personal: true,
  };

  return (
    <div className="min-h-screen bg-slate-950 text-slate-100 flex flex-col">
      {/* Top Application Bar */}
      <header className="border-b border-slate-800/80 bg-slate-900/50 backdrop-blur-md sticky top-0 z-40">
        <div className="max-w-6xl mx-auto px-6 h-16 flex items-center justify-between">
          <div className="flex items-center gap-4">
            <Link href="/" className="flex items-center gap-2.5">
              <span className="p-1.5 rounded-lg bg-blue-600/20 text-blue-400 border border-blue-500/30">
                <ShieldCheck className="w-5 h-5" />
              </span>
              <span className="font-bold text-base text-white tracking-tight">ClaimBridge</span>
            </Link>
            <span className="h-4 w-px bg-slate-800" />
            <div className="flex items-center gap-2 px-2.5 py-1 rounded-md bg-slate-800/60 border border-slate-700/60 text-xs">
              <Building2 className="w-3.5 h-3.5 text-blue-400" />
              <span className="font-medium text-slate-200">{primaryOrg.name}</span>
              <span className="px-1.5 py-0.2 rounded text-[10px] font-mono bg-blue-500/20 text-blue-300">
                {primaryOrg.role}
              </span>
            </div>
          </div>

          <div className="flex items-center gap-3">
            <div className="hidden sm:flex items-center gap-2 text-xs text-slate-400 font-mono">
              <User className="w-3.5 h-3.5 text-slate-500" />
              <span>{user.email}</span>
            </div>
            <Link
              href="/settings/account"
              className="p-2 rounded-lg bg-slate-800/80 hover:bg-slate-700 text-slate-300 hover:text-white transition border border-slate-700 text-xs flex items-center gap-1.5"
              title="Account Settings"
            >
              <Settings className="w-4 h-4" />
              <span className="hidden md:inline">Settings</span>
            </Link>
            <button
              onClick={logout}
              className="p-2 rounded-lg bg-slate-800/80 hover:bg-rose-500/20 hover:text-rose-400 text-slate-400 transition border border-slate-700 text-xs flex items-center gap-1.5"
              title="Sign Out"
            >
              <LogOut className="w-4 h-4" />
              <span className="hidden md:inline">Sign Out</span>
            </button>
          </div>
        </div>
      </header>

      {/* Main Dashboard Content */}
      <main className="flex-1 max-w-6xl w-full mx-auto px-6 py-8 space-y-8">
        {/* Welcome Banner */}
        <div className="p-6 rounded-2xl bg-gradient-to-r from-blue-950/40 to-slate-900 border border-blue-900/40 flex flex-col md:flex-row md:items-center justify-between gap-4">
          <div>
            <div className="flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-blue-400 mb-1">
              <span>Tenant Isolated Workspace</span>
              <span className="w-1.5 h-1.5 rounded-full bg-emerald-400 animate-pulse" />
            </div>
            <h1 className="text-xl font-bold text-white">
              Welcome back, {user.name || user.email.split("@")[0]}
            </h1>
            <p className="text-xs text-slate-400 mt-1 max-w-xl">
              ClaimBridge protects your settlement workflows through deterministic Rust policy evaluation and authenticated multi-tenant isolation.
            </p>
          </div>
          <div className="flex items-center gap-2.5">
            <Link
              href="/settings/account"
              className="px-4 py-2 rounded-lg bg-blue-600 hover:bg-blue-500 text-white text-xs font-medium transition flex items-center gap-2 shadow-lg shadow-blue-600/20"
            >
              <KeyRound className="w-3.5 h-3.5" />
              <span>Session & Credentials</span>
            </Link>
            <a
              href="http://localhost:8025"
              target="_blank"
              rel="noreferrer"
              className="px-4 py-2 rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-200 text-xs font-medium transition flex items-center gap-2 border border-slate-700"
            >
              <Mail className="w-3.5 h-3.5 text-blue-400" />
              <span>Mailpit Inbox</span>
              <ExternalLink className="w-3 h-3 text-slate-500" />
            </a>
          </div>
        </div>

        {/* Security & Organization Cards Grid */}
        <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
          {/* Tenant Details */}
          <div className="p-5 rounded-xl bg-slate-900/60 border border-slate-800 space-y-3">
            <div className="flex items-center justify-between">
              <span className="text-xs font-semibold text-slate-400 uppercase tracking-wider">Tenant Scope</span>
              <Building2 className="w-4 h-4 text-blue-400" />
            </div>
            <div>
              <p className="text-sm font-bold text-white">{primaryOrg.name}</p>
              <p className="text-xs text-slate-500 font-mono mt-0.5">Org ID: {primaryOrg.id}</p>
            </div>
            <div className="border-t border-slate-800/80 pt-3 text-xs text-slate-300 space-y-1.5">
              <div className="flex justify-between">
                <span className="text-slate-500">Role:</span>
                <span className="font-semibold text-emerald-400">{primaryOrg.role}</span>
              </div>
              <div className="flex justify-between">
                <span className="text-slate-500">Isolation Boundary:</span>
                <span className="font-mono text-slate-400">Strict Tenant FK</span>
              </div>
            </div>
          </div>

          {/* User Session Security */}
          <div className="p-5 rounded-xl bg-slate-900/60 border border-slate-800 space-y-3">
            <div className="flex items-center justify-between">
              <span className="text-xs font-semibold text-slate-400 uppercase tracking-wider">Session Security</span>
              <Lock className="w-4 h-4 text-emerald-400" />
            </div>
            <div>
              <p className="text-sm font-bold text-white">HttpOnly Server Session</p>
              <p className="text-xs text-slate-500 font-mono mt-0.5">SHA-256 Opaque Hash</p>
            </div>
            <div className="border-t border-slate-800/80 pt-3 text-xs text-slate-300 space-y-1.5">
              <div className="flex justify-between">
                <span className="text-slate-500">Password Hashing:</span>
                <span className="font-semibold text-blue-400">Argon2id (m=64MB, t=3)</span>
              </div>
              <div className="flex justify-between">
                <span className="text-slate-500">CSRF/Origin Guard:</span>
                <span className="font-mono text-emerald-400">Enforced (Deny-All)</span>
              </div>
            </div>
          </div>

          {/* Deterministic Safety Engine */}
          <div className="p-5 rounded-xl bg-slate-900/60 border border-slate-800 space-y-3">
            <div className="flex items-center justify-between">
              <span className="text-xs font-semibold text-slate-400 uppercase tracking-wider">Safety Policy</span>
              <Cpu className="w-4 h-4 text-amber-400" />
            </div>
            <div>
              <p className="text-sm font-bold text-white">Rust Deterministic Engine</p>
              <p className="text-xs text-slate-500 font-mono mt-0.5">services/policy</p>
            </div>
            <div className="border-t border-slate-800/80 pt-3 text-xs text-slate-300 space-y-1.5">
              <div className="flex justify-between">
                <span className="text-slate-500">Max Single Payout:</span>
                <span className="font-mono font-semibold text-white">$500.00 USD</span>
              </div>
              <div className="flex justify-between">
                <span className="text-slate-500">Dual-Approval Threshold:</span>
                <span className="font-mono text-amber-400">&gt; $250.00 USD</span>
              </div>
            </div>
          </div>
        </div>

        {/* Multi-tenant Claim Queue Placeholder */}
        <section className="p-6 rounded-xl bg-slate-900/40 border border-slate-800 space-y-4">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <FileCheck className="w-5 h-5 text-blue-400" />
              <h2 className="text-base font-semibold text-white">
                Claims & Triage Registry ({primaryOrg.name})
              </h2>
            </div>
            <span className="text-xs px-2.5 py-0.5 rounded-full bg-slate-800 text-slate-400 border border-slate-700">
              Zero Unresolved Anomalies
            </span>
          </div>

          <div className="p-8 rounded-lg border border-dashed border-slate-800 text-center space-y-3">
            <div className="w-10 h-10 rounded-full bg-slate-800/80 text-slate-400 flex items-center justify-center mx-auto">
              <CheckCircle2 className="w-5 h-5 text-emerald-400" />
            </div>
            <div>
              <p className="text-sm font-semibold text-slate-200">
                Organization Authenticated & Isolated
              </p>
              <p className="text-xs text-slate-400 max-w-md mx-auto mt-1">
                Your tenant workspace is provisioned and ready. Mission 002 established user registration, email verification, session authentication, and database multi-tenancy.
              </p>
            </div>
          </div>
        </section>
      </main>
    </div>
  );
}
