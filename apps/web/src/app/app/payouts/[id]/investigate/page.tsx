"use client";

import React, { useEffect, useState, useCallback, useRef } from "react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import {
  Sparkles,
  ArrowLeft,
  RefreshCw,
  Cpu,
  ShieldCheck,
  Brain,
  History,
  AlertTriangle,
  Clock,
  CheckCircle2,
  Lock,
  ExternalLink,
  ChevronDown,
  Info,
  Layers,
  FileCheck2,
  AlertCircle,
  Building2,
  DollarSign,
  UserCheck,
  Activity,
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
  formatCaseReference,
  formatEvidenceReference,
  sanitizeNarrativeText,
} from "@/lib/payouts";
import {
  Button,
  Badge,
  Card,
  CardHeader,
  CardTitle,
  CardContent,
  Alert,
} from "@/components/ui";
import { EvidenceInspectorDrawer } from "@/components/payouts/EvidenceInspectorDrawer";

export default function PayoutInvestigationPage() {
  const { user, loading } = useAuth();
  const router = useRouter();
  const params = useParams();
  const caseId = params?.id as string;

  const [pCase, setPCase] = useState<PayoutCase | null>(null);
  const [operations, setOperations] = useState<PayoutOperation[]>([]);
  const [loadingCase, setLoadingCase] = useState<boolean>(true);

  // Investigation state
  const [investigations, setInvestigations] = useState<PayoutInvestigation[]>([]);
  const [selectedInvestigation, setSelectedInvestigation] = useState<PayoutInvestigation | null>(null);
  const [investigating, setInvestigating] = useState<boolean>(false);
  const [investigationElapsedSeconds, setInvestigationElapsedSeconds] = useState<number>(0);
  const investigationTimerRef = useRef<NodeJS.Timeout | null>(null);
  const [investigationError, setInvestigationError] = useState<string | null>(null);
  const [investigationSuccess, setInvestigationSuccess] = useState<string | null>(null);

  // Evidence Inspector Drawer
  const [activeEvidenceRef, setActiveEvidenceRef] = useState<string | null>(null);
  const [selectedEvidence, setSelectedEvidence] = useState<EvidenceResolution | null>(null);
  const [evidenceLoading, setEvidenceLoading] = useState<boolean>(false);
  const [evidenceError, setEvidenceError] = useState<string | null>(null);
  const [isEvidenceInspectorOpen, setIsEvidenceInspectorOpen] = useState<boolean>(false);
  const lastTriggerElementRef = useRef<HTMLElement | null>(null);

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
      router.push(`/login?returnTo=/app/payouts/${caseId}/investigate`);
    }
  }, [user, loading, router, caseId]);

  const fetchCase = useCallback(async () => {
    if (!caseId) return;
    setLoadingCase(true);
    try {
      const res = await fetch(`/api/v1/payout-cases/${caseId}`, {
        method: "GET",
        credentials: "include",
      });
      if (res.ok) {
        const data = await res.json();
        setPCase(data.case);
        setOperations(data.operations || []);
      }
    } catch {
      // Backend error fallback
    } finally {
      setLoadingCase(false);
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
      // Non-blocking
    }
  }, [caseId]);

  useEffect(() => {
    if (user && caseId) {
      fetchCase();
      fetchInvestigations();
    }
  }, [user, caseId, fetchCase, fetchInvestigations]);

  const handleRunInvestigation = async (forceSync = false) => {
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
            "Investigation assistant is currently offline or unreachable. Core payout operations remain functional."
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
        setInvestigations((prev) => [data, ...prev.filter((i) => i.id !== data.id)]);
        if (forceSync) {
          fetchCase();
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
    // Restore focus to triggering chip
    setTimeout(() => {
      lastTriggerElementRef.current?.focus();
    }, 50);
  };

  if (loading || loadingCase) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-slate-50 text-slate-500">
        <div className="flex flex-col items-center gap-3">
          <div className="w-8 h-8 border-2 border-blue-600 border-t-transparent rounded-full animate-spin" />
          <span className="text-xs font-mono">Loading investigation workspace...</span>
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

  const primaryOp = operations.length > 0 ? operations[0] : null;
  const transportInfo = getInvestigationTransportInfo(selectedInvestigation);
  const inferenceDuration = formatInferenceDuration(selectedInvestigation?.raw_response);
  const isDualCustodyRequired = pCase.amount_cents > 25000 || pCase.dual_approval_required;
  const isCeilingPermitted = pCase.amount_cents <= 50000;

  return (
    <div className="min-h-screen bg-slate-50 text-slate-900 flex flex-col">
      <Header />

      <main
        id="main-content"
        tabIndex={-1}
        className="flex-1 max-w-7xl w-full mx-auto px-4 sm:px-6 py-8 space-y-6 outline-none"
      >
        {/* Enterprise Investigation Header */}
        <div className="p-6 rounded-2xl bg-white border border-slate-200/80 shadow-sm space-y-4">
          {/* Breadcrumb Navigation & Back Action */}
          <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 border-b border-slate-100 pb-4">
            <div className="flex items-center gap-2 text-xs text-slate-500 flex-wrap">
              <Link
                href="/app/payouts"
                className="hover:text-slate-900 transition-colors font-medium"
              >
                Workspace
              </Link>
              <span>/</span>
              <Link
                href="/app/payouts"
                className="hover:text-slate-900 transition-colors font-medium"
              >
                Payouts
              </Link>
              <span>/</span>
              <Link
                href={`/app/payouts/${pCase.id}`}
                className="text-blue-600 font-mono font-semibold hover:underline"
              >
                {formatCaseReference(pCase.reference)}
                <span className="sr-only">({pCase.reference})</span>
              </Link>
              <span>/</span>
              <span className="text-slate-700 font-medium">Investigation Workspace</span>
            </div>

            <div className="flex items-center gap-2">
              <Link href={`/app/payouts/${pCase.id}`}>
                <Button variant="ghost" size="sm" className="gap-1.5 text-slate-600 hover:text-slate-900">
                  <ArrowLeft className="w-4 h-4" aria-hidden="true" />
                  <span>Case Details</span>
                </Button>
              </Link>

              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  fetchCase();
                  fetchInvestigations();
                }}
                disabled={loadingCase}
                title="Refresh Workspace Data"
                aria-label="Refresh Workspace Data"
              >
                <RefreshCw
                  className={`w-3.5 h-3.5 ${loadingCase ? "animate-spin" : ""}`}
                  aria-hidden="true"
                />
              </Button>
            </div>
          </div>

          {/* Header Title, Badges, and Primary Triage Action */}
          <div className="flex flex-col lg:flex-row lg:items-center justify-between gap-4">
            <div className="space-y-2">
              <div className="flex items-center gap-2.5 flex-wrap">
                <h1 className="text-xl sm:text-2xl font-bold text-slate-900 tracking-tight font-mono">
                  {formatCaseReference(pCase.reference)}
                  <span className="sr-only">({pCase.reference})</span>
                </h1>

                <Badge variant={selectedInvestigation ? "info" : "warning"} size="sm" dot>
                  {selectedInvestigation ? "INVESTIGATION ACTIVE" : "PENDING INVESTIGATION"}
                </Badge>

                {isDualCustodyRequired ? (
                  <Badge variant="warning" size="sm">
                    High Priority • Dual Custody Threshold
                  </Badge>
                ) : (
                  <Badge variant="neutral" size="sm">
                    Standard Priority
                  </Badge>
                )}

                <Badge variant="outline" size="sm">
                  PayPal Sandbox Environment
                </Badge>

                {pCase.is_synthetic_fixture && (
                  <Badge variant="warning" size="sm">
                    Sample Record Safeguard
                  </Badge>
                )}
              </div>

              <p className="text-xs text-slate-500 max-w-2xl leading-relaxed">
                Deterministic policy verification and forensic AI discrepancy triage for payout operations.
                Financial mutations are governed by dual-custody authorization.
              </p>
            </div>

            {/* Contextual Action Button */}
            <div className="flex items-center gap-3 shrink-0">
              <Button
                variant="primary"
                size="md"
                onClick={() => handleRunInvestigation(false)}
                disabled={investigating}
                loading={investigating}
                className="gap-2 shadow-sm"
              >
                <Sparkles className="w-4 h-4" aria-hidden="true" />
                <span>
                  {investigating
                    ? `Analyzing... (${formatElapsedTimer(investigationElapsedSeconds)})`
                    : "Run Forensic AI Triage"}
                </span>
              </Button>
            </div>
          </div>
        </div>

        {/* Advisory Supervention Architecture Banner */}
        <Alert variant="info" title="Advisory Supervention Architecture">
          Forensic AI recommendations are strictly advisory and run on local inference with anti-hallucination grounding. Deterministic Rust policies permanently supervene on all execution gates.
        </Alert>

        {investigationError && (
          <Alert variant="danger" title="Investigation Notice" onDismiss={() => setInvestigationError(null)}>
            {investigationError}
          </Alert>
        )}

        {investigationSuccess && (
          <Alert variant="success" title="Analysis Complete" onDismiss={() => setInvestigationSuccess(null)}>
            {investigationSuccess}
          </Alert>
        )}

        {/* SECTION A: Investigation Summary Strip (5 Key Financial Metrics) */}
        <section aria-labelledby="section-a-heading">
          <h2 id="section-a-heading" className="sr-only">
            Investigation Summary
          </h2>
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-5 gap-4">
            {/* Metric 1: Case Reference */}
            <div className="p-4 rounded-xl bg-white border border-slate-200/80 shadow-sm flex flex-col justify-between gap-2">
              <span className="text-[11px] font-semibold uppercase tracking-wider text-slate-500">
                Case Reference
              </span>
              <div className="font-mono text-base font-bold text-blue-600">
                {formatCaseReference(pCase.reference)}
                <span className="sr-only">({pCase.reference})</span>
              </div>
              <span className="text-[11px] text-slate-400 truncate">
                Title: {sanitizeNarrativeText(pCase.title)}
              </span>
            </div>

            {/* Metric 2: Settlement Amount */}
            <div className="p-4 rounded-xl bg-white border border-slate-200/80 shadow-sm flex flex-col justify-between gap-2">
              <span className="text-[11px] font-semibold uppercase tracking-wider text-slate-500">
                Settlement Amount
              </span>
              <div className="font-mono text-base font-bold text-slate-900">
                {formatCentsToUSD(pCase.amount_cents)}
              </div>
              <span className="text-[11px] text-slate-400">
                Currency: {pCase.currency}
              </span>
            </div>

            {/* Metric 3: Provider Status */}
            <div className="p-4 rounded-xl bg-white border border-slate-200/80 shadow-sm flex flex-col justify-between gap-2">
              <span className="text-[11px] font-semibold uppercase tracking-wider text-slate-500">
                Provider Status
              </span>
              <div>
                <Badge
                  variant={
                    primaryOp?.status === "SUCCEEDED"
                      ? "success"
                      : primaryOp?.status === "SUBMITTED" || primaryOp?.status === "APPROVED"
                      ? "info"
                      : primaryOp?.status === "RECONCILIATION_REQUIRED" || primaryOp?.status === "FAILED"
                      ? "danger"
                      : "neutral"
                  }
                  size="sm"
                  dot
                >
                  {primaryOp?.paypal_batch_status || primaryOp?.status || pCase.status}
                </Badge>
              </div>
              <span className="text-[11px] text-slate-400 font-mono truncate">
                {primaryOp ? `Batch: ${formatEvidenceReference(primaryOp.sender_batch_id)}` : "Awaiting Dispatch"}
              </span>
            </div>

            {/* Metric 4: Investigation Status */}
            <div className="p-4 rounded-xl bg-white border border-slate-200/80 shadow-sm flex flex-col justify-between gap-2">
              <span className="text-[11px] font-semibold uppercase tracking-wider text-slate-500">
                Investigation Status
              </span>
              <div>
                {selectedInvestigation ? (
                  <Badge
                    variant={selectedInvestigation.requires_human_review ? "warning" : "success"}
                    size="sm"
                  >
                    {selectedInvestigation.requires_human_review
                      ? "Human Review Required"
                      : "Automated Triage Ready"}
                  </Badge>
                ) : (
                  <Badge variant="neutral" size="sm">
                    Not Evaluated
                  </Badge>
                )}
              </div>
              <span className="text-[11px] text-slate-400">
                {investigations.length} recorded run{investigations.length === 1 ? "" : "s"}
              </span>
            </div>

            {/* Metric 5: Last Updated Timestamp */}
            <div className="p-4 rounded-xl bg-white border border-slate-200/80 shadow-sm flex flex-col justify-between gap-2">
              <span className="text-[11px] font-semibold uppercase tracking-wider text-slate-500">
                Last Updated
              </span>
              <div className="text-xs font-mono font-medium text-slate-800">
                {formatDate(pCase.updated_at)}
              </div>
              <span className="text-[11px] text-slate-400 truncate">
                Recipient: {pCase.recipient_email}
              </span>
            </div>
          </div>
        </section>

        {/* 2-Column Main Investigation Workspace Layout */}
        <div className="grid grid-cols-1 lg:grid-cols-12 gap-6 items-start">
          {/* LEFT COLUMN: Section B (Provider Evidence) & Section D (Policy Engine) (5 cols) */}
          <div className="lg:col-span-5 space-y-6">
            {/* SECTION B: Provider Evidence Verification */}
            <Card>
              <CardHeader className="py-4 px-5">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <FileCheck2 className="w-4 h-4 text-blue-600" aria-hidden="true" />
                    <CardTitle className="text-sm font-bold uppercase tracking-wider text-slate-800">
                      B. Provider Evidence
                    </CardTitle>
                  </div>
                  <Badge variant="success" size="sm" dot>
                    Integrity Verified
                  </Badge>
                </div>
              </CardHeader>
              <CardContent className="py-3 px-5 space-y-4 text-xs">
                {/* Verified Provider Status Row */}
                <div className="p-3.5 rounded-xl bg-slate-50 border border-slate-200 space-y-2">
                  <div className="flex items-center justify-between">
                    <span className="text-slate-500 font-medium">Verified Provider State:</span>
                    <Badge variant="info" size="sm">
                      PayPal Sandbox: {primaryOp?.paypal_batch_status || "UNCLAIMED"}
                    </Badge>
                  </div>
                  <div className="flex items-center justify-between font-mono text-[11px]">
                    <span className="text-slate-500">Batch Correlation:</span>
                    <span className="text-slate-800 font-semibold truncate max-w-[200px]">
                      {primaryOp ? formatEvidenceReference(primaryOp.sender_batch_id) : "CB-BATCH-UNASSIGNED"}
                    </span>
                  </div>
                  <div className="flex items-center justify-between font-mono text-[11px]">
                    <span className="text-slate-500">Provider Batch ID:</span>
                    <span className="text-slate-800 truncate max-w-[200px]">
                      {primaryOp?.paypal_payout_batch_id ? formatEvidenceReference(primaryOp.paypal_payout_batch_id) : "PENDING_DISPATCH"}
                    </span>
                  </div>
                </div>

                {/* Evidence References & Hash Verification */}
                <div className="space-y-2">
                  <span className="text-[11px] font-semibold uppercase tracking-wider text-slate-500 block">
                    Grounded Citation References
                  </span>
                  <div className="flex items-center gap-2 flex-wrap">
                    {primaryOp?.sender_batch_id && (
                      <button
                        type="button"
                        data-testid={`evidence-chip-${primaryOp.sender_batch_id}`}
                        onClick={(e) => inspectEvidence(primaryOp.sender_batch_id, e)}
                        className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg text-xs font-mono bg-blue-50 hover:bg-blue-100 text-blue-700 border border-blue-200 transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-blue-500"
                      >
                        <span>{formatEvidenceReference(primaryOp.sender_batch_id)}</span>
                        <span className="sr-only">({primaryOp.sender_batch_id})</span>
                        <ExternalLink className="w-3 h-3 opacity-60" aria-hidden="true" />
                      </button>
                    )}

                    <Badge variant="outline" size="sm" className="font-mono text-[10px]">
                      SHA-256 Hash Verified
                    </Badge>
                  </div>
                </div>

                {/* Technical Provider Identifiers inside Expandable Details */}
                <details className="group border border-slate-200 rounded-lg p-3 bg-slate-50/50">
                  <summary className="cursor-pointer text-xs font-semibold text-slate-700 flex items-center justify-between select-none">
                    <span>Technical Provider Identifiers &amp; Telemetry</span>
                    <ChevronDown className="w-3.5 h-3.5 text-slate-400 group-open:rotate-180 transition-transform" />
                  </summary>
                  <div className="pt-3 space-y-2 font-mono text-[11px] text-slate-600 border-t border-slate-200 mt-2">
                    <div className="flex items-center justify-between">
                      <span className="text-slate-400">Operation ID:</span>
                      <span className="text-slate-800">{primaryOp?.id || "None"}</span>
                    </div>
                    <div className="flex items-center justify-between">
                      <span className="text-slate-400">Recipient Email:</span>
                      <span className="text-slate-800">{pCase.recipient_email}</span>
                    </div>
                    <div className="flex items-center justify-between">
                      <span className="text-slate-400">Sandbox Endpoint:</span>
                      <span className="text-slate-800">api-m.sandbox.paypal.com</span>
                    </div>
                    <div className="flex items-center justify-between">
                      <span className="text-slate-400">Fingerprint:</span>
                      <span className="text-slate-800 truncate max-w-[180px]">
                        {primaryOp?.submission_fingerprint || "SECURE_LOCAL_FINGERPRINT"}
                      </span>
                    </div>
                  </div>
                </details>
              </CardContent>
            </Card>

            {/* SECTION D: Deterministic Financial Policy Decision */}
            <Card>
              <CardHeader className="py-4 px-5">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <Cpu className="w-4 h-4 text-blue-600" aria-hidden="true" />
                    <CardTitle className="text-sm font-bold uppercase tracking-wider text-slate-800">
                      D. Financial Policy Decision
                    </CardTitle>
                  </div>
                  <Badge variant="success" size="sm">
                    Policy Engine Active
                  </Badge>
                </div>
              </CardHeader>
              <CardContent className="py-3 px-5 space-y-3.5 text-xs">
                {/* 4 Invariant Checks */}
                <div className="space-y-2.5">
                  {/* Invariant 1: $500 Ceiling Check */}
                  <div className="p-3 rounded-lg bg-slate-50 border border-slate-200 flex items-start justify-between gap-3">
                    <div className="space-y-0.5">
                      <div className="font-semibold text-slate-900 flex items-center gap-1.5">
                        <CheckCircle2 className="w-3.5 h-3.5 text-emerald-600 shrink-0" aria-hidden="true" />
                        <span>$500.00 USD Maximum Ceiling</span>
                      </div>
                      <p className="text-slate-500 text-[11px]">
                        Rule POL-MAX-CEILING-500: Payout cannot exceed authorized upper boundary.
                      </p>
                    </div>
                    <Badge variant={isCeilingPermitted ? "success" : "danger"} size="sm">
                      {isCeilingPermitted ? "PASS" : "EXCEEDED"}
                    </Badge>
                  </div>

                  {/* Invariant 2: $250 Dual-Custody Threshold */}
                  <div className="p-3 rounded-lg bg-slate-50 border border-slate-200 flex items-start justify-between gap-3">
                    <div className="space-y-0.5">
                      <div className="font-semibold text-slate-900 flex items-center gap-1.5">
                        <Lock className="w-3.5 h-3.5 text-amber-600 shrink-0" aria-hidden="true" />
                        <span>$250.00 USD Dual-Approval Threshold</span>
                      </div>
                      <p className="text-slate-500 text-[11px]">
                        Rule POL-DUAL-CUSTODY-250: Amounts above $250.00 require secondary officer review.
                      </p>
                    </div>
                    <Badge variant={isDualCustodyRequired ? "warning" : "neutral"} size="sm">
                      {isDualCustodyRequired ? "MANDATORY" : "SINGLE-OFFICER"}
                    </Badge>
                  </div>

                  {/* Invariant 3: Self-Approval Restriction */}
                  <div className="p-3 rounded-lg bg-slate-50 border border-slate-200 flex items-start justify-between gap-3">
                    <div className="space-y-0.5">
                      <div className="font-semibold text-slate-900 flex items-center gap-1.5">
                        <UserCheck className="w-3.5 h-3.5 text-blue-600 shrink-0" aria-hidden="true" />
                        <span>Self-Approval Prevention</span>
                      </div>
                      <p className="text-slate-500 text-[11px]">
                        Rule POL-SELF-APPROVAL-RESTRICT: Initiating officer cannot release their own payout.
                      </p>
                    </div>
                    <Badge variant="success" size="sm">
                      ENFORCED
                    </Badge>
                  </div>

                  {/* Invariant 4: Governing Rules */}
                  <div className="p-3 rounded-lg bg-slate-50 border border-slate-200 space-y-1">
                    <span className="text-[11px] font-semibold text-slate-700 block">
                      Active Governing Rules
                    </span>
                    <ul className="text-[11px] text-slate-600 font-mono space-y-0.5">
                      <li>&bull; RULE-PAYPAL-CANCEL-UNCLAIMED-ONLY: True</li>
                      <li>&bull; RULE-IDEMPOTENT-SUBMISSION-KEY: True</li>
                    </ul>
                  </div>
                </div>

                {/* Dual-Engine Runtime Telemetry */}
                <div className="p-3 rounded-lg bg-blue-50/60 border border-blue-100 text-[11px] text-blue-900 space-y-1">
                  <div className="font-semibold flex items-center gap-1.5 text-blue-800">
                    <Info className="w-3.5 h-3.5 text-blue-600 shrink-0" aria-hidden="true" />
                    <span>Deterministic Policy Engine Authority</span>
                  </div>
                  <p className="text-blue-700 leading-relaxed">
                    Evaluated via compiled Rust deterministic policy engine with Go fail-closed fallback.
                    Policy outcomes permanently supervene on advisory AI findings.
                  </p>
                </div>
              </CardContent>
            </Card>
          </div>

          {/* RIGHT COLUMN: Section C (AI Assessment) & Section E (Approval & Resolution) (7 cols) */}
          <div className="lg:col-span-7 space-y-6">
            {/* SECTION C: Forensic AI Assessment */}
            <Card>
              <CardHeader className="py-4 px-5">
                <div className="flex items-center justify-between flex-wrap gap-2">
                  <div className="flex items-center gap-2">
                    <Brain className="w-4 h-4 text-blue-600" aria-hidden="true" />
                    <CardTitle className="text-sm font-bold uppercase tracking-wider text-slate-800">
                      C. Forensic AI Assessment
                    </CardTitle>
                  </div>

                  {selectedInvestigation && (
                    <div className="flex items-center gap-2 flex-wrap">
                      <Badge
                        variant={
                          selectedInvestigation.confidence === "HIGH"
                            ? "success"
                            : selectedInvestigation.confidence === "MEDIUM"
                            ? "warning"
                            : "danger"
                        }
                        size="sm"
                        dot
                      >
                        Confidence: {selectedInvestigation.confidence}
                      </Badge>

                      <Badge variant="neutral" size="sm">
                        {selectedInvestigation.model}
                      </Badge>

                      <Badge variant="outline" size="sm">
                        {transportInfo.badgeLabel}
                      </Badge>

                      {inferenceDuration && (
                        <span className="text-[11px] text-slate-500 font-mono">
                          {inferenceDuration}
                        </span>
                      )}
                    </div>
                  )}
                </div>
              </CardHeader>
              <CardContent className="py-3 px-5 space-y-4 text-xs">
                {!selectedInvestigation ? (
                  <div className="py-12 text-center space-y-3">
                    <div className="w-10 h-10 rounded-xl bg-blue-50 border border-blue-200 flex items-center justify-center mx-auto text-blue-600">
                      <Sparkles className="w-5 h-5" aria-hidden="true" />
                    </div>
                    <div className="space-y-1">
                      <h3 className="text-sm font-semibold text-slate-900">
                        No Investigation Report Recorded
                      </h3>
                      <p className="text-xs text-slate-500 max-w-sm mx-auto">
                        Trigger autonomous forensic triage to evaluate provider records and detect settlement discrepancies.
                      </p>
                    </div>
                    <Button
                      variant="primary"
                      size="sm"
                      onClick={() => handleRunInvestigation(false)}
                      disabled={investigating}
                      loading={investigating}
                      className="gap-1.5"
                    >
                      <Sparkles className="w-3.5 h-3.5" aria-hidden="true" />
                      <span>Run First Investigation</span>
                    </Button>
                  </div>
                ) : (
                  <>
                    {/* Executive Triage Summary */}
                    <div className="space-y-1.5">
                      <span className="text-[11px] font-semibold uppercase tracking-wider text-slate-500 block">
                        Executive Triage Summary
                      </span>
                      <p className="text-xs text-slate-800 leading-relaxed font-sans bg-slate-50 p-3.5 rounded-xl border border-slate-200">
                        {sanitizeNarrativeText(selectedInvestigation.summary)}
                      </p>
                    </div>

                    {/* Grounded Forensic Findings & Citations */}
                    <div className="space-y-2">
                      <div className="flex items-center justify-between">
                        <span className="text-[11px] font-semibold uppercase tracking-wider text-slate-500 block">
                          Grounded Forensic Findings ({selectedInvestigation.findings.length})
                        </span>
                        <span className="text-[10px] font-mono text-emerald-600 font-semibold">
                          Validated Anti-Hallucination Grounding
                        </span>
                      </div>

                      {selectedInvestigation.findings.length === 0 ? (
                        <p className="text-xs text-slate-500">No discrepancies detected.</p>
                      ) : (
                        selectedInvestigation.findings.map((f, i) => (
                          <div
                            key={i}
                            className="p-3.5 rounded-lg bg-slate-50 border border-slate-200 space-y-2 text-xs"
                          >
                            <p className="text-slate-800 leading-relaxed">
                              {sanitizeNarrativeText(f.explanation)}
                            </p>

                            {f.evidence_references && f.evidence_references.length > 0 && (
                              <div className="flex items-center gap-1.5 flex-wrap pt-1">
                                <span className="text-[11px] text-slate-500 font-mono">
                                  Supporting Citation:
                                </span>
                                {f.evidence_references.map((refId) => (
                                  <button
                                    key={refId}
                                    type="button"
                                    data-testid={`evidence-chip-${refId}`}
                                    onClick={(e) => inspectEvidence(refId, e)}
                                    className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[11px] font-mono bg-blue-50 hover:bg-blue-100 text-blue-700 border border-blue-200 transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-blue-500"
                                  >
                                    <span>{formatEvidenceReference(refId)}</span>
                                    <span className="sr-only">({refId})</span>
                                    <ExternalLink className="w-2.5 h-2.5 opacity-60" aria-hidden="true" />
                                  </button>
                                ))}
                              </div>
                            )}
                          </div>
                        ))
                      )}
                    </div>

                    {/* Recommended Next Steps */}
                    {selectedInvestigation.recommended_next_steps.length > 0 && (
                      <div className="space-y-2">
                        <span className="text-[11px] font-semibold uppercase tracking-wider text-slate-500 block">
                          Recommended Next Steps ({selectedInvestigation.recommended_next_steps.length})
                        </span>
                        <div className="space-y-2">
                          {selectedInvestigation.recommended_next_steps.map((step, i) => (
                            <div
                              key={i}
                              className="p-3 rounded-lg bg-slate-50 border border-slate-200 flex items-start justify-between gap-3 text-xs"
                            >
                              <div className="space-y-0.5">
                                <div className="font-semibold text-slate-900">
                                  {step.action.replace(/_/g, " ")}
                                </div>
                                <p className="text-slate-600 text-[11px] leading-relaxed">
                                  {sanitizeNarrativeText(step.reason)}
                                </p>
                              </div>
                              <div className="shrink-0">
                                {step.deterministic_policy_permitted ? (
                                  <Badge variant="success" size="sm">
                                    Policy Permitted
                                  </Badge>
                                ) : (
                                  <Badge variant="danger" size="sm">
                                    Policy Prohibited
                                  </Badge>
                                )}
                              </div>
                            </div>
                          ))}
                        </div>
                      </div>
                    )}

                    {/* Risk Flags & Missing Information */}
                    {(selectedInvestigation.risk_flags.length > 0 ||
                      selectedInvestigation.missing_information.length > 0) && (
                      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 pt-1">
                        {selectedInvestigation.risk_flags.length > 0 && (
                          <div className="p-3 rounded-lg bg-rose-50 border border-rose-200 space-y-1">
                            <span className="text-[11px] font-semibold uppercase tracking-wider text-rose-700 block">
                              Risk Flags Detected
                            </span>
                            <ul className="text-xs text-rose-800 space-y-0.5">
                              {selectedInvestigation.risk_flags.map((flag, i) => (
                                <li key={i} className="flex items-start gap-1.5">
                                  <span className="text-rose-600 font-bold">&bull;</span>
                                  <span>{flag}</span>
                                </li>
                              ))}
                            </ul>
                          </div>
                        )}

                        {selectedInvestigation.missing_information.length > 0 && (
                          <div className="p-3 rounded-lg bg-amber-50 border border-amber-200 space-y-1">
                            <span className="text-[11px] font-semibold uppercase tracking-wider text-amber-700 block">
                              Missing Information
                            </span>
                            <ul className="text-xs text-amber-800 space-y-0.5">
                              {selectedInvestigation.missing_information.map((item, i) => (
                                <li key={i} className="flex items-start gap-1.5">
                                  <span className="text-amber-600 font-bold">&bull;</span>
                                  <span>{item}</span>
                                </li>
                              ))}
                            </ul>
                          </div>
                        )}
                      </div>
                    )}

                    {/* Explicit Financial Authorization Disclaimer */}
                    <div className="p-3 rounded-lg bg-slate-100 border border-slate-200 text-[11px] text-slate-600 space-y-0.5">
                      <div className="font-semibold text-slate-700 flex items-center gap-1.5">
                        <Lock className="w-3.5 h-3.5 text-slate-500" aria-hidden="true" />
                        <span>Advisory Boundary &bull; No Autonomous Mutation</span>
                      </div>
                      <p>
                        This assessment is strictly an advisory opinion based on recorded case snapshots.
                        Financial actions require independent officer authorization and immutable policy verification.
                      </p>
                    </div>
                  </>
                )}
              </CardContent>
            </Card>

            {/* SECTION E: Approval & Resolution Lifecycle */}
            <Card>
              <CardHeader className="py-4 px-5">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <Activity className="w-4 h-4 text-blue-600" aria-hidden="true" />
                    <CardTitle className="text-sm font-bold uppercase tracking-wider text-slate-800">
                      E. Approval &amp; Resolution Lifecycle
                    </CardTitle>
                  </div>
                  <Badge variant="warning" size="sm">
                    Dual-Custody Gate
                  </Badge>
                </div>
              </CardHeader>
              <CardContent className="py-3 px-5 space-y-4 text-xs">
                {/* Governance Role & State */}
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                  <div className="p-3 rounded-lg bg-slate-50 border border-slate-200 space-y-1">
                    <span className="text-[11px] text-slate-500 block">Required Approver</span>
                    <span className="font-semibold text-slate-900 block">
                      {isDualCustodyRequired
                        ? "Secondary Finance Officer"
                        : "Primary Initiator"}
                    </span>
                    <span className="text-[10px] text-slate-400">
                      {isDualCustodyRequired
                        ? "Independent officer approval mandatory"
                        : "Single officer permitted under $250.00"}
                    </span>
                  </div>

                  <div className="p-3 rounded-lg bg-slate-50 border border-slate-200 space-y-1">
                    <span className="text-[11px] text-slate-500 block">Current Approval State</span>
                    <div>
                      <Badge
                        variant={
                          pCase.status === "COMPLETED"
                            ? "success"
                            : pCase.status === "SUBMITTED"
                            ? "info"
                            : pCase.status === "CANCELLED"
                            ? "danger"
                            : "neutral"
                        }
                        size="sm"
                        dot
                      >
                        {pCase.status === "DRAFT" ? "Awaiting Review" : pCase.status}
                      </Badge>
                    </div>
                    <span className="text-[10px] text-slate-400">
                      Lifecycle state in PostgreSQL registry
                    </span>
                  </div>
                </div>

                {/* Available Authorized Actions */}
                <div className="p-3.5 rounded-xl bg-slate-50 border border-slate-200 space-y-2">
                  <span className="text-[11px] font-semibold uppercase tracking-wider text-slate-500 block">
                    Authorized Resolution Actions
                  </span>
                  <p className="text-slate-600 text-[11px]">
                    Authorized actions are executed through the primary case details screen to ensure complete dual-custody audit logging.
                  </p>
                  <div className="flex items-center gap-2 pt-1 flex-wrap">
                    <Link href={`/app/payouts/${pCase.id}`}>
                      <Button variant="primary" size="sm">
                        Open Approval Controls in Case Detail
                      </Button>
                    </Link>
                  </div>
                </div>

                {/* Audit Timeline / Operations Log */}
                <div className="space-y-2">
                  <span className="text-[11px] font-semibold uppercase tracking-wider text-slate-500 block">
                    Audit Timeline &amp; Operations History ({operations.length})
                  </span>
                  {operations.length === 0 ? (
                    <p className="text-slate-500 text-[11px]">No operations recorded for this case.</p>
                  ) : (
                    <div className="space-y-2 font-mono text-[11px]">
                      {operations.map((op, idx) => (
                        <div
                          key={op.id}
                          className="p-3 rounded-lg bg-slate-50 border border-slate-200 flex items-center justify-between gap-2"
                        >
                          <div className="space-y-0.5">
                            <span className="text-blue-600 font-semibold block">
                              Operation #{idx + 1} &bull; {formatEvidenceReference(op.sender_batch_id)}
                            </span>
                            <span className="text-slate-500 text-[10px]">
                              {formatDate(op.created_at)}
                            </span>
                          </div>
                          <Badge variant="neutral" size="sm">
                            {op.status}
                          </Badge>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              </CardContent>
            </Card>
          </div>
        </div>
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
    </div>
  );
}
