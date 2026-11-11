"use client";

import React, { useEffect, useRef } from "react";
import { ShieldCheck, Clock, FileText, AlertTriangle, Info, X } from "lucide-react";
import { EvidenceResolution, formatDate } from "@/lib/payouts";

export interface EvidenceInspectorDrawerProps {
  isOpen: boolean;
  onClose: () => void;
  evidence: EvidenceResolution | null;
  loading: boolean;
  error: string | null;
  activeEvidenceRef?: string | null;
  onRetry?: (refId: string) => void;
  triggerElement?: HTMLElement | null;
}

export function EvidenceInspectorDrawer({
  isOpen,
  onClose,
  evidence,
  loading,
  error,
  activeEvidenceRef,
  onRetry,
  triggerElement,
}: EvidenceInspectorDrawerProps) {
  const drawerRef = useRef<HTMLDivElement | null>(null);
  const closeButtonRef = useRef<HTMLButtonElement | null>(null);
  const wasOpenRef = useRef<boolean>(false);

  // Focus trap and accessibility management
  useEffect(() => {
    if (isOpen) {
      wasOpenRef.current = true;

      // Focus close button on open
      const focusTimer = requestAnimationFrame(() => {
        closeButtonRef.current?.focus();
      });

      const handleKeyDown = (e: KeyboardEvent) => {
        if (e.key === "Escape") {
          e.preventDefault();
          onClose();
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
            if (
              document.activeElement === firstElement ||
              !drawerRef.current.contains(document.activeElement)
            ) {
              e.preventDefault();
              lastElement.focus();
            }
          } else {
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

      window.addEventListener("keydown", handleKeyDown);
      return () => {
        cancelAnimationFrame(focusTimer);
        window.removeEventListener("keydown", handleKeyDown);
      };
    } else if (wasOpenRef.current) {
      wasOpenRef.current = false;
      if (triggerElement && document.contains(triggerElement) && typeof triggerElement.focus === "function") {
        triggerElement.focus();
      } else if (activeEvidenceRef) {
        const chip = document.querySelector<HTMLElement>(
          `[data-testid="evidence-chip-${activeEvidenceRef}"]`
        );
        chip?.focus();
      }
    }
  }, [isOpen, activeEvidenceRef, triggerElement, onClose]);

  if (!isOpen) return null;

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="evidence-inspector-title"
      data-testid="evidence-inspector-drawer"
      className="fixed inset-0 z-50 flex justify-end bg-black/70 backdrop-blur-sm transition-opacity"
      onClick={(e) => {
        if (e.target === e.currentTarget) {
          onClose();
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
            onClick={onClose}
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
              {activeEvidenceRef || evidence?.reference_id}
            </span>
          </div>

          {evidence && (
            <div className="flex items-center gap-2 font-mono text-[10px]">
              <span
                data-testid="evidence-source-type"
                className="px-2 py-0.5 rounded bg-blue-500/10 text-blue-300 border border-blue-500/30 font-semibold"
              >
                {evidence.source_type}
              </span>
              <span
                data-testid="evidence-resolution-status"
                className="px-2 py-0.5 rounded bg-emerald-500/10 text-emerald-300 border border-emerald-500/30 font-semibold"
              >
                {evidence.resolution_status === "RESOLVED"
                  ? "REFERENCE VALIDATED"
                  : evidence.resolution_status}
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
          {loading && (
            <div
              data-testid="evidence-loading"
              role="status"
              aria-live="polite"
              className="p-8 flex flex-col items-center justify-center space-y-3 text-slate-400 text-xs"
            >
              <div
                className="w-6 h-6 border-2 border-indigo-400 border-t-transparent rounded-full animate-spin"
                aria-hidden="true"
              />
              <span>Resolving citation against case records...</span>
            </div>
          )}

          {/* Error / Unavailable State */}
          {!loading && error && (
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
                {error}
              </p>
              <p className="text-[11px] text-slate-400 leading-normal">
                The identifier could not be validated against records belonging to this organization and case.
                It may be malformed, belonging to another tenant or case, or an ungrounded hallucination.
              </p>
              {activeEvidenceRef && onRetry && (
                <button
                  type="button"
                  onClick={() => onRetry(activeEvidenceRef)}
                  className="mt-2 px-3 py-1.5 rounded-lg bg-rose-500/20 hover:bg-rose-500/30 text-rose-200 text-xs font-medium border border-rose-500/40 transition"
                >
                  Retry Verification
                </button>
              )}
            </div>
          )}

          {/* Resolved Content */}
          {!loading && evidence && (
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
                      evidence.is_synthetic_fixture
                        ? "bg-amber-500/10 text-amber-300 border-amber-500/30"
                        : "bg-emerald-500/10 text-emerald-300 border-emerald-500/30"
                    }`}
                  >
                    {evidence.is_synthetic_fixture
                      ? "Simulated Demo Fixture (Local Data)"
                      : "Live Authorized Record"}
                  </span>
                </div>
                <p className="text-[11px] text-slate-400">
                  {evidence.is_synthetic_fixture
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
                  {evidence.display_label}
                </div>
                {evidence.recorded_at && (
                  <div
                    data-testid="evidence-recorded-at"
                    className="text-xs text-slate-400 flex items-center gap-1.5 font-mono pt-1"
                  >
                    <Clock className="w-3.5 h-3.5 text-slate-500" aria-hidden="true" />
                    <span>Recorded: {formatDate(evidence.recorded_at)}</span>
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
                  {evidence.fields.map((f, fIdx) => (
                    <div
                      key={fIdx}
                      className="px-3.5 py-2.5 flex items-start justify-between gap-3 text-xs"
                    >
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
            onClick={onClose}
            className="min-h-[38px] px-4 py-2 rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-300 text-xs font-medium transition"
            data-testid="evidence-inspector-footer-close"
          >
            Close Inspector
          </button>
        </div>
      </div>
    </div>
  );
}
