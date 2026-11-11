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
        "p-5 rounded-xl bg-slate-900 border border-slate-800 flex flex-col justify-between gap-3 shadow-sm hover:border-slate-700/80 transition-colors",
        className
      )}
    >
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs font-medium text-slate-400 tracking-wide uppercase">
          {label}
        </span>
        {icon && (
          <div className="p-2 rounded-lg bg-slate-800/80 text-slate-300">
            {icon}
          </div>
        )}
      </div>

      <div className="space-y-1">
        {loading ? (
          <div className="h-8 w-24 bg-slate-800 animate-pulse rounded" />
        ) : (
          <div className="text-2xl font-bold font-mono tracking-tight text-white flex items-baseline gap-2">
            <span>{value}</span>
            {badge && <span>{badge}</span>}
          </div>
        )}

        {hint && (
          <p className="text-xs text-slate-400 leading-normal line-clamp-1">
            {hint}
          </p>
        )}
      </div>
    </div>
  );
}

