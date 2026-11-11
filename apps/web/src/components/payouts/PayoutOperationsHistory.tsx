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
            <History className="w-4 h-4 text-blue-400" aria-hidden="true" />
            <CardTitle className="text-sm">Operations History</CardTitle>
          </div>
        </CardHeader>
        <CardContent>
          <div className="text-center py-8 text-xs text-slate-400 space-y-1">
            <p className="font-semibold text-slate-300">No operations executed yet</p>
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
            <History className="w-4 h-4 text-blue-400" aria-hidden="true" />
            <CardTitle className="text-sm">Operations Audit Trail ({operations.length})</CardTitle>
          </div>
          <span className="text-xs text-slate-400 font-mono">
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
                    <TableCell className="pl-6 font-mono">
                      <div className="text-white font-medium text-xs">
                        {op.id.slice(0, 8)}...
                      </div>
                      <div className="text-[11px] text-blue-400 font-mono">
                        {op.sender_batch_id}
                      </div>
                    </TableCell>

                    <TableCell>
                      <Badge
                        variant={
                          op.status === "SUCCEEDED"
                            ? "success"
                            : op.status === "SUBMITTED"
                            ? "info"
                            : op.status === "AWAITING_APPROVAL"
                            ? "warning"
                            : op.status === "RECONCILIATION_REQUIRED" || op.status === "MANUAL_RECONCILIATION_REQUIRED"
                            ? "danger"
                            : op.status === "FAILED" || op.status === "CANCELLED"
                            ? "danger"
                            : "neutral"
                        }
                        size="sm"
                        dot
                      >
                        {op.status}
                      </Badge>
                      {op.error_message && (
                        <div className="text-[10px] text-rose-400 mt-1 max-w-xs truncate" title={op.error_message}>
                          {op.error_message}
                        </div>
                      )}
                    </TableCell>

                    <TableCell>
                      {op.requires_dual_approval ? (
                        op.approved_by_user_id ? (
                          <Badge variant="success" size="sm">
                            Approved
                          </Badge>
                        ) : (
                          <Badge variant="warning" size="sm">
                            Pending Officer
                          </Badge>
                        )
                      ) : (
                        <Badge variant="outline" size="sm">
                          Single Approver
                        </Badge>
                      )}
                    </TableCell>

                    <TableCell className="font-mono text-[11px]">
                      {op.paypal_payout_batch_id ? (
                        <div>
                          <div className="text-slate-200">
                            Batch: {op.paypal_payout_batch_id}
                          </div>
                          {op.paypal_payout_item_id && (
                            <div className="text-slate-400">
                              Item: {op.paypal_payout_item_id}
                            </div>
                          )}
                          {op.paypal_item_status && (
                            <span className="text-[10px] text-emerald-400 font-semibold uppercase">
                              [{op.paypal_item_status}]
                            </span>
                          )}
                        </div>
                      ) : (
                        <span className="text-slate-500">—</span>
                      )}
                    </TableCell>

                    <TableCell className="text-slate-400 text-[11px] whitespace-nowrap">
                      {formatDate(op.created_at)}
                    </TableCell>

                    <TableCell className="text-right pr-6">
                      {hasRawResponse ? (
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => setExpandedOpId(isExpanded ? null : op.id)}
                          className="gap-1 text-slate-400 hover:text-white"
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
                        <span className="text-[11px] text-slate-500 font-mono">—</span>
                      )}
                    </TableCell>
                  </TableRow>

                  {/* Expanded JSON Inspector row */}
                  {isExpanded && hasRawResponse && (
                    <TableRow className="bg-slate-950/80 hover:bg-slate-950/80">
                      <TableCell colSpan={6} className="p-4 pl-6 pr-6">
                        <div className="rounded-lg bg-slate-950 border border-slate-800 p-3 space-y-2">
                          <div className="flex items-center justify-between text-xs text-slate-400 font-mono border-b border-slate-800 pb-2">
                            <span>Raw Provider JSON (Idempotent Response)</span>
                            <span>Operation: {op.id}</span>
                          </div>
                          <pre className="text-[11px] font-mono text-emerald-400 overflow-x-auto p-2 bg-slate-900/60 rounded max-h-60">
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

