import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "ClaimBridge — Security-First Claims & Payout Orchestrator",
  description:
    "Autonomous AI Claims Triage with Deterministic Rust Policy Enforcement and PayPal Payouts Integration",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en" className="dark">
      <body className="antialiased bg-slate-950 text-slate-100 min-h-screen">
        {children}
      </body>
    </html>
  );
}
