"use client";

import React, { useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import {
  PlugZap,
  CheckCircle2,
  AlertTriangle,
  RefreshCw,
  ExternalLink,
  ShieldCheck,
  Server,
  Lock,
  ArrowLeft,
  KeyRound,
  FileCheck,
} from "lucide-react";
import { useAuth } from "@/lib/auth";
import Header from "@/components/Header";
import { ConnectionStatus, formatDate } from "@/lib/payouts";

export default function IntegrationsSettingsPage() {
  const { user, loading } = useAuth();
  const router = useRouter();

  const [status, setStatus] = useState<ConnectionStatus | null>(null);
  const [probing, setProbing] = useState<boolean>(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  useEffect(() => {
    if (!loading && !user) {
      router.push("/login?returnTo=/settings/integrations");
    }
  }, [user, loading, router]);

  const probeConnection = async () => {
    setProbing(true);
    setErrorMsg(null);
    try {
      const res = await fetch("/api/v1/paypal/connection-status", {
        method: "GET",
        credentials: "include",
      });
      const data: ConnectionStatus = await res.json();
      setStatus(data);
      if (!res.ok || !data.connected) {
        setErrorMsg(data.error || "Sandbox authentication check failed");
      }
    } catch {
      setErrorMsg("Network error connecting to API gateway");
      setStatus({
        connected: false,
        environment: "sandbox",
        base_url: "https://api-m.sandbox.paypal.com",
        checked_at: new Date().toISOString(),
        error: "Failed to communicate with Go backend",
      });
    } finally {
      setProbing(false);
    }
  };

  useEffect(() => {
    if (user) {
      probeConnection();
    }
  }, [user]);

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-slate-950 text-slate-400">
        <div className="flex flex-col items-center gap-3">
          <div className="w-8 h-8 border-2 border-blue-500 border-t-transparent rounded-full animate-spin" />
          <span className="text-xs font-mono">Loading integrations...</span>
        </div>
      </div>
    );
  }

  if (!user) {
    return null;
  }

  return (
    <div className="min-h-screen bg-slate-950 text-slate-100 flex flex-col">
      <Header />

      <main id="main-content" tabIndex={-1} className="flex-1 max-w-5xl w-full mx-auto px-6 py-8 space-y-6 outline-none">
        <Link
          href="/app"
          className="inline-flex items-center gap-1.5 min-h-[36px] text-xs text-slate-400 hover:text-white transition font-medium"
        >
          <ArrowLeft className="w-4 h-4" aria-hidden="true" />
          <span>Back to Overview</span>
        </Link>

        {/* Title */}
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
          <div>
            <div className="flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-blue-400 mb-1">
              <span>Environment & Provider Controls</span>
            </div>
            <h1 className="text-xl font-bold text-white tracking-tight">
              PayPal Sandbox Integration
            </h1>
            <p className="text-xs text-slate-400 mt-1">
              Live OAuth2 token authentication status with PayPal REST Sandbox. Production hosts are permanently blocked.
            </p>
          </div>

          <button
            onClick={probeConnection}
            disabled={probing}
            className="min-h-[44px] px-4 py-2.5 rounded-lg bg-blue-600 hover:bg-blue-500 disabled:opacity-50 text-white text-xs font-semibold transition flex items-center gap-2 shadow-lg shadow-blue-600/20"
          >
            <RefreshCw className={`w-4 h-4 ${probing ? "animate-spin" : ""}`} aria-hidden="true" />
            <span>{probing ? "Probing Sandbox..." : "Test Connection Now"}</span>
          </button>
        </div>

        {/* Status Card */}
        <div className="p-6 rounded-xl bg-slate-900/60 border border-slate-800 space-y-5">
          <div className="flex items-center justify-between border-b border-slate-800/80 pb-4">
            <div className="flex items-center gap-3">
              <div
                className={`p-2 rounded-lg ${
                  status?.connected
                    ? "bg-emerald-500/20 text-emerald-400 border border-emerald-500/30"
                    : "bg-amber-500/20 text-amber-400 border border-amber-500/30"
                }`}
              >
                <PlugZap className="w-5 h-5" aria-hidden="true" />
              </div>
              <div>
                <div className="flex items-center gap-2">
                  <h2 className="text-sm font-bold text-white">PayPal REST Sandbox API</h2>
                  <span
                    className={`px-2 py-0.5 rounded text-[10px] font-semibold tracking-wide ${
                      status?.connected
                        ? "bg-emerald-500/20 text-emerald-300 border border-emerald-500/30"
                        : "bg-amber-500/20 text-amber-300 border border-amber-500/30"
                    }`}
                  >
                    {status?.connected ? "AUTHENTICATED" : "DISCONNECTED / UNVERIFIED"}
                  </span>
                </div>
                <p className="text-xs text-slate-400 mt-0.5 font-mono">
                  Target Host: {status?.base_url || "https://api-m.sandbox.paypal.com"}
                </p>
              </div>
            </div>

            <div className="text-right text-xs text-slate-500 font-mono hidden sm:block">
              <div>Last Probe:</div>
              <div className="text-slate-300">{formatDate(status?.checked_at)}</div>
            </div>
          </div>

          {errorMsg && (
            <div
              role="alert"
              aria-live="assertive"
              className="p-3.5 rounded-lg bg-amber-500/10 border border-amber-500/30 text-amber-300 text-xs flex items-center gap-2.5"
            >
              <AlertTriangle className="w-4 h-4 flex-shrink-0 text-amber-400" aria-hidden="true" />
              <span>{errorMsg}</span>
            </div>
          )}

          {/* Connection Details Grid */}
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 text-xs font-mono">
            <div className="p-3.5 rounded-lg bg-slate-950 border border-slate-800 space-y-1">
              <span className="text-[10px] text-slate-500 uppercase">Configured Environment</span>
              <div className="text-emerald-400 font-semibold">sandbox (Strict)</div>
            </div>

            <div className="p-3.5 rounded-lg bg-slate-950 border border-slate-800 space-y-1">
              <span className="text-[10px] text-slate-500 uppercase">Provider Application ID</span>
              <div className="text-white truncate">
                {status?.app_id || (status?.connected ? "Active OAuth Client" : "Not authenticated")}
              </div>
            </div>

            <div className="p-3.5 rounded-lg bg-slate-950 border border-slate-800 space-y-1">
              <span className="text-[10px] text-slate-500 uppercase">Token Cache TTL</span>
              <div className="text-slate-300">
                {status?.token_expires_in
                  ? `${status.token_expires_in} seconds (buffered 60s)`
                  : "Server-side memory cached"}
              </div>
            </div>
          </div>
        </div>

        {/* Safe Setup & Developer Guidance */}
        <div className="p-6 rounded-xl bg-slate-900/40 border border-slate-800 space-y-4">
          <div className="flex items-center gap-2 font-semibold text-sm text-white">
            <KeyRound className="w-4 h-4 text-blue-400" />
            <span>Developer Sandbox Setup Guidance</span>
          </div>

          <div className="space-y-3 text-xs text-slate-300 leading-relaxed">
            <p>
              To authenticate ClaimBridge with your PayPal Sandbox developer account:
            </p>
            <ol className="list-decimal list-inside space-y-2 text-slate-400 pl-1">
              <li>
                Sign in to the{" "}
                <a
                  href="https://developer.paypal.com/dashboard/applications/sandbox"
                  target="_blank"
                  rel="noreferrer"
                  className="text-blue-400 hover:underline inline-flex items-center gap-1"
                >
                  <span>PayPal Developer Dashboard (Sandbox Apps)</span>
                  <ExternalLink className="w-3 h-3" />
                </a>
                .
              </li>
              <li>
                Create or select a Sandbox REST App with the <strong>Payouts</strong> feature enabled.
              </li>
              <li>
                Configure the credentials in your local environment file (<code>.env</code>):
                <pre className="p-3 my-2 rounded-lg bg-slate-950 border border-slate-800 text-blue-300 font-mono text-[11px] overflow-x-auto">
{`PAYPAL_ENV=sandbox
PAYPAL_CLIENT_ID=your_sandbox_client_id_here
PAYPAL_CLIENT_SECRET=your_sandbox_client_secret_here
PAYPAL_SANDBOX_BASE_URL=https://api-m.sandbox.paypal.com`}
                </pre>
              </li>
              <li>
                Restart the Go API server and click &ldquo;Test Connection Now&rdquo; above to verify the live OAuth2 handshake.
              </li>
            </ol>
          </div>

          {/* Security Safeguards Callout */}
          <div className="p-4 rounded-lg bg-slate-950 border border-slate-800/80 space-y-2 text-xs">
            <div className="flex items-center gap-2 font-semibold text-emerald-400">
              <ShieldCheck className="w-4 h-4" />
              <span>Architectural Security Invariants</span>
            </div>
            <ul className="list-disc list-inside space-y-1 text-slate-400 text-[11px]">
              <li>
                <strong>Server-Side Secret Isolation:</strong> PayPal credentials and access tokens are never transmitted to the browser or stored in Next.js public bundles.
              </li>
              <li>
                <strong>Strict Sandbox Host Enforcement:</strong> The Go API gateway immediately terminates at startup if <code>api-m.paypal.com</code> or any production URL is configured.
              </li>
              <li>
                <strong>Real Provider Verification:</strong> The connection status indicator only reports &ldquo;Authenticated&rdquo; when a live OAuth token has been successfully negotiated with PayPal.
              </li>
            </ul>
          </div>
        </div>
      </main>
    </div>
  );
}
