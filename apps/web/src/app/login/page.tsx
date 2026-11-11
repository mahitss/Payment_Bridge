"use client";

import React, { useState, Suspense } from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { ShieldCheck, Mail, Lock, Eye, EyeOff, ArrowRight, AlertCircle, Info } from "lucide-react";
import { useAuth, sanitizeReturnUrl } from "@/lib/auth";

function LoginForm() {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [isUnverified, setIsUnverified] = useState(false);

  const router = useRouter();
  const searchParams = useSearchParams();
  const returnTo = sanitizeReturnUrl(searchParams.get("returnTo"));
  const { refreshUser } = useAuth();

  const handleLogin = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setIsUnverified(false);

    if (!email.trim() || !password) {
      setError("Please enter both email and password.");
      return;
    }

    setLoading(true);

    try {
      const res = await fetch("/api/v1/auth/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "include",
        body: JSON.stringify({ email, password }),
      });

      const data = await res.json();

      if (!res.ok) {
        if (data.code === "EMAIL_NOT_VERIFIED") {
          setIsUnverified(true);
          setError(data.error || "Email not verified. Please check your inbox.");
        } else {
          setError(data.error || "Invalid email or password.");
        }
      } else {
        await refreshUser();
        router.push(returnTo);
      }
    } catch {
      setError("Failed to connect to the authentication server. Please verify backend availability.");
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="bg-white border border-slate-200/80 rounded-2xl p-8 shadow-sm">
      <form onSubmit={handleLogin} className="space-y-4">
        {error && (
          <div
            role="alert"
            aria-live="assertive"
            className="p-3.5 rounded-lg bg-rose-50 border border-rose-200 text-xs text-rose-800 flex items-start gap-2"
          >
            <AlertCircle className="w-4 h-4 shrink-0 mt-0.5 text-rose-600" aria-hidden="true" />
            <div>
              <span>{error}</span>
              {isUnverified && (
                <div className="mt-2 text-slate-700">
                  <span>Haven&apos;t received your verification link? Check{" "}</span>
                  <a
                    href="http://localhost:8025"
                    target="_blank"
                    rel="noreferrer"
                    className="text-blue-600 underline hover:text-blue-700"
                  >
                    Mailpit (http://localhost:8025)
                  </a>
                  {" "}or register again.
                </div>
              )}
            </div>
          </div>
        )}

        {/* Email */}
        <div>
          <label className="block text-xs font-medium text-slate-700 mb-1.5" htmlFor="email">
            Email Address
          </label>
          <div className="relative">
            <Mail className="w-4 h-4 text-slate-400 absolute left-3 top-3.5" aria-hidden="true" />
            <input
              id="email"
              type="email"
              required
              autoComplete="email"
              inputMode="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="user@example.com"
              className="w-full pl-9 pr-3 py-2.5 bg-white border border-slate-200 rounded-lg text-sm text-slate-900 placeholder-slate-400 focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:outline-none transition"
            />
          </div>
        </div>

        {/* Password */}
        <div>
          <div className="flex items-center justify-between mb-1.5">
            <label className="text-xs font-medium text-slate-700" htmlFor="password">
              Password
            </label>
            <Link
              href="/forgot-password"
              className="text-xs text-blue-600 hover:text-blue-700 transition focus-visible:ring-2 focus-visible:ring-blue-500 rounded px-1"
            >
              Forgot password?
            </Link>
          </div>
          <div className="relative">
            <Lock className="w-4 h-4 text-slate-400 absolute left-3 top-3.5" aria-hidden="true" />
            <input
              id="password"
              type={showPassword ? "text" : "password"}
              required
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="••••••••••••"
              className="w-full pl-9 pr-12 py-2.5 bg-white border border-slate-200 rounded-lg text-sm text-slate-900 placeholder-slate-400 focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:outline-none transition"
            />
            <button
              type="button"
              onClick={() => setShowPassword(!showPassword)}
              className="absolute right-2 top-2 p-1.5 min-w-[36px] min-h-[36px] text-slate-400 hover:text-slate-600 transition rounded focus-visible:ring-2 focus-visible:ring-blue-500 flex items-center justify-center"
              aria-label={showPassword ? "Hide password" : "Show password"}
            >
              {showPassword ? (
                <EyeOff className="w-4 h-4" aria-hidden="true" />
              ) : (
                <Eye className="w-4 h-4" aria-hidden="true" />
              )}
            </button>
          </div>
        </div>

        {/* Submit */}
        <button
          type="submit"
          disabled={loading}
          className="w-full mt-2 min-h-[44px] py-2.5 px-4 bg-blue-600 hover:bg-blue-700 disabled:bg-blue-600/50 text-white font-medium text-sm rounded-lg transition flex items-center justify-center gap-2 shadow-sm focus-visible:ring-2 focus-visible:ring-blue-500"
        >
          {loading ? (
            <>
              <div className="w-4 h-4 border-2 border-white border-t-transparent rounded-full animate-spin" aria-hidden="true" />
              <span>Signing in...</span>
            </>
          ) : (
            <>
              <span>Sign In</span>
              <ArrowRight className="w-4 h-4" aria-hidden="true" />
            </>
          )}
        </button>
      </form>

      <div className="mt-6 pt-6 border-t border-slate-100 text-center">
        <p className="text-xs text-slate-500">
          Don&apos;t have an account?{" "}
          <Link href="/register" className="text-blue-600 hover:text-blue-700 font-medium">
            Create an account
          </Link>
        </p>
      </div>
    </div>
  );
}

export default function LoginPage() {
  return (
    <div className="min-h-screen bg-slate-50 text-slate-900 flex items-center justify-center px-4 py-12">
      <div className="w-full max-w-md">
        {/* Branding */}
        <div className="text-center mb-8">
          <Link href="/" className="inline-flex items-center gap-2 mb-3">
            <span className="p-2 rounded-xl bg-blue-600/10 text-blue-600 border border-blue-200">
              <ShieldCheck className="w-6 h-6" />
            </span>
            <span className="text-xl font-bold tracking-tight text-slate-900">ClaimBridge</span>
          </Link>
          <h1 className="text-2xl font-bold text-slate-900 tracking-tight">Sign in to ClaimBridge</h1>
          <p className="text-xs text-slate-500 mt-1">
            Access secure claims adjudication and real-time payout telemetry
          </p>
        </div>

        <Suspense fallback={<div className="text-center text-xs text-slate-500">Loading form...</div>}>
          <LoginForm />
        </Suspense>
      </div>
    </div>
  );
}
