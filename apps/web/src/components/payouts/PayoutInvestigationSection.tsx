"use client";

import React, { useState } from "react";
import Link from "next/link";
import {
  Brain,
  Sparkles,
  RefreshCw,
  ShieldCheck,
  ShieldAlert,
  Info,
  ChevronDown,
  ChevronUp,
  History,
  Bot,
  HelpCircle,
  AlertTriangle,
  CheckCircle2,
  ExternalLink,
} from "lucide-react";
import {
  PayoutCase,
  PayoutInvestigation,
  getInvestigationTransportInfo,
  formatInferenceDuration,
  formatElapsedTimer,
  formatDate,
} from "@/lib/payouts";
import { Button, Badge, Card, CardHeader, CardTitle, CardContent } from "@/components/ui";

export interface PayoutInvestigationSectionProps {
  pCase: PayoutCase;
  investigations: PayoutInvestigation[];
  selectedInvestigation: PayoutInvestigation | null;
  onSelectInvestigation: (inv: PayoutInvestigation) => void;
  investigating: boolean;
  investigationElapsedSeconds: number;
  onRunInvestigation: (forceSync?: boolean) => void;
  onInspectEvidence: (refId: string, event?: React.MouseEvent | React.KeyboardEvent) => void;
  activeEvidenceRef: string | null;
  isEvidenceInspectorOpen: boolean;
  investigationError?: string | null;
  investigationSuccess?: string | null;
}

