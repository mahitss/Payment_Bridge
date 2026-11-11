"use client";

import React from "react";
import Link from "next/link";
import { ArrowLeft, RefreshCw, Sparkles } from "lucide-react";
import { PayoutCase } from "@/lib/payouts";
import { Button, Badge } from "@/components/ui";

export interface PayoutHeaderProps {
  pCase: PayoutCase;
  loading: boolean;
  onRefresh: () => void;
}

export function PayoutHeader({ pCase, loading, onRefresh }: PayoutHeaderProps) {
  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <Link href="/app/payouts">
          <Button variant="ghost" size="sm" className="gap-1.5 -ml-2 text-slate-600 hover:text-slate-900">
            <ArrowLeft className="w-4 h-4" aria-hidden="true" />
            <span>Back to Payout Registry</span>
          </Button>
        </Link>

        <div className="flex items-center gap-2">
          <Link href={`/app/payouts/${pCase.id}/investigate`}>
            <Button variant="secondary" size="sm" className="gap-1.5 text-blue-600">
              <Sparkles className="w-3.5 h-3.5 text-blue-600" aria-hidden="true" />
              <span>AI Investigation Workspace</span>
            </Button>
          </Link>

          <Button
            variant="outline"
            size="sm"
            onClick={onRefresh}
            disabled={loading}
            aria-label="Refresh case details"
            title="Refresh case details"
          >
            <RefreshCw
              className={`w-3.5 h-3.5 ${loading ? "animate-spin" : ""}`}
              aria-hidden="true"
            />
          </Button>
        </div>
      </div>

      {pCase.is_synthetic_fixture && (
        <div className="p-3.5 rounded-xl bg-amber-50 border border-amber-200 text-amber-900 text-xs flex items-center gap-2.5">
          <span className="font-bold">SIMULATED DEMO DATA — NOT A LIVE PAYPAL TRANSACTION: </span>
          <span>This case and its operations are local evaluation fixtures. External provider calls are prevented to maintain safety.</span>
        </div>
      )}

      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 p-6 rounded-2xl bg-white border border-slate-200/80 shadow-sm">
        <div className="space-y-1">
          <div className="flex items-center gap-3 flex-wrap">
            <h1 className="text-xl sm:text-2xl font-bold text-slate-900 tracking-tight font-mono">
              {pCase.reference}
            </h1>

            <Badge
              variant={
                pCase.status === "COMPLETED"
                  ? "success"
                  : pCase.status === "SUBMITTED"
                  ? "info"
                  : pCase.status === "CANCELLED"
                  ? "danger"
                  : pCase.status === "FAILED"
                  ? "danger"
                  : "neutral"
              }
              size="sm"
              dot
            >
              Internal: {pCase.status}
            </Badge>

            {pCase.is_synthetic_fixture && (
              <Badge variant="warning" size="sm">
                SIMULATED FIXTURE
              </Badge>
            )}

            {pCase.dual_approval_required && (
              <Badge variant="warning" size="sm">
                DUAL CUSTODY
              </Badge>
            )}
          </div>

          <div className="text-sm font-semibold text-slate-700">
            {pCase.title}
          </div>

          {pCase.description && (
            <p className="text-xs text-slate-500 max-w-2xl leading-relaxed">
              {pCase.description}
            </p>
          )}
        </div>
      </div>
    </div>
  );
}
