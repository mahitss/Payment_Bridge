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
  ShieldCheck,
  Scale,
  DollarSign,
} from "lucide-react";
import { useAuth } from "@/lib/auth";
import Header from "@/components/Header";
import {
  Button,
  Badge,
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  Alert,
} from "@/components/ui";

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

  // Client preview of policy invariants
  const isCeilingExceeded = amountCents > 50000;
  const isDualApprovalRequired = amountCents > 25000 || riskScore > 20;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErrorMsg(null);

    // Frontend preflight validation
    if (!recipientEmail || !recipientEmail.includes("@")) {
      setErrorMsg("A valid recipient PayPal email address is required.");
      return;
    }
    if (!recipientName.trim()) {
      setErrorMsg("Recipient full name is required.");
      return;
    }
    if (!title.trim()) {
      setErrorMsg("A settlement title is required.");
      return;
    }
    if (amountCents <= 0) {
      setErrorMsg("Payout amount must be greater than $0.00.");
      return;
    }
    if (isCeilingExceeded) {
      setErrorMsg("Ceiling exceeded: Payouts above $500.00 USD are blocked fail-closed by policy.");
      return;
    }

    setSubmitting(true);

    try {
      const payload = {
        reference: reference.trim() || undefined,
        recipient_email: recipientEmail.trim(),
        recipient_name: recipientName.trim(),
        title: title.trim(),
        description: description.trim() || undefined,
        amount_cents: amountCents,
        currency: "USD",
        risk_score: riskScore,
      };

      const res = await fetch("/api/v1/payout-cases", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "include",
        body: JSON.stringify(payload),
      });

      const data = await res.json();

      if (!res.ok) {
        setErrorMsg(data.error || "Failed to create payout case record.");
      } else {
        // Redirect to case detail page
        router.push(`/app/payouts/${data.case.id}`);
      }
    } catch {
      setErrorMsg("Network error connecting to API gateway.");
    } finally {
      setSubmitting(false);
    }
  };

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-slate-50 text-slate-500">
        <div className="flex flex-col items-center gap-3">
          <div className="w-8 h-8 border-2 border-blue-600 border-t-transparent rounded-full animate-spin" />
          <span className="text-xs font-mono">Verifying authorization...</span>
        </div>
      </div>
    );
  }

  if (!user) {
    return null;
  }

  return (
    <div className="min-h-screen bg-slate-50 text-slate-900 flex flex-col">
      <Header />

      <main
        id="main-content"
        tabIndex={-1}
        className="flex-1 max-w-7xl w-full mx-auto px-4 sm:px-6 py-8 space-y-6 focus:outline-none"
      >
        <div className="flex items-center gap-2">
          <Link href="/app/payouts">
            <Button variant="ghost" size="sm" className="gap-1.5 -ml-2 text-slate-600 hover:text-slate-900">
              <ArrowLeft className="w-4 h-4" aria-hidden="true" />
              <span>Back to Payout Registry</span>
            </Button>
          </Link>
        </div>

        <div>
          <h1 className="text-xl sm:text-2xl font-bold text-slate-900 tracking-tight">
            Create Payout Settlement Case
          </h1>
          <p className="text-xs sm:text-sm text-slate-500 mt-1">
            Establish a durable case record in PostgreSQL. Dual custody and fail-closed ceilings are validated server-side.
          </p>
        </div>

        {errorMsg && (
          <Alert variant="danger" title="Validation Error" onDismiss={() => setErrorMsg(null)}>
            {errorMsg}
          </Alert>
        )}

        <div className="grid grid-cols-1 lg:grid-cols-12 gap-6 items-start">
          {/* Left Column: Form (7 cols) */}
          <div className="lg:col-span-7">
            <Card>
              <CardHeader>
                <CardTitle>Case Information</CardTitle>
                <CardDescription>
                  Enter recipient identifiers and settlement details. Values are validated against cryptographic rules.
                </CardDescription>
              </CardHeader>
              <CardContent>
                <form onSubmit={handleSubmit} className="space-y-5" noValidate={false}>
                  {/* Case Reference & Title */}
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <div className="space-y-1.5">
                      <label htmlFor="case-reference" className="text-xs font-medium text-slate-700">
                        Case Reference (Optional)
                      </label>
                      <input
                        id="case-reference"
                        type="text"
                        placeholder="Auto-generated (e.g. CASE-1712...)"
                        value={reference}
                        onChange={(e) => setReference(e.target.value)}
                        className="w-full px-3.5 py-2.5 rounded-lg bg-white border border-slate-200 text-xs sm:text-sm text-slate-900 placeholder-slate-400 focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:outline-none font-mono transition"
                      />
                      <span className="text-[11px] text-slate-500 block">
                        Unique internal reference code
                      </span>
                    </div>

                    <div className="space-y-1.5">
                      <label htmlFor="settlement-title" className="text-xs font-medium text-slate-700">
                        Settlement Title <span className="text-rose-500" aria-label="required">*</span>
                      </label>
                      <input
                        id="settlement-title"
                        type="text"
                        required
                        placeholder="e.g. Policy #9924-A Claim Settlement"
                        value={title}
                        onChange={(e) => setTitle(e.target.value)}
                        className="w-full px-3.5 py-2.5 rounded-lg bg-white border border-slate-200 text-xs sm:text-sm text-slate-900 placeholder-slate-400 focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:outline-none transition"
                      />
                    </div>
                  </div>

                  {/* Recipient Details */}
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <div className="space-y-1.5">
                      <label htmlFor="recipient-email" className="text-xs font-medium text-slate-700">
                        Recipient PayPal Email <span className="text-rose-500" aria-label="required">*</span>
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
                        className="w-full px-3.5 py-2.5 rounded-lg bg-white border border-slate-200 text-xs sm:text-sm text-slate-900 placeholder-slate-400 focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:outline-none font-mono transition"
                      />
                      <span className="text-[11px] text-slate-500 block">
                        Target sandbox account to receive funds
                      </span>
                    </div>

                    <div className="space-y-1.5">
                      <label htmlFor="recipient-name" className="text-xs font-medium text-slate-700">
                        Recipient Full Name <span className="text-rose-500" aria-label="required">*</span>
                      </label>
                      <input
                        id="recipient-name"
                        type="text"
                        required
                        autoComplete="name"
                        placeholder="e.g. Sarah Connor"
                        value={recipientName}
                        onChange={(e) => setRecipientName(e.target.value)}
                        className="w-full px-3.5 py-2.5 rounded-lg bg-white border border-slate-200 text-xs sm:text-sm text-slate-900 placeholder-slate-400 focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:outline-none transition"
                      />
                    </div>
                  </div>

                  {/* Amount & Currency */}
                  <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                    <div className="space-y-1.5 md:col-span-2">
                      <label htmlFor="payout-amount" className="text-xs font-medium text-slate-700">
                        Payout Amount (USD) <span className="text-rose-500" aria-label="required">*</span>
                      </label>
                      <div className="relative">
                        <span className="absolute left-3.5 top-2.5 text-xs font-mono text-slate-500" aria-hidden="true">
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
                          className="w-full pl-8 pr-28 py-2.5 rounded-lg bg-white border border-slate-200 text-xs sm:text-sm text-slate-900 font-mono focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:outline-none transition"
                        />
                        <span className="absolute right-3.5 top-2.5 text-[11px] font-mono text-slate-500" aria-hidden="true">
                          {amountCents} cents
                        </span>
                      </div>
                      <span id="payout-amount-hint" className="text-[11px] text-slate-500 block">
                        Stored as exact integer cents. Maximum ceiling: $500.00 USD.
                      </span>
                    </div>

                    <div className="space-y-1.5">
                      <label htmlFor="payout-currency" className="text-xs font-medium text-slate-700">
                        Currency
                      </label>
                      <input
                        id="payout-currency"
                        type="text"
                        disabled
                        value="USD (Sandbox)"
                        className="w-full px-3.5 py-2.5 rounded-lg bg-slate-50 border border-slate-200 text-xs sm:text-sm text-slate-500 font-mono cursor-not-allowed"
                      />
                      <span className="text-[11px] text-slate-500 block">
                        Fixed to USD in current milestone
                      </span>
                    </div>
                  </div>

                  {/* Risk Score */}
                  <div className="space-y-1.5">
                    <div className="flex items-center justify-between">
                      <label htmlFor="risk-score" className="text-xs font-medium text-slate-700">
                        Claim Risk Score (0 - 100)
                      </label>
                      <Badge variant={riskScore > 20 ? "warning" : "success"} size="sm">
                        {riskScore} / 100
                      </Badge>
                    </div>
                    <input
                      id="risk-score"
                      type="range"
                      min="0"
                      max="100"
                      step="1"
                      value={riskScore}
                      onChange={(e) => setRiskScore(parseInt(e.target.value, 10))}
                      aria-valuemin={0}
                      aria-valuemax={100}
                      aria-valuenow={riskScore}
                      aria-valuetext={`Risk score ${riskScore} out of 100`}
                      className="w-full accent-blue-600 cursor-pointer h-2 bg-slate-200 rounded-lg focus-visible:ring-2 focus-visible:ring-blue-500"
                    />
                    <div className="flex justify-between text-[10px] text-slate-500 font-mono">
                      <span>0 (Low Risk)</span>
                      <span>&gt; 20 (Dual Approval Trigger)</span>
                      <span>100 (High Risk)</span>
                    </div>
                  </div>

                  {/* Description */}
                  <div className="space-y-1.5">
                    <label htmlFor="case-description" className="text-xs font-medium text-slate-700">
                      Claim Description &amp; Internal Notes
                    </label>
                    <textarea
                      id="case-description"
                      rows={3}
                      placeholder="Details of damage inspection, settlement agreement, or claim adjustor review..."
                      value={description}
                      onChange={(e) => setDescription(e.target.value)}
                      className="w-full px-3.5 py-2.5 rounded-lg bg-white border border-slate-200 text-xs sm:text-sm text-slate-900 placeholder-slate-400 focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:outline-none transition resize-none"
                    />
                  </div>

                  {/* Action buttons */}
                  <div className="pt-2 flex items-center justify-end gap-3 border-t border-slate-100">
                    <Link href="/app/payouts">
                      <Button variant="secondary" size="md">
                        Cancel
                      </Button>
                    </Link>
                    <Button
                      type="submit"
                      variant="primary"
                      size="md"
                      disabled={submitting || isCeilingExceeded}
                      loading={submitting}
                    >
                      <PlusCircle className="w-4 h-4" aria-hidden="true" />
                      <span>Create Payout Case</span>
                    </Button>
                  </div>
                </form>
              </CardContent>
            </Card>
          </div>

          {/* Right Column: Live Policy Engine Simulator (5 cols) */}
          <div className="lg:col-span-5 space-y-4">
            <Card>
              <CardHeader>
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <Cpu className="w-4 h-4 text-blue-600" aria-hidden="true" />
                    <CardTitle className="text-sm">Policy Engine Live Evaluation</CardTitle>
                  </div>
                  <Badge
                    variant={
                      isCeilingExceeded
                        ? "danger"
                        : isDualApprovalRequired
                        ? "warning"
                        : "success"
                    }
                    size="sm"
                    dot
                  >
                    {isCeilingExceeded
                      ? "PROHIBITED"
                      : isDualApprovalRequired
                      ? "DUAL CUSTODY"
                      : "PERMITTED"}
                  </Badge>
                </div>
                <CardDescription>
                  Deterministic pre-flight verification matching server-side financial controls.
                </CardDescription>
              </CardHeader>

              <CardContent className="space-y-4 text-xs">
                {/* Metric breakdown */}
                <div className="p-3.5 rounded-lg bg-slate-50 border border-slate-200 space-y-2 font-mono">
                  <div className="flex items-center justify-between">
                    <span className="text-slate-500">Target Amount:</span>
                    <span className="font-semibold text-slate-900">
                      ${(amountCents / 100).toFixed(2)} USD
                    </span>
                  </div>
                  <div className="flex items-center justify-between">
                    <span className="text-slate-500">Minor Units (Cents):</span>
                    <span className="font-semibold text-blue-600">
                      {amountCents}
                    </span>
                  </div>
                  <div className="flex items-center justify-between">
                    <span className="text-slate-500">Risk Assessment:</span>
                    <span className="font-semibold text-slate-900">
                      {riskScore} / 100
                    </span>
                  </div>
                </div>

                {/* Status Callout */}
                {isCeilingExceeded ? (
                  <div className="p-3.5 rounded-lg bg-rose-50 border border-rose-200 text-rose-800 space-y-1">
                    <div className="flex items-center gap-2 font-semibold">
                      <AlertTriangle className="w-4 h-4 text-rose-600 shrink-0" aria-hidden="true" />
                      <span>Ceiling Exceeded ($500.00 Limit)</span>
                    </div>
                    <p className="text-[11px] text-rose-700 leading-relaxed">
                      Amount of ${(amountCents / 100).toFixed(2)} exceeds the maximum safety ceiling ($500.00 USD). This operation will be rejected fail-closed.
                    </p>
                  </div>
                ) : isDualApprovalRequired ? (
                  <div className="p-3.5 rounded-lg bg-amber-50 border border-amber-200 text-amber-800 space-y-1">
                    <div className="flex items-center gap-2 font-semibold">
                      <Lock className="w-4 h-4 text-amber-600 shrink-0" aria-hidden="true" />
                      <span>Dual-Custody Mandated</span>
                    </div>
                    <p className="text-[11px] text-amber-700 leading-relaxed">
                      {amountCents > 25000
                        ? "Amount exceeds $250.00 USD threshold. "
                        : "Risk score exceeds 20.0 threshold. "}
                      A distinct second officer must sign off before outbound submission. Self-approval is strictly forbidden (HTTP 403).
                    </p>
                  </div>
                ) : (
                  <div className="p-3.5 rounded-lg bg-emerald-50 border border-emerald-200 text-emerald-800 space-y-1">
                    <div className="flex items-center gap-2 font-semibold">
                      <CheckCircle2 className="w-4 h-4 text-emerald-600 shrink-0" aria-hidden="true" />
                      <span>Single-Officer Fast Track</span>
                    </div>
                    <p className="text-[11px] text-emerald-700 leading-relaxed">
                      Amount is within $250.00 USD and low risk. Immediate PayPal Sandbox submission is authorized upon initiation.
                    </p>
                  </div>
                )}

                {/* Governance Invariants */}
                <div className="pt-2 border-t border-slate-100 space-y-2">
                  <div className="text-[11px] font-semibold text-slate-500 uppercase tracking-wider">
                    Enforced Governance Invariants
                  </div>
                  <ul className="space-y-1.5 text-[11px] text-slate-600">
                    <li className="flex items-start gap-2">
                      <ShieldCheck className="w-3.5 h-3.5 text-blue-600 shrink-0 mt-0.5" aria-hidden="true" />
                      <span>Integer cents persistence avoids floating-point roundoff</span>
                    </li>
                    <li className="flex items-start gap-2">
                      <ShieldCheck className="w-3.5 h-3.5 text-blue-600 shrink-0 mt-0.5" aria-hidden="true" />
                      <span>Pre-flight idempotency token generated before provider call</span>
                    </li>
                    <li className="flex items-start gap-2">
                      <ShieldCheck className="w-3.5 h-3.5 text-blue-600 shrink-0 mt-0.5" aria-hidden="true" />
                      <span>Cross-tenant boundary enforced at SQL &amp; session layers</span>
                    </li>
                  </ul>
                </div>
              </CardContent>
            </Card>
          </div>
        </div>
      </main>
    </div>
  );
}
