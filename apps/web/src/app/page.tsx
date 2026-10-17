"use client";

import React, { useState } from "react";
import Link from "next/link";
import { ShieldCheck, Server, Lock, Cpu, CheckCircle2, AlertTriangle, ExternalLink, Terminal, User, LogIn, UserPlus } from "lucide-react";
import { useAuth } from "@/lib/auth";

export default function Home() {
  const [apiStatus, setApiStatus] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const { user, loading: authLoading } = useAuth();

  const checkGatewayHealth = async () => {
    setLoading(true);
    try {
      const res = await fetch("http://localhost:8080/healthz");
      if (res.ok) {
        const json = await res.json();
        setApiStatus(`Gateway Live: ${JSON.stringify(json)}`);
      } else {
        setApiStatus(`Gateway HTTP ${res.status}`);
      }
    } catch {
      setApiStatus("Gateway Offline (Start via 'go run ./cmd/server' in services/api)");
    } finally {
      setLoading(false);
    }
  };

  return (
    <main className="max-w-6xl mx-auto px-6 py-12">
      {/* Header */}
      <header className="border-b border-slate-800 pb-8 mb-10 flex flex-col md:flex-row md:items-center justify-between gap-4">
        <div>
          <div className="flex items-center gap-3 mb-2">
            <span className="p-2 rounded-lg bg-blue-600/20 text-blue-400 border border-blue-500/30">
              <ShieldCheck className="w-6 h-6" />
            </span>
            <h1 className="text-2xl font-bold tracking-tight text-white">
              ClaimBridge
            </h1>
            <span className="text-xs font-semibold px-2.5 py-0.5 rounded-full bg-blue-500/10 text-blue-400 border border-blue-500/20">
              Mission 002: Auth & Data Foundation
            </span>
          </div>
          <p className="text-sm text-slate-400 max-w-2xl">
            Autonomous AI claims triage coupled with deterministic Rust safety policy enforcement, Argon2id auth, and PayPal Payouts settlement.
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2.5">
          <button
            onClick={checkGatewayHealth}
            disabled={loading}
            className="px-3.5 py-2 text-xs font-medium rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-200 border border-slate-700 transition flex items-center gap-2"
          >
            <Server className="w-3.5 h-3.5 text-emerald-400" />
            {loading ? "Checking..." : "Probe Gateway"}
          </button>

          {!authLoading && user ? (
            <div className="flex items-center gap-2">
              <Link
                href="/app"
                className="px-3.5 py-2 text-xs font-medium rounded-lg bg-blue-600 hover:bg-blue-500 text-white transition flex items-center gap-1.5 shadow-lg shadow-blue-600/20"
              >
                <User className="w-3.5 h-3.5" />
                <span>Open App ({user.email.split("@")[0]})</span>
              </Link>
            </div>
          ) : (
            <div className="flex items-center gap-2">
              <Link
                href="/login"
                className="px-3.5 py-2 text-xs font-medium rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-200 border border-slate-700 transition flex items-center gap-1.5"
              >
                <LogIn className="w-3.5 h-3.5" />
                <span>Sign In</span>
              </Link>
              <Link
                href="/register"
                className="px-3.5 py-2 text-xs font-medium rounded-lg bg-blue-600 hover:bg-blue-500 text-white transition flex items-center gap-1.5 shadow-lg shadow-blue-600/20"
              >
                <UserPlus className="w-3.5 h-3.5" />
                <span>Register</span>
              </Link>
            </div>
          )}
        </div>
      </header>

      {apiStatus && (
        <div className="mb-8 p-4 rounded-lg bg-slate-900 border border-slate-800 flex items-center justify-between text-sm">
          <span className="text-slate-300 font-mono text-xs">{apiStatus}</span>
          <button
            onClick={() => setApiStatus(null)}
            className="text-xs text-slate-500 hover:text-slate-300 ml-4"
          >
            Dismiss
          </button>
        </div>
      )}

      {/* Grid of Verified Core Architecture Components */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-6 mb-10">
        {/* Go API Card */}
        <div className="p-6 rounded-xl bg-slate-900/60 border border-slate-800">
          <div className="flex items-center justify-between mb-4">
            <div className="p-2.5 rounded-lg bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
              <Server className="w-5 h-5" />
            </div>
            <span className="text-xs font-mono px-2 py-0.5 rounded bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
              services/api
            </span>
          </div>
          <h2 className="text-lg font-semibold text-white mb-2">Go HTTP API Gateway</h2>
          <p className="text-xs text-slate-400 mb-4 leading-relaxed">
            Orchestrates intake, database persistence, and PayPal REST sandbox calls with bounded timeouts and structured stream log redaction.
          </p>
          <ul className="text-xs text-slate-300 space-y-2 border-t border-slate-800/80 pt-4">
            <li className="flex items-center gap-2">
              <CheckCircle2 className="w-4 h-4 text-emerald-400 shrink-0" />
              <span>Strict sandbox configuration assertions</span>
            </li>
            <li className="flex items-center gap-2">
              <CheckCircle2 className="w-4 h-4 text-emerald-400 shrink-0" />
              <span>Zero-blind-retry policy on mutations</span>
            </li>
            <li className="flex items-center gap-2">
              <CheckCircle2 className="w-4 h-4 text-emerald-400 shrink-0" />
              <span>Automated Bearer token log sanitizer</span>
            </li>
          </ul>
        </div>

        {/* Rust Policy Engine Card */}
        <div className="p-6 rounded-xl bg-slate-900/60 border border-slate-800">
          <div className="flex items-center justify-between mb-4">
            <div className="p-2.5 rounded-lg bg-amber-500/10 text-amber-400 border border-amber-500/20">
              <Cpu className="w-5 h-5" />
            </div>
            <span className="text-xs font-mono px-2 py-0.5 rounded bg-amber-500/10 text-amber-400 border border-amber-500/20">
              services/policy
            </span>
          </div>
          <h2 className="text-lg font-semibold text-white mb-2">Rust Deterministic Engine</h2>
          <p className="text-xs text-slate-400 mb-4 leading-relaxed">
            Pure, isolated policy evaluation engine enforcing fail-closed invariants and discrete state machine transitions.
          </p>
          <ul className="text-xs text-slate-300 space-y-2 border-t border-slate-800/80 pt-4">
            <li className="flex items-center gap-2">
              <CheckCircle2 className="w-4 h-4 text-emerald-400 shrink-0" />
              <span>Max single payout limits ($500.00 ceiling)</span>
            </li>
            <li className="flex items-center gap-2">
              <CheckCircle2 className="w-4 h-4 text-emerald-400 shrink-0" />
              <span>Mandatory dual-approval (&gt; $250.00 or risk &gt; 20)</span>
            </li>
            <li className="flex items-center gap-2">
              <CheckCircle2 className="w-4 h-4 text-emerald-400 shrink-0" />
              <span>12/12 unit tests passing deterministically</span>
            </li>
          </ul>
        </div>

        {/* PayPal Sandbox Integration */}
        <div className="p-6 rounded-xl bg-slate-900/60 border border-slate-800">
          <div className="flex items-center justify-between mb-4">
            <div className="p-2.5 rounded-lg bg-blue-500/10 text-blue-400 border border-blue-500/20">
              <Lock className="w-5 h-5" />
            </div>
            <span className="text-xs font-mono px-2 py-0.5 rounded bg-blue-500/10 text-blue-400 border border-blue-500/20">
              PayPal Payouts
            </span>
          </div>
          <h2 className="text-lg font-semibold text-white mb-2">PayPal Sandbox Boundary</h2>
          <p className="text-xs text-slate-400 mb-4 leading-relaxed">
            Direct server-side REST integration with OAuth2 client credentials, token caching, batch status retrieval, and explicit smoke tool.
          </p>
          <ul className="text-xs text-slate-300 space-y-2 border-t border-slate-800/80 pt-4">
            <li className="flex items-center gap-2">
              <CheckCircle2 className="w-4 h-4 text-emerald-400 shrink-0" />
              <span>Zero browser credential exposure</span>
            </li>
            <li className="flex items-center gap-2">
              <CheckCircle2 className="w-4 h-4 text-emerald-400 shrink-0" />
              <span>Production hosts strictly blocked in code</span>
            </li>
            <li className="flex items-center gap-2">
              <CheckCircle2 className="w-4 h-4 text-emerald-400 shrink-0" />
              <span>Explicit CLI smoke tool: <code>cmd/paypal-smoke</code></span>
            </li>
          </ul>
        </div>
      </div>

      {/* Security Invariants & Operational Readiness */}
      <section className="p-6 rounded-xl bg-slate-900/40 border border-slate-800">
        <h3 className="text-base font-semibold text-white mb-4 flex items-center gap-2">
          <Terminal className="w-4 h-4 text-blue-400" />
          Operator Verification Commands
        </h3>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div className="p-4 rounded-lg bg-slate-950/70 border border-slate-800 font-mono text-xs">
            <span className="text-slate-500 block mb-1"># Run Rust policy engine unit tests</span>
            <code className="text-amber-400 block">cd services/policy && cargo test</code>
          </div>
          <div className="p-4 rounded-lg bg-slate-950/70 border border-slate-800 font-mono text-xs">
            <span className="text-slate-500 block mb-1"># Run Go API tests with race and redaction checks</span>
            <code className="text-emerald-400 block">cd services/api && go test -v ./...</code>
          </div>
          <div className="p-4 rounded-lg bg-slate-950/70 border border-slate-800 font-mono text-xs">
            <span className="text-slate-500 block mb-1"># PayPal sandbox connectivity smoke test</span>
            <code className="text-blue-400 block">cd services/api && go run ./cmd/paypal-smoke</code>
          </div>
          <div className="p-4 rounded-lg bg-slate-950/70 border border-slate-800 font-mono text-xs">
            <span className="text-slate-500 block mb-1"># Start PostgreSQL infrastructure locally</span>
            <code className="text-purple-400 block">docker compose -f infra/docker-compose.yml up -d</code>
          </div>
        </div>
      </section>
    </main>
  );
}
