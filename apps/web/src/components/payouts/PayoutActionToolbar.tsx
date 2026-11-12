"use client";

import React from "react";
import {
  Send,
  Lock,
  XCircle,
  RefreshCw,
  PlusCircle,
  AlertTriangle,
  RotateCcw,
  ShieldAlert,
} from "lucide-react";
import { PayoutCase, PayoutOperation } from "@/lib/payouts";
import { Button, Alert, Card, CardHeader, CardTitle, CardContent } from "@/components/ui";

export interface PayoutActionToolbarProps {
  pCase: PayoutCase;
  latestOp: PayoutOperation | null;
  actionLoading: string | null;
  currentUserId?: string;
  onInitiate: () => void;
  onRequestApprove: () => void;
  onRequestSubmit: () => void;
  onRequestCancel: () => void;
  onRequestRecover: () => void;
  onSync: () => void;
}

export function PayoutActionToolbar({
  pCase,
  latestOp,
  actionLoading,
  currentUserId,
  onInitiate,
  onRequestApprove,
  onRequestSubmit,
  onRequestCancel,
  onRequestRecover,
  onSync,
}: PayoutActionToolbarProps) {
  const isFixture = !!pCase.is_synthetic_fixture;
  const isTerminalCase =
    pCase.status === "COMPLETED" || pCase.status === "CANCELLED";

  const isAwaitingApproval = latestOp?.status === "AWAITING_APPROVAL";
  const isInitiator = Boolean(currentUserId && latestOp && latestOp.initiated_by_user_id === currentUserId);

  // Check state of active operation
  const canInitiate =
    !isTerminalCase &&
    (!latestOp ||
      latestOp.status === "FAILED" ||
      latestOp.status === "CANCELLED");

  const canApprove =
    !isTerminalCase &&
    latestOp &&
    latestOp.status === "AWAITING_APPROVAL" &&
    latestOp.requires_dual_approval &&
    !pCase.dual_approval_required;

  const canSubmit =
    !isTerminalCase &&
    latestOp &&
    (latestOp.status === "PREPARED" || latestOp.status === "APPROVED");

  const canSync =
    latestOp &&
    (latestOp.status === "SUBMITTED" || latestOp.status === "SUCCEEDED");

  const canCancel =
    !isTerminalCase &&
    latestOp &&
    (latestOp.status === "PREPARED" ||
      latestOp.status === "AWAITING_APPROVAL" ||
      latestOp.status === "APPROVED" ||
      (latestOp.status === "SUBMITTED" &&
        latestOp.paypal_item_status === "UNCLAIMED"));

  const canRecover =
    latestOp &&
    (latestOp.status === "RECONCILIATION_REQUIRED" ||
      latestOp.status === "RECOVERING");

  return (
    <Card className="border-slate-200/80 bg-white shadow-sm">
      <CardHeader className="py-4 px-6">
        <div className="flex items-center justify-between">
          <div>
            <CardTitle className="text-sm">Operations &amp; Execution Control</CardTitle>
            <p className="text-xs text-slate-500 mt-0.5">
              Cryptographically verified states with dual-custody authorization and idempotency guarantees.
            </p>
          </div>

          {latestOp && (
            <div className="text-right">
              <span className="text-[10px] text-slate-500 font-mono block">
                Active Op ID:
              </span>
              <span className="text-xs font-mono font-semibold text-slate-900">
                {latestOp.id.slice(0, 8)}...
              </span>
            </div>
          )}
        </div>
      </CardHeader>

      <CardContent className="py-4 px-6 space-y-4">
        {pCase.dual_approval_required && (
          <div className="p-4 rounded-xl bg-amber-50 border border-amber-200 space-y-3">
            <div className="flex items-center gap-2 text-amber-800 font-semibold text-xs uppercase tracking-wider">
              <Lock className="w-4 h-4" />
              <span>Dual-Custody Governance Policy Enforced</span>
            </div>
            <p className="text-xs text-slate-700 leading-relaxed">
              This payout exceeds $250.00 USD or high risk threshold and mandates multi-party approval under
              the deterministic policy engine. Separation of duties is enforced at database and engine level:
              initiator and approver must be distinct compliance officers.
              {isAwaitingApproval ? (
                <span className="text-amber-800 font-semibold">
                  {" "}Awaiting approval from a distinct officer.
                </span>
              ) : latestOp?.status === "APPROVED" ? (
                <span className="text-emerald-700 font-semibold">
                  {" "}Approved by officer {latestOp.approved_by_user_id}. Ready for sandbox submission.
                </span>
              ) : null}
            </p>

            {isAwaitingApproval && (
              <div className="pt-1 flex items-center gap-3">
                <Button
                  variant="primary"
                  size="md"
                  onClick={onRequestApprove}
                  disabled={isInitiator || isFixture || !!actionLoading}
                  loading={actionLoading === "approve"}
                  className="bg-emerald-600 hover:bg-emerald-700"
                >
                  <Lock className="w-4 h-4" />
                  <span>Approve Operation (Distinct Officer)</span>
                </Button>
                {isInitiator && (
                  <span className="text-[11px] text-amber-800 font-mono">
                    Self-approval blocked by policy
                  </span>
                )}
              </div>
            )}
          </div>
        )}

        {isFixture && (
          <Alert variant="warning" title="Sample Record Safeguard">
            This case is an isolated sample record. External mutations to PayPal Sandbox are blocked by safeguard controls to maintain environment integrity.
          </Alert>
        )}

        {canRecover && (
          <Alert variant="danger" title="Ambiguous Network Timeout Quarantine">
            This operation reached PayPal with an indeterminate network outcome and was quarantined to prevent duplicate payment. Run idempotent recovery to safely query or complete without repaying.
          </Alert>
        )}

        <div className="flex items-center gap-3 flex-wrap">
          {canInitiate && (
            <Button
              variant="primary"
              size="md"
              onClick={onInitiate}
              loading={actionLoading === "initiate"}
              disabled={isFixture || !!actionLoading}
            >
              <PlusCircle className="w-4 h-4" aria-hidden="true" />
              <span>Initiate Sandbox Payout Operation</span>
            </Button>
          )}

          {canApprove && (
            <Button
              variant="primary"
              size="md"
              onClick={onRequestApprove}
              loading={actionLoading === "approve"}
              disabled={isFixture || !!actionLoading}
              className="bg-amber-600 hover:bg-amber-700"
            >
              <Lock className="w-4 h-4" aria-hidden="true" />
              <span>Approve Payout (Dual Custody)</span>
            </Button>
          )}

          {canSubmit && (
            <Button
              variant="primary"
              size="md"
              onClick={onRequestSubmit}
              loading={actionLoading === "submit"}
              disabled={isFixture || !!actionLoading}
              title={isFixture ? "Disabled: Sample Record (External mutation disabled)" : undefined}
            >
              <Send className="w-4 h-4" aria-hidden="true" />
              <span>Submit to PayPal Sandbox</span>
            </Button>
          )}

          {canSync && (
            <Button
              variant="outline"
              size="md"
              onClick={onSync}
              loading={actionLoading === "sync"}
              disabled={isFixture || !!actionLoading}
              title={isFixture ? "Disabled: Sample Record (External mutation disabled)" : undefined}
            >
              <RefreshCw className="w-4 h-4" aria-hidden="true" />
              <span>Sync with PayPal</span>
            </Button>
          )}

          {canRecover && (
            <Button
              variant="destructive"
              size="md"
              onClick={onRequestRecover}
              loading={actionLoading === "recover"}
              disabled={isFixture || !!actionLoading}
              className="gap-2"
            >
              <RotateCcw className="w-4 h-4" aria-hidden="true" />
              <span>Recover Ambiguous Payout</span>
            </Button>
          )}

          {canCancel && (
            <Button
              variant="secondary"
              size="md"
              onClick={onRequestCancel}
              loading={actionLoading === "cancel"}
              disabled={isFixture || !!actionLoading}
              title={isFixture ? "Disabled: Sample Record (External mutation disabled)" : undefined}
              className="text-rose-600 hover:text-rose-700 hover:bg-rose-50 border-rose-200"
            >
              <XCircle className="w-4 h-4" aria-hidden="true" />
              <span>Cancel Payout</span>
            </Button>
          )}

          {isTerminalCase && (
            <div className="text-xs text-slate-500 flex items-center gap-2">
              <ShieldAlert className="w-4 h-4 text-slate-400" aria-hidden="true" />
              <span>Case is in terminal state ({pCase.status}). No further operations permitted.</span>
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
