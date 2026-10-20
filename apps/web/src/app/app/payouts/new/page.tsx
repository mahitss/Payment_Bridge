"use client";

import React, { useState, useEffect } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import {
  ArrowLeft,
  AlertTriangle,
  Lock,
  CheckCircle2,
  Cpu,
  PlusCircle,
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

      <main id="main-content" tabIndex={-1} className="flex-1 max-w-4xl w-full mx-auto px-4 sm:px-6 py-8 space-y-6 focus:outline-none">
        <Link
          href="/app/payouts"
          className="inline-flex items-center gap-1.5 min-h-[44px] px-2 py-1 -ml-2 text-xs text-slate-400 hover:text-white transition font-medium focus-visible:ring-2 focus-visible:ring-blue-500 rounded-lg"
        >
          <ArrowLeft className="w-4 h-4" aria-hidden="true" />
          <span>Back to Payout Cases</span>
        </Link>

        {/* Title */}
        <div>
          <h1 className="text-xl sm:text-2xl font-bold text-white tracking-tight">
            Create Payout Settlement Case
          </h1>
          <p className="text-xs sm:text-sm text-slate-400 mt-1">
            Establish a durable case record in PostgreSQL. Dual custody and fail-closed ceilings are validated server-side.
          </p>
        </div>

        {/* Form Container */}
        <div className="p-4 sm:p-6 rounded-xl bg-slate-900/60 border border-slate-800 space-y-6">
          {errorMsg && (
            <div
              role="alert"
              aria-live="assertive"
              className="p-3.5 rounded-lg bg-rose-500/10 border border-rose-500/30 text-rose-300 text-xs flex items-center gap-2"
            >
              <AlertTriangle className="w-4 h-4 flex-shrink-0 text-rose-400" aria-hidden="true" />
              <span>{errorMsg}</span>
            </div>
          )}

          <form onSubmit={handleSubmit} className="space-y-5" noValidate={false}>
            {/* Case Reference & Title */}
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div className="space-y-1.5">
                <label htmlFor="case-reference" className="text-xs font-medium text-slate-300">
                  Case Reference (Optional)
                </label>
                <input
                  id="case-reference"
                  type="text"
                  placeholder="Auto-generated (e.g. CASE-1712...)"
                  value={reference}
                  onChange={(e) => setReference(e.target.value)}
                  aria-describedby="case-reference-hint"
                  className="w-full px-3.5 py-2.5 rounded-lg bg-slate-950 border border-slate-700/80 text-xs sm:text-sm text-white placeholder-slate-500 focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:outline-none font-mono transition"
                />
                <span id="case-reference-hint" className="text-[11px] text-slate-500 block">
                  Unique internal reference code
                </span>
              </div>

              <div className="space-y-1.5">
                <label htmlFor="settlement-title" className="text-xs font-medium text-slate-300">
                  Settlement Title <span className="text-rose-400" aria-label="required">*</span>
                </label>
                <input
                  id="settlement-title"
                  type="text"
                  required
                  placeholder="e.g. Policy #9924-A Claim Settlement"
                  value={title}
                  onChange={(e) => setTitle(e.target.value)}
                  className="w-full px-3.5 py-2.5 rounded-lg bg-slate-950 border border-slate-700/80 text-xs sm:text-sm text-white placeholder-slate-500 focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:outline-none transition"
                />
              </div>
            </div>

            {/* Recipient Details */}
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div className="space-y-1.5">
                <label htmlFor="recipient-email" className="text-xs font-medium text-slate-300">
                  Recipient PayPal Email <span className="text-rose-400" aria-label="required">*</span>
                </label>
                <input
                  id="recipient-email"
                  type="email"
                  required
                  autoComplete="email"
                  inputMode="email"
                  placeholder="sandbox-claimant@personal.example.com"
                  value={recipientEmail}
                  onChange={(e) => setRecipientEmail(e.target.value)}
                  aria-describedby="recipient-email-hint"
                  className="w-full px-3.5 py-2.5 rounded-lg bg-slate-950 border border-slate-700/80 text-xs sm:text-sm text-white placeholder-slate-500 focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:outline-none font-mono transition"
                />
                <span id="recipient-email-hint" className="text-[11px] text-slate-500 block">
                  Target sandbox account to receive funds
                </span>
              </div>

              <div className="space-y-1.5">
                <label htmlFor="recipient-name" className="text-xs font-medium text-slate-300">
                  Recipient Full Name <span className="text-rose-400" aria-label="required">*</span>
                </label>
                <input
                  id="recipient-name"
                  type="text"
                  required
                  autoComplete="name"
                  placeholder="e.g. Sarah Connor"
                  value={recipientName}
                  onChange={(e) => setRecipientName(e.target.value)}
                  className="w-full px-3.5 py-2.5 rounded-lg bg-slate-950 border border-slate-700/80 text-xs sm:text-sm text-white placeholder-slate-500 focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:outline-none transition"
                />
              </div>
            </div>

            {/* Amount & Currency */}
            <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
              <div className="space-y-1.5 md:col-span-2">
                <label htmlFor="payout-amount" className="text-xs font-medium text-slate-300">
                  Payout Amount (USD) <span className="text-rose-400" aria-label="required">*</span>
                </label>
                <div className="relative">
                  <span className="absolute left-3.5 top-2.5 text-xs font-mono text-slate-400" aria-hidden="true">
                    $
                  </span>
                  <input
                    id="payout-amount"
                    type="text"
                    required
                    inputMode="decimal"
                    value={amountDollars}
                    onChange={(e) => setAmountDollars(e.target.value)}
                    placeholder="150.00"
                    aria-describedby="payout-amount-hint"
                    className="w-full pl-8 pr-24 py-2.5 rounded-lg bg-slate-950 border border-slate-700/80 text-xs sm:text-sm text-white font-mono focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:outline-none transition"
                  />
                  <span className="absolute right-3.5 top-2.5 text-xs font-mono text-slate-500" aria-hidden="true">
                    {amountCents} minor units
                  </span>
                </div>
                <span id="payout-amount-hint" className="text-[11px] text-slate-500 block">
                  Stored as exact integer cents. Maximum ceiling: $500.00 USD.
                </span>
              </div>

              <div className="space-y-1.5">
                <label htmlFor="payout-currency" className="text-xs font-medium text-slate-300">
                  Currency
                </label>
                <input
                  id="payout-currency"
                  type="text"
                  disabled
                  value="USD (Sandbox)"
                  aria-describedby="currency-hint"
                  className="w-full px-3.5 py-2.5 rounded-lg bg-slate-900 border border-slate-800 text-xs sm:text-sm text-slate-400 font-mono cursor-not-allowed"
                />
                <span id="currency-hint" className="text-[11px] text-slate-500 block">
                  Fixed to USD in current milestone
                </span>
              </div>
            </div>

            {/* Risk Score */}
            <div className="space-y-1.5">
              <div className="flex items-center justify-between">
                <label htmlFor="risk-score" className="text-xs font-medium text-slate-300">
                  Claim Risk Score (0 - 100)
                </label>
                <span className="text-xs font-mono font-bold text-blue-400" aria-live="polite">
                  {riskScore} / 100
                </span>
              </div>
              <input
                id="risk-score"
                type="range"
                min="0"
                max="100"
                step="1"
                value={riskScore}
                onChange={(e) => setRiskScore(parseInt(e.target.value, 10))}
                aria-describedby="risk-score-hint"
                aria-valuemin={0}
                aria-valuemax={100}
                aria-valuenow={riskScore}
                aria-valuetext={`Risk score ${riskScore} out of 100`}
                className="w-full accent-blue-500 cursor-pointer h-2 bg-slate-800 rounded-lg focus-visible:ring-2 focus-visible:ring-blue-500"
              />
              <div id="risk-score-hint" className="flex justify-between text-[10px] text-slate-500 font-mono">
                <span>0 (Low Risk)</span>
                <span>&gt; 20 (Triggers Dual Approval)</span>
                <span>100 (High Risk)</span>
              </div>
            </div>

            {/* Description */}
            <div className="space-y-1.5">
              <label htmlFor="case-description" className="text-xs font-medium text-slate-300">
                Claim Description &amp; Notes
              </label>
              <textarea
                id="case-description"
                rows={3}
                placeholder="Details of damage inspection, settlement agreement, or claim adjustor review..."
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                className="w-full px-3.5 py-2.5 rounded-lg bg-slate-950 border border-slate-700/80 text-xs sm:text-sm text-white placeholder-slate-500 focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:outline-none transition resize-none"
              />
            </div>

            {/* Real-time Policy Preview Card */}
            <div
              role="status"
              aria-live="polite"
              className="p-4 rounded-lg bg-slate-950 border border-slate-800 space-y-2"
            >
              <div className="flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-slate-400">
                <Cpu className="w-3.5 h-3.5 text-blue-400" aria-hidden="true" />
                <span>Deterministic Rust Policy Evaluation</span>
              </div>

              {isCeilingExceeded ? (
                <div className="p-3 rounded-md bg-rose-500/10 border border-rose-500/30 text-rose-300 text-xs flex items-center gap-2">
                  <AlertTriangle className="w-4 h-4 flex-shrink-0 text-rose-400" aria-hidden="true" />
                  <div>
                    <span className="font-semibold">CEILING EXCEEDED: </span>
                    <span>
                      Total payout of ${(amountCents / 100).toFixed(2)} exceeds the $500.00 USD safety limit. Submission will be rejected fail-closed.
                    </span>
                  </div>
                </div>
              ) : isDualApprovalRequired ? (
                <div className="p-3 rounded-md bg-amber-500/10 border border-amber-500/30 text-amber-300 text-xs flex items-center gap-2">
                  <Lock className="w-4 h-4 flex-shrink-0 text-amber-400" aria-hidden="true" />
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
                  <CheckCircle2 className="w-4 h-4 flex-shrink-0 text-emerald-400" aria-hidden="true" />
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
            <div className="pt-2 flex flex-col-reverse sm:flex-row items-stretch sm:items-center justify-end gap-3">
              <Link
                href="/app/payouts"
                className="min-h-[44px] px-4 py-2.5 rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-300 text-xs font-medium transition flex items-center justify-center focus-visible:ring-2 focus-visible:ring-blue-500"
              >
                Cancel
              </Link>
              <button
                type="submit"
                disabled={submitting || isCeilingExceeded}
                className="min-h-[44px] px-5 py-2.5 rounded-lg bg-blue-600 hover:bg-blue-500 disabled:opacity-40 disabled:cursor-not-allowed text-white text-xs font-semibold transition flex items-center justify-center gap-2 shadow-lg shadow-blue-600/20 focus-visible:ring-2 focus-visible:ring-blue-500"
              >
                {submitting ? (
                  <>
                    <div className="w-3.5 h-3.5 border-2 border-white border-t-transparent rounded-full animate-spin" aria-hidden="true" />
                    <span>Creating Case in PostgreSQL...</span>
                  </>
                ) : (
                  <>
                    <PlusCircle className="w-3.5 h-3.5" aria-hidden="true" />
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
