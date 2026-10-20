"use client";

import React, { useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import {
  ShieldCheck,
  Building2,
  Lock,
  Cpu,
  PlugZap,
  CheckCircle2,
  AlertTriangle,
  CreditCard,
  PlusCircle,
  ArrowRight,
  RefreshCw,
  Clock,
  ExternalLink,
} from "lucide-react";
import { useAuth } from "@/lib/auth";
import Header from "@/components/Header";
import { PayoutCase, ConnectionStatus, formatCentsToUSD, formatDate } from "@/lib/payouts";

export default function AppDashboardPage() {
  const { user, organization, loading, logout } = useAuth();
  const router = useRouter();

  const [cases, setCases] = useState<PayoutCase[]>([]);
  const [totalCases, setTotalCases] = useState<number>(0);
  const [connStatus, setConnStatus] = useState<ConnectionStatus | null>(null);
  const [loadingData, setLoadingData] = useState<boolean>(true);

  useEffect(() => {
    if (!loading && !user) {
      router.push("/login?returnTo=/app");
    }
  }, [user, loading, router]);

  const fetchDashboardData = async () => {
    setLoadingData(true);
    try {
      // 1. Fetch Payout Cases
      const casesRes = await fetch("/api/v1/payout-cases?limit=5", {
        method: "GET",
        credentials: "include",
      });
      if (casesRes.ok) {
        const data = await casesRes.json();
        setCases(data.cases || []);
        setTotalCases(data.total || 0);
      }

      // 2. Fetch PayPal Connection Status
      const connRes = await fetch("/api/v1/paypal/connection-status", {
        method: "GET",
        credentials: "include",
      });
      if (connRes.ok) {
        const data: ConnectionStatus = await connRes.json();
        setConnStatus(data);
      } else {
        const errData = await connRes.json().catch(() => ({}));
        setConnStatus({
          connected: false,
          environment: "sandbox",
          base_url: "https://api-m.sandbox.paypal.com",
          checked_at: new Date().toISOString(),
          error: errData.error || "Sandbox authentication failed",
        });
      }
    } catch {
      // Backend error fallback
    } finally {
      setLoadingData(false);
    }
  };

  useEffect(() => {
    if (user) {
      fetchDashboardData();
    }
  }, [user]);

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
    role: "ADMIN",
  };

  const pendingApprovalsCount = cases.filter(
    (c) => c.dual_approval_required && c.status === "DRAFT"
  ).length;

  const totalCompletedCents = cases
    .filter((c) => c.status === "COMPLETED")
    .reduce((acc, c) => acc + c.amount_cents, 0);

  return (
    <div className="min-h-screen bg-slate-950 text-slate-100 flex flex-col">
      <Header />

      {/* Main Dashboard Content */}
      <main id="main-content" tabIndex={-1} className="flex-1 max-w-7xl w-full mx-auto px-6 py-8 space-y-8 outline-none">
        {/* Welcome & Quick Action Banner */}
        <div className="p-6 rounded-2xl bg-gradient-to-r from-blue-950/40 via-slate-900 to-slate-900 border border-blue-900/40 flex flex-col md:flex-row md:items-center justify-between gap-4">
          <div>
            <div className="flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-blue-400 mb-1">
              <span>{primaryOrg.name}</span>
              <span className="w-1.5 h-1.5 rounded-full bg-emerald-400 animate-pulse" />
              <span className="text-[10px] text-slate-400 font-mono">
                [ROLE: {primaryOrg.role}]
              </span>
            </div>
            <h1 className="text-xl font-bold text-white tracking-tight">
              Payout Operations & Sandbox Control
            </h1>
            <p className="text-xs text-slate-400 mt-1 max-w-xl">
              Inspect claims, initiate explicitly authorized PayPal sandbox payouts, enforce dual-custody approval, and track durable audit events.
            </p>
          </div>
          <div className="flex items-center gap-3">
            <Link
              href="/app/payouts/new"
              className="min-h-[44px] px-4 py-2.5 rounded-lg bg-blue-600 hover:bg-blue-500 text-white text-xs font-semibold transition flex items-center gap-2 shadow-lg shadow-blue-600/20"
            >
              <PlusCircle className="w-4 h-4" aria-hidden="true" />
              <span>Create Payout Case</span>
            </Link>
            <button
              onClick={fetchDashboardData}
              disabled={loadingData}
              aria-label="Refresh Dashboard"
              className="min-h-[44px] min-w-[44px] p-2.5 rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-300 hover:text-white transition border border-slate-700 text-xs flex items-center justify-center gap-1.5"
            >
              <RefreshCw className={`w-4 h-4 ${loadingData ? "animate-spin" : ""}`} aria-hidden="true" />
            </button>
          </div>
        </div>

        {/* Real Metrics Grid */}
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-5">
          {/* Total Cases */}
          <div className="p-5 rounded-xl bg-slate-900/60 border border-slate-800 space-y-2">
            <div className="flex items-center justify-between text-xs text-slate-400 font-medium uppercase tracking-wider">
              <span>Total Payout Cases</span>
              <CreditCard className="w-4 h-4 text-blue-400" />
            </div>
            <div className="text-2xl font-bold text-white font-mono">
              {loadingData ? "—" : totalCases}
            </div>
            <p className="text-[11px] text-slate-500">Persistent PostgreSQL records</p>
          </div>

          {/* Pending Dual Approval */}
          <div className="p-5 rounded-xl bg-slate-900/60 border border-slate-800 space-y-2">
            <div className="flex items-center justify-between text-xs text-slate-400 font-medium uppercase tracking-wider">
              <span>Dual Approvals Needed</span>
              <Lock className="w-4 h-4 text-amber-400" />
            </div>
            <div className="text-2xl font-bold text-amber-400 font-mono">
              {loadingData ? "—" : pendingApprovalsCount}
            </div>
            <p className="text-[11px] text-slate-500">Separation of duties enforced</p>
          </div>

          {/* Completed Payouts */}
          <div className="p-5 rounded-xl bg-slate-900/60 border border-slate-800 space-y-2">
            <div className="flex items-center justify-between text-xs text-slate-400 font-medium uppercase tracking-wider">
              <span>Settled Minor Units</span>
              <CheckCircle2 className="w-4 h-4 text-emerald-400" />
            </div>
            <div className="text-2xl font-bold text-emerald-400 font-mono">
              {loadingData ? "—" : formatCentsToUSD(totalCompletedCents)}
            </div>
            <p className="text-[11px] text-slate-500">Integer arithmetic in cents</p>
          </div>

          {/* PayPal Sandbox Status */}
          <div className="p-5 rounded-xl bg-slate-900/60 border border-slate-800 space-y-2">
            <div className="flex items-center justify-between text-xs text-slate-400 font-medium uppercase tracking-wider">
              <span>PayPal Sandbox</span>
              <PlugZap className="w-4 h-4 text-blue-400" />
            </div>
            <div className="flex items-center gap-2">
              <span
                className={`w-2.5 h-2.5 rounded-full ${
                  connStatus?.connected ? "bg-emerald-400" : "bg-amber-400"
                }`}
              />
              <span className="text-sm font-bold text-white">
                {connStatus?.connected ? "Authenticated" : "Disconnected"}
              </span>
            </div>
            <div className="flex justify-between items-center text-[11px]">
              <span className="text-slate-500 font-mono">Host: api-m.sandbox</span>
              <Link
                href="/app/settings/integrations"
                className="text-blue-400 hover:text-blue-300 font-medium flex items-center gap-0.5"
              >
                <span>Details</span>
                <ArrowRight className="w-3 h-3" />
              </Link>
            </div>
          </div>
        </div>

        {/* Recent Payout Cases Section */}
        <section className="p-6 rounded-xl bg-slate-900/40 border border-slate-800 space-y-4">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2.5">
              <CreditCard className="w-5 h-5 text-blue-400" />
              <h2 className="text-base font-semibold text-white">Recent Payout Cases</h2>
            </div>
            <Link
              href="/app/payouts"
              className="text-xs text-blue-400 hover:text-blue-300 font-medium flex items-center gap-1"
            >
              <span>View all cases</span>
              <ArrowRight className="w-3.5 h-3.5" />
            </Link>
          </div>

          {loadingData ? (
            <div className="py-12 flex flex-col items-center justify-center gap-2 text-slate-500 text-xs">
              <div className="w-6 h-6 border-2 border-blue-500 border-t-transparent rounded-full animate-spin" />
              <span>Querying organization payout cases...</span>
            </div>
          ) : cases.length === 0 ? (
            <div className="p-10 rounded-lg border border-dashed border-slate-800 text-center space-y-3">
              <CreditCard className="w-8 h-8 text-slate-600 mx-auto" />
              <div>
                <p className="text-sm font-semibold text-slate-300">No payout cases recorded yet</p>
                <p className="text-xs text-slate-500 mt-1 max-w-sm mx-auto">
                  Create your first settlement case to test policy evaluation, dual approval, and PayPal sandbox submission.
                </p>
              </div>
              <Link
                href="/app/payouts/new"
                className="inline-flex items-center gap-2 px-4 py-2 rounded-lg bg-blue-600 hover:bg-blue-500 text-white text-xs font-semibold transition"
              >
                <PlusCircle className="w-3.5 h-3.5" />
                <span>Create Case Now</span>
              </Link>
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-left text-xs">
                <thead>
                  <tr className="border-b border-slate-800 text-slate-400 uppercase tracking-wider font-semibold">
                    <th className="pb-3 pl-2">Reference</th>
                    <th className="pb-3">Title & Recipient</th>
                    <th className="pb-3">Amount</th>
                    <th className="pb-3">Policy / Dual Approval</th>
                    <th className="pb-3">Case Status</th>
                    <th className="pb-3">Created</th>
                    <th className="pb-3 text-right pr-2">Action</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-800/60">
                  {cases.map((c) => (
                    <tr key={c.id} className="hover:bg-slate-800/30 transition">
                      <td className="py-3.5 pl-2 font-mono font-medium text-blue-400">
                        {c.reference}
                      </td>
                      <td className="py-3.5">
                        <div className="font-medium text-white">{c.title}</div>
                        <div className="text-slate-400 text-[11px] font-mono">
                          {c.recipient_name} ({c.recipient_email})
                        </div>
                      </td>
                      <td className="py-3.5 font-mono font-semibold text-white">
                        {formatCentsToUSD(c.amount_cents)}
                      </td>
                      <td className="py-3.5">
                        {c.dual_approval_required ? (
                          <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[10px] font-medium bg-amber-500/10 text-amber-400 border border-amber-500/20">
                            <Lock className="w-2.5 h-2.5" />
                            Dual-Custody Required
                          </span>
                        ) : (
                          <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[10px] font-medium bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
                            <CheckCircle2 className="w-2.5 h-2.5" />
                            Single Officer
                          </span>
                        )}
                      </td>
                      <td className="py-3.5">
                        <span
                          className={`px-2 py-0.5 rounded text-[10px] font-semibold tracking-wide ${
                            c.status === "COMPLETED"
                              ? "bg-emerald-500/20 text-emerald-300 border border-emerald-500/30"
                              : c.status === "SUBMITTED"
                              ? "bg-blue-500/20 text-blue-300 border border-blue-500/30"
                              : c.status === "CANCELLED"
                              ? "bg-rose-500/20 text-rose-300 border border-rose-500/30"
                              : "bg-slate-800 text-slate-300 border border-slate-700"
                          }`}
                        >
                          {c.status}
                        </span>
                      </td>
                      <td className="py-3.5 text-slate-400 text-[11px] whitespace-nowrap">
                        {formatDate(c.created_at)}
                      </td>
                      <td className="py-3.5 text-right pr-2">
                        <Link
                          href={`/app/payouts/${c.id}`}
                          className="px-2.5 py-1 rounded bg-slate-800 hover:bg-slate-700 text-slate-200 hover:text-white transition font-medium border border-slate-700 inline-flex items-center gap-1 text-[11px]"
                        >
                          <span>Inspect</span>
                          <ArrowRight className="w-3 h-3" />
                        </Link>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>

        {/* Security Architecture & Guarantees Footer Info */}
        <div className="grid grid-cols-1 md:grid-cols-3 gap-5 text-xs">
          <div className="p-4 rounded-xl bg-slate-900/40 border border-slate-800 space-y-1.5">
            <div className="flex items-center gap-2 font-semibold text-slate-300">
              <Cpu className="w-4 h-4 text-blue-400" />
              <span>Deterministic Fail-Closed</span>
            </div>
            <p className="text-slate-400 text-[11px] leading-relaxed">
              Amounts &gt; $500.00 USD are permanently denied by the Rust policy engine. Amounts &gt; $250.00 require two distinct authenticated officers.
            </p>
          </div>

          <div className="p-4 rounded-xl bg-slate-900/40 border border-slate-800 space-y-1.5">
            <div className="flex items-center gap-2 font-semibold text-slate-300">
              <Lock className="w-4 h-4 text-emerald-400" />
              <span>Durable Operation Lifecycle</span>
            </div>
            <p className="text-slate-400 text-[11px] leading-relaxed">
              Unique sender batch IDs are committed to PostgreSQL before PayPal REST calls. Ambiguous network timeouts transition to RECONCILIATION_REQUIRED.
            </p>
          </div>

          <div className="p-4 rounded-xl bg-slate-900/40 border border-slate-800 space-y-1.5">
            <div className="flex items-center gap-2 font-semibold text-slate-300">
              <Building2 className="w-4 h-4 text-purple-400" />
              <span>Multi-Tenant Isolation</span>
            </div>
            <p className="text-slate-400 text-[11px] leading-relaxed">
              All cases, operations, and status snapshots are hard-isolated by organization ID at database and handler boundaries.
            </p>
          </div>
        </div>
      </main>
    </div>
  );
}
