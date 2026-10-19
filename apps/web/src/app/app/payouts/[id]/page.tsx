"use client";

import React, { useEffect, useState, useCallback } from "react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import {
  CreditCard,
  ArrowLeft,
  Lock,
  CheckCircle2,
  AlertTriangle,
  RefreshCw,
  Send,
  XCircle,
  FileText,
  Clock,
  ShieldCheck,
  Building2,
  Info,
  ExternalLink,
  ChevronDown,
  ChevronUp,
  Sparkles,
  Bot,
  Brain,
  History,
  ShieldAlert,
  HelpCircle,
} from "lucide-react";
import { useAuth } from "@/lib/auth";
import Header from "@/components/Header";
import {
  PayoutCase,
  PayoutOperation,
  PayoutInvestigation,
  formatCentsToUSD,
  formatDate,
} from "@/lib/payouts";

export default function PayoutCaseDetailPage() {
  const { user, loading } = useAuth();
  const router = useRouter();
  const params = useParams();
  const caseId = params?.id as string;

  const [pCase, setPCase] = useState<PayoutCase | null>(null);
  const [operations, setOperations] = useState<PayoutOperation[]>([]);
  const [loadingData, setLoadingData] = useState<boolean>(true);
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [successMessage, setSuccessMessage] = useState<string | null>(null);

  // AI Investigation State
  const [investigations, setInvestigations] = useState<PayoutInvestigation[]>([]);
  const [selectedInvestigation, setSelectedInvestigation] = useState<PayoutInvestigation | null>(null);
  const [investigating, setInvestigating] = useState<boolean>(false);
  const [investigationError, setInvestigationError] = useState<string | null>(null);
  const [investigationSuccess, setInvestigationSuccess] = useState<string | null>(null);
  const [activeEvidenceRef, setActiveEvidenceRef] = useState<string | null>(null);

  // Modals confirmation state
  const [confirmSubmitModal, setConfirmSubmitModal] = useState<boolean>(false);
  const [confirmCancelModal, setConfirmCancelModal] = useState<boolean>(false);
  const [confirmApproveModal, setConfirmApproveModal] = useState<boolean>(false);

  // Show raw provider response accordion
  const [expandedRawResponse, setExpandedRawResponse] = useState<boolean>(false);

  useEffect(() => {
    if (!loading && !user) {
      router.push(`/login?returnTo=/app/payouts/${caseId}`);
    }
  }, [user, loading, router, caseId]);

  const fetchCaseDetails = useCallback(async () => {
    if (!caseId) return;
    setLoadingData(true);
    try {
      const res = await fetch(`/api/v1/payout-cases/${caseId}`, {
        method: "GET",
        credentials: "include",
      });
      if (res.ok) {
        const data = await res.json();
        setPCase(data.case);
        setOperations(data.operations || []);
      } else {
        const err = await res.json().catch(() => ({}));
        setErrorMessage(err.error || "Failed to load payout case");
      }
    } catch {
      setErrorMessage("Network error loading case details");
    } finally {
      setLoadingData(false);
    }
  }, [caseId]);

  const fetchInvestigations = useCallback(async () => {
    if (!caseId) return;
    try {
      const res = await fetch(`/api/v1/payout-cases/${caseId}/investigations`, {
        method: "GET",
        credentials: "include",
      });
      if (res.ok) {
        const data = await res.json();
        const list: PayoutInvestigation[] = data.investigations || [];
        setInvestigations(list);
        if (list.length > 0) {
          setSelectedInvestigation((current) => current || list[0]);
        }
      }
    } catch {
      // Non-blocking for investigations
    }
  }, [caseId]);

  const handleRunInvestigation = async (forceSync = false) => {
    setInvestigating(true);
    setInvestigationError(null);
    setInvestigationSuccess(null);
    try {
      const res = await fetch(`/api/v1/payout-cases/${caseId}/investigations`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "include",
        body: JSON.stringify({ force_sync: forceSync }),
      });
      const data = await res.json();
      if (!res.ok) {
        if (data.code === "AI_UNAVAILABLE") {
          setInvestigationError(
            "Investigation assistant is currently offline or unreachable. Core payout operations remain fully functional."
          );
        } else if (data.code === "INVALID_EVIDENCE_REFERENCE") {
          setInvestigationError(
            "AI safety validation triggered: Model output cited ungrounded or fabricated identifiers and was rejected."
          );
        } else {
          setInvestigationError(data.error || "Failed to complete AI investigation.");
        }
      } else {
        setInvestigationSuccess("Investigation completed with grounded evidence.");
        setSelectedInvestigation(data);
        setInvestigations((prev) => [data, ...prev.filter((item) => item.id !== data.id)]);
        if (forceSync) {
          fetchCaseDetails();
        }
      }
    } catch {
      setInvestigationError("Network error connecting to investigation service.");
    } finally {
      setInvestigating(false);
    }
  };

  useEffect(() => {
    if (user && caseId) {
      fetchCaseDetails();
      fetchInvestigations();
    }
  }, [user, caseId, fetchCaseDetails, fetchInvestigations]);

  // Latest operation is the active operation
  const latestOp = operations.length > 0 ? operations[0] : null;

  // Actions
  const handleInitiateOperation = async () => {
    setActionLoading("initiate");
    setErrorMessage(null);
    setSuccessMessage(null);
    try {
      const res = await fetch(`/api/v1/payout-cases/${caseId}/payout-operations`, {
        method: "POST",
        credentials: "include",
      });
      const data = await res.json();
      if (!res.ok) {
        setErrorMessage(data.error || "Failed to initiate operation");
      } else {
        setSuccessMessage("Payout operation initiated. Idempotency keys generated.");
        fetchCaseDetails();
      }
    } catch {
      setErrorMessage("Network error initiating payout operation");
    } finally {
      setActionLoading(null);
    }
  };

  const handleApproveOperation = async () => {
    if (!latestOp) return;
    setActionLoading("approve");
    setErrorMessage(null);
    setSuccessMessage(null);
    try {
      const res = await fetch(`/api/v1/payout-operations/${latestOp.id}/approve`, {
        method: "POST",
        credentials: "include",
      });
      const data = await res.json();
      if (!res.ok) {
        setErrorMessage(data.error || "Approval denied");
      } else {
        setSuccessMessage("Dual-custody approval recorded successfully.");
        setConfirmApproveModal(false);
        fetchCaseDetails();
      }
    } catch {
      setErrorMessage("Network error recording approval");
    } finally {
      setActionLoading(null);
    }
  };

  const handleSubmitOperation = async () => {
    if (!latestOp) return;
    setActionLoading("submit");
    setErrorMessage(null);
    setSuccessMessage(null);
    try {
      const res = await fetch(`/api/v1/payout-operations/${latestOp.id}/submit`, {
        method: "POST",
        credentials: "include",
      });
      const data = await res.json();
      if (!res.ok) {
        if (res.status === 504) {
          setErrorMessage("Payout timed out! Status marked RECONCILIATION_REQUIRED for safety.");
        } else {
          setErrorMessage(data.error || "Failed to submit payout to PayPal Sandbox");
        }
      } else {
        setSuccessMessage("Payout batch successfully accepted by PayPal Sandbox.");
      }
      setConfirmSubmitModal(false);
      fetchCaseDetails();
    } catch {
      setErrorMessage("Network error submitting payout to PayPal Sandbox");
    } finally {
      setActionLoading(null);
    }
  };

  const handleSyncStatus = async () => {
    setActionLoading("sync");
    setErrorMessage(null);
    setSuccessMessage(null);
    try {
      const res = await fetch(`/api/v1/payout-cases/${caseId}/sync`, {
        method: "POST",
        credentials: "include",
      });
      const data = await res.json();
      if (!res.ok) {
        setErrorMessage(data.error || "Failed to synchronize status with PayPal");
      } else {
        setSuccessMessage("Successfully synchronized status with PayPal Sandbox.");
        fetchCaseDetails();
      }
    } catch {
      setErrorMessage("Network error synchronizing with PayPal");
    } finally {
      setActionLoading(null);
    }
  };

  const handleCancelOperation = async () => {
    if (!latestOp) return;
    setActionLoading("cancel");
    setErrorMessage(null);
    setSuccessMessage(null);
    try {
      const res = await fetch(`/api/v1/payout-operations/${latestOp.id}/cancel`, {
        method: "POST",
        credentials: "include",
      });
      const data = await res.json();
      if (!res.ok) {
        setErrorMessage(data.error || "Failed to cancel payout operation");
      } else {
        setSuccessMessage("Payout operation cancelled successfully.");
        setConfirmCancelModal(false);
        fetchCaseDetails();
      }
    } catch {
      setErrorMessage("Network error cancelling payout operation");
    } finally {
      setActionLoading(null);
    }
  };

  if (loading || loadingData) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-slate-950 text-slate-400">
        <div className="flex flex-col items-center gap-3">
          <div className="w-8 h-8 border-2 border-blue-500 border-t-transparent rounded-full animate-spin" />
          <span className="text-xs font-mono">Loading case details...</span>
        </div>
      </div>
    );
  }

  if (!user || !pCase) {
    return (
      <div className="min-h-screen bg-slate-950 text-slate-100 flex flex-col">
        <Header />
        <main className="flex-1 max-w-4xl w-full mx-auto px-6 py-12 text-center space-y-4">
          <AlertTriangle className="w-10 h-10 text-amber-400 mx-auto" />
          <h1 className="text-lg font-bold text-white">Payout Case Not Found</h1>
          <p className="text-xs text-slate-400">
            This case may not exist or does not belong to your active organization.
          </p>
          <Link
            href="/app/payouts"
            className="inline-flex items-center gap-2 px-4 py-2 rounded-lg bg-blue-600 hover:bg-blue-500 text-white text-xs font-semibold"
          >
            <ArrowLeft className="w-4 h-4" />
            <span>Return to Cases</span>
          </Link>
        </main>
      </div>
    );
  }

  // Separation of duties check
  const isInitiator = latestOp && latestOp.initiated_by_user_id === user.id;
  const canSelfApprove = !latestOp?.requires_dual_approval || !isInitiator;
  const isAwaitingApproval = latestOp && latestOp.status === "AWAITING_APPROVAL";
  const isReadyForSubmit =
    latestOp &&
    (latestOp.status === "PREPARED" || latestOp.status === "APPROVED");
  const canSync = latestOp && latestOp.paypal_payout_batch_id;
  const canCancel =
    latestOp &&
    (latestOp.status === "PREPARED" ||
      latestOp.status === "AWAITING_APPROVAL" ||
      latestOp.status === "APPROVED" ||
      (latestOp.status === "SUBMITTED" &&
        latestOp.paypal_item_status?.toUpperCase() === "UNCLAIMED"));

  return (
    <div className="min-h-screen bg-slate-950 text-slate-100 flex flex-col">
      <Header />

      <main className="flex-1 max-w-7xl w-full mx-auto px-6 py-8 space-y-6">
        {/* Back Link & Header */}
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
          <div className="space-y-1">
            <Link
              href="/app/payouts"
              className="inline-flex items-center gap-1.5 text-xs text-slate-400 hover:text-white transition font-medium mb-1"
            >
              <ArrowLeft className="w-4 h-4" />
              <span>Back to Payout Cases</span>
            </Link>
            <div className="flex items-center gap-3">
              <h1 className="text-xl font-bold text-white tracking-tight font-mono">
                {pCase.reference}
              </h1>
              {pCase.is_synthetic_fixture && (
                <span className="px-2 py-0.5 rounded text-[10px] font-semibold tracking-wider uppercase bg-amber-500/10 text-amber-400 border border-amber-500/30">
                  SIMULATED FIXTURE
                </span>
              )}
              <span
                className={`px-2.5 py-0.5 rounded text-xs font-semibold tracking-wide ${
                  pCase.status === "COMPLETED"
                    ? "bg-emerald-500/20 text-emerald-300 border border-emerald-500/30"
                    : pCase.status === "SUBMITTED"
                    ? "bg-blue-500/20 text-blue-300 border border-blue-500/30"
                    : pCase.status === "CANCELLED"
                    ? "bg-rose-500/20 text-rose-300 border border-rose-500/30"
                    : "bg-slate-800 text-slate-300 border border-slate-700"
                }`}
              >
                Internal: {pCase.status}
              </span>
            </div>
            <p className="text-xs text-slate-400">{pCase.title}</p>
          </div>

          {/* Quick Refresh */}
          <div className="flex items-center gap-2">
            {canSync && (
              <button
                onClick={handleSyncStatus}
                disabled={actionLoading !== null || pCase.is_synthetic_fixture || latestOp?.is_synthetic_fixture}
                className="px-3.5 py-2 rounded-lg bg-blue-600/20 hover:bg-blue-600/30 disabled:opacity-40 disabled:cursor-not-allowed text-blue-400 border border-blue-500/30 text-xs font-semibold transition flex items-center gap-1.5"
                title={pCase.is_synthetic_fixture ? "Disabled: Synthetic Demo Fixture" : undefined}
              >
                <RefreshCw
                  className={`w-3.5 h-3.5 ${actionLoading === "sync" ? "animate-spin" : ""}`}
                />
                <span>Sync with PayPal</span>
              </button>
            )}
            <button
              onClick={fetchCaseDetails}
              disabled={loadingData}
              className="p-2 rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-300 hover:text-white transition border border-slate-700 text-xs"
              title="Refresh"
            >
              <RefreshCw className={`w-4 h-4 ${loadingData ? "animate-spin" : ""}`} />
            </button>
          </div>
        </div>

        {/* Synthetic Demo Provenance Banner */}
        {(pCase.is_synthetic_fixture || latestOp?.is_synthetic_fixture) && (
          <div className="p-4 rounded-xl bg-amber-500/10 border border-amber-500/30 text-amber-300 text-xs flex items-start gap-3 shadow-lg shadow-amber-500/5">
            <AlertTriangle className="w-5 h-5 flex-shrink-0 text-amber-400 mt-0.5" />
            <div className="space-y-1">
              <div className="font-bold uppercase tracking-wider text-amber-200">
                SIMULATED DEMO DATA — NOT A LIVE PAYPAL TRANSACTION
              </div>
              <p className="text-slate-300 leading-relaxed">
                This case and its operations are local evaluation fixtures. Real PayPal submission, synchronization, and cancellation are disabled server-side to guarantee provider integrity and prevent fabricated fixture IDs from reaching external financial networks.
              </p>
            </div>
          </div>
        )}

        {/* Banners for Alerts */}
        {errorMessage && (
          <div className="p-4 rounded-xl bg-rose-500/10 border border-rose-500/30 text-rose-300 text-xs flex items-center gap-2.5">
            <AlertTriangle className="w-4 h-4 flex-shrink-0 text-rose-400" />
            <span>{errorMessage}</span>
          </div>
        )}

        {successMessage && (
          <div className="p-4 rounded-xl bg-emerald-500/10 border border-emerald-500/30 text-emerald-300 text-xs flex items-center gap-2.5">
            <CheckCircle2 className="w-4 h-4 flex-shrink-0 text-emerald-400" />
            <span>{successMessage}</span>
          </div>
        )}

        {/* Top Summary Cards */}
        <div className="grid grid-cols-1 md:grid-cols-4 gap-5">
          {/* Amount in USD & Cents */}
          <div className="p-5 rounded-xl bg-slate-900/60 border border-slate-800 space-y-1.5">
            <span className="text-[11px] font-semibold text-slate-400 uppercase tracking-wider">
              Settlement Amount
            </span>
            <div className="text-2xl font-bold font-mono text-white">
              {formatCentsToUSD(pCase.amount_cents)}
            </div>
            <div className="text-[11px] font-mono text-slate-500">
              {pCase.amount_cents} integer minor units ({pCase.currency})
            </div>
          </div>

          {/* Recipient */}
          <div className="p-5 rounded-xl bg-slate-900/60 border border-slate-800 space-y-1.5">
            <span className="text-[11px] font-semibold text-slate-400 uppercase tracking-wider">
              Recipient Sandbox Target
            </span>
            <div className="text-sm font-bold text-white truncate">
              {pCase.recipient_name}
            </div>
            <div className="text-[11px] font-mono text-blue-400 truncate">
              {pCase.recipient_email}
            </div>
          </div>

          {/* Policy Decision */}
          <div className="p-5 rounded-xl bg-slate-900/60 border border-slate-800 space-y-1.5">
            <span className="text-[11px] font-semibold text-slate-400 uppercase tracking-wider">
              Rust Policy Mandate
            </span>
            <div>
              {pCase.dual_approval_required ? (
                <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded text-xs font-semibold bg-amber-500/10 text-amber-400 border border-amber-500/20">
                  <Lock className="w-3.5 h-3.5" />
                  Dual-Custody Required
                </span>
              ) : (
                <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded text-xs font-semibold bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
                  <CheckCircle2 className="w-3.5 h-3.5" />
                  Single Officer Allowed
                </span>
              )}
            </div>
            <div className="text-[11px] text-slate-500 font-mono">
              Risk Score: {pCase.risk_score} / 100
            </div>
          </div>

          {/* Provider Status */}
          <div className="p-5 rounded-xl bg-slate-900/60 border border-slate-800 space-y-1.5">
            <span className="text-[11px] font-semibold text-slate-400 uppercase tracking-wider">
              PayPal Provider Status
            </span>
            <div className="text-sm font-bold font-mono">
              {latestOp?.paypal_batch_status ? (
                <span className="text-emerald-400">
                  Batch: {latestOp.paypal_batch_status}
                  {latestOp.paypal_item_status && ` (${latestOp.paypal_item_status})`}
                </span>
              ) : (
                <span className="text-slate-500">Not Submitted</span>
              )}
            </div>
            <div className="text-[10px] text-slate-500">
              Source: {pCase.is_synthetic_fixture || latestOp?.is_synthetic_fixture ? "Simulated Demo Fixture (Local State)" : "PayPal Sandbox REST API v1"}
            </div>
          </div>
        </div>

        {/* Dual-Custody Approval Guard Panel */}
        {latestOp && latestOp.requires_dual_approval && (
          <div className="p-5 rounded-xl bg-slate-900/40 border border-amber-500/30 space-y-3">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <Lock className="w-4 h-4 text-amber-400" />
                <h3 className="text-xs font-bold uppercase tracking-wider text-amber-300">
                  Dual-Custody Separation of Duties
                </h3>
              </div>
              <span className="text-[11px] font-mono text-slate-400">
                Op ID: {latestOp.id.slice(0, 8)}...
              </span>
            </div>

            <p className="text-xs text-slate-300 leading-relaxed">
              This payout exceeds the $250.00 threshold or risk threshold. The operation was initiated by{" "}
              <span className="font-mono text-white">{latestOp.initiated_by_user_id}</span>.
              {isInitiator ? (
                <strong className="text-amber-400">
                  {" "}You initiated this operation and are strictly forbidden from approving it. A distinct authorized officer must approve this payout.
                </strong>
              ) : latestOp.status === "AWAITING_APPROVAL" ? (
                <strong className="text-emerald-400">
                  {" "}You are a distinct authorized officer and are eligible to review and approve this payout operation.
                </strong>
              ) : latestOp.status === "APPROVED" ? (
                <span className="text-emerald-400 font-semibold">
                  {" "}Approved by officer {latestOp.approved_by_user_id}. Ready for sandbox submission.
                </span>
              ) : null}
            </p>

            {isAwaitingApproval && (
              <div className="pt-1 flex items-center gap-3">
                <button
                  onClick={() => setConfirmApproveModal(true)}
                  disabled={isInitiator || actionLoading !== null}
                  className="px-4 py-2 rounded-lg bg-emerald-600 hover:bg-emerald-500 disabled:opacity-40 disabled:cursor-not-allowed text-white text-xs font-semibold transition flex items-center gap-2 shadow-lg shadow-emerald-600/20"
                >
                  <CheckCircle2 className="w-4 h-4" />
                  <span>Approve Operation (Distinct Officer)</span>
                </button>
                {isInitiator && (
                  <span className="text-[11px] text-amber-400 font-mono">
                    Self-approval blocked by policy
                  </span>
                )}
              </div>
            )}
          </div>
        )}

        {/* Action Toolbar */}
        <div className="p-6 rounded-xl bg-slate-900/60 border border-slate-800 space-y-4">
          <h2 className="text-xs font-semibold text-slate-400 uppercase tracking-wider">
            Permitted Financial Operations
          </h2>

          <div className="flex flex-wrap items-center gap-3">
            {/* 1. Initiate Payout Operation */}
            {(!latestOp || latestOp.status === "CANCELLED" || latestOp.status === "FAILED") &&
              pCase.status !== "COMPLETED" && (
                <button
                  onClick={handleInitiateOperation}
                  disabled={actionLoading !== null}
                  className="px-4 py-2.5 rounded-lg bg-blue-600 hover:bg-blue-500 disabled:opacity-50 text-white text-xs font-semibold transition flex items-center gap-2 shadow-lg shadow-blue-600/20"
                >
                  <Send className="w-4 h-4" />
                  <span>Initiate Sandbox Payout Operation</span>
                </button>
              )}

            {/* 2. Submit to PayPal Sandbox */}
            {isReadyForSubmit && (
              <button
                onClick={() => setConfirmSubmitModal(true)}
                disabled={actionLoading !== null || pCase.is_synthetic_fixture || latestOp?.is_synthetic_fixture}
                className="px-4 py-2.5 rounded-lg bg-emerald-600 hover:bg-emerald-500 disabled:opacity-40 disabled:cursor-not-allowed text-white text-xs font-semibold transition flex items-center gap-2 shadow-lg shadow-emerald-600/20"
                title={pCase.is_synthetic_fixture ? "Disabled: Synthetic Demo Fixture" : undefined}
              >
                <Send className="w-4 h-4" />
                <span>Submit to PayPal Sandbox</span>
              </button>
            )}

            {/* 3. Sync with PayPal */}
            {canSync && (
              <button
                onClick={handleSyncStatus}
                disabled={actionLoading !== null || pCase.is_synthetic_fixture || latestOp?.is_synthetic_fixture}
                className="px-4 py-2.5 rounded-lg bg-slate-800 hover:bg-slate-700 disabled:opacity-40 disabled:cursor-not-allowed text-slate-200 text-xs font-semibold transition flex items-center gap-2 border border-slate-700"
                title={pCase.is_synthetic_fixture ? "Disabled: Synthetic Demo Fixture" : undefined}
              >
                <RefreshCw
                  className={`w-4 h-4 ${actionLoading === "sync" ? "animate-spin" : ""}`}
                />
                <span>Sync Latest Status</span>
              </button>
            )}

            {/* 4. Cancel Payout */}
            {canCancel && (
              <button
                onClick={() => setConfirmCancelModal(true)}
                disabled={actionLoading !== null || pCase.is_synthetic_fixture || latestOp?.is_synthetic_fixture}
                className="px-4 py-2.5 rounded-lg bg-rose-500/15 hover:bg-rose-500/25 disabled:opacity-40 disabled:cursor-not-allowed text-rose-400 border border-rose-500/30 text-xs font-semibold transition flex items-center gap-2"
                title={pCase.is_synthetic_fixture ? "Disabled: Synthetic Demo Fixture" : undefined}
              >
                <XCircle className="w-4 h-4" />
                <span>Cancel Payout</span>
              </button>
            )}
          </div>
        </div>

        {/* ========================================================================= */}
        {/* MISSION 004: AI PAYOUT INVESTIGATION ENGINE                              */}
        {/* Evidence-Grounded Recommendations & Safety Verification                 */}
        {/* ========================================================================= */}
        <section className="p-6 rounded-2xl bg-gradient-to-br from-slate-900/90 via-slate-900/60 to-indigo-950/20 border border-indigo-500/30 space-y-6 shadow-xl relative overflow-hidden">
          {/* Subtle decorative glow */}
          <div className="absolute top-0 right-0 w-96 h-96 bg-indigo-500/5 rounded-full blur-3xl pointer-events-none" />

          {/* Section Header */}
          <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 border-b border-slate-800/80 pb-5">
            <div className="space-y-1">
              <div className="flex items-center gap-2.5">
                <div className="p-2 rounded-lg bg-indigo-500/10 border border-indigo-500/30 text-indigo-400">
                  <Brain className="w-5 h-5" />
                </div>
                <div>
                  <div className="flex items-center gap-2">
                    <h2 className="text-base font-bold text-white tracking-tight">
                      AI Payout Investigation Engine
                    </h2>
                    <span className="px-2 py-0.5 rounded text-[10px] font-mono font-semibold bg-indigo-500/20 text-indigo-300 border border-indigo-500/30">
                      Grounded Evidence
                    </span>
                  </div>
                  <p className="text-xs text-slate-400">
                    Forensic root-cause analysis grounded in immutable case and provider records.
                  </p>
                </div>
              </div>
            </div>

            {/* Investigation Actions */}
            <div className="flex items-center gap-2">
              <button
                onClick={() => handleRunInvestigation(false)}
                disabled={investigating}
                className="px-4 py-2 rounded-xl bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-xs font-semibold transition flex items-center gap-2 shadow-lg shadow-indigo-600/20"
              >
                {investigating ? (
                  <>
                    <RefreshCw className="w-3.5 h-3.5 animate-spin" />
                    <span>Investigating...</span>
                  </>
                ) : (
                  <>
                    <Sparkles className="w-3.5 h-3.5" />
                    <span>Run Investigation</span>
                  </>
                )}
              </button>

              {canSync && (
                <button
                  onClick={() => handleRunInvestigation(true)}
                  disabled={investigating}
                  title="Query PayPal Sandbox status first, then analyze with fresh provider records"
                  className="px-3.5 py-2 rounded-xl bg-slate-800/80 hover:bg-slate-800 text-slate-300 hover:text-white border border-slate-700 text-xs font-medium transition flex items-center gap-1.5"
                >
                  <RefreshCw className={`w-3.5 h-3.5 ${investigating ? "animate-spin" : ""}`} />
                  <span>Sync &amp; Investigate</span>
                </button>
              )}
            </div>
          </div>

          {/* Error / Offline Alert */}
          {investigationError && (
            <div className="p-4 rounded-xl bg-rose-500/10 border border-rose-500/30 text-rose-300 text-xs flex items-start gap-3">
              <ShieldAlert className="w-4 h-4 flex-shrink-0 text-rose-400 mt-0.5" />
              <div className="space-y-1">
                <span className="font-semibold text-rose-200">Investigation Unavailable</span>
                <p className="text-rose-300/90 leading-relaxed">{investigationError}</p>
                <div className="text-[11px] text-slate-400 pt-1">
                  Note: Payout authorization, approval, and PayPal execution remain fully operable.
                </div>
              </div>
            </div>
          )}

          {/* Success Banner */}
          {investigationSuccess && (
            <div className="p-3 rounded-xl bg-emerald-500/10 border border-emerald-500/30 text-emerald-300 text-xs flex items-center gap-2">
              <CheckCircle2 className="w-4 h-4 text-emerald-400 flex-shrink-0" />
              <span>{investigationSuccess}</span>
            </div>
          )}

          {/* Empty State */}
          {!selectedInvestigation && !investigating && (
            <div className="p-8 rounded-xl bg-slate-950/60 border border-dashed border-slate-800 text-center space-y-3">
              <div className="w-12 h-12 rounded-2xl bg-indigo-500/10 border border-indigo-500/20 text-indigo-400 flex items-center justify-center mx-auto">
                <Bot className="w-6 h-6" />
              </div>
              <h3 className="text-sm font-semibold text-white">No Investigations Run Yet</h3>
              <p className="text-xs text-slate-400 max-w-lg mx-auto leading-relaxed">
                Trigger a grounded investigation to analyze lifecycle anomalies, examine PayPal provider status, identify missing information, and review policy-checked next steps.
              </p>
              <button
                onClick={() => handleRunInvestigation(false)}
                className="mt-2 px-4 py-2 rounded-lg bg-indigo-600 hover:bg-indigo-500 text-white text-xs font-semibold transition inline-flex items-center gap-2 shadow-lg shadow-indigo-600/20"
              >
                <Sparkles className="w-3.5 h-3.5" />
                <span>Run First Investigation</span>
              </button>
            </div>
          )}

          {/* Selected Investigation Results Display */}
          {selectedInvestigation && (
            <div className="space-y-6">
              {/* History Bar (if multiple investigations exist) */}
              {investigations.length > 1 && (
                <div className="flex items-center gap-2 overflow-x-auto pb-2 border-b border-slate-800/60">
                  <span className="text-[11px] font-medium text-slate-400 flex items-center gap-1.5 flex-shrink-0">
                    <History className="w-3.5 h-3.5 text-slate-500" />
                    <span>Past Investigations:</span>
                  </span>
                  {investigations.map((inv, idx) => (
                    <button
                      key={inv.id}
                      onClick={() => setSelectedInvestigation(inv)}
                      className={`px-2.5 py-1 rounded-md text-[11px] font-mono transition flex-shrink-0 flex items-center gap-1.5 ${
                        selectedInvestigation.id === inv.id
                          ? "bg-indigo-500/20 text-indigo-300 border border-indigo-500/40 font-semibold"
                          : "bg-slate-900 hover:bg-slate-800 text-slate-400 border border-slate-800"
                      }`}
                    >
                      <span>#{investigations.length - idx}</span>
                      <span>{formatDate(inv.created_at)}</span>
                      {inv.confidence === "HIGH" && (
                        <span className="w-1.5 h-1.5 rounded-full bg-emerald-400" />
                      )}
                    </button>
                  ))}
                </div>
              )}

              {/* Top Summary Banner */}
              <div className="p-5 rounded-xl bg-slate-950/80 border border-slate-800/90 space-y-3">
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <div className="flex items-center gap-2.5">
                    <span className="text-xs font-semibold text-slate-400 uppercase tracking-wider">
                      Forensic Summary
                    </span>
                    {/* Confidence Badge */}
                    <span
                      className={`px-2 py-0.5 rounded text-[10px] font-semibold tracking-wide ${
                        selectedInvestigation.confidence === "HIGH"
                          ? "bg-emerald-500/20 text-emerald-300 border border-emerald-500/30"
                          : selectedInvestigation.confidence === "MEDIUM"
                          ? "bg-blue-500/20 text-blue-300 border border-blue-500/30"
                          : selectedInvestigation.confidence === "LOW"
                          ? "bg-amber-500/20 text-amber-300 border border-amber-500/30"
                          : "bg-purple-500/20 text-purple-300 border border-purple-500/30"
                      }`}
                    >
                      Confidence: {selectedInvestigation.confidence}
                    </span>

                    {/* Human Review Status */}
                    {selectedInvestigation.requires_human_review ? (
                      <span className="px-2 py-0.5 rounded text-[10px] font-semibold bg-amber-500/10 text-amber-300 border border-amber-500/30">
                        Human Review Mandatory
                      </span>
                    ) : (
                      <span className="px-2 py-0.5 rounded text-[10px] font-semibold bg-emerald-500/10 text-emerald-300 border border-emerald-500/30">
                        Automated Consensus
                      </span>
                    )}
                  </div>

                  <span className="text-[11px] font-mono text-slate-500">
                    Recorded {formatDate(selectedInvestigation.created_at)}
                  </span>
                </div>

                <p className="text-sm text-slate-200 leading-relaxed font-sans">
                  {selectedInvestigation.summary}
                </p>

                {/* Explicit Disclaimer */}
                <div className="pt-2 border-t border-slate-900 flex items-center justify-between text-[11px] text-slate-500">
                  <span className="flex items-center gap-1.5">
                    <Info className="w-3.5 h-3.5 text-slate-500" />
                    <span>AI ADVISORY OPINION: Advisory only. All payments require deterministic officer authorization.</span>
                  </span>
                  <span className="font-mono text-[10px] text-slate-500">
                    Engine: {selectedInvestigation.provider} ({selectedInvestigation.model})
                  </span>
                </div>
              </div>

              {/* Two Column Grid: Evidence vs Recommendations */}
              <div className="grid grid-cols-1 lg:grid-cols-2 gap-5">
                {/* Column 1: Grounded Findings & Facts */}
                <div className="p-5 rounded-xl bg-slate-950/60 border border-slate-800 space-y-4">
                  <div className="flex items-center justify-between border-b border-slate-800/80 pb-2.5">
                    <div className="flex items-center gap-2">
                      <ShieldCheck className="w-4 h-4 text-emerald-400" />
                      <h3 className="text-xs font-bold uppercase tracking-wider text-white">
                        Observed Facts &amp; Grounded Findings
                      </h3>
                    </div>
                    <span className="text-[10px] font-mono text-emerald-400">
                      Validated References
                    </span>
                  </div>

                  {/* Observed Facts */}
                  {selectedInvestigation.observed_facts && selectedInvestigation.observed_facts.length > 0 && (
                    <div className="space-y-1.5">
                      <span className="text-[10px] uppercase font-semibold text-slate-500 tracking-wider">
                        Immutable Snapshot Facts
                      </span>
                      <ul className="space-y-1 text-xs text-slate-300">
                        {selectedInvestigation.observed_facts.map((fact, idx) => (
                          <li key={idx} className="flex items-start gap-2">
                            <span className="text-emerald-400 font-bold">•</span>
                            <span>{fact}</span>
                          </li>
                        ))}
                      </ul>
                    </div>
                  )}

                  {/* Grounded Findings */}
                  <div className="space-y-2.5 pt-2">
                    <span className="text-[10px] uppercase font-semibold text-slate-500 tracking-wider">
                      Diagnostic Findings
                    </span>
                    <div className="space-y-2.5">
                      {selectedInvestigation.findings && selectedInvestigation.findings.length > 0 ? (
                        selectedInvestigation.findings.map((f, idx) => (
                          <div
                            key={idx}
                            className="p-3 rounded-lg bg-slate-900/60 border border-slate-800 space-y-2 text-xs"
                          >
                            <div className="flex items-start justify-between gap-2">
                              <span className="text-slate-200 leading-snug">{f.explanation}</span>
                              <span className="px-1.5 py-0.5 rounded text-[9px] font-mono font-semibold bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 whitespace-nowrap">
                                VERIFIED
                              </span>
                            </div>
                            {/* Evidence Reference Chips */}
                            {f.evidence_references && f.evidence_references.length > 0 && (
                              <div className="flex flex-wrap items-center gap-1.5 pt-1">
                                <span className="text-[10px] text-slate-500">Evidence:</span>
                                {f.evidence_references.map((refId, rIdx) => (
                                  <button
                                    key={rIdx}
                                    onClick={() => setActiveEvidenceRef(refId)}
                                    className={`px-2 py-0.5 rounded text-[10px] font-mono transition border ${
                                      activeEvidenceRef === refId
                                        ? "bg-indigo-500/30 text-indigo-200 border-indigo-400"
                                        : "bg-slate-950 text-slate-400 hover:text-white border-slate-800 hover:border-slate-700"
                                    }`}
                                    title="Click to view reference identifier"
                                  >
                                    {refId}
                                  </button>
                                ))}
                              </div>
                            )}
                          </div>
                        ))
                      ) : (
                        <p className="text-xs text-slate-500">No specific anomalies detected.</p>
                      )}
                    </div>
                  </div>

                  {/* Missing Information Callout */}
                  {selectedInvestigation.missing_information && selectedInvestigation.missing_information.length > 0 && (
                    <div className="p-3.5 rounded-lg bg-amber-500/10 border border-amber-500/20 space-y-1.5">
                      <div className="flex items-center gap-1.5 text-amber-300 text-xs font-semibold">
                        <HelpCircle className="w-3.5 h-3.5" />
                        <span>Information Gaps (Missing Evidence)</span>
                      </div>
                      <ul className="text-xs text-amber-200/80 space-y-1 list-disc list-inside">
                        {selectedInvestigation.missing_information.map((item, idx) => (
                          <li key={idx}>{item}</li>
                        ))}
                      </ul>
                    </div>
                  )}

                  {/* Limitations */}
                  {selectedInvestigation.limitations && selectedInvestigation.limitations.length > 0 && (
                    <div className="text-[11px] text-slate-500 space-y-1 pt-1 border-t border-slate-900">
                      <span className="font-semibold text-slate-400">Analysis Limitations:</span>
                      <ul className="list-disc list-inside space-y-0.5">
                        {selectedInvestigation.limitations.map((lim, idx) => (
                          <li key={idx}>{lim}</li>
                        ))}
                      </ul>
                    </div>
                  )}
                </div>

                {/* Column 2: Recommended Next Steps & Deterministic Policy Check */}
                <div className="p-5 rounded-xl bg-slate-950/60 border border-slate-800 space-y-4">
                  <div className="flex items-center justify-between border-b border-slate-800/80 pb-2.5">
                    <div className="flex items-center gap-2">
                      <Lock className="w-4 h-4 text-indigo-400" />
                      <h3 className="text-xs font-bold uppercase tracking-wider text-white">
                        Recommended Next Steps
                      </h3>
                    </div>
                    <span className="text-[10px] font-mono text-indigo-400">
                      Rust Policy Checked
                    </span>
                  </div>

                  <div className="space-y-3">
                    {selectedInvestigation.recommended_next_steps && selectedInvestigation.recommended_next_steps.length > 0 ? (
                      selectedInvestigation.recommended_next_steps.map((rec, idx) => (
                        <div
                          key={idx}
                          className="p-4 rounded-xl bg-slate-900/60 border border-slate-800 space-y-3 text-xs"
                        >
                          <div className="flex flex-wrap items-center justify-between gap-2">
                            <span className="font-mono text-xs font-bold text-white tracking-wide bg-slate-950 px-2.5 py-1 rounded border border-slate-800">
                              {rec.action}
                            </span>
                            {/* Deterministic Policy Badge */}
                            {rec.deterministic_policy_permitted ? (
                              <span className="px-2 py-0.5 rounded text-[10px] font-semibold bg-emerald-500/20 text-emerald-300 border border-emerald-500/30 flex items-center gap-1">
                                <CheckCircle2 className="w-3 h-3 text-emerald-400" />
                                <span>Permitted by Policy</span>
                              </span>
                            ) : (
                              <span className="px-2 py-0.5 rounded text-[10px] font-semibold bg-rose-500/20 text-rose-300 border border-rose-500/30 flex items-center gap-1">
                                <XCircle className="w-3 h-3 text-rose-400" />
                                <span>Disallowed by Policy</span>
                              </span>
                            )}
                          </div>

                          <p className="text-slate-300 leading-relaxed font-sans">
                            {rec.reason}
                          </p>

                          {/* Rule ID and Guard */}
                          <div className="flex items-center justify-between text-[11px] text-slate-500 pt-1 border-t border-slate-800/60">
                            <span>
                              Rule: <code className="text-slate-400 font-mono">{rec.policy_rule_id || "POLICY-VERIFIED"}</code>
                            </span>
                            <span className="text-[10px] text-amber-400/90">
                              Requires explicit officer approval
                            </span>
                          </div>
                        </div>
                      ))
                    ) : (
                      <p className="text-xs text-slate-500">No active next steps recommended.</p>
                    )}
                  </div>

                  {/* Safety Notice Card */}
                  <div className="p-3.5 rounded-lg bg-indigo-950/20 border border-indigo-500/20 text-[11px] text-indigo-300/80 leading-relaxed space-y-1">
                    <span className="font-semibold text-indigo-300 flex items-center gap-1">
                      <ShieldCheck className="w-3.5 h-3.5" />
                      Financial Execution Boundary
                    </span>
                    <p>
                      Displaying an AI recommendation does not execute it. Any subsequent action must be initiated and approved through the dual-custody operation toolbar above.
                    </p>
                  </div>
                </div>
              </div>
            </div>
          )}
        </section>

        {/* Durable Operation & Provider Status Section */}
        <section className="p-6 rounded-xl bg-slate-900/40 border border-slate-800 space-y-4">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2.5">
              <FileText className="w-5 h-5 text-blue-400" />
              <h2 className="text-base font-semibold text-white">
                Durable Payout Operation Record
              </h2>
            </div>
            {latestOp && (
              <span className="text-xs px-2.5 py-0.5 rounded-full bg-slate-800 text-slate-300 font-mono border border-slate-700">
                Status: {latestOp.status}
              </span>
            )}
          </div>

          {!latestOp ? (
            <div className="p-8 text-center border border-dashed border-slate-800 rounded-lg text-slate-400 text-xs space-y-2">
              <Clock className="w-6 h-6 text-slate-600 mx-auto" />
              <p>No payout operation has been initiated for this case yet.</p>
              <p className="text-[11px] text-slate-500">
                Click &ldquo;Initiate Sandbox Payout Operation&rdquo; above to generate durable idempotency identifiers.
              </p>
            </div>
          ) : (
            <div className="space-y-4">
              <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 text-xs font-mono">
                <div className="p-3.5 rounded-lg bg-slate-950 border border-slate-800 space-y-1">
                  <span className="text-[10px] text-slate-500 uppercase">Sender Batch ID</span>
                  <div className="text-blue-400 truncate">{latestOp.sender_batch_id}</div>
                </div>

                <div className="p-3.5 rounded-lg bg-slate-950 border border-slate-800 space-y-1">
                  <span className="text-[10px] text-slate-500 uppercase">PayPal Payout Batch ID</span>
                  <div className="text-white truncate">
                    {latestOp.paypal_payout_batch_id || "Awaiting submission"}
                  </div>
                </div>

                <div className="p-3.5 rounded-lg bg-slate-950 border border-slate-800 space-y-1">
                  <span className="text-[10px] text-slate-500 uppercase">PayPal Payout Item ID</span>
                  <div className="text-white truncate">
                    {latestOp.paypal_payout_item_id || "Awaiting provider processing"}
                  </div>
                </div>
              </div>

              {/* Status Mapping Explainer */}
              <div className="p-4 rounded-lg bg-slate-950 border border-slate-800/80 space-y-2">
                <div className="flex items-center justify-between text-xs font-semibold text-slate-300">
                  <span>Provider vs Local State Mapping</span>
                  <span className="text-[11px] font-mono text-emerald-400">
                    Source: {pCase.is_synthetic_fixture || latestOp?.is_synthetic_fixture ? "Simulated Demo State (Local Fixture)" : "Verified via PayPal Sandbox REST API v1"}
                  </span>
                </div>
                <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 text-xs pt-1">
                  <div>
                    <span className="text-slate-500">Internal Case Status:</span>
                    <div className="font-semibold text-white mt-0.5">{pCase.status}</div>
                  </div>
                  <div>
                    <span className="text-slate-500">Durable Operation State:</span>
                    <div className="font-semibold text-white mt-0.5">{latestOp.status}</div>
                  </div>
                  <div>
                    <span className="text-slate-500">PayPal Provider Status:</span>
                    <div className="font-semibold text-emerald-400 mt-0.5">
                      {latestOp.paypal_batch_status || "UNSUBMITTED"}
                      {latestOp.paypal_item_status && ` [Item: ${latestOp.paypal_item_status}]`}
                    </div>
                  </div>
                </div>
              </div>

              {/* Raw Provider Response Inspector */}
              {latestOp.raw_provider_response && (
                <div className="border border-slate-800 rounded-lg overflow-hidden text-xs">
                  <button
                    onClick={() => setExpandedRawResponse(!expandedRawResponse)}
                    className="w-full px-4 py-2.5 bg-slate-900/60 hover:bg-slate-900 flex items-center justify-between text-slate-400 hover:text-white transition"
                  >
                    <span className="font-mono text-[11px]">
                      Raw Provider Response Payload (JSON)
                    </span>
                    {expandedRawResponse ? (
                      <ChevronUp className="w-4 h-4" />
                    ) : (
                      <ChevronDown className="w-4 h-4" />
                    )}
                  </button>
                  {expandedRawResponse && (
                    <div className="p-4 bg-slate-950 border-t border-slate-800 font-mono text-[11px] text-slate-300 overflow-x-auto max-h-60">
                      <pre>{JSON.stringify(latestOp.raw_provider_response, null, 2)}</pre>
                    </div>
                  )}
                </div>
              )}
            </div>
          )}
        </section>

        {/* Audit and Operation History Table */}
        <section className="p-6 rounded-xl bg-slate-900/40 border border-slate-800 space-y-4">
          <h2 className="text-base font-semibold text-white">Durable Operations History</h2>
          {operations.length === 0 ? (
            <p className="text-xs text-slate-500">No operations recorded.</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-left text-xs">
                <thead>
                  <tr className="border-b border-slate-800 text-slate-400 font-semibold uppercase tracking-wider">
                    <th className="pb-3 pl-2">Operation ID</th>
                    <th className="pb-3">Sender Batch ID</th>
                    <th className="pb-3">Provider Batch ID</th>
                    <th className="pb-3">Status</th>
                    <th className="pb-3">Initiated By</th>
                    <th className="pb-3">Approved By</th>
                    <th className="pb-3 text-right pr-2">Created</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-800/60 font-mono text-[11px]">
                  {operations.map((op) => (
                    <tr key={op.id} className="hover:bg-slate-800/30">
                      <td className="py-3 pl-2 text-blue-400">{op.id.slice(0, 8)}...</td>
                      <td className="py-3 text-slate-300">{op.sender_batch_id}</td>
                      <td className="py-3 text-slate-400">
                        {op.paypal_payout_batch_id || "—"}
                      </td>
                      <td className="py-3">
                        <div className="flex items-center gap-1.5 flex-wrap">
                          <span className="font-semibold text-white">{op.status}</span>
                          {op.is_synthetic_fixture && (
                            <span className="px-1 py-0.2 rounded text-[9px] bg-amber-500/10 text-amber-400 border border-amber-500/30">
                              SIMULATED
                            </span>
                          )}
                        </div>
                      </td>
                      <td className="py-3 text-slate-400">{op.initiated_by_user_id.slice(0, 8)}...</td>
                      <td className="py-3 text-slate-400">
                        {op.approved_by_user_id ? `${op.approved_by_user_id.slice(0, 8)}...` : "—"}
                      </td>
                      <td className="py-3 text-right pr-2 text-slate-500">
                        {formatDate(op.created_at)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      </main>

      {/* Confirmation Modal: Submit Payout to PayPal Sandbox */}
      {confirmSubmitModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm p-4">
          <div className="max-w-md w-full bg-slate-900 border border-slate-800 rounded-2xl p-6 space-y-4 shadow-2xl">
            <div className="flex items-center gap-3 text-emerald-400">
              <Send className="w-5 h-5" />
              <h3 className="text-base font-bold text-white">
                Confirm PayPal Sandbox Payout
              </h3>
            </div>
            <p className="text-xs text-slate-300 leading-relaxed">
              You are about to submit an explicitly authorized sandbox payout request to PayPal REST API v1.
            </p>
            <div className="p-3.5 rounded-lg bg-slate-950 border border-slate-800 space-y-1.5 text-xs font-mono">
              <div className="flex justify-between">
                <span className="text-slate-500">Amount:</span>
                <span className="text-white font-bold">{formatCentsToUSD(pCase.amount_cents)}</span>
              </div>
              <div className="flex justify-between">
                <span className="text-slate-500">Recipient:</span>
                <span className="text-blue-400">{pCase.recipient_email}</span>
              </div>
              <div className="flex justify-between">
                <span className="text-slate-500">Sender Batch ID:</span>
                <span className="text-slate-300 truncate max-w-[200px]">
                  {latestOp?.sender_batch_id}
                </span>
              </div>
            </div>
            <div className="flex items-center justify-end gap-3 pt-2">
              <button
                onClick={() => setConfirmSubmitModal(false)}
                className="px-4 py-2 rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-300 text-xs font-medium transition"
              >
                Cancel
              </button>
              <button
                onClick={handleSubmitOperation}
                disabled={actionLoading === "submit"}
                className="px-4 py-2 rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white text-xs font-semibold transition flex items-center gap-2"
              >
                {actionLoading === "submit" ? (
                  <>
                    <div className="w-3.5 h-3.5 border-2 border-white border-t-transparent rounded-full animate-spin" />
                    <span>Submitting to Sandbox...</span>
                  </>
                ) : (
                  <span>Execute Sandbox Payout</span>
                )}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Confirmation Modal: Approve Payout Operation */}
      {confirmApproveModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm p-4">
          <div className="max-w-md w-full bg-slate-900 border border-slate-800 rounded-2xl p-6 space-y-4 shadow-2xl">
            <div className="flex items-center gap-3 text-emerald-400">
              <CheckCircle2 className="w-5 h-5" />
              <h3 className="text-base font-bold text-white">
                Authorize Dual-Custody Approval
              </h3>
            </div>
            <p className="text-xs text-slate-300 leading-relaxed">
              As a distinct authorized officer, your approval will permit this payout operation to be submitted to PayPal Sandbox.
            </p>
            <div className="flex items-center justify-end gap-3 pt-2">
              <button
                onClick={() => setConfirmApproveModal(false)}
                className="px-4 py-2 rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-300 text-xs font-medium transition"
              >
                Cancel
              </button>
              <button
                onClick={handleApproveOperation}
                disabled={actionLoading === "approve"}
                className="px-4 py-2 rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white text-xs font-semibold transition flex items-center gap-2"
              >
                {actionLoading === "approve" ? (
                  <span>Recording Approval...</span>
                ) : (
                  <span>Confirm Approval</span>
                )}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Confirmation Modal: Cancel Payout */}
      {confirmCancelModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm p-4">
          <div className="max-w-md w-full bg-slate-900 border border-slate-800 rounded-2xl p-6 space-y-4 shadow-2xl">
            <div className="flex items-center gap-3 text-rose-400">
              <XCircle className="w-5 h-5" />
              <h3 className="text-base font-bold text-white">
                Cancel Payout Operation
              </h3>
            </div>
            <p className="text-xs text-slate-300 leading-relaxed">
              Are you sure you want to cancel this payout operation?
              {latestOp?.paypal_payout_item_id && (
                <span>
                  {" "}This will send an explicit cancellation request to PayPal Sandbox for item ID {latestOp.paypal_payout_item_id}.
                </span>
              )}
            </p>
            <div className="flex items-center justify-end gap-3 pt-2">
              <button
                onClick={() => setConfirmCancelModal(false)}
                className="px-4 py-2 rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-300 text-xs font-medium transition"
              >
                Close
              </button>
              <button
                onClick={handleCancelOperation}
                disabled={actionLoading === "cancel"}
                className="px-4 py-2 rounded-lg bg-rose-600 hover:bg-rose-500 text-white text-xs font-semibold transition flex items-center gap-2"
              >
                {actionLoading === "cancel" ? (
                  <span>Cancelling...</span>
                ) : (
                  <span>Confirm Cancellation</span>
                )}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
