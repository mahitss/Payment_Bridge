"use client";

import React, { useEffect } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import {
  User,
  Building2,
  Lock,
  LogOut,
  ArrowLeft,
  KeyRound,
  CheckCircle2,
  Clock,
  Fingerprint,
} from "lucide-react";
import { useAuth } from "@/lib/auth";

export default function AccountSettingsPage() {
  const { user, organization, logout, loading } = useAuth();
  const router = useRouter();

  useEffect(() => {
    if (!loading && !user) {
      router.push("/login?returnTo=/settings/account");
    }
  }, [user, loading, router]);

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-slate-50 text-slate-500">
        <div className="flex flex-col items-center gap-3">
          <div className="w-8 h-8 border-2 border-blue-600 border-t-transparent rounded-full animate-spin" />
          <span className="text-xs font-mono">Loading profile data...</span>
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
    role: "OWNER",
  };

  return (
    <div className="min-h-screen bg-slate-50 text-slate-900 flex flex-col">
      {/* Top Navigation Bar: Restrained Dark Navy */}
      <header className="border-b border-slate-800 bg-slate-950 text-slate-100 sticky top-0 z-40 shadow-sm">
        <div className="max-w-4xl mx-auto px-6 h-16 flex items-center justify-between">
          <div className="flex items-center gap-4">
            <Link
              href="/app"
              className="min-h-[40px] px-3 py-1.5 rounded-lg bg-slate-900 hover:bg-slate-800 text-slate-200 transition flex items-center gap-1.5 text-xs border border-slate-800"
            >
              <ArrowLeft className="w-4 h-4" aria-hidden="true" />
              <span>Back to App</span>
            </Link>
            <span className="h-4 w-px bg-slate-800" />
            <span className="font-bold text-sm text-white">Account &amp; Tenant Settings</span>
          </div>

          <button
            onClick={logout}
            className="min-h-[40px] px-3.5 py-1.5 rounded-lg bg-slate-900 hover:bg-rose-950/40 text-slate-300 hover:text-rose-400 border border-slate-800 transition text-xs flex items-center gap-1.5"
          >
            <LogOut className="w-3.5 h-3.5" aria-hidden="true" />
            <span>Sign Out</span>
          </button>
        </div>
      </header>

      {/* Main Content */}
      <main id="main-content" tabIndex={-1} className="flex-1 max-w-4xl w-full mx-auto px-6 py-8 space-y-6 outline-none">
        <div>
          <h1 className="text-2xl font-bold text-slate-900 tracking-tight">Account &amp; Security</h1>
          <p className="text-xs text-slate-500 mt-1">
            Manage your personal profile, multi-tenant workspace association, and cryptographic credentials.
          </p>
        </div>

        {/* Profile Card */}
        <section className="p-6 rounded-xl bg-white border border-slate-200/80 space-y-4 shadow-sm">
          <div className="flex items-center justify-between border-b border-slate-100 pb-3">
            <div className="flex items-center gap-2.5">
              <User className="w-4 h-4 text-blue-600" />
              <h2 className="text-sm font-semibold text-slate-900">User Profile</h2>
            </div>
            <span className="text-xs font-mono px-2 py-0.5 rounded bg-emerald-50 text-emerald-700 border border-emerald-200 flex items-center gap-1">
              <CheckCircle2 className="w-3 h-3" />
              <span>Verified Account</span>
            </span>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-4 text-xs">
            <div>
              <span className="text-slate-500 block mb-1">Full Name</span>
              <p className="font-medium text-slate-900">{user.name || "—"}</p>
            </div>
            <div>
              <span className="text-slate-500 block mb-1">Email Address (Normalized)</span>
              <p className="font-mono text-slate-900">{user.email}</p>
            </div>
            <div>
              <span className="text-slate-500 block mb-1">User Identifier</span>
              <p className="font-mono text-slate-600">{user.id}</p>
            </div>
            <div>
              <span className="text-slate-500 block mb-1">Account Created At</span>
              <p className="text-slate-700 flex items-center gap-1.5">
                <Clock className="w-3.5 h-3.5 text-slate-400" />
                <span>{new Date(user.created_at).toUTCString()}</span>
              </p>
            </div>
          </div>
        </section>

        {/* Organization & Tenant Memberships */}
        <section className="p-6 rounded-xl bg-white border border-slate-200/80 space-y-4 shadow-sm">
          <div className="flex items-center justify-between border-b border-slate-100 pb-3">
            <div className="flex items-center gap-2.5">
              <Building2 className="w-4 h-4 text-blue-600" />
              <h2 className="text-sm font-semibold text-slate-900">Tenant Organization Membership</h2>
            </div>
            <span className="text-xs px-2.5 py-0.5 rounded-full bg-blue-50 text-blue-700 border border-blue-200">
              Multi-Tenant Isolated
            </span>
          </div>

          <div className="p-4 rounded-lg bg-slate-50 border border-slate-200 flex flex-col md:flex-row md:items-center justify-between gap-3 text-xs">
            <div>
              <p className="font-semibold text-slate-900 text-sm">{primaryOrg.name}</p>
              <p className="font-mono text-slate-500 text-[11px] mt-0.5">ID: {primaryOrg.id}</p>
              <p className="text-slate-500 text-[11px] mt-1">Slug: <span className="font-mono text-slate-700">{primaryOrg.slug}</span></p>
            </div>
            <div className="flex items-center gap-2">
              <span className="px-2.5 py-1 rounded-md bg-emerald-50 text-emerald-700 border border-emerald-200 font-medium">
                Role: {primaryOrg.role}
              </span>
            </div>
          </div>
        </section>

        {/* Security & Authentication Invariants */}
        <section className="p-6 rounded-xl bg-white border border-slate-200/80 space-y-4 shadow-sm">
          <div className="flex items-center justify-between border-b border-slate-100 pb-3">
            <div className="flex items-center gap-2.5">
              <Lock className="w-4 h-4 text-emerald-600" />
              <h2 className="text-sm font-semibold text-slate-900">Security &amp; Cryptography Details</h2>
            </div>
            <Fingerprint className="w-4 h-4 text-slate-400" />
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-4 text-xs">
            <div className="p-3.5 rounded-lg bg-slate-50 border border-slate-200 space-y-1">
              <span className="font-semibold text-slate-900">Password Encryption</span>
              <p className="text-slate-600">
                Argon2id (64MB memory, 3 iterations, 4 parallelism threads, constant-time verification). Plaintext passwords are never stored.
              </p>
            </div>

            <div className="p-3.5 rounded-lg bg-slate-50 border border-slate-200 space-y-1">
              <span className="font-semibold text-slate-900">Session Cookie Security</span>
              <p className="text-slate-600">
                HttpOnly, SameSite=Lax (or Strict in prod), Secure flag enabled, opaque 32-byte token with SHA-256 hash in PostgreSQL.
              </p>
            </div>
          </div>

          <div className="pt-2 flex flex-col sm:flex-row items-center justify-between gap-3 border-t border-slate-100">
            <div>
              <p className="text-xs font-medium text-slate-700">Need to change your password?</p>
              <p className="text-[11px] text-slate-500">
                Initiate a password recovery flow to receive a single-use reset token.
              </p>
            </div>
            <Link
              href="/forgot-password"
              className="px-3.5 py-2 rounded-lg bg-white hover:bg-slate-50 text-slate-700 text-xs font-medium transition border border-slate-200 shadow-sm flex items-center gap-1.5"
            >
              <KeyRound className="w-3.5 h-3.5 text-blue-600" />
              <span>Request Password Reset</span>
            </Link>
          </div>
        </section>
      </main>
    </div>
  );
}
