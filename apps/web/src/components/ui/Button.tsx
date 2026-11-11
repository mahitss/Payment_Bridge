import React from "react";
import { cn } from "@/lib/utils";

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: "primary" | "secondary" | "outline" | "ghost" | "destructive";
  size?: "sm" | "md" | "lg";
  loading?: boolean;
}

export const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  (
    {
      className,
      variant = "primary",
      size = "md",
      loading = false,
      disabled,
      children,
      ...props
    },
    ref
  ) => {
    const baseStyles =
      "inline-flex items-center justify-center font-medium rounded-lg transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:ring-offset-2 focus-visible:ring-offset-slate-950 disabled:opacity-50 disabled:cursor-not-allowed select-none active:scale-[0.98]";

    const sizeStyles = {
      sm: "min-h-[36px] px-3 py-1.5 text-xs gap-1.5",
      md: "min-h-[44px] px-4 py-2 text-xs font-semibold gap-2",
      lg: "min-h-[48px] px-5 py-2.5 text-sm font-semibold gap-2.5",
    };

    const variantStyles = {
      primary:
        "bg-blue-600 hover:bg-blue-500 text-white shadow-sm shadow-blue-900/30 active:bg-blue-700",
      secondary:
        "bg-slate-800 hover:bg-slate-700 text-slate-200 border border-slate-700 active:bg-slate-800",
      outline:
        "border border-slate-700 text-slate-300 hover:bg-slate-800/60 hover:text-white active:bg-slate-800",
      ghost:
        "text-slate-400 hover:text-slate-200 hover:bg-slate-800/40 active:bg-slate-800/60",
      destructive:
        "bg-rose-600 hover:bg-rose-500 text-white shadow-sm shadow-rose-950/40 active:bg-rose-700",
    };

    return (
      <button
        ref={ref}
        disabled={disabled || loading}
        className={cn(baseStyles, sizeStyles[size], variantStyles[variant], className)}
        {...props}
      >
        {loading && (
          <svg
            className="animate-spin -ml-0.5 h-3.5 w-3.5 text-current"
            xmlns="http://www.w3.org/2000/svg"
            fill="none"
            viewBox="0 0 24 24"
            aria-hidden="true"
          >
            <circle
              className="opacity-25"
              cx="12"
              cy="12"
              r="10"
              stroke="currentColor"
              strokeWidth="4"
            />
            <path
              className="opacity-75"
              fill="currentColor"
              d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
            />
          </svg>
        )}
        {children}
      </button>
    );
  }
);

Button.displayName = "Button";

