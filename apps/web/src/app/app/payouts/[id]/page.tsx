"use client";

import React, { useEffect, useState, useCallback, useRef } from "react";
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
  X,
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
  EvidenceResolution,
  formatCentsToUSD,
  formatDate,
  getInvestigationTransportInfo,
  formatInferenceDuration,
  formatElapsedTimer,
  recoverPayoutOperation,
  RecoveryOutcome,
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
  const [investigationElapsedSeconds, setInvestigationElapsedSeconds] = useState<number>(0);
  const investigationTimerRef = useRef<NodeJS.Timeout | null>(null);
  const [investigationError, setInvestigationError] = useState<string | null>(null);
  const [investigationSuccess, setInvestigationSuccess] = useState<string | null>(null);
  const [activeEvidenceRef, setActiveEvidenceRef] = useState<string | null>(null);

  // Evidence Inspector State & Focus Management
  const [selectedEvidence, setSelectedEvidence] = useState<EvidenceResolution | null>(null);
  const [evidenceLoading, setEvidenceLoading] = useState<boolean>(false);
  const [evidenceError, setEvidenceError] = useState<string | null>(null);
  const [isEvidenceInspectorOpen, setIsEvidenceInspectorOpen] = useState<boolean>(false);
  const lastTriggerElementRef = useRef<HTMLElement | null>(null);
  const drawerRef = useRef<HTMLDivElement | null>(null);
  const closeButtonRef = useRef<HTMLButtonElement | null>(null);
  const wasInspectorOpenRef = useRef<boolean>(false);

  // Modals confirmation state
  const [confirmSubmitModal, setConfirmSubmitModal] = useState<boolean>(false);
  const [confirmCancelModal, setConfirmCancelModal] = useState<boolean>(false);
  const [confirmApproveModal, setConfirmApproveModal] = useState<boolean>(false);
  const [confirmRecoverModal, setConfirmRecoverModal] = useState<boolean>(false);
  const [recoveryOutcome, setRecoveryOutcome] = useState<RecoveryOutcome | null>(null);

  // Show raw provider response accordion
  const [expandedRawResponse, setExpandedRawResponse] = useState<boolean>(false);

  // Clean up timer on unmount
  useEffect(() => {
    return () => {
      if (investigationTimerRef.current) {
        clearInterval(investigationTimerRef.current);
        investigationTimerRef.current = null;
      }
    };
  }, []);

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        setConfirmSubmitModal(false);
        setConfirmApproveModal(false);
        setConfirmCancelModal(false);
        setConfirmRecoverModal(false);
        setIsEvidenceInspectorOpen(false);
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, []);

  // Accessible Focus Management & Trap for Evidence Inspector Drawer
  useEffect(() => {
    if (isEvidenceInspectorOpen) {
      wasInspectorOpenRef.current = true;

      // Move focus inside dialog when opened
      const focusTimer = requestAnimationFrame(() => {
        closeButtonRef.current?.focus();
      });

      const handleTrapKeyDown = (e: KeyboardEvent) => {
        if (e.key === "Escape") {
          e.preventDefault();
          setIsEvidenceInspectorOpen(false);
          return;
        }

        if (e.key === "Tab") {
          if (!drawerRef.current) return;
          const focusableSelector =
            'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';
          const focusables = Array.from(
            drawerRef.current.querySelectorAll<HTMLElement>(focusableSelector)
          ).filter(
            (el) =>
              !el.hasAttribute("disabled") &&
              (el.offsetParent !== null || el.getClientRects().length > 0)
          );

          if (focusables.length === 0) {
            e.preventDefault();
            return;
          }

          const firstElement = focusables[0];
          const lastElement = focusables[focusables.length - 1];

          if (e.shiftKey) {
            // Shift + Tab: wrapping backwards
            if (
              document.activeElement === firstElement ||
              !drawerRef.current.contains(document.activeElement)
            ) {
              e.preventDefault();
              lastElement.focus();
            }
          } else {
            // Tab: wrapping forwards
            if (
              document.activeElement === lastElement ||
              !drawerRef.current.contains(document.activeElement)
            ) {
              e.preventDefault();
              firstElement.focus();
            }
          }
        }
      };

      window.addEventListener("keydown", handleTrapKeyDown);
      return () => {
        cancelAnimationFrame(focusTimer);
        window.removeEventListener("keydown", handleTrapKeyDown);
      };
    } else if (wasInspectorOpenRef.current) {
      wasInspectorOpenRef.current = false;
      // Restore focus to triggering evidence chip if available
      const trigger = lastTriggerElementRef.current;
      if (trigger && document.contains(trigger) && typeof trigger.focus === "function") {
        trigger.focus();
      } else if (activeEvidenceRef) {
        const chip = document.querySelector<HTMLElement>(
          `[data-testid="evidence-chip-${activeEvidenceRef}"]`
        );
        chip?.focus();
      }
    }
  }, [isEvidenceInspectorOpen, activeEvidenceRef]);

  const inspectEvidence = useCallback(
    async (refId: string, event?: React.MouseEvent | React.KeyboardEvent | React.UIEvent) => {
      if (!caseId || !refId) return;

      // Capture the trigger element before state transition
      if (event?.currentTarget && (event.currentTarget as HTMLElement).focus) {
        lastTriggerElementRef.current = event.currentTarget as HTMLElement;
      } else if (document.activeElement instanceof HTMLElement) {
        lastTriggerElementRef.current = document.activeElement;
      }

      setActiveEvidenceRef(refId);
      setIsEvidenceInspectorOpen(true);
      setEvidenceLoading(true);
      setEvidenceError(null);
      setSelectedEvidence(null);

      try {
        const res = await fetch(`/api/v1/payout-cases/${caseId}/evidence/${encodeURIComponent(refId)}`, {
          method: "GET",
          credentials: "include",
        });
        if (res.ok) {
          const data: EvidenceResolution = await res.json();
          setSelectedEvidence(data);
        } else {
          const err = await res.json().catch(() => ({}));
          setEvidenceError(
            err.error ||
            "Evidence reference could not be validated against authorized case records or snapshots."
          );
        }
      } catch {
        setEvidenceError("Network error attempting to resolve evidence reference");
      } finally {
        setEvidenceLoading(false);
      }
    },
    [caseId]
  );

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
    // Guard against duplicate concurrent execution
    if (investigating) return;

    setInvestigating(true);
    setInvestigationError(null);
    setInvestigationSuccess(null);
    setInvestigationElapsedSeconds(0);

    if (investigationTimerRef.current) {
      clearInterval(investigationTimerRef.current);
      investigationTimerRef.current = null;
    }

    const startTime = Date.now();
    investigationTimerRef.current = setInterval(() => {
      const elapsed = Math.floor((Date.now() - startTime) / 1000);
      setInvestigationElapsedSeconds(elapsed);
    }, 1000);

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
      if (investigationTimerRef.current) {
        clearInterval(investigationTimerRef.current);
        investigationTimerRef.current = null;
      }
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

  const handleRecoverOperation = async () => {
    if (!latestOp) return;
    setActionLoading("recover");
    setErrorMessage(null);
    setSuccessMessage(null);
    try {
      const data = await recoverPayoutOperation(latestOp.id, caseId);
      setRecoveryOutcome(data.outcome);
      if (data.outcome === "ORIGINAL_BATCH_RECOVERED") {
        setSuccessMessage(
          `Original PayPal batch successfully recovered (${data.operation.paypal_payout_batch_id || "batch recovered"}). Provider status reconciled.`
        );
      } else if (data.outcome === "REQUEST_NEWLY_ACCEPTED") {
        setSuccessMessage(
          `Request newly accepted by PayPal Sandbox (Batch ID: ${data.operation.paypal_payout_batch_id || "accepted"}).`
        );
      } else {
        setSuccessMessage(`Recovery finished: ${data.message}`);
      }
      setConfirmRecoverModal(false);
      fetchCaseDetails();
    } catch (err: any) {
      if (err.outcome) {
        setRecoveryOutcome(err.outcome);
      }
      setErrorMessage(err.message || "Failed to recover ambiguous payout submission");
      fetchCaseDetails();
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
        <main id="main-content" tabIndex={-1} className="flex-1 max-w-4xl w-full mx-auto px-6 py-12 text-center space-y-4 outline-none">
          <AlertTriangle className="w-10 h-10 text-amber-400 mx-auto" aria-hidden="true" />
          <h1 className="text-lg font-bold text-white">Payout Case Not Found</h1>
          <p className="text-xs text-slate-400">
            This case may not exist or does not belong to your active organization.
          </p>
          <Link
            href="/app/payouts"
            className="inline-flex items-center gap-2 min-h-[44px] px-4 py-2 rounded-lg bg-blue-600 hover:bg-blue-500 text-white text-xs font-semibold"
          >
            <ArrowLeft className="w-4 h-4" aria-hidden="true" />
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
  const isSubmissionExpired = Boolean(
    latestOp?.submitted_at &&
      Date.now() - new Date(latestOp.submitted_at).getTime() > 27 * 24 * 60 * 60 * 1000
  );
  const isSubmissionTimestampMissing = Boolean(
    latestOp &&
      latestOp.status === "RECONCILIATION_REQUIRED" &&
      !latestOp.submitted_at
  );
  const isStaleRecoveryClaim = Boolean(
    latestOp &&
      latestOp.status === "RECOVERING" &&
      latestOp.recovery_lease_expires_at &&
      Date.now() > new Date(latestOp.recovery_lease_expires_at).getTime()
  );
  const canRecover = Boolean(
    latestOp &&
      latestOp.status === "RECONCILIATION_REQUIRED" &&
      !latestOp.paypal_payout_batch_id &&
      !pCase.is_synthetic_fixture &&
      !latestOp.is_synthetic_fixture &&
      pCase.status !== "COMPLETED" &&
      pCase.status !== "CANCELLED" &&
      !isSubmissionExpired &&
      !isSubmissionTimestampMissing
  );

  return (
    <div className="min-h-screen bg-slate-950 text-slate-100 flex flex-col">
      <div
        aria-hidden={isEvidenceInspectorOpen ? true : undefined}
        inert={isEvidenceInspectorOpen ? true : undefined}
        className="flex flex-col flex-1"
      >
        <Header />

        <main id="main-content" tabIndex={-1} className="flex-1 max-w-7xl w-full mx-auto px-6 py-8 space-y-6 outline-none">
        {/* Back Link & Header */}
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
          <div className="space-y-1">
            <Link
              href="/app/payouts"
              className="inline-flex items-center gap-1.5 min-h-[36px] text-xs text-slate-400 hover:text-white transition font-medium mb-1"
            >
              <ArrowLeft className="w-4 h-4" aria-hidden="true" />
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
                aria-label="Sync status with PayPal Sandbox"
                className="min-h-[44px] px-3.5 py-2 rounded-lg bg-blue-600/20 hover:bg-blue-600/30 disabled:opacity-40 disabled:cursor-not-allowed text-blue-400 border border-blue-500/30 text-xs font-semibold transition flex items-center gap-1.5"
                title={pCase.is_synthetic_fixture ? "Disabled: Synthetic Demo Fixture" : undefined}
              >
                <RefreshCw
                  className={`w-3.5 h-3.5 ${actionLoading === "sync" ? "animate-spin" : ""}`}
                  aria-hidden="true"
                />
                <span>Sync with PayPal</span>
              </button>
            )}
            <button
              onClick={fetchCaseDetails}
              disabled={loadingData}
              aria-label="Refresh case details"
              className="min-h-[44px] min-w-[44px] p-2 rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-300 hover:text-white transition border border-slate-700 text-xs flex items-center justify-center"
            >
              <RefreshCw className={`w-4 h-4 ${loadingData ? "animate-spin" : ""}`} aria-hidden="true" />
            </button>
          </div>
        </div>

        {/* Synthetic Demo Provenance Banner */}
        {(pCase.is_synthetic_fixture || latestOp?.is_synthetic_fixture) && (
          <div className="p-4 rounded-xl bg-amber-500/10 border border-amber-500/30 text-amber-300 text-xs flex items-start gap-3 shadow-lg shadow-amber-500/5">
            <AlertTriangle className="w-5 h-5 flex-shrink-0 text-amber-400 mt-0.5" aria-hidden="true" />
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
          <div
            role="alert"
            aria-live="assertive"
            className="p-4 rounded-xl bg-rose-500/10 border border-rose-500/30 text-rose-300 text-xs flex items-center gap-2.5"
          >
            <AlertTriangle className="w-4 h-4 flex-shrink-0 text-rose-400" aria-hidden="true" />
            <span>{errorMessage}</span>
          </div>
        )}

        {successMessage && (
          <div
            role="status"
            aria-live="polite"
            className="p-4 rounded-xl bg-emerald-500/10 border border-emerald-500/30 text-emerald-300 text-xs flex items-center gap-2.5"
          >
            <CheckCircle2 className="w-4 h-4 flex-shrink-0 text-emerald-400" aria-hidden="true" />
            <span>{successMessage}</span>
          </div>
        )}

        {/* Distinct Recovery in Progress Banner */}
        {latestOp?.status === "RECOVERING" && (
          <div
            role="status"
            aria-live="polite"
            data-testid="recovery-in-progress-panel"
            className="p-4 rounded-xl bg-indigo-500/10 border border-indigo-500/30 text-indigo-200 text-xs flex items-center gap-3 animate-pulse"
          >
            <RefreshCw className="w-5 h-5 text-indigo-400 animate-spin flex-shrink-0" aria-hidden="true" />
            <div className="space-y-0.5">
              <span className="font-bold uppercase tracking-wider text-indigo-300">
                Safe Recovery Protocol in Progress
              </span>
              <p className="text-slate-300">
                Replaying verified request to PayPal Sandbox to recover original batch evidence without creating duplicate transactions...
              </p>
            </div>
          </div>
        )}

        {/* Distinct Recovery Outcome Banner */}
        {recoveryOutcome && (
          <div
            role="status"
            data-testid={`recovery-outcome-${recoveryOutcome.toLowerCase().replace(/_/g, "-")}`}
            className={`p-4 rounded-xl text-xs space-y-1.5 border ${
              recoveryOutcome === "ORIGINAL_BATCH_RECOVERED"
                ? "bg-emerald-500/10 border-emerald-500/30 text-emerald-200"
                : recoveryOutcome === "REQUEST_NEWLY_ACCEPTED"
                ? "bg-blue-500/10 border-blue-500/30 text-blue-200"
                : recoveryOutcome === "MANUAL_RECONCILIATION_REQUIRED"
                ? "bg-rose-500/10 border-rose-500/30 text-rose-200"
                : "bg-amber-500/10 border-amber-500/30 text-amber-200"
            }`}
          >
            <div className="flex items-center gap-2 font-bold uppercase tracking-wider">
              {recoveryOutcome === "ORIGINAL_BATCH_RECOVERED" && <CheckCircle2 className="w-4 h-4 text-emerald-400" />}
              {recoveryOutcome === "REQUEST_NEWLY_ACCEPTED" && <Send className="w-4 h-4 text-blue-400" />}
              {recoveryOutcome === "MANUAL_RECONCILIATION_REQUIRED" && <XCircle className="w-4 h-4 text-rose-400" />}
              {recoveryOutcome === "UNRESOLVED_OUTCOME" && <AlertTriangle className="w-4 h-4 text-amber-400" />}
              <span>
                Recovery State: {recoveryOutcome.replace(/_/g, " ")}
              </span>
            </div>
            <p className="text-slate-300 leading-relaxed text-[11px]">
              {recoveryOutcome === "ORIGINAL_BATCH_RECOVERED" &&
                "Original PayPal batch was identified via duplicate correlation link and its authoritative provider status was successfully reconciled."}
              {recoveryOutcome === "REQUEST_NEWLY_ACCEPTED" &&
                "PayPal confirmed the original submission was never received and newly accepted this payout request."}
              {recoveryOutcome === "UNRESOLVED_OUTCOME" &&
                "The recovery attempt encountered an unconfirmed outcome or network failure. Operation retained in RECONCILIATION_REQUIRED."}
              {recoveryOutcome === "MANUAL_RECONCILIATION_REQUIRED" &&
                "Automated recovery failed closed due to an untrusted or malformed provider response, missing fingerprint, or intent mismatch. Manual reconciliation is required."}
            </p>
          </div>
        )}

        {/* Ambiguous Submission / Reconciliation Required Panel */}
        {latestOp?.status === "RECONCILIATION_REQUIRED" && (
          <div
            role="alert"
            data-testid="reconciliation-required-panel"
            className="p-5 rounded-xl bg-amber-500/10 border border-amber-500/30 text-amber-200 text-xs space-y-3 shadow-lg shadow-amber-500/5"
          >
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <AlertTriangle className="w-5 h-5 text-amber-400 flex-shrink-0" aria-hidden="true" />
                <h3 className="text-sm font-bold uppercase tracking-wider text-amber-300">
                  Ambiguous Payout Submission — Reconciliation Required
                </h3>
              </div>
              <span className="px-2 py-0.5 rounded text-[10px] font-mono font-semibold bg-amber-500/20 text-amber-300 border border-amber-500/40">
                STATE: RECONCILIATION_REQUIRED
              </span>
            </div>

            <p className="text-slate-300 leading-relaxed">
              {latestOp.paypal_payout_batch_id ? (
                <>PayPal batch ID is known (<span className="font-mono text-white">{latestOp.paypal_payout_batch_id}</span>). Use <strong>Sync Latest Status</strong> to refresh item-authoritative status from PayPal.</>
              ) : (
                <>The previous payout mutation encountered an ambiguous network timeout. The request outcome with PayPal is unconfirmed. Do not retry with a new sender batch ID or create duplicate payouts.</>
              )}
            </p>

            {latestOp.error_message && (
              <div className="p-3 rounded-lg bg-slate-950/80 border border-amber-500/20 text-[11px] font-mono text-amber-400/90 break-words">
                Provider Diagnostic: {latestOp.error_message}
              </div>
            )}

            {isSubmissionExpired && (
              <div data-testid="recovery-expired-banner" className="p-3.5 rounded-lg bg-rose-500/10 border border-rose-500/30 text-xs text-rose-300 space-y-1">
                <div className="font-semibold flex items-center gap-2">
                  <AlertTriangle className="w-4 h-4 flex-shrink-0 text-rose-400" />
                  <span>Replay Safety Cutoff Expired (&gt;27 Days)</span>
                </div>
                <p className="text-slate-300 text-[11px] leading-relaxed">
                  The original submission attempt was recorded on <span className="font-mono text-white">{latestOp.submitted_at ? formatDate(latestOp.submitted_at) : "unknown"}</span>, which exceeds ClaimBridge&apos;s conservative 27-day recovery cutoff. PayPal documents duplicate batch protection for up to 30 days. To eliminate duplicate payment risks, automated replay is strictly disabled.
                </p>
                <div className="pt-1 text-[11px] text-amber-300/90 font-medium">
                  <strong>What Manual Reconciliation Means:</strong> A human payments officer must verify whether the recipient was paid directly in the PayPal Business Portal or Resolution Center before taking further action. Never issue a replacement payout until provider settlement status is confirmed.
                </div>
              </div>
            )}

            {isSubmissionTimestampMissing && (
              <div data-testid="recovery-missing-timestamp-banner" className="p-3.5 rounded-lg bg-rose-500/10 border border-rose-500/30 text-xs text-rose-300 space-y-1">
                <div className="font-semibold flex items-center gap-2">
                  <AlertTriangle className="w-4 h-4 flex-shrink-0 text-rose-400" />
                  <span>Original Submission Timestamp Missing</span>
                </div>
                <p className="text-slate-300 text-[11px] leading-relaxed">
                  The original submission timestamp is missing or unverifiable. To ensure financial safety, automated replay is disabled. Please perform manual reconciliation via PayPal portal inspection.
                </p>
              </div>
            )}

            {canRecover && (
              <div className="pt-1 flex flex-wrap items-center gap-3">
                <button
                  onClick={() => setConfirmRecoverModal(true)}
                  disabled={actionLoading !== null}
                  data-testid="recover-payout-banner-button"
                  className="px-4 py-2 rounded-lg bg-amber-600 hover:bg-amber-500 disabled:opacity-40 disabled:cursor-not-allowed text-white text-xs font-semibold transition flex items-center gap-2 shadow-lg shadow-amber-600/20"
                >
                  <RefreshCw className={`w-3.5 h-3.5 ${actionLoading === "recover" ? "animate-spin" : ""}`} />
                  <span>Initiate Safe Recovery Protocol</span>
                </button>
                <span className="text-[11px] text-slate-400">
                  Resubmits identical request with durable sender batch ID ({latestOp.sender_batch_id}) to locate original batch.
                </span>
              </div>
            )}
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

            {/* 5. Recover Ambiguous Submission */}
            {canRecover && (
              <button
                onClick={() => setConfirmRecoverModal(true)}
                disabled={actionLoading !== null || latestOp?.status === "RECOVERING"}
                data-testid="recover-payout-toolbar-button"
                className="px-4 py-2.5 rounded-lg bg-amber-600 hover:bg-amber-500 disabled:opacity-40 disabled:cursor-not-allowed text-white text-xs font-semibold transition flex items-center gap-2 shadow-lg shadow-amber-600/20"
              >
                <RefreshCw
                  className={`w-4 h-4 ${actionLoading === "recover" || latestOp?.status === "RECOVERING" ? "animate-spin" : ""}`}
                />
                <span>Recover Ambiguous Submission</span>
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
                    {pCase.is_synthetic_fixture && (
                      <span className="px-2 py-0.5 rounded text-[10px] font-mono font-semibold bg-amber-500/20 text-amber-300 border border-amber-500/30">
                        Simulated Evaluation Fixture
                      </span>
                    )}
                  </div>
                  <p className="text-xs text-slate-400">
                    {pCase.is_synthetic_fixture
                      ? "Forensic root-cause analysis based on pre-seeded synthetic evaluation records (isolated from live provider networks)."
                      : "Forensic root-cause analysis grounded in immutable case and provider records."}
                  </p>
                </div>
              </div>
            </div>

            {/* Investigation Actions */}
            <div className="flex items-center gap-2">
              <button
                onClick={() => handleRunInvestigation(false)}
                disabled={investigating}
                className="px-4 py-2 rounded-xl bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 disabled:cursor-not-allowed text-white text-xs font-semibold transition flex items-center gap-2 shadow-lg shadow-indigo-600/20"
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
                  className="px-3.5 py-2 rounded-xl bg-slate-800/80 hover:bg-slate-800 disabled:opacity-50 disabled:cursor-not-allowed text-slate-300 hover:text-white border border-slate-700 text-xs font-medium transition flex items-center gap-1.5"
                >
                  <RefreshCw className={`w-3.5 h-3.5 ${investigating ? "animate-spin" : ""}`} />
                  <span>Sync &amp; Investigate</span>
                </button>
              )}
            </div>
          </div>

          {/* Error / Offline Alert */}
          {investigationError && (
            <div
              role="alert"
              data-testid="investigation-error-banner"
              className="p-4 rounded-xl bg-rose-500/10 border border-rose-500/30 text-rose-300 text-xs flex items-start gap-3"
            >
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
            <div
              role="status"
              data-testid="investigation-success-banner"
              className="p-3 rounded-xl bg-emerald-500/10 border border-emerald-500/30 text-emerald-300 text-xs flex items-center gap-2"
            >
              <CheckCircle2 className="w-4 h-4 text-emerald-400 flex-shrink-0" />
              <span>{investigationSuccess}</span>
            </div>
          )}

          {/* Accessible In-Progress Investigation Panel */}
          {investigating && (
            <div
              role="status"
              aria-live="polite"
              aria-atomic="true"
              data-testid="investigation-pending-panel"
              className="p-5 rounded-xl bg-indigo-950/40 border border-indigo-500/40 shadow-lg shadow-indigo-950/50 space-y-3.5 transition-all animate-pulse"
            >
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div className="flex items-center gap-2.5">
                  <RefreshCw className="w-4 h-4 text-indigo-400 animate-spin flex-shrink-0" />
                  <span className="text-xs font-bold uppercase tracking-wider text-white">
                    Forensic AI Investigation in Progress
                  </span>
                  <span
                    data-testid="elapsed-timer"
                    className="px-2.5 py-0.5 rounded-full text-xs font-mono font-bold bg-indigo-500/20 text-indigo-300 border border-indigo-500/40"
                  >
                    Elapsed: {formatElapsedTimer(investigationElapsedSeconds)}
                  </span>
                </div>
                <span className="text-[11px] font-mono text-indigo-300/80">
                  Awaiting model response...
                </span>
              </div>

              <p className="text-xs text-slate-300 leading-relaxed font-sans">
                Local CPU inference is executing. On CPU-only development hardware, processing local model weights may take over a minute. Actual timings vary depending on host hardware, CPU thread allocation, and model context size.
              </p>

              <div className="pt-2 border-t border-indigo-500/20 flex items-center gap-2 text-[11px] text-amber-300/90">
                <ShieldAlert className="w-3.5 h-3.5 flex-shrink-0 text-amber-400" />
                <span>
                  AI ADVISORY SCOPE: This investigation is strictly forensic and advisory. It will not submit, approve, execute, or cancel any financial payout.
                </span>
              </div>
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
                disabled={investigating}
                className="mt-2 px-4 py-2 rounded-lg bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 disabled:cursor-not-allowed text-white text-xs font-semibold transition inline-flex items-center gap-2 shadow-lg shadow-indigo-600/20"
              >
                <Sparkles className="w-3.5 h-3.5" />
                <span>Run First Investigation</span>
              </button>
            </div>
          )}

          {/* Selected Investigation Results Display */}
          {selectedInvestigation && (() => {
            const transportInfo = getInvestigationTransportInfo(selectedInvestigation);
            const formattedDuration = formatInferenceDuration(selectedInvestigation.raw_response);

            return (
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
                    <div className="flex flex-wrap items-center gap-2.5">
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

                      {/* Transport Badge */}
                      <span
                        data-testid="header-transport-badge"
                        className={`px-2 py-0.5 rounded text-[10px] font-semibold tracking-wide ${
                          transportInfo.type === "native_ollama"
                            ? "bg-purple-500/20 text-purple-300 border border-purple-500/30"
                            : transportInfo.type === "openai_compatible"
                            ? "bg-blue-500/20 text-blue-300 border border-blue-500/30"
                            : "bg-slate-800/80 text-slate-400 border border-slate-700"
                        }`}
                      >
                        {transportInfo.badgeLabel}
                      </span>

                      {/* Duration Badge */}
                      {formattedDuration && (
                        <span
                          data-testid="header-duration-badge"
                          className="px-2 py-0.5 rounded text-[10px] font-semibold bg-emerald-500/10 text-emerald-300 border border-emerald-500/30 font-mono"
                        >
                          Inference: {formattedDuration}
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

                  {/* Explicit Disclaimer and Telemetry Metadata */}
                  <div className="pt-2.5 border-t border-slate-900 flex flex-wrap items-center justify-between gap-3 text-[11px] text-slate-400">
                    <span className="flex items-center gap-1.5 text-slate-500">
                      <Info className="w-3.5 h-3.5 text-slate-500 flex-shrink-0" />
                      <span>AI ADVISORY OPINION: Advisory only. All payments require deterministic officer authorization.</span>
                    </span>
                    <div className="flex flex-wrap items-center gap-2 font-mono text-[10px]">
                      <span
                        data-testid="investigation-transport"
                        className={`px-2 py-0.5 rounded border ${
                          transportInfo.type === "native_ollama"
                            ? "bg-purple-500/10 text-purple-300 border-purple-500/30"
                            : transportInfo.type === "openai_compatible"
                            ? "bg-blue-500/10 text-blue-300 border-blue-500/30"
                            : "bg-slate-800 text-slate-400 border-slate-700"
                        }`}
                      >
                        Transport: {transportInfo.label}
                      </span>
                      <span data-testid="investigation-engine" className="text-slate-400">
                        Engine: {selectedInvestigation.provider} ({selectedInvestigation.model})
                      </span>
                      {formattedDuration ? (
                        <span
                          data-testid="investigation-duration"
                          className="px-2 py-0.5 rounded bg-emerald-500/10 text-emerald-300 border border-emerald-500/30 font-semibold"
                        >
                          Duration: {formattedDuration}
                        </span>
                      ) : (
                        <span
                          data-testid="investigation-duration-historical"
                          className="text-slate-500 italic"
                        >
                          Duration: Not recorded (historical)
                        </span>
                      )}
                    </div>
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
                              <span
                                data-testid="finding-validated-badge"
                                className="px-1.5 py-0.5 rounded text-[9px] font-mono font-semibold bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 whitespace-nowrap"
                              >
                                REFERENCE VALIDATED
                              </span>
                            </div>
                            {/* Evidence Reference Chips */}
                            {f.evidence_references && f.evidence_references.length > 0 && (
                              <div className="flex flex-wrap items-center gap-1.5 pt-1">
                                <span className="text-[10px] text-slate-500">Evidence:</span>
                                {f.evidence_references.map((refId, rIdx) => (
                                  <button
                                    key={rIdx}
                                    type="button"
                                    onClick={(e) => inspectEvidence(refId, e)}
                                    className={`px-2 py-0.5 rounded text-[10px] font-mono transition border ${
                                      activeEvidenceRef === refId && isEvidenceInspectorOpen
                                        ? "bg-indigo-500/30 text-indigo-200 border-indigo-400 ring-1 ring-indigo-400"
                                        : "bg-slate-950 text-slate-400 hover:text-white border-slate-800 hover:border-slate-700"
                                    }`}
                                    title={`Inspect evidence reference ${refId}`}
                                    aria-label={`Inspect evidence reference ${refId}`}
                                    data-testid={`evidence-chip-${refId}`}
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

              {/* Raw Provider Diagnostic Telemetry Accordion */}
              {selectedInvestigation.raw_response && (
                <div className="p-4 rounded-xl bg-slate-950/60 border border-slate-800 space-y-3">
                  <button
                    onClick={() => setExpandedRawResponse(!expandedRawResponse)}
                    data-testid="toggle-raw-telemetry"
                    className="text-slate-400 hover:text-white text-xs font-mono flex items-center gap-2 transition"
                  >
                    {expandedRawResponse ? <ChevronUp className="w-3.5 h-3.5" /> : <ChevronDown className="w-3.5 h-3.5" />}
                    <span>{expandedRawResponse ? "Hide Diagnostic Telemetry Envelope" : "View Diagnostic Telemetry Envelope (Safe Metadata)"}</span>
                  </button>
                  {expandedRawResponse && (
                    <div className="p-3.5 rounded-lg bg-slate-950 border border-slate-900 font-mono text-[11px] text-slate-300 overflow-x-auto space-y-2">
                      <div className="text-[10px] uppercase font-sans text-slate-500 tracking-wider">
                        Provider Envelope (Excludes Prompts, Credentials, and Snapshots)
                      </div>
                      <pre data-testid="raw-telemetry-json">{JSON.stringify(selectedInvestigation.raw_response, null, 2)}</pre>
                    </div>
                  )}
                </div>
              )}
            </div>
          );
        })()}
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

                {latestOp.submission_fingerprint && (
                  <div className="p-3.5 rounded-lg bg-slate-950 border border-slate-800 space-y-1 col-span-full">
                    <span className="text-[10px] text-slate-500 uppercase">Cryptographic Submission Fingerprint</span>
                    <div className="text-indigo-400 font-mono text-[11px] break-all">{latestOp.submission_fingerprint}</div>
                  </div>
                )}

                {latestOp.recovery_attempt_id && (
                  <div className="p-3.5 rounded-lg bg-slate-950 border border-slate-800 space-y-1 col-span-full">
                    <span className="text-[10px] text-slate-500 uppercase">Atomic Recovery Claim Ownership</span>
                    <div className="text-amber-400 font-mono text-[11px] flex flex-wrap gap-4">
                      <span>Attempt ID: {latestOp.recovery_attempt_id}</span>
                      {latestOp.recovery_claimed_at && (
                        <span>Claimed: {formatDate(latestOp.recovery_claimed_at)}</span>
                      )}
                      {latestOp.recovery_lease_expires_at && (
                        <span>Lease Expiry: {formatDate(latestOp.recovery_lease_expires_at)}</span>
                      )}
                    </div>
                  </div>
                )}
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
      </div>

      {/* Confirmation Modal: Submit Payout to PayPal Sandbox */}
      {confirmSubmitModal && (
        <div
          role="dialog"
          aria-modal="true"
          aria-labelledby="submit-modal-title"
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm p-4"
        >
          <div className="max-w-md w-full bg-slate-900 border border-slate-800 rounded-2xl p-6 space-y-4 shadow-2xl">
            <div className="flex items-center gap-3 text-emerald-400">
              <Send className="w-5 h-5" aria-hidden="true" />
              <h3 id="submit-modal-title" className="text-base font-bold text-white">
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
                className="min-h-[44px] px-4 py-2 rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-300 text-xs font-medium transition"
              >
                Cancel
              </button>
              <button
                onClick={handleSubmitOperation}
                disabled={actionLoading === "submit"}
                className="min-h-[44px] px-4 py-2 rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white text-xs font-semibold transition flex items-center gap-2"
              >
                {actionLoading === "submit" ? (
                  <>
                    <div className="w-3.5 h-3.5 border-2 border-white border-t-transparent rounded-full animate-spin" aria-hidden="true" />
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
        <div
          role="dialog"
          aria-modal="true"
          aria-labelledby="approve-modal-title"
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm p-4"
        >
          <div className="max-w-md w-full bg-slate-900 border border-slate-800 rounded-2xl p-6 space-y-4 shadow-2xl">
            <div className="flex items-center gap-3 text-emerald-400">
              <CheckCircle2 className="w-5 h-5" aria-hidden="true" />
              <h3 id="approve-modal-title" className="text-base font-bold text-white">
                Authorize Dual-Custody Approval
              </h3>
            </div>
            <p className="text-xs text-slate-300 leading-relaxed">
              As a distinct authorized officer, your approval will permit this payout operation to be submitted to PayPal Sandbox.
            </p>
            <div className="flex items-center justify-end gap-3 pt-2">
              <button
                onClick={() => setConfirmApproveModal(false)}
                className="min-h-[44px] px-4 py-2 rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-300 text-xs font-medium transition"
              >
                Cancel
              </button>
              <button
                onClick={handleApproveOperation}
                disabled={actionLoading === "approve"}
                className="min-h-[44px] px-4 py-2 rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white text-xs font-semibold transition flex items-center gap-2"
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
        <div
          role="dialog"
          aria-modal="true"
          aria-labelledby="cancel-modal-title"
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm p-4"
        >
          <div className="max-w-md w-full bg-slate-900 border border-slate-800 rounded-2xl p-6 space-y-4 shadow-2xl">
            <div className="flex items-center gap-3 text-rose-400">
              <XCircle className="w-5 h-5" aria-hidden="true" />
              <h3 id="cancel-modal-title" className="text-base font-bold text-white">
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
                className="min-h-[44px] px-4 py-2 rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-300 text-xs font-medium transition"
              >
                Close
              </button>
              <button
                onClick={handleCancelOperation}
                disabled={actionLoading === "cancel"}
                className="min-h-[44px] px-4 py-2 rounded-lg bg-rose-600 hover:bg-rose-500 text-white text-xs font-semibold transition flex items-center gap-2"
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

      {/* Confirmation Modal: Safe Recovery of Ambiguous PayPal Submission */}
      {confirmRecoverModal && (
        <div
          role="dialog"
          aria-modal="true"
          aria-labelledby="recover-modal-title"
          data-testid="confirm-recover-modal"
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm p-4"
        >
          <div className="max-w-lg w-full bg-slate-900 border border-slate-800 rounded-2xl p-6 space-y-4 shadow-2xl">
            <div className="flex items-center gap-3 text-amber-400">
              <AlertTriangle className="w-5 h-5 flex-shrink-0" aria-hidden="true" />
              <h3 id="recover-modal-title" className="text-base font-bold text-white">
                Safe Recovery of Ambiguous Submission
              </h3>
            </div>

            <div className="p-3.5 rounded-xl bg-amber-500/10 border border-amber-500/20 text-xs text-amber-200/90 space-y-2 leading-relaxed">
              <div className="font-semibold text-amber-300">Important Safety Warning:</div>
              <ul className="list-disc pl-4 space-y-1 text-slate-300 text-[11px]">
                <li><strong>Unknown Outcome:</strong> The previous submission&apos;s outcome is unknown due to a network timeout.</li>
                <li><strong>Exact Request Replay:</strong> ClaimBridge will resend the exact previously approved request using the same sender batch ID (<span className="font-mono text-white">{latestOp?.sender_batch_id}</span>).</li>
                <li><strong>Potential Acceptance:</strong> If PayPal did not accept the original request, the recovery action may cause that original payout to be accepted.</li>
                <li><strong>No Replacement IDs:</strong> The operation will not receive a new identifier.</li>
                <li><strong>Time-Bounded Safety Policy:</strong> ClaimBridge enforces a conservative 27-day cutoff (inside PayPal&apos;s 30-day duplicate window) derived from the original submission attempt timestamp.</li>
                <li><strong>Safety Policy Intact:</strong> The action does not bypass dual approval or payout safety policy.</li>
              </ul>
            </div>

            <div className="p-3 rounded-lg bg-slate-950 border border-slate-800 space-y-1 text-xs font-mono">
              <div className="flex justify-between">
                <span className="text-slate-500">Sender Batch ID:</span>
                <span className="text-white truncate max-w-[220px]">{latestOp?.sender_batch_id}</span>
              </div>
              <div className="flex justify-between">
                <span className="text-slate-500">Sender Item ID:</span>
                <span className="text-white truncate max-w-[220px]">{latestOp?.sender_item_id}</span>
              </div>
              <div className="flex justify-between">
                <span className="text-slate-500">Amount:</span>
                <span className="text-white font-bold">{formatCentsToUSD(pCase.amount_cents)}</span>
              </div>
              <div className="flex justify-between">
                <span className="text-slate-500">Recipient:</span>
                <span className="text-blue-400 truncate max-w-[220px]">{pCase.recipient_email}</span>
              </div>
              {latestOp?.submission_fingerprint && (
                <div className="flex justify-between text-[10px] pt-1 border-t border-slate-900">
                  <span className="text-slate-500">Fingerprint:</span>
                  <span className="text-indigo-400 truncate max-w-[220px]">{latestOp.submission_fingerprint.slice(0, 16)}...</span>
                </div>
              )}
            </div>

            <div className="flex items-center justify-end gap-3 pt-2">
              <button
                type="button"
                onClick={() => setConfirmRecoverModal(false)}
                className="min-h-[44px] px-4 py-2 rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-300 text-xs font-medium transition"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={handleRecoverOperation}
                disabled={actionLoading === "recover"}
                data-testid="confirm-recover-submit-button"
                className="min-h-[44px] px-4 py-2 rounded-lg bg-amber-600 hover:bg-amber-500 disabled:opacity-40 disabled:cursor-not-allowed text-white text-xs font-semibold transition flex items-center gap-2"
              >
                {actionLoading === "recover" ? (
                  <>
                    <RefreshCw className="w-3.5 h-3.5 animate-spin" />
                    <span>Executing Safe Recovery...</span>
                  </>
                ) : (
                  <span>Confirm Safe Recovery</span>
                )}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Grounded Evidence Inspector Drawer */}
      {isEvidenceInspectorOpen && (
        <div
          role="dialog"
          aria-modal="true"
          aria-labelledby="evidence-inspector-title"
          data-testid="evidence-inspector-drawer"
          className="fixed inset-0 z-50 flex justify-end bg-black/70 backdrop-blur-sm transition-opacity"
          onClick={(e) => {
            if (e.target === e.currentTarget) {
              setIsEvidenceInspectorOpen(false);
            }
          }}
        >
          <div
            ref={drawerRef}
            tabIndex={-1}
            className="w-full max-w-lg h-full bg-slate-900 border-l border-slate-800 flex flex-col shadow-2xl overflow-hidden animate-in slide-in-from-right duration-200 outline-none"
          >
            {/* Header */}
            <div className="p-5 border-b border-slate-800 flex items-center justify-between bg-slate-950/70">
              <div className="space-y-1">
                <div className="flex items-center gap-2">
                  <ShieldCheck className="w-4 h-4 text-emerald-400" aria-hidden="true" />
                  <h3 id="evidence-inspector-title" className="text-sm font-bold text-white tracking-wide">
                    Evidence Reference Inspector
                  </h3>
                </div>
                <p className="text-[11px] text-slate-400">
                  Read-only citation explainability &amp; source record verification
                </p>
              </div>
              <button
                ref={closeButtonRef}
                type="button"
                onClick={() => setIsEvidenceInspectorOpen(false)}
                className="p-1.5 rounded-lg text-slate-400 hover:text-white hover:bg-slate-800 transition"
                aria-label="Close evidence inspector"
                data-testid="evidence-inspector-close"
              >
                <X className="w-5 h-5" aria-hidden="true" />
              </button>
            </div>

            {/* Subheader: Active Identifier & Badges */}
            <div className="px-5 py-3.5 bg-slate-950/40 border-b border-slate-800/80 flex flex-wrap items-center justify-between gap-2 text-xs">
              <div className="flex items-center gap-1.5 font-mono text-[11px]">
                <span className="text-slate-500">Ref:</span>
                <span
                  data-testid="evidence-inspector-ref-id"
                  className="px-2 py-0.5 rounded bg-slate-800 text-indigo-300 font-semibold border border-slate-700 font-mono"
                >
                  {activeEvidenceRef}
                </span>
              </div>

              {selectedEvidence && (
                <div className="flex items-center gap-2 font-mono text-[10px]">
                  <span
                    data-testid="evidence-source-type"
                    className="px-2 py-0.5 rounded bg-blue-500/10 text-blue-300 border border-blue-500/30 font-semibold"
                  >
                    {selectedEvidence.source_type}
                  </span>
                  <span
                    data-testid="evidence-resolution-status"
                    className="px-2 py-0.5 rounded bg-emerald-500/10 text-emerald-300 border border-emerald-500/30 font-semibold"
                  >
                    {selectedEvidence.resolution_status === "RESOLVED" ? "REFERENCE VALIDATED" : selectedEvidence.resolution_status}
                  </span>
                </div>
              )}
            </div>

            {/* Drawer Body */}
            <div className="flex-1 overflow-y-auto p-5 space-y-4">
              {/* Grounding vs Fact Distinction Caveat */}
              <div
                data-testid="evidence-grounding-caveat"
                className="p-3 rounded-lg bg-indigo-500/10 border border-indigo-500/20 text-xs space-y-1 text-indigo-200/90 leading-relaxed"
              >
                <div className="flex items-center gap-1.5 font-semibold text-indigo-300">
                  <Info className="w-3.5 h-3.5 flex-shrink-0" aria-hidden="true" />
                  <span>Citation Grounding Boundary</span>
                </div>
                <p className="text-[11px] text-indigo-300/80">
                  Validating this citation confirms that the cited record exists in tenant-scoped case history.
                  It distinguishes verifiable citations from fabricated IDs, but does not autonomously certify
                  the AI model&apos;s narrative conclusions as absolute ground truth.
                </p>
              </div>

              {/* Loading State */}
              {evidenceLoading && (
                <div
                  data-testid="evidence-loading"
                  role="status"
                  aria-live="polite"
                  className="p-8 flex flex-col items-center justify-center space-y-3 text-slate-400 text-xs"
                >
                  <div className="w-6 h-6 border-2 border-indigo-400 border-t-transparent rounded-full animate-spin" aria-hidden="true" />
                  <span>Resolving citation against case records...</span>
                </div>
              )}

              {/* Error / Unavailable State */}
              {!evidenceLoading && evidenceError && (
                <div
                  data-testid="evidence-error"
                  role="alert"
                  className="p-4 rounded-xl bg-rose-500/10 border border-rose-500/30 space-y-2 text-xs"
                >
                  <div className="flex items-center gap-2 text-rose-400 font-semibold">
                    <AlertTriangle className="w-4 h-4 flex-shrink-0" aria-hidden="true" />
                    <span>Citation Unavailable or Inaccessible</span>
                  </div>
                  <p className="text-rose-200/90 text-xs leading-relaxed">
                    {evidenceError}
                  </p>
                  <p className="text-[11px] text-slate-400 leading-normal">
                    The identifier could not be validated against records belonging to this organization and case.
                    It may be malformed, belonging to another tenant or case, or an ungrounded hallucination.
                  </p>
                  {activeEvidenceRef && (
                    <button
                      type="button"
                      onClick={() => inspectEvidence(activeEvidenceRef)}
                      className="mt-2 px-3 py-1.5 rounded-lg bg-rose-500/20 hover:bg-rose-500/30 text-rose-200 text-xs font-medium border border-rose-500/40 transition"
                    >
                      Retry Verification
                    </button>
                  )}
                </div>
              )}

              {/* Resolved Content */}
              {!evidenceLoading && selectedEvidence && (
                <div className="space-y-4">
                  {/* Provenance Card */}
                  <div className="p-3 rounded-lg bg-slate-950 border border-slate-800 space-y-1.5">
                    <div className="flex items-center justify-between">
                      <span className="text-[10px] uppercase font-semibold text-slate-500 tracking-wider">
                        Data Provenance
                      </span>
                      <span
                        data-testid="evidence-provenance"
                        className={`px-2 py-0.5 rounded text-[10px] font-mono font-semibold border ${
                          selectedEvidence.is_synthetic_fixture
                            ? "bg-amber-500/10 text-amber-300 border-amber-500/30"
                            : "bg-emerald-500/10 text-emerald-300 border-emerald-500/30"
                        }`}
                      >
                        {selectedEvidence.is_synthetic_fixture
                          ? "Simulated Demo Fixture (Local Data)"
                          : "Live Authorized Record"}
                      </span>
                    </div>
                    <p className="text-[11px] text-slate-400">
                      {selectedEvidence.is_synthetic_fixture
                        ? "Guaranteed zero financial mutation: strictly isolated from live PayPal destinations."
                        : "Verified record scoped to authenticated tenant organization."}
                    </p>
                  </div>

                  {/* Source Display Label & Recorded Timestamp */}
                  <div className="p-3.5 rounded-lg bg-slate-950/80 border border-slate-800 space-y-1.5">
                    <span className="text-[10px] uppercase font-semibold text-slate-500 tracking-wider">
                      Resolved Entity
                    </span>
                    <div
                      data-testid="evidence-display-label"
                      className="text-sm font-semibold text-white"
                    >
                      {selectedEvidence.display_label}
                    </div>
                    {selectedEvidence.recorded_at && (
                      <div
                        data-testid="evidence-recorded-at"
                        className="text-xs text-slate-400 flex items-center gap-1.5 font-mono pt-1"
                      >
                        <Clock className="w-3.5 h-3.5 text-slate-500" aria-hidden="true" />
                        <span>Recorded: {formatDate(selectedEvidence.recorded_at)}</span>
                      </div>
                    )}
                  </div>

                  {/* Allowlisted Fields Table */}
                  <div className="space-y-2">
                    <span className="text-[10px] uppercase font-semibold text-slate-500 tracking-wider">
                      Allowlisted Safe Fields
                    </span>
                    <div
                      data-testid="evidence-fields"
                      className="rounded-lg bg-slate-950 border border-slate-800 divide-y divide-slate-900 overflow-hidden"
                    >
                      {selectedEvidence.fields.map((f, fIdx) => (
                        <div key={fIdx} className="px-3.5 py-2.5 flex items-start justify-between gap-3 text-xs">
                          <span className="text-slate-400 font-medium whitespace-nowrap">
                            {f.name}
                          </span>
                          <span className="text-slate-200 font-mono text-right break-all max-w-[280px]">
                            {f.value}
                          </span>
                        </div>
                      ))}
                    </div>
                  </div>
                </div>
              )}
            </div>

            {/* Footer */}
            <div className="p-4 border-t border-slate-800 bg-slate-950/70 flex items-center justify-between">
              <span className="text-[10px] text-slate-500 font-mono">
                Scope: Authenticated Tenant Only
              </span>
              <button
                type="button"
                onClick={() => setIsEvidenceInspectorOpen(false)}
                className="min-h-[38px] px-4 py-2 rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-300 text-xs font-medium transition"
                data-testid="evidence-inspector-footer-close"
              >
                Close Inspector
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
