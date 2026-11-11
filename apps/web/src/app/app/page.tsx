"use client";

import React, { useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import {
  CreditCard,
  PlusCircle,
  ArrowRight,
  RefreshCw,
  Lock,
  CheckCircle2,
  AlertTriangle,
  PlugZap,
  Cpu,
  Building2,
  Sparkles,
} from "lucide-react";
import { useAuth } from "@/lib/auth";
import Header from "@/components/Header";
import {
  PayoutCase,
  ConnectionStatus,
  PayoutCasesMetrics,
  formatCentsToUSD,
  formatDate,
  fetchPayoutCasesMetrics,
} from "@/lib/payouts";
import {
  StatCard,
  Badge,
  Button,
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from "@/components/ui";

export default function AppDashboardPage() {
  const { user, organization, loading } = useAuth();
  const router = useRouter();

  const [cases, setCases] = useState<PayoutCase[]>([]);
  const [metrics, setMetrics] = useState<PayoutCasesMetrics | null>(null);
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
      // 1. Fetch exact database aggregates
      const metricsData = await fetchPayoutCasesMetrics().catch(() => null);
      if (metricsData) {
        setMetrics(metricsData);
      }

      // 2. Fetch Recent Cases (up to 8)
      const casesRes = await fetch("/api/v1/payout-cases?limit=8", {
        method: "GET",
        credentials: "include",
      });
      if (casesRes.ok) {
        const data = await casesRes.json();
        setCases(data.cases || []);
      }

      // 3. Fetch PayPal Connection Status
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
      // Fail safely
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
      <div className="min-h-screen flex items-center justify-center bg-slate-50 text-slate-500">
        <div className="flex flex-col items-center gap-3">
          <div className="w-8 h-8 border-2 border-blue-600 border-t-transparent rounded-full animate-spin" />
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

  return (
    <div className="min-h-screen bg-slate-50 text-slate-900 flex flex-col">
      <Header />

      <main
        id="main-content"
        tabIndex={-1}
        className="flex-1 max-w-7xl w-full mx-auto px-4 sm:px-6 py-8 space-y-8 outline-none"
      >
        {/* Welcome & Command Header */}
        <div className="p-6 rounded-2xl bg-white border border-slate-200/80 flex flex-col md:flex-row md:items-center justify-between gap-5 shadow-sm">
          <div className="space-y-1.5">
            <div className="flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-blue-600">
              <span>{primaryOrg.name}</span>
              <span className="w-1.5 h-1.5 rounded-full bg-emerald-500 animate-pulse" />
              <Badge variant="neutral" size="sm">
                ROLE: {primaryOrg.role}
              </Badge>
            </div>
            <h1 className="text-xl sm:text-2xl font-bold text-slate-900 tracking-tight">
              Operational Command &amp; Sandbox Control
            </h1>
            <p className="text-xs text-slate-500 max-w-2xl leading-relaxed">
              Real-time payment triage with deterministic fail-closed safety, multi-tenant dual custody, and verified PayPal Sandbox execution.
            </p>
          </div>

          <div className="flex items-center gap-3">
            <Link href="/app/payouts/new">
              <Button variant="primary" size="md">
                <PlusCircle className="w-4 h-4" aria-hidden="true" />
                <span>Create Payout Case</span>
              </Button>
            </Link>
            <Button
              variant="outline"
              size="md"
              onClick={fetchDashboardData}
              disabled={loadingData}
              aria-label="Refresh Dashboard Metrics"
              title="Refresh Dashboard"
            >
              <RefreshCw
                className={`w-4 h-4 ${loadingData ? "animate-spin" : ""}`}
                aria-hidden="true"
              />
            </Button>
          </div>
        </div>

        {/* Real Metrics Grid with exact PostgreSQL Aggregates */}
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 sm:gap-5">
          <StatCard
            label="Total Payout Cases"
            value={metrics?.total_cases ?? "—"}
            hint="Database aggregate records"
            icon={<CreditCard className="w-4 h-4 text-blue-600" aria-hidden="true" />}
            loading={loadingData && !metrics}
          />

          <StatCard
            label="Awaiting Dual Approval"
            value={metrics?.pending_dual_approvals ?? "—"}
            hint="Cases requiring secondary officer"
            icon={<Lock className="w-4 h-4 text-amber-600" aria-hidden="true" />}
            badge={
              metrics && metrics.pending_dual_approvals > 0 ? (
                <Badge variant="warning" size="sm">
                  Action Required
                </Badge>
              ) : undefined
            }
            loading={loadingData && !metrics}
          />

          <StatCard
            label="Settled USD (Minor Units)"
            value={
              metrics
                ? formatCentsToUSD(metrics.total_completed_cents)
                : "—"
            }
            hint={
              metrics
                ? `${metrics.completed_cases_count} completed payout cases`
                : "Integer arithmetic"
            }
            icon={<CheckCircle2 className="w-4 h-4 text-emerald-600" aria-hidden="true" />}
            loading={loadingData && !metrics}
          />

          <StatCard
            label="PayPal Sandbox Gateway"
            value={connStatus?.connected ? "Operational" : "Disconnected"}
            hint="api-m.sandbox.paypal.com"
            icon={<PlugZap className="w-4 h-4 text-blue-600" aria-hidden="true" />}
            badge={
              connStatus ? (
                <Badge
                  variant={connStatus.connected ? "success" : "danger"}
                  size="sm"
                  dot
                >
                  {connStatus.connected ? "Live OAuth" : "Offline"}
                </Badge>
              ) : undefined
            }
            loading={loadingData && !connStatus}
          />
        </div>

        {/* Recent Payout Cases Section */}
        <section className="rounded-xl bg-white border border-slate-200/80 overflow-hidden shadow-sm">
          <div className="p-6 border-b border-slate-100 flex items-center justify-between">
            <div className="flex items-center gap-2.5">
              <CreditCard className="w-5 h-5 text-blue-600" aria-hidden="true" />
              <div>
                <h2 className="text-base font-semibold text-slate-900 tracking-tight">
                  Recent Payout Operations
                </h2>
                <p className="text-xs text-slate-500">
                  Latest activity across your active workspace
                </p>
              </div>
            </div>
            <Link
              href="/app/payouts"
              className="text-xs font-semibold text-blue-600 hover:text-blue-700 transition inline-flex items-center gap-1 focus-visible:outline-none focus-visible:underline"
            >
              <span>View All Registry</span>
              <ArrowRight className="w-3.5 h-3.5" aria-hidden="true" />
            </Link>
          </div>

          {loadingData && cases.length === 0 ? (
            <div className="p-12 flex flex-col items-center justify-center gap-3 text-slate-500 text-xs">
              <div className="w-6 h-6 border-2 border-blue-600 border-t-transparent rounded-full animate-spin" />
              <span>Querying organization payout cases...</span>
            </div>
          ) : cases.length === 0 ? (
            <div className="p-12 text-center space-y-4">
              <div className="w-12 h-12 rounded-xl bg-slate-100 border border-slate-200 flex items-center justify-center mx-auto text-slate-400">
                <CreditCard className="w-6 h-6" aria-hidden="true" />
              </div>
              <div className="space-y-1">
                <p className="text-sm font-semibold text-slate-900">No payout cases recorded</p>
                <p className="text-xs text-slate-500 max-w-sm mx-auto">
                  Create your first settlement case to evaluate deterministic policy rules and execute verified sandbox transfers.
                </p>
              </div>
              <Link href="/app/payouts/new">
                <Button variant="primary" size="md">
                  <PlusCircle className="w-4 h-4" aria-hidden="true" />
                  <span>Create First Case</span>
                </Button>
              </Link>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="pl-6">Reference</TableHead>
                  <TableHead>Recipient</TableHead>
                  <TableHead>Amount</TableHead>
                  <TableHead>Governance Policy</TableHead>
                  <TableHead>Case Status</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead className="text-right pr-6">Workspaces</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {cases.map((c) => (
                  <TableRow key={c.id}>
                    <TableCell className="pl-6 font-mono font-semibold text-blue-600">
                      <Link
                        href={`/app/payouts/${c.id}`}
                        className="hover:underline focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-blue-500 rounded"
                      >
                        {c.reference}
                      </Link>
                    </TableCell>
                    <TableCell>
                      <div className="font-medium text-slate-900">{c.recipient_name}</div>
                      <div className="text-slate-500 text-[11px] font-mono">
                        {c.recipient_email}
                      </div>
                    </TableCell>
                    <TableCell className="font-mono font-semibold text-slate-900">
                      {formatCentsToUSD(c.amount_cents)}
                    </TableCell>
                    <TableCell>
                      {c.dual_approval_required ? (
                        <Badge variant="warning" size="sm" dot>
                          Dual-Custody
                        </Badge>
                      ) : (
                        <Badge variant="success" size="sm">
                          Single-Officer
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell>
                      <Badge
                        variant={
                          c.status === "COMPLETED"
                            ? "success"
                            : c.status === "SUBMITTED"
                            ? "info"
                            : c.status === "CANCELLED"
                            ? "danger"
                            : c.status === "FAILED"
                            ? "danger"
                            : "neutral"
                        }
                        size="sm"
                      >
                        {c.status}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-slate-500 text-[11px] whitespace-nowrap">
                      {formatDate(c.created_at)}
                    </TableCell>
                    <TableCell className="text-right pr-6">
                      <div className="inline-flex items-center gap-2">
                        <Link href={`/app/payouts/${c.id}`}>
                          <Button variant="outline" size="sm">
                            <span>Inspect</span>
                          </Button>
                        </Link>
                        <Link href={`/app/payouts/${c.id}/investigate`}>
                          <Button variant="secondary" size="sm" className="gap-1 text-blue-600">
                            <Sparkles className="w-3 h-3 text-blue-600" aria-hidden="true" />
                            <span>AI Triage</span>
                          </Button>
                        </Link>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </section>

        {/* Security Architecture & Guarantees */}
        <div className="grid grid-cols-1 md:grid-cols-3 gap-5 text-xs">
          <div className="p-5 rounded-xl bg-white border border-slate-200/80 space-y-2 shadow-sm">
            <div className="flex items-center gap-2 font-semibold text-slate-900">
              <Cpu className="w-4 h-4 text-blue-600" aria-hidden="true" />
              <span>Deterministic Fail-Closed</span>
            </div>
            <p className="text-slate-500 text-[11px] leading-relaxed">
              Amounts &gt; $500.00 USD are permanently denied by the policy engine. Amounts &gt; $250.00 require two distinct authenticated officers.
            </p>
          </div>

          <div className="p-5 rounded-xl bg-white border border-slate-200/80 space-y-2 shadow-sm">
            <div className="flex items-center gap-2 font-semibold text-slate-900">
              <Lock className="w-4 h-4 text-emerald-600" aria-hidden="true" />
              <span>Durable Operation Lifecycle</span>
            </div>
            <p className="text-slate-500 text-[11px] leading-relaxed">
              Unique sender batch IDs are committed to PostgreSQL before PayPal REST calls. Ambiguous network timeouts transition to RECONCILIATION_REQUIRED.
            </p>
          </div>

          <div className="p-5 rounded-xl bg-white border border-slate-200/80 space-y-2 shadow-sm">
            <div className="flex items-center gap-2 font-semibold text-slate-900">
              <Building2 className="w-4 h-4 text-purple-600" aria-hidden="true" />
              <span>Multi-Tenant Isolation</span>
            </div>
            <p className="text-slate-500 text-[11px] leading-relaxed">
              All cases, operations, and status snapshots are hard-isolated by organization ID at database and handler boundaries.
            </p>
          </div>
        </div>
      </main>
    </div>
  );
}
