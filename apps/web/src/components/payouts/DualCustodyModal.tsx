"use client";

import React from "react";
import { Lock, AlertTriangle, ShieldCheck } from "lucide-react";
import { PayoutOperation, formatCentsToUSD } from "@/lib/payouts";
import { Modal, Button, Alert } from "@/components/ui";

export interface DualCustodyModalProps {
  isOpen: boolean;
  onClose: () => void;
  operation: PayoutOperation | null;
  currentUserId?: string;
  loading: boolean;
  onConfirmApprove: () => void;
}

export function DualCustodyModal({
  isOpen,
  onClose,
  operation,
  currentUserId,
  loading,
  onConfirmApprove,
}: DualCustodyModalProps) {
  if (!operation) return null;

  const isSelfApproval =
    Boolean(currentUserId && operation.initiated_by_user_id === currentUserId);

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title="Authorize Dual-Custody Approval"
      description="Financial compliance policy mandates independent secondary officer authorization."
    >
      <div className="space-y-4">
        <div className="p-4 rounded-xl bg-slate-950 border border-slate-800 space-y-2 text-xs font-mono">
          <div className="flex justify-between">
            <span className="text-slate-400">Operation ID:</span>
            <span className="text-white font-semibold">{operation.id}</span>
          </div>
          <div className="flex justify-between">
            <span className="text-slate-400">Sender Batch ID:</span>
            <span className="text-blue-400 font-semibold">{operation.sender_batch_id}</span>
          </div>
          <div className="flex justify-between">
            <span className="text-slate-400">Settlement Amount:</span>
            <span className="text-emerald-400 font-bold">
              {formatCentsToUSD(operation.amount_cents)}
            </span>
          </div>
          <div className="flex justify-between">
            <span className="text-slate-400">Initiated By User:</span>
            <span className="text-slate-300">{operation.initiated_by_user_id}</span>
          </div>
        </div>

        {isSelfApproval ? (
          <Alert variant="danger" title="Self-Approval Prohibited">
            You initiated this payout operation. Separation of duties strictly prohibits an officer from approving their own financial transaction. Another authenticated officer must approve this case (HTTP 403 Forbidden).
          </Alert>
        ) : (
          <Alert variant="warning" title="Irrevocable Authorization Notice">
            By signing this approval, you attest that you are an independent officer and have verified recipient legitimacy. Once submitted to PayPal Sandbox, funds are committed.
          </Alert>
        )}

        <div className="pt-3 flex items-center justify-end gap-3 border-t border-slate-800">
          <Button variant="secondary" size="md" onClick={onClose} disabled={loading}>
            Cancel
          </Button>
          <Button
            variant="primary"
            size="md"
            onClick={onConfirmApprove}
            disabled={loading || isSelfApproval}
            loading={loading}
            className="bg-amber-600 hover:bg-amber-500 shadow-amber-950/30"
          >
            <Lock className="w-4 h-4" aria-hidden="true" />
            <span>Confirm Approval</span>
          </Button>
        </div>
      </div>
    </Modal>
  );
}
