"use client";

import React from "react";
import { User, DollarSign, ShieldCheck, Clock, Lock, CheckCircle2 } from "lucide-react";
import { PayoutCase, formatCentsToUSD, formatDate } from "@/lib/payouts";
import { Card, CardHeader, CardTitle, CardContent, Badge } from "@/components/ui";

export interface PayoutSummaryCardsProps {
  pCase: PayoutCase;
}

export function PayoutSummaryCards({ pCase }: PayoutSummaryCardsProps) {
  return (
    <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
      {/* Recipient Card */}
      <Card>
        <CardHeader className="py-4 px-5">
          <div className="flex items-center justify-between">
            <span className="text-[11px] font-semibold text-slate-500 uppercase tracking-wider">
              Recipient
            </span>
            <User className="w-4 h-4 text-slate-400" aria-hidden="true" />
          </div>
        </CardHeader>
        <CardContent className="py-2 px-5 space-y-1">
          <div className="text-sm font-semibold text-slate-900 truncate">
            {pCase.recipient_name}
          </div>
          <div className="text-xs text-slate-500 font-mono truncate">
            {pCase.recipient_email}
          </div>
          <span className="text-[10px] text-slate-400">PayPal Sandbox Account</span>
        </CardContent>
      </Card>

      {/* Settlement Value Card */}
      <Card>
        <CardHeader className="py-4 px-5">
          <div className="flex items-center justify-between">
            <span className="text-[11px] font-semibold text-slate-500 uppercase tracking-wider">
              Settlement Amount
            </span>
            <DollarSign className="w-4 h-4 text-emerald-600" aria-hidden="true" />
          </div>
        </CardHeader>
        <CardContent className="py-2 px-5 space-y-1">
          <div className="text-xl font-bold font-mono text-slate-900">
            {formatCentsToUSD(pCase.amount_cents)}
          </div>
          <div className="text-xs text-slate-500 font-mono">
            {pCase.amount_cents} integer cents
          </div>
          <span className="text-[10px] text-slate-400">Currency: {pCase.currency}</span>
        </CardContent>
      </Card>

      {/* Governance & Policy Card */}
      <Card>
        <CardHeader className="py-4 px-5">
          <div className="flex items-center justify-between">
            <span className="text-[11px] font-semibold text-slate-500 uppercase tracking-wider">
              Governance Policy
            </span>
            <ShieldCheck className="w-4 h-4 text-blue-600" aria-hidden="true" />
          </div>
        </CardHeader>
        <CardContent className="py-2 px-5 space-y-1.5">
          <div className="flex items-center gap-1.5">
            {pCase.dual_approval_required ? (
              <Badge variant="warning" size="sm" dot>
                Dual-Custody Required
              </Badge>
            ) : (
              <Badge variant="success" size="sm">
                Single Officer
              </Badge>
            )}
          </div>
          <div className="text-xs text-slate-500 font-mono">
            Risk Score: {pCase.risk_score} / 100
          </div>
          <span className="text-[10px] text-slate-400">Deterministic Rust Rules</span>
        </CardContent>
      </Card>

      {/* Record Timestamps Card */}
      <Card>
        <CardHeader className="py-4 px-5">
          <div className="flex items-center justify-between">
            <span className="text-[11px] font-semibold text-slate-500 uppercase tracking-wider">
              Durable Audit
            </span>
            <Clock className="w-4 h-4 text-slate-400" aria-hidden="true" />
          </div>
        </CardHeader>
        <CardContent className="py-2 px-5 space-y-1">
          <div className="text-xs text-slate-700">
            <span className="text-slate-400">Created: </span>
            <span className="font-mono">{formatDate(pCase.created_at)}</span>
          </div>
          <div className="text-xs text-slate-700">
            <span className="text-slate-400">Updated: </span>
            <span className="font-mono">{formatDate(pCase.updated_at)}</span>
          </div>
          <span className="text-[10px] text-slate-400 font-mono truncate block">
            ID: {pCase.id.slice(0, 18)}...
          </span>
        </CardContent>
      </Card>
    </div>
  );
}
