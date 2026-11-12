"use client";

import React, { useState } from "react";
import Link from "next/link";
import {
  ShieldCheck,
  Server,
  Lock,
  Cpu,
  CheckCircle2,
  Brain,
  LogIn,
  UserPlus,
  ArrowRight,
  User,
  Activity,
  AlertTriangle,
  XCircle,
  Search,
  FileCheck,
  UserCheck,
  ChevronRight,
  Scale,
  History,
  ExternalLink,
  ShieldAlert,
  Layers,
} from "lucide-react";
import { useAuth } from "@/lib/auth";

interface ProbeState {
  gateway: "online" | "offline";
  sandbox: "authenticated" | "blocked" | "rejected" | "error" | "unknown" | "unreachable";
  message: string;
  timestamp: string;
}

export default function Home() {
  const [probeState, setProbeState] = useState<ProbeState | null>(null);
  const [loading, setLoading] = useState(false);
  const { user, loading: authLoading } = useAuth();

  const checkGatewayHealth = async () => {
    setLoading(true);
    const timeStr = new Date().toLocaleTimeString();
    try {
      // Step 1: Probe Gateway Health
      const healthRes = await fetch("/healthz");
      if (!healthRes.ok) {
        setProbeState({
          gateway: "offline",
          sandbox: "unreachable",
          message: "API Gateway reported unhealthy status or is restarting.",
          timestamp: timeStr,
        });
        return;
      }

      // Step 2: Probe PayPal Sandbox Status
      try {
        const sandboxRes = await fetch("/api/v1/sandbox/status", { credentials: "include" });
        const json = await sandboxRes.json().catch(() => null);

        if (sandboxRes.ok) {
          if (
            json &&
            json.status === "authenticated" &&
            json.token_valid === true &&
            json.paypal_env === "sandbox"
          ) {
            setProbeState({
              gateway: "online",
              sandbox: "authenticated",
              message: "API Gateway is operational. PayPal Sandbox OAuth2 authentication verified with official endpoint.",
              timestamp: timeStr,
            });
          } else {
            setProbeState({
              gateway: "online",
              sandbox: "unknown",
              message: "API Gateway is operational, but PayPal Sandbox status response was incomplete or unexpected.",
              timestamp: timeStr,
            });
          }
        } else {
          const errStr = json?.error || "";
          if (errStr.includes("Credentials not set") || errStr.includes("missing")) {
            setProbeState({
              gateway: "online",
              sandbox: "blocked",
              message: "API Gateway is operational. PayPal Sandbox credentials are not configured in local environment.",
              timestamp: timeStr,
            });
          } else if (errStr.includes("Invalid credentials") || sandboxRes.status === 401) {
            setProbeState({
              gateway: "online",
              sandbox: "rejected",
              message: "API Gateway is operational, but PayPal Sandbox rejected credentials with 401 Unauthorized.",
              timestamp: timeStr,
            });
          } else {
            setProbeState({
              gateway: "online",
              sandbox: "error",
              message: `API Gateway is operational; PayPal Sandbox returned status HTTP ${sandboxRes.status}.`,
              timestamp: timeStr,
            });
          }
        }
      } catch {
        setProbeState({
          gateway: "online",
          sandbox: "unreachable",
          message: "API Gateway is operational, but could not reach sandbox status route.",
          timestamp: timeStr,
        });
      }
    } catch {
      setProbeState({
        gateway: "offline",
        sandbox: "unreachable",
        message: "API Gateway is unreachable. Start the backend service via 'go run ./cmd/server' in services/api.",
        timestamp: timeStr,
      });
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen bg-slate-50 text-slate-900 flex flex-col">
      {/* Restrained Dark Navy Navigation Frame */}
      <nav
        aria-label="Public Header Navigation"
        className="border-b border-slate-800 bg-slate-950 text-slate-100 sticky top-0 z-40 shadow-sm"
      >
        <div className="max-w-7xl mx-auto px-4 sm:px-6 h-16 flex items-center justify-between">
          <div className="flex items-center gap-3">
            <Link
              href="/"
              className="flex items-center gap-2.5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 rounded-lg p-1"
            >
              <span className="p-1.5 rounded-lg bg-blue-600/20 text-blue-400 border border-blue-500/30">
                <ShieldCheck className="w-5 h-5" aria-hidden="true" />
              </span>
              <span className="font-bold text-base text-white tracking-tight">ClaimBridge</span>
            </Link>

            <span className="hidden sm:inline-block h-4 w-px bg-slate-800" aria-hidden="true" />

            <span className="inline-flex items-center text-[11px] font-semibold px-2.5 py-0.5 rounded-full bg-blue-500/10 text-blue-400 border border-blue-500/20">
              PayPal Sandbox Environment
            </span>
          </div>

          <div className="flex items-center gap-3">
            <button
              onClick={checkGatewayHealth}
              disabled={loading}
              className="min-h-[38px] px-3 py-1.5 text-xs font-medium rounded-lg bg-slate-900 hover:bg-slate-800 text-slate-300 hover:text-white border border-slate-800 transition flex items-center gap-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 disabled:opacity-50"
              aria-label="Probe Gateway Health"
            >
              <Server className="w-3.5 h-3.5 text-blue-400" aria-hidden="true" />
              <span>{loading ? "Probing..." : "Probe Gateway"}</span>
            </button>

            {!authLoading && user ? (
              <Link
                href="/app"
                className="min-h-[38px] px-4 py-1.5 text-xs font-semibold rounded-lg bg-blue-600 hover:bg-blue-700 text-white transition flex items-center gap-1.5 shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
              >
                <User className="w-3.5 h-3.5" aria-hidden="true" />
                <span>Enter Workspace</span>
                <ArrowRight className="w-3.5 h-3.5" aria-hidden="true" />
              </Link>
            ) : (
              <div className="flex items-center gap-2">
                <Link
                  href="/login"
                  className="min-h-[38px] px-3.5 py-1.5 text-xs font-medium rounded-lg bg-slate-900 hover:bg-slate-800 text-slate-200 border border-slate-800 transition flex items-center gap-1.5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
                >
                  <LogIn className="w-3.5 h-3.5 text-slate-400" aria-hidden="true" />
                  <span>Sign In</span>
                </Link>
                <Link
                  href="/register"
                  className="min-h-[38px] px-3.5 py-1.5 text-xs font-semibold rounded-lg bg-blue-600 hover:bg-blue-700 text-white transition flex items-center gap-1.5 shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
                >
                  <UserPlus className="w-3.5 h-3.5" aria-hidden="true" />
                  <span>Create Account</span>
                </Link>
              </div>
            )}
          </div>
        </div>
      </nav>

      {/* Main Operational Workspace */}
      <main id="main-content" tabIndex={-1} className="flex-1 max-w-7xl w-full mx-auto px-4 sm:px-6 py-12 space-y-16 outline-none">
        {/* Discreet Gateway Diagnostic Panel */}
        {probeState && (
          <div
            className="p-4 rounded-xl bg-white border border-slate-200 shadow-sm space-y-3 text-xs"
            role="status"
            aria-live="polite"
          >
            <div className="flex items-center justify-between border-b border-slate-100 pb-2.5">
              <div className="flex items-center gap-2">
                <Activity className="w-4 h-4 text-blue-600" aria-hidden="true" />
                <span className="font-semibold text-slate-900 uppercase tracking-wider text-[11px]">
                  System Diagnostic Probe
                </span>
              </div>
              <div className="flex items-center gap-3">
                <span className="text-[10px] text-slate-500 font-mono">{probeState.timestamp}</span>
                <button
                  onClick={() => setProbeState(null)}
                  className="text-xs text-slate-500 hover:text-slate-800 transition focus-visible:outline-none focus-visible:underline"
                >
                  Dismiss
                </button>
              </div>
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 pt-1">
              <div className="p-3 rounded-lg bg-slate-50 border border-slate-200/80 flex items-center justify-between">
                <span className="text-slate-600 font-medium">API Gateway:</span>
                {probeState.gateway === "online" ? (
                  <span className="inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-[11px] font-semibold bg-emerald-50 text-emerald-700 border border-emerald-200">
                    <span className="w-1.5 h-1.5 rounded-full bg-emerald-500 animate-pulse" />
                    Online &amp; Reachable
                  </span>
                ) : (
                  <span className="inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-[11px] font-semibold bg-rose-50 text-rose-700 border border-rose-200">
                    <span className="w-1.5 h-1.5 rounded-full bg-rose-500" />
                    Offline
                  </span>
                )}
              </div>

              <div className="p-3 rounded-lg bg-slate-50 border border-slate-200/80 flex items-center justify-between">
                <span className="text-slate-600 font-medium">PayPal Sandbox:</span>
                {probeState.sandbox === "authenticated" ? (
                  <span className="inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-[11px] font-semibold bg-emerald-50 text-emerald-700 border border-emerald-200">
                    <CheckCircle2 className="w-3.5 h-3.5 text-emerald-600" />
                    OAuth Verified
                  </span>
                ) : probeState.sandbox === "blocked" ? (
                  <span className="inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-[11px] font-semibold bg-amber-50 text-amber-700 border border-amber-200">
                    <AlertTriangle className="w-3.5 h-3.5 text-amber-600" />
                    Unconfigured / Blocked
                  </span>
                ) : probeState.sandbox === "rejected" ? (
                  <span className="inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-[11px] font-semibold bg-rose-50 text-rose-700 border border-rose-200">
                    <XCircle className="w-3.5 h-3.5 text-rose-600" />
                    Auth Rejected
                  </span>
                ) : probeState.sandbox === "unreachable" ? (
                  <span className="inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-[11px] font-semibold bg-slate-100 text-slate-600 border border-slate-200">
                    Unreachable
                  </span>
                ) : (
                  <span className="inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-[11px] font-semibold bg-rose-50 text-rose-700 border border-rose-200">
                    Provider Error / Unknown
                  </span>
                )}
              </div>
            </div>

            <p className="text-slate-700 text-xs leading-relaxed font-mono bg-slate-50 p-2.5 rounded border border-slate-200/80">
              {probeState.message}
            </p>
          </div>
        )}

        {/* Hero Section */}
        <section className="text-center max-w-4xl mx-auto space-y-6 pt-2">
          <div className="inline-flex items-center gap-2 px-3.5 py-1 rounded-full bg-blue-50 border border-blue-200 text-blue-700 text-xs font-semibold">
            <ShieldCheck className="w-3.5 h-3.5" aria-hidden="true" />
            <span>Financial Operations &amp; Compliance Platform</span>
          </div>

          <h1 className="text-3xl sm:text-4xl md:text-5xl font-bold tracking-tight text-slate-900 leading-[1.15]">
            <span className="text-blue-600 block sm:inline">ClaimBridge:</span> Autonomous Payout Investigation &amp; Controlled Financial Resolution
          </h1>

          <p className="text-base sm:text-lg text-slate-600 leading-relaxed max-w-3xl mx-auto">
            ClaimBridge helps payout operators investigate unresolved payouts using provider status records, evidence-grounded AI explanations, and deterministic safety policy enforcement.
          </p>

          <div className="flex flex-col sm:flex-row items-center justify-center gap-3.5 pt-2">
            {!authLoading && user ? (
              <Link
                href="/app"
                className="w-full sm:w-auto min-h-[46px] px-6 py-2.5 rounded-lg bg-blue-600 hover:bg-blue-700 text-white font-semibold text-sm transition flex items-center justify-center gap-2 shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:ring-offset-2"
              >
                <span>Enter Workspace</span>
                <ArrowRight className="w-4 h-4" aria-hidden="true" />
              </Link>
            ) : (
              <>
                <Link
                  href="/login"
                  className="w-full sm:w-auto min-h-[46px] px-6 py-2.5 rounded-lg bg-blue-600 hover:bg-blue-700 text-white font-semibold text-sm transition flex items-center justify-center gap-2 shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:ring-offset-2"
                >
                  <span>Enter Workspace</span>
                  <ArrowRight className="w-4 h-4" aria-hidden="true" />
                </Link>
                <Link
                  href="/register"
                  className="w-full sm:w-auto min-h-[46px] px-6 py-2.5 rounded-lg bg-white hover:bg-slate-50 text-slate-800 font-semibold text-sm border border-slate-200 shadow-sm transition flex items-center justify-center gap-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-slate-400 focus-visible:ring-offset-2"
                >
                  <span>Create Account</span>
                </Link>
              </>
            )}
          </div>

          {/* Institutional Trust Indicators */}
          <div className="pt-4 flex flex-wrap items-center justify-center gap-x-6 gap-y-2 text-xs text-slate-500 font-medium">
            <span className="flex items-center gap-1.5">
              <CheckCircle2 className="w-3.5 h-3.5 text-emerald-600" aria-hidden="true" />
              PayPal Sandbox REST OAuth2
            </span>
            <span className="hidden sm:inline text-slate-300">•</span>
            <span className="flex items-center gap-1.5">
              <CheckCircle2 className="w-3.5 h-3.5 text-emerald-600" aria-hidden="true" />
              Deterministic Fail-Closed Policy
            </span>
            <span className="hidden sm:inline text-slate-300">•</span>
            <span className="flex items-center gap-1.5">
              <CheckCircle2 className="w-3.5 h-3.5 text-emerald-600" aria-hidden="true" />
              Mandatory Dual Custody &gt; $250
            </span>
            <span className="hidden sm:inline text-slate-300">•</span>
            <span className="flex items-center gap-1.5">
              <CheckCircle2 className="w-3.5 h-3.5 text-emerald-600" aria-hidden="true" />
              Tenant-Isolated Audit Trail
            </span>
          </div>
        </section>

        {/* Product Preview Showcase: Genuine Operational Interface */}
        <section aria-label="Operational Product Preview" className="max-w-5xl mx-auto">
          <div className="rounded-2xl border border-slate-200/90 bg-white shadow-lg overflow-hidden">
            {/* Enterprise Application Header Bar */}
            <div className="border-b border-slate-800 bg-slate-950 px-5 py-3 text-slate-200 flex items-center justify-between">
              <div className="flex items-center gap-2 text-xs font-medium text-slate-300">
                <span className="text-slate-400">Workspace</span>
                <span className="text-slate-600">/</span>
                <span className="text-slate-400">Payouts</span>
                <span className="text-slate-600">/</span>
                <span className="font-mono text-blue-400 font-semibold">CB-2026-09821</span>
              </div>
              <div className="flex items-center gap-2">
                <span className="text-[11px] font-medium px-2.5 py-0.5 rounded-full bg-blue-500/10 text-blue-400 border border-blue-500/20">
                  INVESTIGATION ACTIVE
                </span>
                <span className="hidden sm:inline-flex text-[11px] font-medium px-2 py-0.5 rounded bg-slate-800 text-slate-300 border border-slate-700">
                  Priority: Standard
                </span>
              </div>
            </div>

            {/* Showcase Body: 3-Pillar Architecture */}
            <div className="p-6 grid grid-cols-1 lg:grid-cols-3 gap-5">
              {/* Pillar 1: Evidence Resolution */}
              <div className="rounded-xl border border-slate-200 bg-slate-50/70 p-4 space-y-3">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <FileCheck className="w-4 h-4 text-blue-600" aria-hidden="true" />
                    <span className="text-xs font-bold text-slate-900 uppercase tracking-wider">
                      Provider Evidence
                    </span>
                  </div>
                  <span className="text-[10px] font-mono px-2 py-0.5 rounded bg-emerald-50 text-emerald-700 border border-emerald-200 font-semibold">
                    UNCLAIMED
                  </span>
                </div>
                <div className="space-y-1.5 text-xs">
                  <div className="flex justify-between text-slate-600">
                    <span>Target Amount:</span>
                    <span className="font-mono font-bold text-slate-900">$185.00 USD</span>
                  </div>
                  <div className="flex justify-between text-slate-600">
                    <span>Batch Correlation:</span>
                    <span className="font-mono text-slate-800 text-[11px]">CB-BATCH-09821</span>
                  </div>
                  <div className="flex justify-between text-slate-600">
                    <span>PayPal Debug ID:</span>
                    <span className="font-mono text-slate-800 text-[11px]">SANDBOX-PAYPAL-09821</span>
                  </div>
                </div>
                <div className="p-2.5 rounded-lg bg-white border border-slate-200 text-[11px] text-slate-600 space-y-1 font-mono">
                  <div className="text-slate-500 text-[10px] uppercase">Grounded Hash Ref</div>
                  <div className="text-blue-700 font-semibold truncate">EVD-PAYPAL-STATUS-09821</div>
                </div>
              </div>

              {/* Pillar 2: AI Triage Recommendations */}
              <div className="rounded-xl border border-blue-200 bg-blue-50/40 p-4 space-y-3">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <Brain className="w-4 h-4 text-blue-600" aria-hidden="true" />
                    <span className="text-xs font-bold text-slate-900 uppercase tracking-wider">
                      AI Triage Engine
                    </span>
                  </div>
                  <span className="text-[10px] font-mono px-2 py-0.5 rounded bg-blue-100 text-blue-800 font-semibold">
                    Confidence: HIGH
                  </span>
                </div>
                <p className="text-xs text-slate-700 leading-relaxed">
                  Payout transfer remains in UNCLAIMED status at provider. Cancellation and reissue recommended once identity is verified.
                </p>
                <div className="p-2.5 rounded-lg bg-white border border-blue-200/80 text-[11px] text-slate-700 space-y-1">
                  <div className="flex items-center gap-1.5 text-amber-700 font-semibold">
                    <AlertTriangle className="w-3.5 h-3.5" aria-hidden="true" />
                    <span>Independent Review Mandated</span>
                  </div>
                  <div className="text-slate-500 text-[10px]">
                    AI output does not possess execution authority; human approval required.
                  </div>
                </div>
              </div>

              {/* Pillar 3: Deterministic Financial Policy */}
              <div className="rounded-xl border border-slate-200 bg-slate-50/70 p-4 space-y-3">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <Scale className="w-4 h-4 text-blue-600" aria-hidden="true" />
                    <span className="text-xs font-bold text-slate-900 uppercase tracking-wider">
                      Deterministic Policy
                    </span>
                  </div>
                  <span className="text-[10px] font-mono px-2 py-0.5 rounded bg-emerald-50 text-emerald-700 border border-emerald-200 font-semibold">
                    Enforced
                  </span>
                </div>
                <div className="space-y-1.5 text-xs">
                  <div className="flex justify-between items-center text-slate-600">
                    <span>Ceiling Check (&le; $500):</span>
                    <span className="font-semibold text-emerald-700 flex items-center gap-1">
                      <CheckCircle2 className="w-3.5 h-3.5 text-emerald-600" />
                      Passed ($185.00)
                    </span>
                  </div>
                  <div className="flex justify-between items-center text-slate-600">
                    <span>Dual Custody (&gt; $250):</span>
                    <span className="font-semibold text-slate-800">Single Officer</span>
                  </div>
                  <div className="flex justify-between items-center text-slate-600">
                    <span>Self-Approval Guard:</span>
                    <span className="font-semibold text-emerald-700">Blocked Fail-Closed</span>
                  </div>
                </div>
                <div className="p-2.5 rounded-lg bg-white border border-slate-200 text-[11px] text-slate-600 space-y-1">
                  <div className="text-slate-500 text-[10px] uppercase font-mono">Governance Rule</div>
                  <div className="text-slate-800 font-semibold">RULE-PAYPAL-CANCEL-UNCLAIMED-ONLY</div>
                </div>
              </div>
            </div>
          </div>
        </section>

        {/* 6-Stage Resolution Workflow Section */}
        <section className="space-y-8" aria-labelledby="workflow-heading">
          <div className="text-center space-y-2">
            <h2 id="workflow-heading" className="text-xl sm:text-2xl font-bold text-slate-900 tracking-tight">
              Grounded End-to-End Investigation Workflow
            </h2>
            <p className="text-xs sm:text-sm text-slate-500 max-w-xl mx-auto">
              Every payout operation passes through six disciplined stages to eliminate ambiguity, enforce dual custody, and maintain complete auditability.
            </p>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {/* Step 1 */}
            <div className="p-5 rounded-xl bg-white border border-slate-200/80 shadow-sm flex flex-col justify-between space-y-3">
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <span className="w-7 h-7 rounded-lg bg-blue-50 text-blue-700 border border-blue-200 flex items-center justify-center text-xs font-bold font-mono">
                    01
                  </span>
                  <Search className="w-4 h-4 text-slate-400" aria-hidden="true" />
                </div>
                <h3 className="text-sm font-semibold text-slate-900">Investigate Payouts</h3>
                <p className="text-xs text-slate-500 leading-relaxed">
                  Identify unresolved, unconfirmed, or disputed transfers across multi-tenant ledger partitions.
                </p>
              </div>
              <div className="pt-2 border-t border-slate-100 text-[11px] text-slate-400 font-mono">
                Lifecycle Tracking
              </div>
            </div>

            {/* Step 2 */}
            <div className="p-5 rounded-xl bg-white border border-slate-200/80 shadow-sm flex flex-col justify-between space-y-3">
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <span className="w-7 h-7 rounded-lg bg-blue-50 text-blue-700 border border-blue-200 flex items-center justify-center text-xs font-bold font-mono">
                    02
                  </span>
                  <FileCheck className="w-4 h-4 text-slate-400" aria-hidden="true" />
                </div>
                <h3 className="text-sm font-semibold text-slate-900">Review Evidence</h3>
                <p className="text-xs text-slate-500 leading-relaxed">
                  Inspect raw PayPal status, item batches, timestamps, and immutable PostgreSQL audit trails.
                </p>
              </div>
              <div className="pt-2 border-t border-slate-100 text-[11px] text-slate-400 font-mono">
                Cryptographic Audit
              </div>
            </div>

            {/* Step 3 */}
            <div className="p-5 rounded-xl bg-white border border-slate-200/80 shadow-sm flex flex-col justify-between space-y-3">
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <span className="w-7 h-7 rounded-lg bg-blue-50 text-blue-700 border border-blue-200 flex items-center justify-center text-xs font-bold font-mono">
                    03
                  </span>
                  <Brain className="w-4 h-4 text-blue-600" aria-hidden="true" />
                </div>
                <h3 className="text-sm font-semibold text-slate-900">AI Recommendations</h3>
                <p className="text-xs text-slate-500 leading-relaxed">
                  Receive structured triage recommendations grounded strictly in verified provider evidence without hallucination.
                </p>
              </div>
              <div className="pt-2 border-t border-slate-100 text-[11px] text-slate-400 font-mono">
                Evidence-Grounded
              </div>
            </div>

            {/* Step 4 */}
            <div className="p-5 rounded-xl bg-white border border-slate-200/80 shadow-sm flex flex-col justify-between space-y-3">
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <span className="w-7 h-7 rounded-lg bg-blue-50 text-blue-700 border border-blue-200 flex items-center justify-center text-xs font-bold font-mono">
                    04
                  </span>
                  <Scale className="w-4 h-4 text-slate-400" aria-hidden="true" />
                </div>
                <h3 className="text-sm font-semibold text-slate-900">Deterministic Policy</h3>
                <p className="text-xs text-slate-500 leading-relaxed">
                  Deterministic policy engine enforces fail-closed ceilings, dual-custody thresholds, and integer-cent precision.
                </p>
              </div>
              <div className="pt-2 border-t border-slate-100 text-[11px] text-slate-400 font-mono">
                Fail-Closed Controls
              </div>
            </div>

            {/* Step 5 */}
            <div className="p-5 rounded-xl bg-white border border-slate-200/80 shadow-sm flex flex-col justify-between space-y-3">
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <span className="w-7 h-7 rounded-lg bg-blue-50 text-blue-700 border border-blue-200 flex items-center justify-center text-xs font-bold font-mono">
                    05
                  </span>
                  <UserCheck className="w-4 h-4 text-slate-400" aria-hidden="true" />
                </div>
                <h3 className="text-sm font-semibold text-slate-900">Authorized Human Approval</h3>
                <p className="text-xs text-slate-500 leading-relaxed">
                  Require distinct compliance officer approval. Self-approval is strictly prohibited and blocked fail-closed.
                </p>
              </div>
              <div className="pt-2 border-t border-slate-100 text-[11px] text-slate-400 font-mono">
                Dual Custody Enforced
              </div>
            </div>

            {/* Step 6 */}
            <div className="p-5 rounded-xl bg-white border border-slate-200/80 shadow-sm flex flex-col justify-between space-y-3">
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <span className="w-7 h-7 rounded-lg bg-blue-50 text-blue-700 border border-blue-200 flex items-center justify-center text-xs font-bold font-mono">
                    06
                  </span>
                  <History className="w-4 h-4 text-slate-400" aria-hidden="true" />
                </div>
                <h3 className="text-sm font-semibold text-slate-900">Complete Audit Trail</h3>
                <p className="text-xs text-slate-500 leading-relaxed">
                  Record complete lifecycle history with immutable cryptographic hashes and timestamped operator signatures.
                </p>
              </div>
              <div className="pt-2 border-t border-slate-100 text-[11px] text-slate-400 font-mono">
                Immutable Ledger
              </div>
            </div>
          </div>
        </section>

        {/* Financial Trust & Safeguards Section */}
        <section className="rounded-2xl bg-white border border-slate-200/80 p-8 shadow-sm space-y-6" aria-labelledby="safeguards-heading">
          <div className="max-w-2xl">
            <h2 id="safeguards-heading" className="text-xl font-bold text-slate-900 tracking-tight">
              Institutional Financial Safeguards
            </h2>
            <p className="text-xs sm:text-sm text-slate-500 mt-1 leading-relaxed">
              Engineered with strict zero-trust boundaries to protect capital and prevent unauthorized disbursements.
            </p>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6 pt-2">
            <div className="space-y-2">
              <div className="flex items-center gap-2 text-sm font-semibold text-slate-900">
                <CheckCircle2 className="w-4 h-4 text-emerald-600 shrink-0" aria-hidden="true" />
                <span>Deterministic Safety</span>
              </div>
              <p className="text-xs text-slate-500 leading-relaxed">
                Deterministic policy engine enforces a hard fail-closed $500 disbursement ceiling and verified risk thresholds with zero AI bypass.
              </p>
            </div>

            <div className="space-y-2">
              <div className="flex items-center gap-2 text-sm font-semibold text-slate-900">
                <CheckCircle2 className="w-4 h-4 text-emerald-600 shrink-0" aria-hidden="true" />
                <span>Dual-Custody Approvals</span>
              </div>
              <p className="text-xs text-slate-500 leading-relaxed">
                Transactions over $250.00 mandate independent officer authorization. Initiator self-approval is blocked fail-closed.
              </p>
            </div>

            <div className="space-y-2">
              <div className="flex items-center gap-2 text-sm font-semibold text-slate-900">
                <CheckCircle2 className="w-4 h-4 text-emerald-600 shrink-0" aria-hidden="true" />
                <span>Preflight Idempotency</span>
              </div>
              <p className="text-xs text-slate-500 leading-relaxed">
                Durable pre-generation and database-locking of sender_batch_id ensures ambiguous timeouts can never cause double disbursement.
              </p>
            </div>

            <div className="space-y-2">
              <div className="flex items-center gap-2 text-sm font-semibold text-slate-900">
                <CheckCircle2 className="w-4 h-4 text-emerald-600 shrink-0" aria-hidden="true" />
                <span>Secret Isolation</span>
              </div>
              <p className="text-xs text-slate-500 leading-relaxed">
                OAuth2 credentials never cross the server boundary into browser bundles. PayPal production hosts are permanently rejected.
              </p>
            </div>
          </div>
        </section>
      </main>

      {/* Footer */}
      <footer className="border-t border-slate-200 bg-white py-6 text-xs text-slate-500">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 flex flex-col sm:flex-row items-center justify-between gap-3">
          <div className="flex items-center gap-2">
            <ShieldCheck className="w-4 h-4 text-blue-600" aria-hidden="true" />
            <span className="font-semibold text-slate-900">ClaimBridge</span>
            <span>• Financial Operations Platform</span>
          </div>
          <div className="text-[11px] text-slate-400">
            PayPal Sandbox Environment • Multi-Tenant Financial Controls
          </div>
        </div>
      </footer>
    </div>
  );
}
