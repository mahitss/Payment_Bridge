import React from "react";
import { cn } from "@/lib/utils";

export interface StatCardProps {
  label: string;
  value: React.ReactNode;
  hint?: React.ReactNode;
  icon?: React.ReactNode;
  badge?: React.ReactNode;
  className?: string;
  loading?: boolean;
}

export function StatCard({
  label,
  value,
  hint,
  icon,
  badge,
  className,
  loading = false,
}: StatCardProps) {
  return (
    <div
      className={cn(
        "p-5 rounded-xl bg-white border border-slate-200/80 flex flex-col justify-between gap-3 shadow-sm hover:border-slate-300 transition-colors",
        className
      )}
    >
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs font-semibold text-slate-500 tracking-wide uppercase">
          {label}
        </span>
        {icon && (
          <div className="p-2 rounded-lg bg-slate-50 border border-slate-100 text-slate-600">
            {icon}
          </div>
        )}
      </div>

      <div className="space-y-1">
        {loading ? (
          <div className="h-8 w-24 bg-slate-100 animate-pulse rounded" />
        ) : (
          <div className="text-2xl font-bold font-mono tracking-tight text-slate-900 flex items-baseline gap-2">
            <span>{value}</span>
            {badge && <span>{badge}</span>}
          </div>
        )}

        {hint && (
          <p className="text-xs text-slate-500 leading-normal line-clamp-1">
            {hint}
          </p>
        )}
      </div>
    </div>
  );
}
