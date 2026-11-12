"use client";

import React, { useEffect, useState, useCallback, useRef } from "react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import {
  Sparkles,
  ArrowLeft,
  RefreshCw,
  ShieldCheck,
  Bot,
  Brain,
  History,
  FileText,
  AlertTriangle,
  Clock,
  CheckCircle2,
  Lock,
  ExternalLink,
  ChevronDown,
  Info,
  Building2,
  ArrowRight,
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

  const [investigations, setInvestigations] = useState<PayoutInvestigation[]>([]);
  const [selectedInvestigation, setSelectedInvestigation] = useState<PayoutInvestigation | null>(null);
  const [investigating, setInvestigating] = useState<boolean>(false);
  const [investigationElapsedSeconds, setInvestigationElapsedSeconds] = useState<number>(0);
  const investigationTimerRef = useRef<NodeJS.Timeout | null>(null);
  const [investigationError, setInvestigationError] = useState<string | null>(null);
  const [investigationSuccess, setInvestigationSuccess] = useState<string | null>(null);

  const [activeEvidenceRef, setActiveEvidenceRef] = useState<string | null>(null);
  const [selectedEvidence, setSelectedEvidence] = useState<EvidenceResolution | null>(null);
  const [evidenceLoading, setEvidenceLoading] = useState<boolean>(false);
  const [evidenceError, setEvidenceError] = useState<string | null>(null);
  const [isEvidenceInspectorOpen, setIsEvidenceInspectorOpen] = useState<boolean>(false);
  const lastTriggerElementRef = useRef<HTMLElement | null>(null);

  const [technicalDetailsOpen, setTechnicalDetailsOpen] = useState<boolean>(false);

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
    setTimeout(() => {
      lastTriggerElementRef.current?.focus();
    }, 50);
  };

  if (loading || loadingCase) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-slate-50 text-slate-500">
        <div className="flex flex-col items-center gap-3">
          <div className="w-8 h-8 border-2 border-blue-600 border-t-transparent rounded-full animate-spin" />
          <span className="text-sm text-slate-500">Loading investigation workspace...</span>
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

  const latestOp = operations.length > 0 ? operations[operations.length - 1] : null;
  const transportInfo = getInvestigationTransportInfo(selectedInvestigation);
  const inferenceDuration = formatInferenceDuration(selectedInvestigation?.raw_response);
  const highRisk = (pCase.risk_score || 0) > 20;

  return (
    <div className="min-h-screen bg-slate-50 text-slate-900 flex flex-col">
      <Header />

      <main
        id="main-content"
        tabIndex={-1}
        className="flex-1 max-w-7xl w-full mx-auto px-4 sm:px-6 py-8 space-y-6 outline-none"
      >
        {/* Page header with breadcrumb and status */}
        <div className="flex flex-col gap-4">
          <nav aria-label="Breadcrumb" className="flex items-center gap-2 text-xs text-slate-500">
            <Link href="/app" className="hover:text-slate-900">Workspace</Link>
            <span className="text-slate-300">/</span>
            <Link href="/app/payouts" className="hover:text-slate-900">Payouts</Link>
            <span className="text-slate-300">/</span>
            <span className="font-mono text-blue-600 font-semibold">{pCase.reference}</span>
          </nav>

          <div className="flex flex-col lg:flex-row lg:items-center lg:justify-between gap-4">
            <div className="space-y-1">
              <div className="flex items-center gap-3 flex-wrap">
                <h1 className="text-2xl font-bold text-slate-900 tracking-tight">Investigation Workspace</h1>
                <Badge variant={pCase.status === "COMPLETED" ? "success" : "neutral"} size="sm" dot>
                  {pCase.status}
                </Badge>
                {highRisk && (
                  <Badge variant="danger" size="sm" dot>
                    High Risk
                  </Badge>
                )}
                {selectedInvestigation && (
                  <Badge
                    variant={
                      selectedInvestigation.confidence === "HIGH"
                        ? "success"
                        : selectedInvestigation.confidence === "MEDIUM"
                        ? "warning"
                        : "danger"
                    }
                    size="sm"
                  >
                    AI Confidence: {selectedInvestigation.confidence}
                  </Badge>
                )}
              </div>
              <p className="text-sm text-slate-500">
                Forensic review, provider evidence, and deterministic policy assessment for this payout case.
              </p>
            </div>

            <div className="flex items-center gap-2 flex-wrap">
              <Link href={`/app/payouts/${pCase.id}`}>
                <Button variant="secondary" size="sm" className="gap-1.5">
                  <ArrowLeft className="w-4 h-4" aria-hidden="true" />
                  Case Detail
                </Button>
              </Link>
              <Button
                variant="primary"
                size="sm"
                onClick={() => handleRunInvestigation(false)}
                disabled={investigating}
                loading={investigating}
                className="gap-2"
              >
                <Sparkles className="w-3.5 h-3.5" aria-hidden="true" />
                {investigating
                  ? `Analyzing... (${formatElapsedTimer(investigationElapsedSeconds)})`
                  : "Run Forensic AI Triage"}
              </Button>
            </div>
          </div>
        </div>

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

        {/* Advisory note */}
        <Alert variant="info" title="Advisory Supervision">
          AI recommendations are advisory only. Deterministic financial policy and independent human approval always govern execution.
        </Alert>

        {/* Section A: Investigation summary */}
        <Card>
          <CardHeader>
            <CardTitle className="text-base">A. Investigation Summary</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 text-sm">
              <div>
                <span className="block text-xs text-slate-500">Case Reference</span>
                <span className="font-mono font-semibold text-slate-900">{pCase.reference}</span>
              </div>
              <div>
                <span className="block text-xs text-slate-500">Payout Amount</span>
                <span className="font-mono font-bold text-emerald-600">{formatCentsToUSD(pCase.amount_cents)}</span>
                <span className="ml-1 text-xs text-slate-500">{pCase.currency}</span>
              </div>
              <div>
                <span className="block text-xs text-slate-500">Current Provider Status</span>
                <span className="font-medium text-slate-900">{latestOp?.paypal_item_status || latestOp?.status || "Not submitted"}</span>
              </div>
              <div>
                <span className="block text-xs text-slate-500">Investigation Status</span>
                <span className="font-medium text-slate-900">{selectedInvestigation?.status || "No investigation run"}</span>
              </div>
              <div>
                <span className="block text-xs text-slate-500">Case Status</span>
                <Badge variant="neutral" size="sm">{pCase.status}</Badge>
              </div>
              <div>
                <span className="block text-xs text-slate-500">Risk Score</span>
                <span className="font-mono text-slate-900">{pCase.risk_score}</span>
              </div>
              <div>
                <span className="block text-xs text-slate-500">Dual Approval Required</span>
                <span className="text-slate-900">{pCase.dual_approval_required ? "Yes" : "No"}</span>
              </div>
              <div>
                <span className="block text-xs text-slate-500">Last Updated</span>
                <span className="text-slate-900">{formatDate(pCase.updated_at)}</span>
              </div>
            </div>
          </CardContent>
        </Card>

        {/* Section B: Provider evidence */}
        <Card>
          <CardHeader>
            <div className="flex items-center gap-2">
              <ShieldCheck className="w-4 h-4 text-emerald-600" aria-hidden="true" />
              <CardTitle className="text-base">B. Provider Evidence</CardTitle>
            </div>
          </CardHeader>
          <CardContent className="space-y-4">
            {operations.length === 0 ? (
              <p className="text-sm text-slate-500">No provider operations recorded for this case.</p>
            ) : (
              operations.map((op, idx) => (
                <div key={op.id} className="rounded-lg border border-slate-200 bg-slate-50 p-4 space-y-3">
                  <div className="flex items-center justify-between flex-wrap gap-2">
                    <span className="text-sm font-semibold text-slate-900">Operation {idx + 1}</span>
                    <Badge variant="neutral" size="sm">{op.status}</Badge>
                  </div>
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 text-sm">
                    <div>
                      <span className="block text-xs text-slate-500">Verified Provider Status</span>
                      <span className="text-slate-900">{op.paypal_item_status || op.paypal_batch_status || "Pending"}</span>
                    </div>
                    <div>
                      <span className="block text-xs text-slate-500">Batch Correlation</span>
                      <span className="font-mono text-xs text-slate-900 break-all">{op.sender_batch_id}</span>
                    </div>
                    <div>
                      <span className="block text-xs text-slate-500">Evidence Reference</span>
                      <span className="font-mono text-xs text-blue-700">{op.id}</span>
                    </div>
                    <div>
                      <span className="block text-xs text-slate-500">Recorded At</span>
                      <span className="text-slate-900">{formatDate(op.created_at)}</span>
                    </div>
                  </div>

                  <button
                    type="button"
                    onClick={() => setTechnicalDetailsOpen((v) => !v)}
                    className="text-xs text-blue-600 hover:text-blue-700 font-medium flex items-center gap-1"
                  >
                    <ChevronDown className={`w-3.5 h-3.5 transition-transform ${technicalDetailsOpen ? "rotate-180" : ""}`} aria-hidden="true" />
                    Technical provider identifiers
                  </button>
                  {technicalDetailsOpen && (
                    <div className="rounded-md bg-white border border-slate-200 p-3 space-y-1 text-xs font-mono text-slate-700">
                      <div className="break-all">sender_batch_id: {op.sender_batch_id}</div>
                      <div className="break-all">sender_item_id: {op.sender_item_id}</div>
                      {op.paypal_payout_batch_id && <div className="break-all">paypal_payout_batch_id: {op.paypal_payout_batch_id}</div>}
                      {op.paypal_payout_item_id && <div className="break-all">paypal_payout_item_id: {op.paypal_payout_item_id}</div>}
                      <div>amount_cents: {op.amount_cents}</div>
                      <div>currency: {op.currency}</div>
                    </div>
                  )}

                  {op.paypal_payout_batch_id && (
                    <button
                      type="button"
                      onClick={(e) => inspectEvidence(op.paypal_payout_batch_id as string, e)}
                      className="inline-flex items-center gap-1 px-2 py-1 rounded text-xs font-mono bg-blue-50 hover:bg-blue-100 text-blue-700 border border-blue-200 transition-colors"
                    >
                      Inspect evidence <ExternalLink className="w-3 h-3" aria-hidden="true" />
                    </button>
                  )}
                </div>
              ))
            )}
          </CardContent>
        </Card>

        {/* Section C: AI assessment */}
        <Card>
          <CardHeader>
            <div className="flex items-center gap-2">
              <Brain className="w-4 h-4 text-blue-600" aria-hidden="true" />
              <CardTitle className="text-base">C. AI Assessment</CardTitle>
            </div>
          </CardHeader>
          <CardContent className="space-y-4">
            {!selectedInvestigation ? (
              <div className="text-center py-8 space-y-3">
                <Sparkles className="w-8 h-8 text-blue-600 mx-auto" aria-hidden="true" />
                <p className="text-sm text-slate-500">No forensic triage record yet. Run an investigation to generate an AI assessment.</p>
                <Button
                  variant="primary"
                  size="sm"
                  onClick={() => handleRunInvestigation(false)}
                  disabled={investigating}
                  loading={investigating}
                  className="gap-2"
                >
                  <Sparkles className="w-4 h-4" aria-hidden="true" />
                  Run First Investigation
                </Button>
              </div>
            ) : (
              <>
                <div className="flex items-center gap-2 flex-wrap">
                  <Badge variant="neutral" size="sm">Model: {selectedInvestigation.model}</Badge>
                  <Badge variant="outline" size="sm">{transportInfo.badgeLabel}</Badge>
                  {inferenceDuration && <span className="text-xs text-slate-500">{inferenceDuration}</span>}
                </div>

                <div>
                  <h3 className="text-sm font-semibold text-slate-900 mb-1">Recommendation</h3>
                  <p className="text-sm text-slate-800 leading-relaxed">{selectedInvestigation.summary}</p>
                </div>

                <div>
                  <h3 className="text-sm font-semibold text-slate-900 mb-1">Confidence</h3>
                  <div className="flex items-center gap-2">
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
                      {selectedInvestigation.confidence}
                    </Badge>
                    <span className="text-xs text-slate-500">
                      Grounded in model output and verified evidence references.
                    </span>
                  </div>
                </div>

                {selectedInvestigation.findings.length > 0 && (
                  <div>
                    <h3 className="text-sm font-semibold text-slate-900 mb-2">Supporting Evidence</h3>
                    <div className="space-y-2">
                      {selectedInvestigation.findings.map((f, i) => (
                        <div key={i} className="rounded-lg border border-slate-200 bg-slate-50 p-3 text-sm space-y-2">
                          <p className="text-slate-800 leading-relaxed">{f.explanation}</p>
                          {f.evidence_references && f.evidence_references.length > 0 && (
                            <div className="flex items-center gap-1.5 flex-wrap">
                              <span className="text-xs text-slate-500">Evidence:</span>
                              {f.evidence_references.map((refId) => (
                                <button
                                  key={refId}
                                  type="button"
                                  onClick={(e) => inspectEvidence(refId, e)}
                                  className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs font-mono bg-blue-50 hover:bg-blue-100 text-blue-700 border border-blue-200 transition-colors"
                                >
                                  {refId}
                                  <ExternalLink className="w-2.5 h-2.5 opacity-60" aria-hidden="true" />
                                </button>
                              ))}
                            </div>
                          )}
                        </div>
                      ))}
                    </div>
                  </div>
                )}

                {selectedInvestigation.recommended_next_steps.length > 0 && (
                  <div>
                    <h3 className="text-sm font-semibold text-slate-900 mb-2">Recommended Next Steps</h3>
                    <div className="space-y-2">
                      {selectedInvestigation.recommended_next_steps.map((step, i) => (
                        <div key={i} className="rounded-lg border border-slate-200 bg-slate-50 p-3 text-sm flex items-start justify-between gap-3">
                          <div>
                            <div className="font-medium text-slate-900">{step.action.replace(/_/g, " ")}</div>
                            <p className="text-xs text-slate-600 mt-0.5">{step.reason}</p>
                          </div>
                          <Badge variant={step.deterministic_policy_permitted ? "success" : "danger"} size="sm">
                            {step.deterministic_policy_permitted ? "Policy Permitted" : "Policy Prohibited"}
                          </Badge>
                        </div>
                      ))}
                    </div>
                  </div>
                )}

                <div className="rounded-lg bg-amber-50 border border-amber-200 p-3 text-xs text-amber-800">
                  AI recommendation is not an authorised action. Human approval is required before any payout execution.
                </div>

                {selectedInvestigation.requires_human_review && (
                  <div className="rounded-lg bg-amber-50 border border-amber-200 p-3 text-xs text-amber-800 flex items-center gap-2">
                    <AlertTriangle className="w-4 h-4 text-amber-600 shrink-0" aria-hidden="true" />
                    Independent human review required by governance policy.
                  </div>
                )}
              </>
            )}
          </CardContent>
        </Card>

        {/* Section D: Financial policy decision */}
        <Card>
          <CardHeader>
            <div className="flex items-center gap-2">
              <Lock className="w-4 h-4 text-blue-600" aria-hidden="true" />
              <CardTitle className="text-base">D. Financial Policy Decision</CardTitle>
            </div>
          </CardHeader>
          <CardContent className="space-y-3 text-sm">
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <div className="rounded-lg border border-slate-200 bg-slate-50 p-3">
                <span className="block text-xs text-slate-500">Ceiling Check</span>
                <span className="font-mono text-slate-900">$500.00 USD maximum single payout</span>
                <div className="text-xs text-slate-500 mt-1">Case amount {formatCentsToUSD(pCase.amount_cents)} is within ceiling.</div>
              </div>
              <div className="rounded-lg border border-slate-200 bg-slate-50 p-3">
                <span className="block text-xs text-slate-500">Dual-Approval Requirement</span>
                <span className="text-slate-900">Required above $250.00 USD</span>
                <div className="text-xs text-slate-500 mt-1">This case: {pCase.dual_approval_required ? "Yes" : "No"}.</div>
              </div>
              <div className="rounded-lg border border-slate-200 bg-slate-50 p-3">
                <span className="block text-xs text-slate-500">Self-Approval Restriction</span>
                <span className="text-slate-900">Initiator cannot approve own payout.</span>
              </div>
              <div className="rounded-lg border border-slate-200 bg-slate-50 p-3">
                <span className="block text-xs text-slate-500">Applicable Governance Rule</span>
                <span className="text-slate-900">Deterministic Rust policy engine (fail-closed).</span>
              </div>
            </div>
            <div className="rounded-lg bg-slate-900 text-slate-100 p-3 text-xs">
              Policy outcome: {selectedInvestigation ? "AI recommendations evaluated against policy rules; prohibited actions blocked." : "Run investigation to evaluate policy outcomes."}
            </div>
          </CardContent>
        </Card>

        {/* Section E: Approval and resolution */}
        <Card>
          <CardHeader>
            <div className="flex items-center gap-2">
              <CheckCircle2 className="w-4 h-4 text-emerald-600" aria-hidden="true" />
              <CardTitle className="text-base">E. Approval and Resolution</CardTitle>
            </div>
          </CardHeader>
          <CardContent className="space-y-4 text-sm">
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <div>
                <span className="block text-xs text-slate-500">Required Approver</span>
                <span className="text-slate-900">Distinct compliance officer (separation of duties).</span>
              </div>
              <div>
                <span className="block text-xs text-slate-500">Current Approval State</span>
                <span className="text-slate-900">{latestOp?.approved_by_user_id ? "Approved" : "Pending approval"}</span>
              </div>
            </div>

            <div>
              <span className="block text-xs text-slate-500 mb-2">Available Authorised Actions</span>
              <div className="flex items-center gap-2 flex-wrap">
                <Link href={`/app/payouts/${pCase.id}`}>
                  <Button variant="secondary" size="sm">Open Case Detail</Button>
                </Link>
                <Button
                  variant="primary"
                  size="sm"
                  onClick={() => handleRunInvestigation(true)}
                  disabled={investigating}
                  loading={investigating}
                >
                  Re-run with Sync
                </Button>
              </div>
            </div>

            <div>
              <span className="block text-xs text-slate-500 mb-2">Audit Timeline</span>
              <div className="space-y-2">
                {investigations.length === 0 ? (
                  <p className="text-xs text-slate-500">No investigation history.</p>
                ) : (
                  investigations.map((inv) => (
                    <button
                      key={inv.id}
                      type="button"
                      onClick={() => setSelectedInvestigation(inv)}
                      className={`w-full text-left rounded-lg border p-3 text-xs transition-colors ${
                        selectedInvestigation?.id === inv.id
                          ? "bg-blue-50 border-blue-200"
                          : "bg-white border-slate-200 hover:bg-slate-50"
                      }`}
                    >
                      <div className="flex items-center justify-between">
                        <span className="font-medium text-slate-900">{inv.model}</span>
                        <span className="text-slate-500">{formatDate(inv.created_at)}</span>
                      </div>
                      <div className="text-slate-500 mt-0.5">Confidence: {inv.confidence} · {inv.status}</div>
                    </button>
                  ))
                )}
              </div>
            </div>
          </CardContent>
        </Card>
      </main>

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