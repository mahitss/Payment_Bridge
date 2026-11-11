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
    <div className="bg-white border border-slate-200/80 rounded-2xl p-8 shadow-sm">
      {status === "loading" && (
        <div className="text-center py-8 space-y-4">
          <Loader2 className="w-12 h-12 text-blue-600 animate-spin mx-auto" />
          <h2 className="text-lg font-semibold text-slate-900">Verifying your email address...</h2>
          <p className="text-xs text-slate-600">
            Checking cryptographic token against single-use security records.
          </p>
        </div>
      )}

      {status === "success" && (
        <div className="text-center py-6 space-y-4">
          <div className="w-14 h-14 rounded-full bg-emerald-50 border border-emerald-200 text-emerald-600 flex items-center justify-center mx-auto">
            <CheckCircle2 className="w-8 h-8" />
          </div>
          <div>
            <h2 className="text-xl font-bold text-slate-900 mb-1">Email Verified!</h2>
            <p className="text-xs text-slate-600 max-w-sm mx-auto">{message}</p>
          </div>
          <div className="pt-4">
            <Link
              href="/login"
              className="inline-flex items-center justify-center gap-2 px-6 py-2.5 rounded-lg bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium transition shadow-sm"
            >
              <span>Proceed to Login</span>
              <ArrowRight className="w-4 h-4" />
            </Link>
          </div>
        </div>
      )}

      {status === "error" && (
        <div className="space-y-6">
          <div className="p-4 rounded-lg bg-rose-50 border border-rose-200 text-rose-700 flex items-start gap-3">
            <AlertCircle className="w-5 h-5 shrink-0 mt-0.5 text-rose-600" />
            <div>
              <p className="text-sm font-semibold">Verification Failed</p>
              <p className="text-xs text-rose-600 mt-1">{message}</p>
            </div>
          </div>

          <form onSubmit={handleManualSubmit} className="space-y-4">
            <div>
              <label htmlFor="token" className="block text-xs font-semibold text-slate-700 mb-1.5">
                Paste token manually
              </label>
              <input
                id="token"
                type="text"
                value={token}
                onChange={(e) => setToken(e.target.value)}
                placeholder="Paste the verification token here..."
                className="w-full px-3.5 py-2.5 rounded-lg bg-white border border-slate-300 text-slate-900 placeholder:text-slate-400 text-sm focus:outline-none focus:border-blue-600 focus:ring-1 focus:ring-blue-600 transition font-mono"
              />
            </div>
            <button
              type="submit"
              className="w-full py-2.5 px-4 rounded-lg bg-slate-100 hover:bg-slate-200 text-slate-700 text-sm font-medium transition border border-slate-300"
            >
              Verify Token
            </button>
          </form>

          <div className="p-3.5 rounded-lg bg-slate-50 border border-slate-200 text-xs text-slate-600 space-y-1">
            <div className="font-semibold text-slate-800 flex items-center gap-1.5">
              <Mail className="w-3.5 h-3.5 text-blue-600" />
              <span>Local Mailpit Testing</span>
            </div>
            <p>
              In development, emails are routed to the local Mailpit inbox:{" "}
              <a
                href="http://localhost:8025"
                target="_blank"
                rel="noreferrer"
                className="text-blue-600 underline hover:text-blue-700"
              >
                http://localhost:8025
              </a>
            </p>
          </div>

          <div className="text-center pt-2">
            <Link href="/login" className="text-xs text-slate-500 hover:text-slate-800 underline">
              Return to Login
            </Link>
          </div>
        </div>
      )}

      {status === "idle" && (
        <form onSubmit={handleManualSubmit} className="space-y-4">
          <div className="text-center mb-6">
            <h2 className="text-lg font-bold text-slate-900 mb-1">Verify Email Address</h2>
            <p className="text-xs text-slate-600">
              Enter the verification token received in your inbox or Mailpit.
            </p>
          </div>
          <div>
            <label htmlFor="token" className="block text-xs font-semibold text-slate-700 mb-1.5">
              Verification Token
            </label>
            <input
              id="token"
              type="text"
              required
              value={token}
              onChange={(e) => setToken(e.target.value)}
              placeholder="Paste verification token..."
              className="w-full px-3.5 py-2.5 rounded-lg bg-white border border-slate-300 text-slate-900 placeholder:text-slate-400 text-sm focus:outline-none focus:border-blue-600 focus:ring-1 focus:ring-blue-600 transition font-mono"
            />
          </div>
          <button
            type="submit"
            className="w-full py-2.5 px-4 rounded-lg bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium transition shadow-sm"
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
          className="inline-flex items-center gap-2 px-3 py-1.5 rounded-lg bg-slate-900 border border-slate-800 text-slate-200 hover:text-white transition text-xs mb-6 shadow-sm"
        >
          <ShieldCheck className="w-4 h-4 text-blue-400" />
          <span>ClaimBridge Auth</span>
        </Link>
        <h1 className="text-2xl font-bold tracking-tight text-slate-900 mb-2">
          Email Verification
        </h1>
        <p className="text-xs text-slate-600">
          Cryptographically authenticated, single-use token validation
        </p>
      </div>

      <Suspense
        fallback={
          <div className="bg-white border border-slate-200/80 rounded-2xl p-8 text-center text-slate-500 text-sm shadow-sm">
            <Loader2 className="w-6 h-6 animate-spin mx-auto mb-2 text-blue-600" />
            Loading verification...
          </div>
        }
      >
        <VerifyEmailContent />
      </Suspense>
    </main>
  );
}