export function PayoutInvestigationSection({
  pCase,
  investigations,
  selectedInvestigation,
  onSelectInvestigation,
  investigating,
  investigationElapsedSeconds,
  onRunInvestigation,
  onInspectEvidence,
  activeEvidenceRef,
  isEvidenceInspectorOpen,
  investigationError,
  investigationSuccess,
}: PayoutInvestigationSectionProps) {
  const [expandedRawTelemetry, setExpandedRawTelemetry] = useState(false);

  const transportInfo = getInvestigationTransportInfo(selectedInvestigation);
  const formattedDuration = formatInferenceDuration(selectedInvestigation?.raw_response);

  return (
    <section className="p-6 rounded-2xl bg-white border border-slate-200/80 space-y-6 shadow-sm relative overflow-hidden">
      {/* Section Header */}
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 border-b border-slate-100 pb-5">
        <div className="space-y-1">
          <div className="flex items-center gap-2.5">
            <div className="p-2 rounded-lg bg-blue-50 border border-blue-200 text-blue-600">
              <Brain className="w-5 h-5" aria-hidden="true" />
            </div>
            <div>
              <div className="flex items-center gap-2 flex-wrap">
                <h2 className="text-base font-bold text-slate-900 tracking-tight">
                  AI Payout Investigation Engine
                </h2>
                <Badge variant="info" size="sm">
                  Grounded Evidence
                </Badge>
                {pCase.is_synthetic_fixture && (
                  <Badge variant="warning" size="sm">
                    Simulated Evaluation Fixture
                  </Badge>
                )}
              </div>
              <p className="text-xs text-slate-500">
                {pCase.is_synthetic_fixture
                  ? "Forensic root-cause analysis based on pre-seeded synthetic evaluation records (isolated from live provider networks)."
                  : "Forensic root-cause analysis grounded in immutable case and provider records."}
              </p>
            </div>
          </div>
        </div>

        {/* Investigation Actions */}
        <div className="flex items-center gap-2">
          <Button
            variant="primary"
            size="md"
            onClick={() => onRunInvestigation(false)}
            disabled={investigating}
            loading={investigating}
            className="bg-blue-600 hover:bg-blue-700"
          >
            <Sparkles className="w-3.5 h-3.5" aria-hidden="true" />
            <span>
              {investigating ? "Investigating..." : "Run Investigation"}
            </span>
          </Button>

          <Link href={`/app/payouts/${pCase.id}/investigate`}>
            <Button variant="outline" size="md" className="gap-1.5 text-xs text-blue-600">
              <span>Full Workspace</span>
              <ExternalLink className="w-3 h-3" aria-hidden="true" />
            </Button>
          </Link>
        </div>
      </div>

      {/* Error / Offline Alert */}
      {investigationError && (
        <div
          role="alert"
          data-testid="investigation-error-banner"
          className="p-4 rounded-xl bg-rose-50 border border-rose-200 text-rose-800 text-xs flex items-start gap-3"
        >
          <ShieldAlert className="w-4 h-4 flex-shrink-0 text-rose-600 mt-0.5" />
          <div className="space-y-1">
            <span className="font-semibold text-rose-900">Investigation Unavailable</span>
            <p className="text-rose-800 leading-relaxed">{investigationError}</p>
            <div className="text-[11px] text-slate-500 pt-1">
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
          className="p-3 rounded-xl bg-emerald-50 border border-emerald-200 text-emerald-800 text-xs flex items-center gap-2"
        >
          <CheckCircle2 className="w-4 h-4 text-emerald-600 flex-shrink-0" />
          <span>{investigationSuccess}</span>
        </div>
      )}

      {/* Accessible In-Progress Panel */}
      {investigating && (
        <div
          role="status"
          aria-live="polite"
          aria-atomic="true"
          data-testid="investigation-pending-panel"
          className="p-5 rounded-xl bg-blue-50 border border-blue-200 shadow-sm space-y-3.5 transition-all animate-pulse"
        >
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex items-center gap-2.5">
              <RefreshCw className="w-4 h-4 text-blue-600 animate-spin flex-shrink-0" />
              <span className="text-xs font-bold uppercase tracking-wider text-slate-900">
                Forensic AI Investigation in Progress
              </span>
              <span
                data-testid="elapsed-timer"
                className="px-2.5 py-0.5 rounded-full text-xs font-mono font-bold bg-blue-100 text-blue-700 border border-blue-200"
              >
                Elapsed: {formatElapsedTimer(investigationElapsedSeconds)}
              </span>
            </div>
            <span className="text-[11px] font-mono text-blue-700">
              Awaiting model response...
            </span>
          </div>

          <p className="text-xs text-slate-600 leading-relaxed font-sans">
            Local CPU inference is executing. On CPU-only development hardware, processing local model weights may take over a minute. Actual timings vary depending on host hardware, CPU thread allocation, and model context size.
          </p>

          <div className="pt-2 border-t border-blue-200 flex items-center gap-2 text-[11px] text-amber-800">
            <ShieldAlert className="w-3.5 h-3.5 flex-shrink-0 text-amber-600" />
            <span>
              AI ADVISORY SCOPE: This investigation is strictly forensic and advisory. It will not submit, approve, execute, or cancel any financial payout.
            </span>
          </div>
        </div>
      )}

      {/* Empty State */}
      {investigations.length === 0 && !investigating && (
        <div className="text-center py-12 px-4 space-y-3">
          <div className="w-12 h-12 rounded-xl bg-slate-100 border border-slate-200 flex items-center justify-center mx-auto text-slate-400">
            <Bot className="w-6 h-6" />
          </div>
          <h3 className="text-sm font-semibold text-slate-900">No Investigations Run Yet</h3>
          <p className="text-xs text-slate-500 max-w-lg mx-auto leading-relaxed">
            Trigger a grounded investigation to analyze lifecycle anomalies, examine PayPal provider status, identify missing information, and review policy-checked next steps.
          </p>
          <Button
            variant="primary"
            size="md"
            onClick={() => onRunInvestigation(false)}
            disabled={investigating}
            className="bg-blue-600 hover:bg-blue-700"
          >
            <Sparkles className="w-3.5 h-3.5" />
            <span>Run First Investigation</span>
          </Button>
        </div>
      )}

      {/* Selected Investigation Results Display */}
      {selectedInvestigation && (
        <div className="space-y-6">
          {/* History Bar */}
          {investigations.length > 1 && (
            <div className="flex items-center gap-2 overflow-x-auto pb-2 border-b border-slate-100">
              <span className="text-[11px] font-medium text-slate-500 flex items-center gap-1.5 flex-shrink-0">
                <History className="w-3.5 h-3.5 text-slate-400" />
                <span>Past Investigations:</span>
              </span>
              {investigations.map((inv, idx) => (
                <button
                  key={inv.id}
                  onClick={() => onSelectInvestigation(inv)}
                  className={`px-2.5 py-1 rounded-md text-[11px] font-mono transition flex-shrink-0 flex items-center gap-1.5 ${
                    selectedInvestigation.id === inv.id
                      ? "bg-blue-50 text-blue-700 border border-blue-200 font-semibold"
                      : "bg-white hover:bg-slate-50 text-slate-600 border border-slate-200"
                  }`}
                >
                  <span>#{investigations.length - idx}</span>
                  <span>{formatDate(inv.created_at)}</span>
                  {inv.confidence === "HIGH" && (
                    <span className="w-1.5 h-1.5 rounded-full bg-emerald-500" />
                  )}
                </button>
              ))}
            </div>
          )}

          {/* Top Summary Banner */}
          <div className="p-5 rounded-xl bg-slate-50 border border-slate-200 space-y-3">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div className="flex flex-wrap items-center gap-2.5">
                <span className="text-xs font-semibold text-slate-600 uppercase tracking-wider">
                  Forensic Summary
                </span>

                <Badge
                  variant={
                    selectedInvestigation.confidence === "HIGH"
                      ? "success"
                      : selectedInvestigation.confidence === "MEDIUM"
                      ? "info"
                      : selectedInvestigation.confidence === "LOW"
                      ? "warning"
                      : "purple"
                  }
                  size="sm"
                >
                  Confidence: {selectedInvestigation.confidence}
                </Badge>

                {selectedInvestigation.requires_human_review ? (
                  <Badge variant="warning" size="sm">
                    Human Review Mandatory
                  </Badge>
                ) : (
                  <Badge variant="success" size="sm">
                    Automated Consensus
                  </Badge>
                )}

                <span
                  data-testid="header-transport-badge"
                  className={`px-2 py-0.5 rounded text-[10px] font-semibold tracking-wide ${
                    transportInfo.type === "native_ollama"
                      ? "bg-purple-50 text-purple-700 border border-purple-200"
                      : transportInfo.type === "openai_compatible"
                      ? "bg-blue-50 text-blue-700 border border-blue-200"
                      : "bg-slate-100 text-slate-600 border border-slate-200"
                  }`}
                >
                  {transportInfo.badgeLabel}
                </span>

                {formattedDuration && (
                  <span
                    data-testid="header-duration-badge"
                    className="px-2 py-0.5 rounded text-[10px] font-semibold bg-emerald-50 text-emerald-700 border border-emerald-200 font-mono"
                  >
                    Inference: {formattedDuration}
                  </span>
                )}
              </div>

              <span className="text-[11px] font-mono text-slate-500">
                Recorded {formatDate(selectedInvestigation.created_at)}
              </span>
            </div>

            <p className="text-sm text-slate-800 leading-relaxed font-sans">
              {selectedInvestigation.summary}
            </p>

            {/* Explicit Disclaimer and Telemetry Metadata */}
            <div className="pt-2.5 border-t border-slate-200 flex flex-wrap items-center justify-between gap-3 text-[11px] text-slate-500">
              <span className="flex items-center gap-1.5 text-slate-600">
                <Info className="w-3.5 h-3.5 text-slate-400 flex-shrink-0" />
                <span>AI ADVISORY OPINION: Advisory only. All payments require deterministic officer authorization.</span>
              </span>
              <div className="flex flex-wrap items-center gap-2 font-mono text-[10px]">
                <span
                  data-testid="investigation-transport"
                  className={`px-2 py-0.5 rounded border ${
                    transportInfo.type === "native_ollama"
                      ? "bg-purple-50 text-purple-700 border-purple-200"
                      : transportInfo.type === "openai_compatible"
                      ? "bg-blue-50 text-blue-700 border-blue-200"
                      : "bg-slate-100 text-slate-600 border-slate-200"
                  }`}
                >
                  Transport: {transportInfo.label}
                </span>
                <span data-testid="investigation-engine" className="text-slate-600">
                  Engine: {selectedInvestigation.provider} ({selectedInvestigation.model})
                </span>
                {formattedDuration ? (
                  <span
                    data-testid="investigation-duration"
                    className="px-2 py-0.5 rounded bg-emerald-50 text-emerald-700 border border-emerald-200 font-semibold"
                  >
                    Duration: {formattedDuration}
                  </span>
                ) : (
                  <span
                    data-testid="investigation-duration-historical"
                    className="text-slate-400 italic"
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
            <div className="p-5 rounded-xl bg-white border border-slate-200/80 space-y-4 shadow-sm">
              <div className="flex items-center justify-between border-b border-slate-100 pb-2.5">
                <div className="flex items-center gap-2">
                  <ShieldCheck className="w-4 h-4 text-emerald-600" />
                  <h3 className="text-xs font-bold uppercase tracking-wider text-slate-900">
                    Observed Facts &amp; Grounded Findings
                  </h3>
                </div>
                <span className="text-[10px] font-mono text-emerald-600 font-semibold">
                  Validated References
                </span>
              </div>

              {/* Observed Facts */}
              {selectedInvestigation.observed_facts && selectedInvestigation.observed_facts.length > 0 && (
                <div className="space-y-1.5">
                  <span className="text-[10px] uppercase font-semibold text-slate-500 tracking-wider">
                    Immutable Snapshot Facts
                  </span>
                  <ul className="space-y-1 text-xs text-slate-700">
                    {selectedInvestigation.observed_facts.map((fact, idx) => (
                      <li key={idx} className="flex items-start gap-2">
                        <span className="text-emerald-600 font-bold">•</span>
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
                        className="p-3 rounded-lg bg-slate-50 border border-slate-200/80 space-y-2 text-xs"
                      >
                        <div className="flex items-start justify-between gap-2">
                          <span className="text-slate-800 leading-snug">{f.explanation}</span>
                          <span
                            data-testid="finding-validated-badge"
                            className="px-1.5 py-0.5 rounded text-[9px] font-mono font-semibold bg-emerald-50 text-emerald-700 border border-emerald-200 whitespace-nowrap"
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
                                onClick={(e) => onInspectEvidence(refId, e)}
                                className={`px-2 py-0.5 rounded text-[10px] font-mono transition border ${
                                  activeEvidenceRef === refId && isEvidenceInspectorOpen
                                    ? "bg-blue-100 text-blue-700 border-blue-300 ring-1 ring-blue-400 font-semibold"
                                    : "bg-white text-slate-700 hover:text-slate-900 border-slate-200 hover:border-slate-300 shadow-sm"
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
                <div className="p-3.5 rounded-lg bg-amber-50 border border-amber-200 space-y-1.5">
                  <div className="flex items-center gap-1.5 text-amber-800 text-xs font-semibold">
                    <HelpCircle className="w-3.5 h-3.5 text-amber-600" />
                    <span>Information Gaps (Missing Evidence)</span>
                  </div>
                  <ul className="space-y-1 text-xs text-amber-800 pl-5 list-disc">
                    {selectedInvestigation.missing_information.map((item, idx) => (
                      <li key={idx}>{item}</li>
                    ))}
                  </ul>
                </div>
              )}
            </div>

            {/* Column 2: Recommended Next Steps & Safety Guarantees */}
            <div className="p-5 rounded-xl bg-white border border-slate-200/80 space-y-4 shadow-sm">
              <div className="flex items-center justify-between border-b border-slate-100 pb-2.5">
                <div className="flex items-center gap-2">
                  <CheckCircle2 className="w-4 h-4 text-blue-600" />
                  <h3 className="text-xs font-bold uppercase tracking-wider text-slate-900">
                    Policy-Checked Action Recommendations
                  </h3>
                </div>
                <span className="text-[10px] font-mono text-blue-600 font-semibold">
                  Deterministic Gates
                </span>
              </div>

              <div className="space-y-3">
                {selectedInvestigation.recommended_next_steps && selectedInvestigation.recommended_next_steps.length > 0 ? (
                  selectedInvestigation.recommended_next_steps.map((rec, idx) => (
                    <div
                      key={idx}
                      className="p-3.5 rounded-lg bg-slate-50 border border-slate-200/80 space-y-2 text-xs"
                    >
                      <div className="flex items-start justify-between gap-2">
                        <span className="font-semibold text-slate-900 tracking-wide">
                          {rec.action.replace(/_/g, " ")}
                        </span>
                        <span
                          className={`px-2 py-0.5 rounded text-[10px] font-mono font-semibold ${
                            rec.deterministic_policy_permitted
                              ? "bg-emerald-50 text-emerald-700 border border-emerald-200"
                              : "bg-rose-50 text-rose-700 border border-rose-200"
                          }`}
                        >
                          {rec.deterministic_policy_permitted ? "PERMITTED" : "BLOCKED"}
                        </span>
                      </div>
                      <p className="text-slate-600 leading-relaxed text-[11px]">{rec.reason}</p>
                    </div>
                  ))
                ) : (
                  <p className="text-xs text-slate-500">No actions recommended.</p>
                )}
              </div>

              {/* Risk Flags Banner */}
              {selectedInvestigation.risk_flags && selectedInvestigation.risk_flags.length > 0 && (
                <div className="p-3.5 rounded-lg bg-rose-50 border border-rose-200 space-y-1.5">
                  <div className="flex items-center gap-1.5 text-rose-800 text-xs font-semibold">
                    <AlertTriangle className="w-3.5 h-3.5 text-rose-600" />
                    <span>Active Risk Flags</span>
                  </div>
                  <ul className="space-y-1 text-xs text-rose-800 pl-5 list-disc">
                    {selectedInvestigation.risk_flags.map((flag, idx) => (
                      <li key={idx}>{flag}</li>
                    ))}
                  </ul>
                </div>
              )}
            </div>
          </div>

          {/* Raw Telemetry Accordion */}
          {selectedInvestigation.raw_response && (
            <div className="rounded-xl border border-slate-200 bg-white overflow-hidden text-xs shadow-sm">
              <button
                type="button"
                data-testid="toggle-raw-telemetry"
                onClick={() => setExpandedRawTelemetry(!expandedRawTelemetry)}
                className="w-full p-4 flex items-center justify-between text-slate-600 hover:text-slate-900 transition"
              >
                <div className="flex items-center gap-2">
                  <Brain className="w-4 h-4 text-blue-600" />
                  <span className="font-semibold text-xs text-slate-900">Raw Model Telemetry &amp; Execution Output</span>
                </div>
                {expandedRawTelemetry ? (
                  <ChevronUp className="w-4 h-4 text-slate-400" />
                ) : (
                  <ChevronDown className="w-4 h-4 text-slate-400" />
                )}
              </button>

              {expandedRawTelemetry && (
                <div data-testid="raw-telemetry-body" className="p-4 border-t border-slate-100 bg-slate-900 text-slate-100">
                  <pre data-testid="raw-telemetry-json" className="font-mono text-[11px] text-emerald-400 overflow-x-auto max-h-80 p-3 bg-slate-950/80 rounded-lg">
                    {JSON.stringify(selectedInvestigation.raw_response, null, 2)}
                  </pre>
                </div>
              )}
            </div>
          )}
        </div>
      )}
    </section>
  );
}
