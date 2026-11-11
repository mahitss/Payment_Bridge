"use client";

import React, { useEffect, useRef } from "react";
import { X } from "lucide-react";
import { cn } from "@/lib/utils";

export interface DrawerProps {
  isOpen: boolean;
  onClose: () => void;
  title: string;
  description?: string;
  children: React.ReactNode;
  width?: "sm" | "md" | "lg" | "xl";
  className?: string;
  dataTestId?: string;
  closeButtonTestId?: string;
}

export function Drawer({
  isOpen,
  onClose,
  title,
  description,
  children,
  width = "md",
  className,
  dataTestId,
  closeButtonTestId,
}: DrawerProps) {
  const drawerRef = useRef<HTMLDivElement | null>(null);
  const titleId = React.useId();
  const descId = React.useId();

  // Escape key listener & body scroll lock
  useEffect(() => {
    if (!isOpen) return;

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        onClose();
      }
    };

    document.addEventListener("keydown", handleKeyDown);
    document.body.style.overflow = "hidden";

    return () => {
      document.removeEventListener("keydown", handleKeyDown);
      document.body.style.overflow = "";
    };
  }, [isOpen, onClose]);

  if (!isOpen) return null;

  const widthStyles = {
    sm: "max-w-sm",
    md: "max-w-md",
    lg: "max-w-lg",
    xl: "max-w-xl",
  };

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby={titleId}
      aria-describedby={description ? descId : undefined}
      className="fixed inset-0 z-50 overflow-hidden"
    >
      {/* Backdrop */}
      <div
        onClick={onClose}
        className="fixed inset-0 bg-slate-950/50 backdrop-blur-sm transition-opacity animate-in fade-in-0 duration-200"
        aria-hidden="true"
      />

      {/* Drawer panel */}
      <div className="fixed inset-y-0 right-0 max-w-full flex pl-10">
        <div
          ref={drawerRef}
          data-testid={dataTestId}
          className={cn(
            "w-screen bg-white border-l border-slate-200 shadow-2xl flex flex-col overscroll-contain animate-in slide-in-from-right duration-200 text-slate-900",
            widthStyles[width],
            className
          )}
        >
          {/* Header */}
          <div className="p-6 border-b border-slate-100 flex items-start justify-between bg-slate-50/50">
            <div className="space-y-1 pr-4">
              <h2
                id={titleId}
                className="text-base font-semibold text-slate-900 tracking-tight"
              >
                {title}
              </h2>
              {description && (
                <p id={descId} className="text-xs text-slate-500 leading-relaxed">
                  {description}
                </p>
              )}
            </div>
            <button
              type="button"
              onClick={onClose}
              data-testid={closeButtonTestId}
              aria-label="Close panel"
              className="min-h-[44px] min-w-[44px] -mr-2 -mt-2 p-2 rounded-lg text-slate-400 hover:text-slate-700 hover:bg-slate-100 transition-colors flex items-center justify-center focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
            >
              <X className="w-5 h-5" aria-hidden="true" />
            </button>
          </div>

          {/* Body */}
          <div className="p-6 overflow-y-auto overscroll-contain flex-1">
            {children}
          </div>
        </div>
      </div>
    </div>
  );
}
