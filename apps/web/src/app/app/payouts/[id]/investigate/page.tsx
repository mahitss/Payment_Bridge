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

  const transportInfo = getInvestigationTransportInfo(selectedInvestigation);
  const inferenceDuration = formatInferenceDuration(selectedInvestigation?.raw_response);

  return (
    <div className="min-h-screen bg-slate-50 text-slate-900 flex flex-col">
      <Header />

      <main
        id="main-content"
        tabIndex={-1}
        className="flex-1 max-w-7xl w-full mx-auto px-4 sm:px-6 py-8 space-y-6 outline-none"
      >
        {/* Workspace Top Navigation */}
        <div className="flex items-center justify-between gap-4">
          <div className="flex items-center gap-3">
            <Link href={`/app/payouts/${pCase.id}`}>
              <Button variant="ghost" size="sm" className="gap-1.5 -ml-2 text-slate-600 hover:text-slate-900">
                <ArrowLeft className="w-4 h-4" aria-hidden="true" />
                <span>Back to Case Detail</span>
              </Button>
            </Link>
            <span className="text-slate-300">/</span>
            <span className="text-xs font-mono text-blue-600 font-semibold">
              {pCase.reference}
            </span>
          </div>

          <div className="flex items-center gap-3">
            <Button
              variant="primary"
              size="sm"
              onClick={() => handleRunInvestigation(false)}
              disabled={investigating}
              loading={investigating}
              className="gap-2"
            >
              <Sparkles className="w-3.5 h-3.5" aria-hidden="true" />
              <span>
                {investigating
                  ? `Analyzing... (${formatElapsedTimer(investigationElapsedSeconds)})`
                  : "Run Forensic AI Triage"}
              </span>
            </Button>
          </div>
        </div>

        {/* Advisory Supervention Banner */}
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

        {/* 3-Panel Workspace Layout */}
        <div className="grid grid-cols-1 lg:grid-cols-12 gap-6 items-start">
          {/* Left Panel: Case Context & Snapshots Timeline (3 cols) */}
          <div className="lg:col-span-3 space-y-4">
            <Card>
              <CardHeader className="py-4 px-5">
                <CardTitle className="text-xs uppercase tracking-wider text-slate-500">
                  Case Overview
                </CardTitle>
              </CardHeader>
              <CardContent className="py-2 px-5 space-y-3 text-xs">
                <div>
                  <span className="text-slate-500 block text-[11px]">Title</span>
                  <span className="font-semibold text-slate-900">{pCase.title}</span>
                </div>
                <div>
                  <span className="text-slate-500 block text-[11px]">Recipient</span>
                  <span className="font-mono text-slate-700">{pCase.recipient_email}</span>
                </div>
                <div>
                  <span className="text-slate-500 block text-[11px]">Settlement Amount</span>
                  <span className="font-mono font-bold text-emerald-600">
                    {formatCentsToUSD(pCase.amount_cents)}
                  </span>
                </div>
                <div>
                  <span className="text-slate-500 block text-[11px]">Case Status</span>
                  <Badge variant="neutral" size="sm">
                    {pCase.status}
                  </Badge>
                </div>
              </CardContent>
            </Card>

            {/* Provider Operations Snapshots */}
            <Card>
              <CardHeader className="py-4 px-5">
                <CardTitle className="text-xs uppercase tracking-wider text-slate-500">
                  Operations &amp; Snapshots
                </CardTitle>
              </CardHeader>
              <CardContent className="py-2 px-5 space-y-3 text-xs">
                {operations.length === 0 ? (
                  <p className="text-slate-500 text-[11px]">No operations recorded.</p>
                ) : (
                  operations.map((op, idx) => (
                    <div
                      key={op.id}
                      className="p-3 rounded-lg bg-slate-50 border border-slate-200 space-y-1 font-mono text-[11px]"
                    >
                      <div className="flex items-center justify-between">
                        <span className="text-blue-600 font-semibold">Op #{idx + 1}</span>
                        <Badge variant="neutral" size="sm">
                          {op.status}
                        </Badge>
                      </div>
                      <div className="text-slate-600 truncate">
                        Batch: {op.sender_batch_id}
                      </div>
                      {op.paypal_payout_batch_id && (
                        <div className="text-slate-800 truncate">
                          PayPal: {op.paypal_payout_batch_id}
                        </div>
                      )}
                      <div className="text-[10px] text-slate-400 pt-1">
                        {formatDate(op.created_at)}
                      </div>
                    </div>
                  ))
                )}
              </CardContent>
            </Card>
          </div>

          {/* Center Panel: Forensic AI Analysis (6 cols) */}
          <div className="lg:col-span-6 space-y-4">
            {!selectedInvestigation ? (
              <Card>
                <CardContent className="py-16 text-center space-y-4">
                  <div className="w-12 h-12 rounded-xl bg-blue-50 border border-blue-200 flex items-center justify-center mx-auto text-blue-600">
                    <Sparkles className="w-6 h-6" aria-hidden="true" />
                  </div>
                  <div className="space-y-1">
                    <h3 className="text-base font-semibold text-slate-900">No Forensic Triage Record</h3>
                    <p className="text-xs text-slate-500 max-w-md mx-auto">
                      Run an autonomous investigation to evaluate observed case facts, inspect provider telemetry, and detect settlement discrepancies.
                    </p>
                  </div>
                  <Button
                    variant="primary"
                    size="md"
                    onClick={() => handleRunInvestigation(false)}
                    disabled={investigating}
                    loading={investigating}
                    className="gap-2"
                  >
                    <Sparkles className="w-4 h-4" aria-hidden="true" />
                    <span>Run First Investigation</span>
                  </Button>
                </CardContent>
              </Card>
            ) : (
              <>
                {/* Executive Summary Card */}
                <Card>
                  <CardHeader>
                    <div className="flex items-center justify-between flex-wrap gap-2">
                      <div className="flex items-center gap-2">
                        <Brain className="w-4 h-4 text-blue-600" aria-hidden="true" />
                        <CardTitle className="text-base">Executive Triage Summary</CardTitle>
                      </div>

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
                          Model: {selectedInvestigation.model}
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
                    </div>
                  </CardHeader>
                  <CardContent className="space-y-4">
                    <p className="text-xs text-slate-800 leading-relaxed font-sans">
                      {selectedInvestigation.summary}
                    </p>

                    {selectedInvestigation.requires_human_review && (
                      <div className="p-3 rounded-lg bg-amber-50 border border-amber-200 text-amber-800 text-xs flex items-center gap-2">
                        <AlertTriangle className="w-4 h-4 text-amber-600 shrink-0" aria-hidden="true" />
                        <span>Human Review Recommended by AI Evaluator.</span>
                      </div>
                    )}
                  </CardContent>
                </Card>

                {/* Grounded Forensic Findings */}
                <Card>
                  <CardHeader>
                    <div className="flex items-center gap-2">
                      <ShieldCheck className="w-4 h-4 text-emerald-600" aria-hidden="true" />
                      <CardTitle className="text-sm">
                        Grounded Forensic Findings ({selectedInvestigation.findings.length})
                      </CardTitle>
                    </div>
                  </CardHeader>
                  <CardContent className="space-y-3">
                    {selectedInvestigation.findings.length === 0 ? (
                      <p className="text-xs text-slate-500">No specific discrepancies detected.</p>
                    ) : (
                      selectedInvestigation.findings.map((f, i) => (
                        <div
                          key={i}
                          className="p-3.5 rounded-lg bg-slate-50 border border-slate-200 space-y-2 text-xs"
                        >
                          <p className="text-slate-800 leading-relaxed">
                            {f.explanation}
                          </p>

                          {f.evidence_references && f.evidence_references.length > 0 && (
                            <div className="flex items-center gap-1.5 flex-wrap pt-1">
                              <span className="text-[11px] text-slate-500 font-mono">
                                Grounded Evidence:
                              </span>
                              {f.evidence_references.map((refId) => (
                                <button
                                  key={refId}
                                  type="button"
                                  data-testid={`evidence-chip-${refId}`}
                                  onClick={(e) => inspectEvidence(refId, e)}
                                  className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[11px] font-mono bg-blue-50 hover:bg-blue-100 text-blue-700 border border-blue-200 transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-blue-500"
                                >
                                  <span>{refId}</span>
                                  <ExternalLink className="w-2.5 h-2.5 opacity-60" aria-hidden="true" />
                                </button>
                              ))}
                            </div>
                          )}
                        </div>
                      ))
                    )}
                  </CardContent>
                </Card>

                {/* Recommended Next Steps */}
                <Card>
                  <CardHeader>
                    <div className="flex items-center gap-2">
                      <CheckCircle2 className="w-4 h-4 text-emerald-600" aria-hidden="true" />
                      <CardTitle className="text-sm">
                        Recommended Next Steps ({selectedInvestigation.recommended_next_steps.length})
                      </CardTitle>
                    </div>
                  </CardHeader>
                  <CardContent className="space-y-3">
                    {selectedInvestigation.recommended_next_steps.length === 0 ? (
                      <p className="text-xs text-slate-500">No further action necessary.</p>
                    ) : (
                      selectedInvestigation.recommended_next_steps.map((step, i) => (
                        <div
                          key={i}
                          className="p-3.5 rounded-lg bg-slate-50 border border-slate-200 flex items-start justify-between gap-3 text-xs"
                        >
                          <div className="space-y-1">
                            <div className="font-semibold text-slate-900">
                              {step.action.replace(/_/g, " ")}
                            </div>
                            <p className="text-slate-600 text-[11px] leading-relaxed">
                              {step.reason}
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
                      ))
                    )}
                  </CardContent>
                </Card>

                {/* Risk Flags & Missing Information */}
                {(selectedInvestigation.risk_flags.length > 0 ||
                  selectedInvestigation.missing_information.length > 0) && (
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                    {selectedInvestigation.risk_flags.length > 0 && (
                      <Card>
                        <CardHeader className="py-3 px-4">
                          <CardTitle className="text-xs uppercase tracking-wider text-rose-600">
                            Risk Flags Detected
                          </CardTitle>
                        </CardHeader>
                        <CardContent className="py-2 px-4 space-y-1.5">
                          {selectedInvestigation.risk_flags.map((flag, i) => (
                            <div key={i} className="text-xs text-rose-700 flex items-start gap-1.5">
                              <span className="text-rose-600 font-bold">&bull;</span>
                              <span>{flag}</span>
                            </div>
                          ))}
                        </CardContent>
                      </Card>
                    )}

                    {selectedInvestigation.missing_information.length > 0 && (
                      <Card>
                        <CardHeader className="py-3 px-4">
                          <CardTitle className="text-xs uppercase tracking-wider text-amber-600">
                            Missing Information
                          </CardTitle>
                        </CardHeader>
                        <CardContent className="py-2 px-4 space-y-1.5">
                          {selectedInvestigation.missing_information.map((item, i) => (
                            <div key={i} className="text-xs text-amber-800 flex items-start gap-1.5">
                              <span className="text-amber-600 font-bold">&bull;</span>
                              <span>{item}</span>
                            </div>
                          ))}
                        </CardContent>
                      </Card>
                    )}
                  </div>
                )}
              </>
            )}
          </div>

          {/* Right Panel: Policy Governance & History (3 cols) */}
          <div className="lg:col-span-3 space-y-4">
            {/* Deterministic Policy Supervention */}
            <Card>
              <CardHeader className="py-4 px-5">
                <div className="flex items-center gap-2">
                  <Cpu className="w-4 h-4 text-blue-600" aria-hidden="true" />
                  <CardTitle className="text-xs uppercase tracking-wider text-slate-500">
                    Policy Engine Rules
                  </CardTitle>
                </div>
              </CardHeader>
              <CardContent className="py-2 px-5 space-y-3 text-xs">
                <div className="p-3 rounded-lg bg-slate-50 border border-slate-200 space-y-2">
                  <div className="flex items-center justify-between">
                    <span className="text-slate-500">Max Limit:</span>
                    <span className="font-mono text-slate-900 font-semibold">$500.00 USD</span>
                  </div>
                  <div className="flex items-center justify-between">
                    <span className="text-slate-500">Dual Custody:</span>
                    <span className="font-mono text-amber-600 font-semibold">&gt; $250.00 USD</span>
                  </div>
                  <div className="flex items-center justify-between">
                    <span className="text-slate-500">Risk Threshold:</span>
                    <span className="font-mono text-amber-600 font-semibold">&gt; 20.0 Score</span>
                  </div>
                </div>

                <p className="text-[11px] text-slate-500 leading-relaxed">
                  In case of disagreement between AI outputs and policy rules, server-side policies strictly supervene.
                </p>
              </CardContent>
            </Card>

            {/* Historical Investigations Timeline */}
            <Card>
              <CardHeader className="py-4 px-5">
                <div className="flex items-center gap-2">
                  <History className="w-4 h-4 text-blue-600" aria-hidden="true" />
                  <CardTitle className="text-xs uppercase tracking-wider text-slate-500">
                    Investigation History ({investigations.length})
                  </CardTitle>
                </div>
              </CardHeader>
              <CardContent className="py-2 px-5 space-y-2 text-xs">
                {investigations.length === 0 ? (
                  <p className="text-slate-500 text-[11px]">No previous runs.</p>
                ) : (
                  investigations.map((inv) => {
                    const active = selectedInvestigation?.id === inv.id;
                    return (
                      <button
                        key={inv.id}
                        type="button"
                        onClick={() => setSelectedInvestigation(inv)}
                        className={`w-full text-left p-3 rounded-lg border transition-colors ${
                          active
                            ? "bg-blue-50 border-blue-200 text-blue-900 shadow-sm"
                            : "bg-white border-slate-200 text-slate-600 hover:text-slate-900 hover:bg-slate-50"
                        }`}
                      >
                        <div className="flex items-center justify-between text-[11px] font-mono">
                          <span className="font-semibold text-blue-600">
                            {inv.model}
                          </span>
                          <span>{inv.confidence}</span>
                        </div>
                        <div className="text-[10px] text-slate-400 mt-1">
                          {formatDate(inv.created_at)}
                        </div>
                      </button>
                    );
                  })
                )}
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
      />
    </div>
  );
}
