"use client";

import React, { useState, useEffect } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import {
  CreditCard,
  PlusCircle,
  ArrowLeft,
  ShieldCheck,
  AlertTriangle,
  Lock,
  CheckCircle2,
  Cpu,
  Info,
} from "lucide-react";
import { useAuth } from "@/lib/auth";
import Header from "@/components/Header";

export default function NewPayoutCasePage() {
  const { user, loading } = useAuth();
  const router = useRouter();

  const [reference, setReference] = useState("");
  const [recipientEmail, setRecipientEmail] = useState("");
  const [recipientName, setRecipientName] = useState("");
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [amountDollars, setAmountDollars] = useState("150.00");
  const [riskScore, setRiskScore] = useState(10);

  const [submitting, setSubmitting] = useState(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  useEffect(() => {
    if (!loading && !user) {
      router.push("/login?returnTo=/app/payouts/new");
    }
  }, [user, loading, router]);

  // Compute minor units (cents) cleanly without floating-point inaccuracies
  const parseAmountToCents = (val: string): number => {
    const cleaned = val.replace(/[^0-9.]/g, "");
    const parts = cleaned.split(".");
    if (parts.length === 0 || parts[0] === "") return 0;
    const dollars = parseInt(parts[0], 10) || 0;
    let cents = 0;
    if (parts.length > 1) {
      const centPart = parts[1].padEnd(2, "0").slice(0, 2);
      cents = parseInt(centPart, 10) || 0;
    }
    return dollars * 100 + cents;
  };

  const amountCents = parseAmountToCents(amountDollars);
  const isCeilingExceeded = amountCents > 50000; // $500.00 USD
  const isDualApprovalRequired = amountCents > 25000 || riskScore > 20.0; // > $250.00 USD or risk > 20.0

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErrorMsg(null);

    if (amountCents <= 0) {
      setErrorMsg("Amount must be greater than $0.00.");
      return;
    }
    if (isCeilingExceeded) {
      setErrorMsg("Amount exceeds the $500.00 USD maximum policy ceiling for this milestone.");
      return;
    }
    if (!recipientEmail || !recipientEmail.includes("@")) {
      setErrorMsg("A valid recipient email address is required.");
      return;
    }
    if (!recipientName.trim()) {
      setErrorMsg("Recipient name cannot be empty.");
      return;
    }
    if (!title.trim()) {
      setErrorMsg("Title cannot be empty.");
      return;
    }

    setSubmitting(true);
    try {
      const payload = {
        reference: reference.trim() || undefined,
        recipient_email: recipientEmail.trim().toLowerCase(),
        recipient_name: recipientName.trim(),
        title: title.trim(),
        description: description.trim() || undefined,
        currency: "USD",
        amount_cents: amountCents,
        risk_score: parseFloat(riskScore.toString()),
      };

      const res = await fetch("/api/v1/payout-cases", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "include",
        body: JSON.stringify(payload),
      });

      const data = await res.json();
      if (!res.ok) {
        setErrorMsg(data.error || "Failed to create payout case");
        setSubmitting(false);
        return;
      }

      // Redirect to the newly created case detail view
      router.push(`/app/payouts/${data.case.id}`);
    } catch {
      setErrorMsg("Network error connecting to API gateway.");
      setSubmitting(false);
    }
  };

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-slate-950 text-slate-400">
        <div className="flex flex-col items-center gap-3">
          <div className="w-8 h-8 border-2 border-blue-500 border-t-transparent rounded-full animate-spin" />
          <span className="text-xs font-mono">Loading...</span>
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

      <main className="flex-1 max-w-4xl w-full mx-auto px-6 py-8 space-y-6">
        <Link
          href="/app/payouts"
          className="inline-flex items-center gap-1.5 text-xs text-slate-400 hover:text-white transition font-medium"
        >
          <ArrowLeft className="w-4 h-4" />
          <span>Back to Payout Cases</span>
        </Link>

        {/* Title */}
        <div>
          <h1 className="text-xl font-bold text-white tracking-tight">
            Create Payout Settlement Case
          </h1>
          <p className="text-xs text-slate-400 mt-1">
            Establish a durable case record in PostgreSQL. Dual custody and fail-closed ceilings are validated server-side.
          </p>
        </div>

        {/* Form Container */}
        <div className="p-6 rounded-xl bg-slate-900/60 border border-slate-800 space-y-6">
          {errorMsg && (
            <div className="p-3.5 rounded-lg bg-rose-500/10 border border-rose-500/30 text-rose-300 text-xs flex items-center gap-2">
              <AlertTriangle className="w-4 h-4 flex-shrink-0 text-rose-400" />
              <span>{errorMsg}</span>
            </div>
          )}

          <form onSubmit={handleSubmit} className="space-y-5">
            {/* Case Reference & Title */}
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-slate-300">
                  Case Reference (Optional)
                </label>
                <input
                  type="text"
                  placeholder="Auto-generated (e.g. CASE-1712...)"
                  value={reference}
                  onChange={(e) => setReference(e.target.value)}
                  className="w-full px-3 py-2 rounded-lg bg-slate-950 border border-slate-700/80 text-xs text-white placeholder-slate-500 focus:outline-none focus:border-blue-500 font-mono transition"
                />
                <span className="text-[11px] text-slate-500">
                  Unique internal reference code
                </span>
              </div>

              <div className="space-y-1.5">
                <label className="text-xs font-medium text-slate-300">
                  Settlement Title *
                </label>
                <input
                  type="text"
                  required
                  placeholder="e.g. Policy #9924-A Claim Settlement"
                  value={title}
                  onChange={(e) => setTitle(e.target.value)}
                  className="w-full px-3 py-2 rounded-lg bg-slate-950 border border-slate-700/80 text-xs text-white placeholder-slate-500 focus:outline-none focus:border-blue-500 transition"
                />
              </div>
            </div>

            {/* Recipient Details */}
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-slate-300">
                  Recipient PayPal Email *
                </label>
                <input
                  type="email"
                  required
                  placeholder="sandbox-claimant@personal.example.com"
                  value={recipientEmail}
                  onChange={(e) => setRecipientEmail(e.target.value)}
                  className="w-full px-3 py-2 rounded-lg bg-slate-950 border border-slate-700/80 text-xs text-white placeholder-slate-500 focus:outline-none focus:border-blue-500 font-mono transition"
                />
                <span className="text-[11px] text-slate-500">
                  Target sandbox account to receive funds
                </span>
              </div>

              <div className="space-y-1.5">
                <label className="text-xs font-medium text-slate-300">
                  Recipient Full Name *
                </label>
                <input
                  type="text"
                  required
                  placeholder="e.g. Sarah Connor"
                  value={recipientName}
                  onChange={(e) => setRecipientName(e.target.value)}
                  className="w-full px-3 py-2 rounded-lg bg-slate-950 border border-slate-700/80 text-xs text-white placeholder-slate-500 focus:outline-none focus:border-blue-500 transition"
                />
              </div>
            </div>

            {/* Amount & Currency */}
            <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
              <div className="space-y-1.5 md:col-span-2">
                <label className="text-xs font-medium text-slate-300">
                  Payout Amount (USD) *
                </label>
                <div className="relative">
                  <span className="absolute left-3 top-2 text-xs font-mono text-slate-400">
                    $
                  </span>
                  <input
                    type="text"
                    required
                    value={amountDollars}
                    onChange={(e) => setAmountDollars(e.target.value)}
                    placeholder="150.00"
                    className="w-full pl-7 pr-24 py-2 rounded-lg bg-slate-950 border border-slate-700/80 text-xs text-white font-mono focus:outline-none focus:border-blue-500 transition"
                  />
                  <span className="absolute right-3 top-2 text-xs font-mono text-slate-500">
                    {amountCents} minor units
                  </span>
                </div>
                <span className="text-[11px] text-slate-500">
                  Stored as exact integer cents. Maximum ceiling: $500.00 USD.
                </span>
              </div>

              <div className="space-y-1.5">
                <label className="text-xs font-medium text-slate-300">Currency</label>
                <input
                  type="text"
                  disabled
                  value="USD (Sandbox)"
                  className="w-full px-3 py-2 rounded-lg bg-slate-900 border border-slate-800 text-xs text-slate-400 font-mono cursor-not-allowed"
                />
                <span className="text-[11px] text-slate-500">
                  Fixed to USD in current milestone
                </span>
              </div>
            </div>

            {/* Risk Score */}
            <div className="space-y-1.5">
              <div className="flex items-center justify-between">
                <label className="text-xs font-medium text-slate-300">
                  Claim Risk Score (0 - 100)
                </label>
                <span className="text-xs font-mono font-bold text-blue-400">
                  {riskScore} / 100
                </span>
              </div>
              <input
                type="range"
                min="0"
                max="100"
                step="1"
                value={riskScore}
                onChange={(e) => setRiskScore(parseInt(e.target.value, 10))}
                className="w-full accent-blue-500 cursor-pointer"
              />
              <div className="flex justify-between text-[10px] text-slate-500 font-mono">
                <span>0 (Low Risk)</span>
                <span>&gt; 20 (Triggers Dual Approval)</span>
                <span>100 (High Risk)</span>
              </div>
            </div>

            {/* Description */}
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-slate-300">
                Claim Description & Notes
              </label>
              <textarea
                rows={3}
                placeholder="Details of damage inspection, settlement agreement, or claim adjustor review..."
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                className="w-full px-3 py-2 rounded-lg bg-slate-950 border border-slate-700/80 text-xs text-white placeholder-slate-500 focus:outline-none focus:border-blue-500 transition resize-none"
              />
            </div>

            {/* Real-time Policy Preview Card */}
            <div className="p-4 rounded-lg bg-slate-950 border border-slate-800 space-y-2">
              <div className="flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-slate-400">
                <Cpu className="w-3.5 h-3.5 text-blue-400" />
                <span>Deterministic Rust Policy Evaluation</span>
              </div>

              {isCeilingExceeded ? (
                <div className="p-3 rounded-md bg-rose-500/10 border border-rose-500/30 text-rose-300 text-xs flex items-center gap-2">
                  <AlertTriangle className="w-4 h-4 flex-shrink-0 text-rose-400" />
                  <div>
                    <span className="font-semibold">CEILING EXCEEDED: </span>
                    <span>
                      Total payout of ${(amountCents / 100).toFixed(2)} exceeds the $500.00 USD safety limit. Submission will be rejected fail-closed.
                    </span>
                  </div>
                </div>
              ) : isDualApprovalRequired ? (
                <div className="p-3 rounded-md bg-amber-500/10 border border-amber-500/30 text-amber-300 text-xs flex items-center gap-2">
                  <Lock className="w-4 h-4 flex-shrink-0 text-amber-400" />
                  <div>
                    <span className="font-semibold">DUAL-CUSTODY MANDATED: </span>
                    <span>
                      {amountCents > 25000
                        ? "Amount exceeds $250.00 USD threshold. "
                        : "Risk score exceeds 20.0 threshold. "}
                      Once initiated, a distinct authorized officer must approve this payout before submission. You cannot self-approve.
                    </span>
                  </div>
                </div>
              ) : (
                <div className="p-3 rounded-md bg-emerald-500/10 border border-emerald-500/30 text-emerald-300 text-xs flex items-center gap-2">
                  <CheckCircle2 className="w-4 h-4 flex-shrink-0 text-emerald-400" />
                  <div>
                    <span className="font-semibold">SINGLE OFFICER AUTHORIZED: </span>
                    <span>
                      Amount is within the single-approver threshold ($250.00 USD) and low risk. Immediate sandbox payout submission permitted after initiation.
                    </span>
                  </div>
                </div>
              )}
            </div>

            {/* Submit Button */}
            <div className="pt-2 flex items-center justify-end gap-3">
              <Link
                href="/app/payouts"
                className="px-4 py-2 rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-300 text-xs font-medium transition"
              >
                Cancel
              </Link>
              <button
                type="submit"
                disabled={submitting || isCeilingExceeded}
                className="px-5 py-2 rounded-lg bg-blue-600 hover:bg-blue-500 disabled:opacity-40 disabled:cursor-not-allowed text-white text-xs font-semibold transition flex items-center gap-2 shadow-lg shadow-blue-600/20"
              >
                {submitting ? (
                  <>
                    <div className="w-3.5 h-3.5 border-2 border-white border-t-transparent rounded-full animate-spin" />
                    <span>Creating Case in PostgreSQL...</span>
                  </>
                ) : (
                  <>
                    <PlusCircle className="w-3.5 h-3.5" />
                    <span>Create Payout Case</span>
                  </>
                )}
              </button>
            </div>
          </form>
        </div>
      </main>
    </div>
  );
}
