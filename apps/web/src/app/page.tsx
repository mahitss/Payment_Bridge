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
  Terminal,
  User,
  LogIn,
  UserPlus,
  ArrowRight,
  Sparkles,
  KeyRound,
  FileText,
} from "lucide-react";
import { useAuth } from "@/lib/auth";

export default function Home() {
  const [apiStatus, setApiStatus] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const { user, loading: authLoading } = useAuth();

  const checkGatewayHealth = async () => {
    setLoading(true);
    try {
      const res = await fetch("/api/v1/sandbox/status", { credentials: "include" });
      if (res.ok) {
        const json = await res.json();
        setApiStatus(`Gateway Live & Sandbox Connected: ${JSON.stringify(json)}`);
      } else {
        const json = await res.json().catch(() => ({}));
        setApiStatus(`Gateway Online (Sandbox Status: ${json.status || res.status})`);
      }
    } catch {
      setApiStatus("Gateway Offline (Start via 'go run ./cmd/server' in services/api)");
    } finally {
      setLoading(false);
    }
  };

  return (
    <main className="max-w-6xl mx-auto px-6 py-10 space-y-10">
      {/* Header */}
      <header className="border-b border-slate-800 pb-8 flex flex-col md:flex-row md:items-center justify-between gap-4">
        <div>
          <div className="flex items-center gap-3 mb-2 flex-wrap">
            <span className="p-2 rounded-lg bg-blue-600/20 text-blue-400 border border-blue-500/30">
              <ShieldCheck className="w-6 h-6" />
            </span>
            <h1 className="text-2xl font-bold tracking-tight text-white">
              ClaimBridge
            </h1>
            <span className="text-xs font-semibold px-2.5 py-0.5 rounded-full bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
              PayPal AI Hackathon 2026 • Verified Release
            </span>
          </div>
          <p className="text-sm text-slate-300 max-w-2xl leading-relaxed">
            ClaimBridge helps payout operators investigate unresolved payouts using verified PayPal status data, evidence-grounded AI explanations, and controlled recovery workflows.
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2.5">
          <button
            onClick={checkGatewayHealth}
            disabled={loading}
            className="px-3.5 py-2 text-xs font-medium rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-200 border border-slate-700 transition flex items-center gap-2"
          >
            <Server className="w-3.5 h-3.5 text-emerald-400" />
            {loading ? "Probing..." : "Probe Gateway"}
          </button>

          {!authLoading && user ? (
            <Link
              href="/app"
              className="px-4 py-2 text-xs font-semibold rounded-lg bg-blue-600 hover:bg-blue-500 text-white transition flex items-center gap-1.5 shadow-lg shadow-blue-600/20"
            >
              <User className="w-3.5 h-3.5" />
              <span>Enter Workspace</span>
              <ArrowRight className="w-3.5 h-3.5 ml-1" />
            </Link>
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
        <div className="p-4 rounded-lg bg-slate-900 border border-slate-800 flex items-center justify-between text-xs font-mono">
          <span className="text-slate-300">{apiStatus}</span>
          <button
            onClick={() => setApiStatus(null)}
            className="text-xs text-slate-500 hover:text-slate-300 ml-4 font-sans"
          >
            Dismiss
          </button>
        </div>
      )}

      {/* Evaluator Quick-Start Credentials Banner (Demo Mode Only) */}
      {process.env.NEXT_PUBLIC_DEMO_MODE === "true" && (
        <section className="p-5 rounded-xl bg-gradient-to-r from-blue-950/40 via-slate-900/60 to-purple-950/40 border border-blue-500/30 flex flex-col md:flex-row md:items-center justify-between gap-4">
          <div className="space-y-1">
            <div className="flex items-center gap-2 text-xs font-semibold text-blue-400">
              <Sparkles className="w-4 h-4 text-amber-400" />
              <span>Local Evaluation Fixtures (Demo Mode Enabled)</span>
            </div>
            <p className="text-xs text-slate-300">
              Pre-seeded accounts are local testing fixtures for evaluation purposes only. Normal registration and authentication remain fully active:
            </p>
            <div className="flex flex-wrap gap-4 text-xs font-mono text-slate-400 pt-1">
              <span>Operator: <code className="text-blue-300">operator@claimbridge.internal</code> (Pass: <code className="text-slate-200">OperatorPass123!</code>)</span>
              <span>Signer: <code className="text-purple-300">officer@claimbridge.internal</code> (Pass: <code className="text-slate-200">OfficerPass123!</code>)</span>
            </div>
          </div>

          <Link
            href="/login"
            className="self-start md:self-center px-4 py-2 rounded-lg bg-blue-600 hover:bg-blue-500 text-white text-xs font-semibold flex items-center gap-1.5 shrink-0 transition"
          >
            <span>Sign In to Demo Workspace</span>
            <ArrowRight className="w-3.5 h-3.5" />
          </Link>
        </section>
      )}

      {/* 4 Architectural Pillars */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-5">
        {/* Pillar 1: Go Orchestration Gateway */}
        <div className="p-5 rounded-xl bg-slate-900/60 border border-slate-800 space-y-3">
          <div className="flex items-center justify-between">
            <div className="p-2 rounded-lg bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
              <Server className="w-4 h-4" />
            </div>
            <span className="text-[10px] font-mono px-2 py-0.5 rounded bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
              Go Gateway
            </span>
          </div>
          <h2 className="text-sm font-bold text-white">Go API Orchestrator</h2>
          <p className="text-xs text-slate-400 leading-relaxed">
            High-concurrency HTTP API with Argon2id auth, organization isolation, 1MB payload limits, and durable operation lifecycle.
          </p>
          <ul className="text-[11px] text-slate-300 space-y-1.5 border-t border-slate-800/80 pt-3">
            <li className="flex items-center gap-1.5">
              <CheckCircle2 className="w-3.5 h-3.5 text-emerald-400 shrink-0" />
              <span>Durable sender_batch_id locking</span>
            </li>
            <li className="flex items-center gap-1.5">
              <CheckCircle2 className="w-3.5 h-3.5 text-emerald-400 shrink-0" />
              <span>Stream log secret sanitization</span>
            </li>
          </ul>
        </div>

        {/* Pillar 2: Deterministic Rust Policy Engine */}
        <div className="p-5 rounded-xl bg-slate-900/60 border border-slate-800 space-y-3">
          <div className="flex items-center justify-between">
            <div className="p-2 rounded-lg bg-amber-500/10 text-amber-400 border border-amber-500/20">
              <Cpu className="w-4 h-4" />
            </div>
            <span className="text-[10px] font-mono px-2 py-0.5 rounded bg-amber-500/10 text-amber-400 border border-amber-500/20">
              Rust Engine
            </span>
          </div>
          <h2 className="text-sm font-bold text-white">Deterministic Safety Policy</h2>
          <p className="text-xs text-slate-400 leading-relaxed">
            Isolated Rust engine enforcing fail-closed invariant validation, $500 ceiling, and separation-of-duties dual-custody approval.
          </p>
          <ul className="text-[11px] text-slate-300 space-y-1.5 border-t border-slate-800/80 pt-3">
            <li className="flex items-center gap-1.5">
              <CheckCircle2 className="w-3.5 h-3.5 text-amber-400 shrink-0" />
              <span>Mandatory dual-approval (&gt; $250.00)</span>
            </li>
            <li className="flex items-center gap-1.5">
              <CheckCircle2 className="w-3.5 h-3.5 text-amber-400 shrink-0" />
              <span>17/17 deterministic tests passed</span>
            </li>
          </ul>
        </div>

        {/* Pillar 3: AI Investigation Engine */}
        <div className="p-5 rounded-xl bg-slate-900/60 border border-slate-800 space-y-3">
          <div className="flex items-center justify-between">
            <div className="p-2 rounded-lg bg-purple-500/10 text-purple-400 border border-purple-500/20">
              <Brain className="w-4 h-4" />
            </div>
            <span className="text-[10px] font-mono px-2 py-0.5 rounded bg-purple-500/10 text-purple-400 border border-purple-500/20">
              Grounded AI
            </span>
          </div>
          <h2 className="text-sm font-bold text-white">AI Investigation Engine</h2>
          <p className="text-xs text-slate-400 leading-relaxed">
            Replaceable free-first model provider (local Ollama Llama 3.2) generating structured explanations with strict evidence grounding.
          </p>
          <ul className="text-[11px] text-slate-300 space-y-1.5 border-t border-slate-800/80 pt-3">
            <li className="flex items-center gap-1.5">
              <CheckCircle2 className="w-3.5 h-3.5 text-purple-400 shrink-0" />
              <span>Hallucinated evidence rejected (422)</span>
            </li>
            <li className="flex items-center gap-1.5">
              <CheckCircle2 className="w-3.5 h-3.5 text-purple-400 shrink-0" />
              <span>6-action strict recommendation whitelist</span>
            </li>
          </ul>
        </div>

        {/* Pillar 4: PayPal Payouts REST Boundary */}
        <div className="p-5 rounded-xl bg-slate-900/60 border border-slate-800 space-y-3">
          <div className="flex items-center justify-between">
            <div className="p-2 rounded-lg bg-blue-500/10 text-blue-400 border border-blue-500/20">
              <Lock className="w-4 h-4" />
            </div>
            <span className="text-[10px] font-mono px-2 py-0.5 rounded bg-blue-500/10 text-blue-400 border border-blue-500/20">
              PayPal REST
            </span>
          </div>
          <h2 className="text-sm font-bold text-white">PayPal Sandbox Boundary</h2>
          <p className="text-xs text-slate-400 leading-relaxed">
            Direct server-side OAuth2 integration with token caching, live status synchronization, and UNCLAIMED payout cancellation.
          </p>
          <ul className="text-[11px] text-slate-300 space-y-1.5 border-t border-slate-800/80 pt-3">
            <li className="flex items-center gap-1.5">
              <CheckCircle2 className="w-3.5 h-3.5 text-blue-400 shrink-0" />
              <span>Zero browser secret exposure</span>
            </li>
            <li className="flex items-center gap-1.5">
              <CheckCircle2 className="w-3.5 h-3.5 text-blue-400 shrink-0" />
              <span>Ambiguous timeout quarantine</span>
            </li>
          </ul>
        </div>
      </div>

      {/* Operator Verification Commands */}
      <section className="p-6 rounded-xl bg-slate-900/40 border border-slate-800 space-y-4">
        <h3 className="text-sm font-semibold text-white flex items-center gap-2">
          <Terminal className="w-4 h-4 text-blue-400" />
          <span>Operator Verification & Diagnostic Commands</span>
        </h3>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-3">
          <div className="p-3 rounded-lg bg-slate-950/70 border border-slate-800 font-mono text-[11px] space-y-1">
            <span className="text-slate-500 block"># 32 Acceptance Scenarios</span>
            <code className="text-emerald-400 block">go run ./cmd/acceptance</code>
          </div>
          <div className="p-3 rounded-lg bg-slate-950/70 border border-slate-800 font-mono text-[11px] space-y-1">
            <span className="text-slate-500 block"># Pre-Demo Readiness Probe</span>
            <code className="text-blue-400 block">go run ./cmd/readiness</code>
          </div>
          <div className="p-3 rounded-lg bg-slate-950/70 border border-slate-800 font-mono text-[11px] space-y-1">
            <span className="text-slate-500 block"># Seed Demo Fixtures</span>
            <code className="text-purple-400 block">go run ./cmd/seed</code>
          </div>
          <div className="p-3 rounded-lg bg-slate-950/70 border border-slate-800 font-mono text-[11px] space-y-1">
            <span className="text-slate-500 block"># Rust Policy Engine Tests</span>
            <code className="text-amber-400 block">cargo test</code>
          </div>
        </div>
      </section>
    </main>
  );
}
