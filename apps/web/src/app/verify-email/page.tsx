"use client";

import React, { useState, useEffect, Suspense } from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { ShieldCheck, CheckCircle2, AlertCircle, Loader2, ArrowRight, Mail } from "lucide-react";

function VerifyEmailContent() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const tokenParam = searchParams.get("token") || "";

  const [token, setToken] = useState(tokenParam);
  const [status, setStatus] = useState<"idle" | "loading" | "success" | "error">(
    tokenParam ? "loading" : "idle"
  );
  const [message, setMessage] = useState<string>("");

  useEffect(() => {
    if (tokenParam) {
      setToken(tokenParam);
      executeVerification(tokenParam);
    }
  }, [tokenParam]);

  const executeVerification = async (verifyToken: string) => {
    if (!verifyToken.trim()) {
      setStatus("error");
      setMessage("Verification token is required.");
      return;
    }

    setStatus("loading");
    setMessage("");

    try {
      const res = await fetch("/api/v1/auth/verify-email", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "include",
        body: JSON.stringify({ token: verifyToken.trim() }),
      });

      const data = await res.json();

      if (res.ok) {
        setStatus("success");
        setMessage(data.message || "Email address verified successfully.");
      } else {
        setStatus("error");
        setMessage(data.error || "The verification link is invalid, expired, or has already been used.");
      }
    } catch {
      setStatus("error");
      setMessage("Unable to contact verification service. Please ensure the backend is running.");
    }
  };

  const handleManualSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    executeVerification(token);
  };

  return (
    <div className="bg-slate-900/80 border border-slate-800 rounded-2xl p-8 backdrop-blur-sm shadow-xl">
      {status === "loading" && (
        <div className="text-center py-8 space-y-4">
          <Loader2 className="w-12 h-12 text-blue-500 animate-spin mx-auto" />
          <h2 className="text-lg font-semibold text-white">Verifying your email address...</h2>
          <p className="text-xs text-slate-400">
            Checking cryptographic token against single-use security records.
          </p>
        </div>
      )}

      {status === "success" && (
        <div className="text-center py-6 space-y-4">
          <div className="w-14 h-14 rounded-full bg-emerald-500/10 border border-emerald-500/20 text-emerald-400 flex items-center justify-center mx-auto">
            <CheckCircle2 className="w-8 h-8" />
          </div>
          <div>
            <h2 className="text-xl font-bold text-white mb-1">Email Verified!</h2>
            <p className="text-xs text-slate-400 max-w-sm mx-auto">{message}</p>
          </div>
          <div className="pt-4">
            <Link
              href="/login"
              className="inline-flex items-center justify-center gap-2 px-6 py-2.5 rounded-lg bg-blue-600 hover:bg-blue-500 text-white text-sm font-medium transition shadow-lg shadow-blue-600/20"
            >
              <span>Proceed to Login</span>
              <ArrowRight className="w-4 h-4" />
            </Link>
          </div>
        </div>
      )}

      {status === "error" && (
        <div className="space-y-6">
          <div className="p-4 rounded-lg bg-rose-500/10 border border-rose-500/20 text-rose-400 flex items-start gap-3">
            <AlertCircle className="w-5 h-5 shrink-0 mt-0.5" />
            <div>
              <p className="text-sm font-semibold">Verification Failed</p>
              <p className="text-xs text-rose-300 mt-1">{message}</p>
            </div>
          </div>

          <form onSubmit={handleManualSubmit} className="space-y-4">
            <div>
              <label htmlFor="token" className="block text-xs font-medium text-slate-300 mb-1.5">
                Paste token manually
              </label>
              <input
                id="token"
                type="text"
                value={token}
                onChange={(e) => setToken(e.target.value)}
                placeholder="Paste the verification token here..."
                className="w-full px-3.5 py-2.5 rounded-lg bg-slate-950/60 border border-slate-800 text-slate-100 text-sm focus:outline-none focus:border-blue-500 focus:ring-1 focus:ring-blue-500 transition font-mono"
              />
            </div>
            <button
              type="submit"
              className="w-full py-2.5 px-4 rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-200 text-sm font-medium transition border border-slate-700"
            >
              Verify Token
            </button>
          </form>

          <div className="p-3.5 rounded-lg bg-slate-950 border border-slate-800 text-xs text-slate-400 space-y-1">
            <div className="font-semibold text-slate-300 flex items-center gap-1.5">
              <Mail className="w-3.5 h-3.5 text-blue-400" />
              <span>Local Mailpit Testing</span>
            </div>
            <p>
              In development, emails are routed to the local Mailpit inbox:{" "}
              <a
                href="http://localhost:8025"
                target="_blank"
                rel="noreferrer"
                className="text-blue-400 underline hover:text-blue-300"
              >
                http://localhost:8025
              </a>
            </p>
          </div>

          <div className="text-center pt-2">
            <Link href="/login" className="text-xs text-slate-400 hover:text-slate-300 underline">
              Return to Login
            </Link>
          </div>
        </div>
      )}

      {status === "idle" && (
        <form onSubmit={handleManualSubmit} className="space-y-4">
          <div className="text-center mb-6">
            <h2 className="text-lg font-bold text-white mb-1">Verify Email Address</h2>
            <p className="text-xs text-slate-400">
              Enter the verification token received in your inbox or Mailpit.
            </p>
          </div>
          <div>
            <label htmlFor="token" className="block text-xs font-medium text-slate-300 mb-1.5">
              Verification Token
            </label>
            <input
              id="token"
              type="text"
              required
              value={token}
              onChange={(e) => setToken(e.target.value)}
              placeholder="Paste verification token..."
              className="w-full px-3.5 py-2.5 rounded-lg bg-slate-950/60 border border-slate-800 text-slate-100 text-sm focus:outline-none focus:border-blue-500 focus:ring-1 focus:ring-blue-500 transition font-mono"
            />
          </div>
          <button
            type="submit"
            className="w-full py-2.5 px-4 rounded-lg bg-blue-600 hover:bg-blue-500 text-white text-sm font-medium transition shadow-lg shadow-blue-600/20"
          >
            Verify Email
          </button>
        </form>
      )}
    </div>
  );
}

export default function VerifyEmailPage() {
  return (
    <main className="max-w-md mx-auto px-4 py-16">
      <div className="text-center mb-8">
        <Link
          href="/"
          className="inline-flex items-center gap-2 px-3 py-1.5 rounded-lg bg-slate-900 border border-slate-800 text-slate-300 hover:text-white transition text-xs mb-6"
        >
          <ShieldCheck className="w-4 h-4 text-blue-400" />
          <span>ClaimBridge Auth</span>
        </Link>
        <h1 className="text-2xl font-bold tracking-tight text-white mb-2">
          Email Verification
        </h1>
        <p className="text-xs text-slate-400">
          Cryptographically authenticated, single-use token validation
        </p>
      </div>

      <Suspense
        fallback={
          <div className="bg-slate-900/80 border border-slate-800 rounded-2xl p-8 text-center text-slate-400 text-sm">
            <Loader2 className="w-6 h-6 animate-spin mx-auto mb-2 text-blue-500" />
            Loading verification...
          </div>
        }
      >
        <VerifyEmailContent />
      </Suspense>
    </main>
  );
}
