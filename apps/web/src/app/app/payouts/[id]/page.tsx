"use client";

import React, { useEffect, useState, useCallback, useRef } from "react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import {
  Sparkles,
  ArrowRight,
  Send,
  XCircle,
  AlertTriangle,
  Brain,
  ShieldCheck,
  CheckCircle2,
} from "lucide-react";
import { useAuth } from "@/lib/auth";
import Header from "@/components/Header";
import {
  PayoutCase,
  PayoutOperation,
  PayoutInvestigation,
  EvidenceResolution,
  RecoveryOutcome,
  formatCentsToUSD,
  recoverPayoutOperation,
} from "@/lib/payouts";
import {
  Button,
  Badge,
  Card,
  CardHeader,
  CardTitle,
  CardContent,
  Alert,
  Modal,
} from "@/components/ui";
import { PayoutHeader } from "@/components/payouts/PayoutHeader";
import { PayoutSummaryCards } from "@/components/payouts/PayoutSummaryCards";
import { PayoutActionToolbar } from "@/components/payouts/PayoutActionToolbar";
import { PayoutOperationsHistory } from "@/components/payouts/PayoutOperationsHistory";
import { DualCustodyModal } from "@/components/payouts/DualCustodyModal";
import { RecoveryModal } from "@/components/payouts/RecoveryModal";
import { EvidenceInspectorDrawer } from "@/components/payouts/EvidenceInspectorDrawer";
import { PayoutInvestigationSection } from "@/components/payouts/PayoutInvestigationSection";

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

  // Investigation state
  const [investigations, setInvestigations] = useState<PayoutInvestigation[]>([]);
  const [selectedInvestigation, setSelectedInvestigation] = useState<PayoutInvestigation | null>(null);
  const [investigating, setInvestigating] = useState<boolean>(false);
  const [investigationElapsedSeconds, setInvestigationElapsedSeconds] = useState<number>(0);
  const [investigationError, setInvestigationError] = useState<string | null>(null);
  const [investigationSuccess, setInvestigationSuccess] = useState<string | null>(null);
  const investigationTimerRef = useRef<NodeJS.Timeout | null>(null);

  // Evidence Inspector state
  const [activeEvidenceRef, setActiveEvidenceRef] = useState<string | null>(null);
  const [selectedEvidence, setSelectedEvidence] = useState<EvidenceResolution | null>(null);
  const [evidenceLoading, setEvidenceLoading] = useState<boolean>(false);
  const [evidenceError, setEvidenceError] = useState<string | null>(null);
  const [isEvidenceInspectorOpen, setIsEvidenceInspectorOpen] = useState<boolean>(false);
  const lastTriggerElementRef = useRef<HTMLElement | null>(null);

  // Modals state
  const [confirmSubmitModal, setConfirmSubmitModal] = useState<boolean>(false);
  const [confirmCancelModal, setConfirmCancelModal] = useState<boolean>(false);
  const [confirmApproveModal, setConfirmApproveModal] = useState<boolean>(false);
  const [confirmRecoverModal, setConfirmRecoverModal] = useState<boolean>(false);
  const [recoveryOutcome, setRecoveryOutcome] = useState<RecoveryOutcome | null>(null);

  useEffect(() => {
    return () => {
      if (investigationTimerRef.current) {
        clearInterval(investigationTimerRef.current);
        investigationTimerRef.current = null;
      }
    };
  }, []);

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
          setSelectedInvestigation((curr) => curr || list[0]);
        }
      }
    } catch {
      // Non-blocking
    }
  }, [caseId]);

  useEffect(() => {
    if (user && caseId) {
      fetchCaseDetails();
      fetchInvestigations();
    }
  }, [user, caseId, fetchCaseDetails, fetchInvestigations]);

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

  const handleRunInvestigation = async (forceSync = false) => {
    if (investigating) return;

    setInvestigating(true);
    setInvestigationError(null);
    setInvestigationSuccess(null);
    setErrorMessage(null);
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
        if (res.status === 503 || data?.code === "AI_UNAVAILABLE") {
          setInvestigationError(
            "Investigation assistant is currently offline or unreachable. Core payout operations remain fully functional."
          );
        } else {
          setInvestigationError(
            data.error || "Investigation assistant is currently offline or unreachable."
          );
        }
      } else {
        setInvestigationSuccess("Investigation completed with grounded evidence.");
        setSelectedInvestigation(data);
        setInvestigations((prev) => [data, ...prev.filter((i) => i.id !== data.id)]);
        if (forceSync) {
          fetchCaseDetails();
        }
      }
    } catch {
      setInvestigationError("Investigation assistant is currently offline or unreachable.");
    } finally {
      if (investigationTimerRef.current) {
        clearInterval(investigationTimerRef.current);
        investigationTimerRef.current = null;
      }
      setInvestigating(false);
    }
  };

  const inspectEvidence = useCallback(
    async (refId: string, event?: React.MouseEvent | React.KeyboardEvent) => {
      if (!caseId || !refId) return;

      if (event?.currentTarget && (event.currentTarget as HTMLElement).focus) {
        lastTriggerElementRef.current = event.currentTarget as HTMLElement;
      }

      setActiveEvidenceRef(refId);
      setIsEvidenceInspectorOpen(true);
      setEvidenceLoading(true);
      setEvidenceError(null);
      setSelectedEvidence(null);

      try {
        const res = await fetch(
          `/api/v1/payout-cases/${caseId}/evidence/${encodeURIComponent(refId)}`,
          {
            method: "GET",
            credentials: "include",
          }
        );
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
        setEvidenceError("Network error resolving evidence reference");
      } finally {
        setEvidenceLoading(false);
      }
    },
    [caseId]
  );

  const handleCloseEvidenceInspector = () => {
    setIsEvidenceInspectorOpen(false);
  };

  if (loading || loadingData) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-slate-50 text-slate-500">
        <div className="flex flex-col items-center gap-3">
          <div className="w-8 h-8 border-2 border-blue-600 border-t-transparent rounded-full animate-spin" />
          <span className="text-xs font-mono">Loading case details...</span>
        </div>
      </div>
    );
  }

  if (!user) {
    return null;
  }

  if (!pCase) {
    return (
      <div className="min-h-screen bg-slate-50 text-slate-900 flex flex-col">
        <Header />
        <main className="flex-1 max-w-7xl w-full mx-auto p-6 text-center space-y-4">
          <p className="text-slate-500 text-sm">Payout case not found.</p>
          <Link href="/app/payouts">
            <Button variant="primary" size="md">
              Return to Registry
            </Button>
          </Link>
        </main>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-slate-50 text-slate-900 flex flex-col">
      <Header />

      <main
        id="main-content"
        tabIndex={-1}
        className="flex-1 max-w-7xl w-full mx-auto px-4 sm:px-6 py-8 space-y-6 outline-none"
      >
        {/* Case Header */}
        <PayoutHeader
          pCase={pCase}
          loading={loadingData}
          onRefresh={() => {
            fetchCaseDetails();
            fetchInvestigations();
          }}
        />

        {/* Feedback Banners */}
        {errorMessage && (
          <Alert variant="danger" title="Operation Notice" onDismiss={() => setErrorMessage(null)}>
            {errorMessage}
          </Alert>
        )}

        {successMessage && (
          <Alert variant="success" title="Success" onDismiss={() => setSuccessMessage(null)}>
            {successMessage}
          </Alert>
        )}

        {/* Summary Metric Cards */}
        <PayoutSummaryCards pCase={pCase} />

        {/* Action Toolbar */}
        <PayoutActionToolbar
          pCase={pCase}
          latestOp={latestOp}
          actionLoading={actionLoading}
          currentUserId={user?.id}
          onInitiate={handleInitiateOperation}
          onRequestApprove={() => setConfirmApproveModal(true)}
          onRequestSubmit={() => setConfirmSubmitModal(true)}
          onRequestCancel={() => setConfirmCancelModal(true)}
          onRequestRecover={() => setConfirmRecoverModal(true)}
          onSync={handleSyncStatus}
        />

        {/* Forensic AI Investigation Section (Integrated & E2E Test Compatible) */}
        <PayoutInvestigationSection
          pCase={pCase}
          investigations={investigations}
          selectedInvestigation={selectedInvestigation}
          onSelectInvestigation={(inv) => setSelectedInvestigation(inv)}
          investigating={investigating}
          investigationElapsedSeconds={investigationElapsedSeconds}
          onRunInvestigation={handleRunInvestigation}
          onInspectEvidence={inspectEvidence}
          activeEvidenceRef={activeEvidenceRef}
          isEvidenceInspectorOpen={isEvidenceInspectorOpen}
          investigationError={investigationError}
          investigationSuccess={investigationSuccess}
        />

        {/* Operations Audit Trail Table */}
        <PayoutOperationsHistory operations={operations} />
      </main>

      {/* Grounded Evidence Inspector Drawer */}
      <EvidenceInspectorDrawer
        isOpen={isEvidenceInspectorOpen}
        onClose={handleCloseEvidenceInspector}
        evidence={selectedEvidence}
        loading={evidenceLoading}
        error={evidenceError}
        activeEvidenceRef={activeEvidenceRef}
        onRetry={(refId) => inspectEvidence(refId)}
        triggerElement={lastTriggerElementRef.current}
      />

      {/* Dual Custody Modal */}
      <DualCustodyModal
        isOpen={confirmApproveModal}
        onClose={() => setConfirmApproveModal(false)}
        operation={latestOp}
        currentUserId={user?.id}
        loading={actionLoading === "approve"}
        onConfirmApprove={handleApproveOperation}
      />

      {/* Recovery Modal */}
      <RecoveryModal
        isOpen={confirmRecoverModal}
        onClose={() => setConfirmRecoverModal(false)}
        operation={latestOp}
        loading={actionLoading === "recover"}
        recoveryOutcome={recoveryOutcome}
        onConfirmRecover={handleRecoverOperation}
      />

      {/* Submit Confirmation Modal */}
      {latestOp && (
        <Modal
          isOpen={confirmSubmitModal}
          onClose={() => setConfirmSubmitModal(false)}
          title="Submit Payout to PayPal Sandbox"
          description="Transfers funds from active PayPal Sandbox credentials using durable sender batch idempotency."
        >
          <div className="space-y-4">
            <div className="p-4 rounded-xl bg-slate-50 border border-slate-200 space-y-2 text-xs font-mono">
              <div className="flex justify-between">
                <span className="text-slate-500">Recipient Email:</span>
                <span className="text-slate-900 font-semibold">{pCase.recipient_email}</span>
              </div>
              <div className="flex justify-between">
                <span className="text-slate-500">Amount:</span>
                <span className="text-emerald-600 font-bold">{formatCentsToUSD(latestOp.amount_cents)}</span>
              </div>
              <div className="flex justify-between">
                <span className="text-slate-500">Sender Batch ID:</span>
                <span className="text-blue-600">{latestOp.sender_batch_id}</span>
              </div>
            </div>

            <Alert variant="info" title="Pre-flight Idempotency">
              This request commits an explicit sender batch ID before making the REST call to PayPal. In the event of network disconnection, the payout can be recovered idempotently without double-spending.
            </Alert>

            <div className="pt-3 flex items-center justify-end gap-3 border-t border-slate-100">
              <Button variant="secondary" size="md" onClick={() => setConfirmSubmitModal(false)} disabled={actionLoading === "submit"}>
                Cancel
              </Button>
              <Button
                variant="primary"
                size="md"
                onClick={handleSubmitOperation}
                loading={actionLoading === "submit"}
                disabled={actionLoading === "submit"}
              >
                <Send className="w-4 h-4" aria-hidden="true" />
                <span>Confirm Sandbox Submission</span>
              </Button>
            </div>
          </div>
        </Modal>
      )}

      {/* Cancel Confirmation Modal */}
      {latestOp && (
        <Modal
          isOpen={confirmCancelModal}
          onClose={() => setConfirmCancelModal(false)}
          title="Cancel Payout Operation"
          description="Safely revoke this prepared or unclaimed payout operation."
        >
          <div className="space-y-4">
            <p className="text-xs text-slate-600 leading-relaxed">
              Are you sure you want to cancel operation <span className="font-mono text-blue-600">{latestOp.id}</span>? Once cancelled, this operation cannot be re-opened.
            </p>

            <div className="pt-3 flex items-center justify-end gap-3 border-t border-slate-100">
              <Button variant="secondary" size="md" onClick={() => setConfirmCancelModal(false)} disabled={actionLoading === "cancel"}>
                Go Back
              </Button>
              <Button
                variant="destructive"
                size="md"
                onClick={handleCancelOperation}
                loading={actionLoading === "cancel"}
                disabled={actionLoading === "cancel"}
              >
                <XCircle className="w-4 h-4" aria-hidden="true" />
                <span>Confirm Cancellation</span>
              </Button>
            </div>
          </div>
        </Modal>
      )}
    </div>
  );
}
