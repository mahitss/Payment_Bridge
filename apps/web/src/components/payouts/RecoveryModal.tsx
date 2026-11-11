"use client";

import React from "react";
import { RotateCcw, AlertTriangle, ShieldCheck } from "lucide-react";
import { PayoutOperation, RecoveryOutcome, formatCentsToUSD } from "@/lib/payouts";
import { Modal, Button, Alert } from "@/components/ui";

export interface RecoveryModalProps {
  isOpen: boolean;
  onClose: () => void;
  operation: PayoutOperation | null;
  loading: boolean;
  recoveryOutcome?: RecoveryOutcome | null;
  onConfirmRecover: () => void;
}

export function RecoveryModal({
  isOpen,
  onClose,
  operation,
  loading,
  recoveryOutcome,
  onConfirmRecover,
}: RecoveryModalProps) {
  if (!operation) return null;

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title="Idempotent Ambiguous Payout Recovery"
      description="Safely resolve indeterminate network outcomes without risk of duplicate payment."
    >
      <div className="space-y-4">
        <div className="p-4 rounded-xl bg-slate-50 border border-slate-200 space-y-2 text-xs font-mono">
          <div className="flex justify-between">
            <span className="text-slate-500">Operation ID:</span>
            <span className="text-slate-900 font-semibold">{operation.id}</span>
          </div>
          <div className="flex justify-between">
            <span className="text-slate-500">Durable Sender Batch ID:</span>
            <span className="text-blue-600 font-semibold">{operation.sender_batch_id}</span>
          </div>
          <div className="flex justify-between">
            <span className="text-slate-500">Quarantined Status:</span>
            <span className="text-rose-600 font-bold">{operation.status}</span>
          </div>
          <div className="flex justify-between">
            <span className="text-slate-500">Amount:</span>
            <span className="text-emerald-600 font-bold">
              {formatCentsToUSD(operation.amount_cents)}
            </span>
          </div>
        </div>

        <Alert variant="warning" title="Durable Recovery Guarantees">
          ClaimBridge uses the exact sender batch ID committed to PostgreSQL before the original request. PayPal will either return the original accepted batch ID without re-executing payment, or safely accept the pending item if never received.
        </Alert>

        <div className="pt-3 flex items-center justify-end gap-3 border-t border-slate-100">
          <Button variant="secondary" size="md" onClick={onClose} disabled={loading}>
            Dismiss
          </Button>
          <Button
            variant="destructive"
            size="md"
            onClick={onConfirmRecover}
            disabled={loading}
            loading={loading}
            className="gap-2"
          >
            <RotateCcw className="w-4 h-4" aria-hidden="true" />
            <span>Execute Idempotent Recovery</span>
          </Button>
        </div>
      </div>
    </Modal>
  );
}
