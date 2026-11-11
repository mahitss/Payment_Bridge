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
      "bg-slate-100 text-slate-700 border border-slate-200",
    success:
      "bg-emerald-50 text-emerald-700 border border-emerald-200",
    warning:
      "bg-amber-50 text-amber-800 border border-amber-200",
    danger:
      "bg-rose-50 text-rose-700 border border-rose-200",
    info:
      "bg-blue-50 text-blue-700 border border-blue-200",
    purple:
      "bg-purple-50 text-purple-700 border border-purple-200",
    outline:
      "bg-transparent text-slate-600 border border-slate-300",
  };

  const dotColors = {
    neutral: "bg-slate-400",
    success: "bg-emerald-500",
    warning: "bg-amber-500",
    danger: "bg-rose-500",
    info: "bg-blue-500",
    purple: "bg-purple-500",
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
