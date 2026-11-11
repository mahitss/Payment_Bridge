import React from "react";
import { cn } from "@/lib/utils";

export interface BadgeProps extends React.HTMLAttributes<HTMLSpanElement> {
  variant?:
    | "neutral"
    | "success"
    | "warning"
    | "danger"
    | "info"
    | "purple"
    | "outline";
  size?: "sm" | "md";
  dot?: boolean;
}

export function Badge({
  className,
  variant = "neutral",
  size = "sm",
  dot = false,
  children,
  ...props
}: BadgeProps) {
  const sizeStyles = {
    sm: "px-2 py-0.5 text-[11px] font-medium tracking-tight",
    md: "px-2.5 py-1 text-xs font-medium tracking-tight",
  };

  const variantStyles = {
    neutral:
      "bg-slate-800 text-slate-300 border border-slate-700/80",
    success:
      "bg-emerald-950/70 text-emerald-300 border border-emerald-800/60",
    warning:
      "bg-amber-950/70 text-amber-300 border border-amber-800/60",
    danger:
      "bg-rose-950/70 text-rose-300 border border-rose-800/60",
    info:
      "bg-blue-950/70 text-blue-300 border border-blue-800/60",
    purple:
      "bg-purple-950/70 text-purple-300 border border-purple-800/60",
    outline:
      "bg-transparent text-slate-400 border border-slate-700",
  };

  const dotColors = {
    neutral: "bg-slate-400",
    success: "bg-emerald-400",
    warning: "bg-amber-400",
    danger: "bg-rose-400",
    info: "bg-blue-400",
    purple: "bg-purple-400",
    outline: "bg-slate-400",
  };

  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full select-none font-mono",
        sizeStyles[size],
        variantStyles[variant],
        className
      )}
      {...props}
    >
      {dot && (
        <span
          className={cn("w-1.5 h-1.5 rounded-full", dotColors[variant])}
          aria-hidden="true"
        />
      )}
      {children}
    </span>
  );
}

