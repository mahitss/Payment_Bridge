"use client";

import React, { useState } from "react";
import {
  History,
  FileText,
  ChevronDown,
  ChevronUp,
  Lock,
  CheckCircle2,
  AlertTriangle,
} from "lucide-react";
import { PayoutOperation, formatDate, formatCentsToUSD } from "@/lib/payouts";
import {
  Card,
  CardHeader,
  CardTitle,
  CardContent,
  Badge,
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
  Button,
} from "@/components/ui";

export interface PayoutOperationsHistoryProps {
  operations: PayoutOperation[];
}

export function PayoutOperationsHistory({
  operations,
}: PayoutOperationsHistoryProps) {
  const [expandedOpId, setExpandedOpId] = useState<string | null>(null);

  if (operations.length === 0) {
    return (
      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <History className="w-4 h-4 text-blue-600" aria-hidden="true" />
            <CardTitle className="text-sm">Operations History</CardTitle>
          </div>
        </CardHeader>
        <CardContent>
          <div className="text-center py-8 text-xs text-slate-500 space-y-1">
            <p className="font-semibold text-slate-900">No operations executed yet</p>
            <p>Initiate an operation to generate sender batch idempotency tokens.</p>
          </div>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card className="overflow-hidden">
      <CardHeader>
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <History className="w-4 h-4 text-blue-600" aria-hidden="true" />
            <CardTitle className="text-sm">Operations Audit Trail ({operations.length})</CardTitle>
          </div>
          <span className="text-xs text-slate-500 font-mono">
            PostgreSQL Durable Log
          </span>
        </div>
      </CardHeader>

      <CardContent className="p-0">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="pl-6">Operation ID / Batch</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Dual Custody</TableHead>
              <TableHead>PayPal Batch / Item</TableHead>
              <TableHead>Created</TableHead>
              <TableHead className="text-right pr-6">Raw Log</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {operations.map((op) => {
              const isExpanded = expandedOpId === op.id;
              const hasRawResponse =
                op.raw_provider_response &&
                Object.keys(op.raw_provider_response).length > 0;

              return (
                <React.Fragment key={op.id}>
                  <TableRow>
                    <TableCell className="pl-6 font-mono text-xs">
                      <div className="font-semibold text-blue-600">{op.id.slice(0, 8)}...</div>
                      <div className="text-slate-500 text-[11px] truncate max-w-[180px]">
                        Batch: {op.sender_batch_id}
                      </div>
                    </TableCell>

                    <TableCell>
                      <Badge
                        variant={
                          op.status === "SUCCEEDED"
                            ? "success"
                            : op.status === "SUBMITTED"
                            ? "info"
                            : op.status === "APPROVED"
                            ? "success"
                            : op.status === "AWAITING_APPROVAL"
                            ? "warning"
                            : op.status === "RECONCILIATION_REQUIRED"
                            ? "danger"
                            : op.status === "CANCELLED"
                            ? "danger"
                            : op.status === "FAILED"
                            ? "danger"
                            : "neutral"
                        }
                        size="sm"
                        dot
                      >
                        {op.status}
                      </Badge>
                    </TableCell>

                    <TableCell>
                      {op.requires_dual_approval ? (
                        <div className="space-y-0.5">
                          <span className="inline-flex items-center gap-1 text-[11px] font-mono font-medium text-amber-700">
                            <Lock className="w-3 h-3 text-amber-600" />
                            <span>Mandatory</span>
                          </span>
                          {op.approved_by_user_id ? (
                            <div className="text-[10px] text-emerald-700 font-mono">
                              Signed: {op.approved_by_user_id.slice(0, 6)}...
                            </div>
                          ) : (
                            <div className="text-[10px] text-amber-600 font-mono">
                              Pending sign-off
                            </div>
                          )}
                        </div>
                      ) : (
                        <span className="text-[11px] text-slate-400 font-mono">Single</span>
                      )}
                    </TableCell>

                    <TableCell className="font-mono text-xs">
                      {op.paypal_payout_batch_id ? (
                        <div className="space-y-0.5">
                          <div className="text-slate-800">
                            Batch: {op.paypal_payout_batch_id}
                          </div>
                          {op.paypal_payout_item_id && (
                            <div className="text-slate-500">
                              Item: {op.paypal_payout_item_id}
                            </div>
                          )}
                          {op.paypal_item_status && (
                            <span className="text-[10px] text-emerald-600 font-semibold uppercase">
                              [{op.paypal_item_status}]
                            </span>
                          )}
                        </div>
                      ) : (
                        <span className="text-slate-400">—</span>
                      )}
                    </TableCell>

                    <TableCell className="text-slate-500 text-[11px] whitespace-nowrap">
                      {formatDate(op.created_at)}
                    </TableCell>

                    <TableCell className="text-right pr-6">
                      {hasRawResponse ? (
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => setExpandedOpId(isExpanded ? null : op.id)}
                          className="gap-1 text-slate-600 hover:text-slate-900"
                        >
                          <FileText className="w-3.5 h-3.5" aria-hidden="true" />
                          <span>{isExpanded ? "Hide" : "Payload"}</span>
                          {isExpanded ? (
                            <ChevronUp className="w-3.5 h-3.5" aria-hidden="true" />
                          ) : (
                            <ChevronDown className="w-3.5 h-3.5" aria-hidden="true" />
                          )}
                        </Button>
                      ) : (
                        <span className="text-[11px] text-slate-400 font-mono">—</span>
                      )}
                    </TableCell>
                  </TableRow>

                  {/* Expanded JSON Inspector row */}
                  {isExpanded && hasRawResponse && (
                    <TableRow className="bg-slate-50/80 hover:bg-slate-50/80">
                      <TableCell colSpan={6} className="p-4 pl-6 pr-6">
                        <div className="rounded-lg bg-slate-900 border border-slate-800 p-3 space-y-2 text-slate-100">
                          <div className="flex items-center justify-between text-xs text-slate-400 font-mono border-b border-slate-800 pb-2">
                            <span>Raw Provider JSON (Idempotent Response)</span>
                            <span>Operation: {op.id}</span>
                          </div>
                          <pre className="text-[11px] font-mono text-emerald-400 overflow-x-auto p-2 bg-slate-950/80 rounded max-h-60">
                            {JSON.stringify(op.raw_provider_response, null, 2)}
                          </pre>
                        </div>
                      </TableCell>
                    </TableRow>
                  )}
                </React.Fragment>
              );
            })}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  );
}
