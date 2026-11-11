import React from "react";
import { AlertCircle, AlertTriangle, CheckCircle2, Info, X } from "lucide-react";
import { cn } from "@/lib/utils";

export interface AlertProps extends React.HTMLAttributes<HTMLDivElement> {
  variant?: "info" | "warning" | "danger" | "success";
  title?: string;
  onDismiss?: () => void;
}

export function Alert({
  variant = "info",
  title,
  onDismiss,
  className,
  children,
  ...props
}: AlertProps) {
  const variantStyles = {
    info: "bg-blue-950/50 border-blue-800/80 text-blue-200",
    warning: "bg-amber-950/50 border-amber-800/80 text-amber-200",
    danger: "bg-rose-950/50 border-rose-800/80 text-rose-200",
    success: "bg-emerald-950/50 border-emerald-800/80 text-emerald-200",
  };

  const icons = {
    info: <Info className="w-4 h-4 text-blue-400 shrink-0 mt-0.5" aria-hidden="true" />,
    warning: <AlertTriangle className="w-4 h-4 text-amber-400 shrink-0 mt-0.5" aria-hidden="true" />,
    danger: <AlertCircle className="w-4 h-4 text-rose-400 shrink-0 mt-0.5" aria-hidden="true" />,
    success: <CheckCircle2 className="w-4 h-4 text-emerald-400 shrink-0 mt-0.5" aria-hidden="true" />,
  };

  return (
    <div
      role="alert"
      className={cn(
        "p-4 rounded-xl border text-xs flex items-start gap-3 relative shadow-sm",
        variantStyles[variant],
        className
      )}
      {...props}
    >
      {icons[variant]}
      <div className="flex-1 space-y-1">
        {title && <h4 className="font-semibold text-white tracking-tight">{title}</h4>}
        <div className="text-slate-300 leading-relaxed">{children}</div>
      </div>
      {onDismiss && (
        <button
          type="button"
          onClick={onDismiss}
          aria-label="Dismiss alert"
          className="min-h-[36px] min-w-[36px] -mr-2 -mt-2 p-1.5 rounded-lg text-slate-400 hover:text-white hover:bg-slate-800/40 transition-colors flex items-center justify-center focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
        >
          <X className="w-4 h-4" aria-hidden="true" />
        </button>
      )}
    </div>
  );
}

