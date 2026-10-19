"use client";

import React, { useEffect, useState, useMemo, useCallback } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import {
  CreditCard,
  PlusCircle,
  Search,
  Filter,
  ArrowRight,
  RefreshCw,
  Lock,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  AlertCircle,
} from "lucide-react";
import { useAuth } from "@/lib/auth";
import Header from "@/components/Header";
import { PayoutCase, formatCentsToUSD, formatDate } from "@/lib/payouts";

export default function PayoutsListPage() {
  const { user, loading } = useAuth();
  const router = useRouter();

  const [cases, setCases] = useState<PayoutCase[]>([]);
  const [totalCases, setTotalCases] = useState<number>(0);
  const [loadingCases, setLoadingCases] = useState<boolean>(true);
  const [searchTerm, setSearchTerm] = useState<string>("");
  const [statusFilter, setStatusFilter] = useState<string>("ALL");
  const [page, setPage] = useState<number>(0);
  const pageSize = 15;

  useEffect(() => {
    if (!loading && !user) {
      router.push("/login?returnTo=/app/payouts");
    }
  }, [user, loading, router]);

  const fetchCases = useCallback(async () => {
    setLoadingCases(true);
    try {
      const offset = page * pageSize;
      const res = await fetch(`/api/v1/payout-cases?limit=${pageSize}&offset=${offset}`, {
        method: "GET",
        credentials: "include",
      });
      if (res.ok) {
        const data = await res.json();
        setCases(data.cases || []);
        setTotalCases(data.total || 0);
      }
    } catch {
      // Backend error fallback
    } finally {
      setLoadingCases(false);
    }
  }, [page, pageSize]);

  useEffect(() => {
    if (user) {
      fetchCases();
    }
  }, [user, fetchCases]);

  // Client-side filtering across current loaded page
  const filteredCases = useMemo(() => {
    return cases.filter((c) => {
      const matchesSearch =
        c.reference.toLowerCase().includes(searchTerm.toLowerCase()) ||
        c.recipient_email.toLowerCase().includes(searchTerm.toLowerCase()) ||
        c.recipient_name.toLowerCase().includes(searchTerm.toLowerCase()) ||
        c.title.toLowerCase().includes(searchTerm.toLowerCase());

      const matchesStatus =
        statusFilter === "ALL" || c.status === statusFilter;

      return matchesSearch && matchesStatus;
    });
  }, [cases, searchTerm, statusFilter]);

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-slate-950 text-slate-400">
        <div className="flex flex-col items-center gap-3">
          <div className="w-8 h-8 border-2 border-blue-500 border-t-transparent rounded-full animate-spin" />
          <span className="text-xs font-mono">Loading cases...</span>
        </div>
      </div>
    );
  }

  if (!user) {
    return null;
  }

  const totalPages = Math.ceil(totalCases / pageSize);

  return (
    <div className="min-h-screen bg-slate-950 text-slate-100 flex flex-col">
      <Header />

      <main className="flex-1 max-w-7xl w-full mx-auto px-6 py-8 space-y-6">
        {/* Page Header */}
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
          <div>
            <div className="flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-blue-400 mb-1">
              <span>Settlement Registry</span>
            </div>
            <h1 className="text-xl font-bold text-white tracking-tight">
              Payout Cases & Investigation
            </h1>
            <p className="text-xs text-slate-400 mt-1">
              Organization-scoped payout cases. Dual approval is enforced on operations exceeding $250.00 USD.
            </p>
          </div>

          <div className="flex items-center gap-3">
            <Link
              href="/app/payouts/new"
              className="px-4 py-2.5 rounded-lg bg-blue-600 hover:bg-blue-500 text-white text-xs font-semibold transition flex items-center gap-2 shadow-lg shadow-blue-600/20"
            >
              <PlusCircle className="w-4 h-4" />
              <span>Create Payout Case</span>
            </Link>
            <button
              onClick={fetchCases}
              disabled={loadingCases}
              className="p-2.5 rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-300 hover:text-white transition border border-slate-700 text-xs flex items-center gap-1.5"
              title="Refresh"
            >
              <RefreshCw className={`w-4 h-4 ${loadingCases ? "animate-spin" : ""}`} />
            </button>
          </div>
        </div>

        {/* Filter & Search Bar */}
        <div className="p-4 rounded-xl bg-slate-900/60 border border-slate-800 flex flex-col sm:flex-row gap-3 items-center justify-between">
          <div className="relative w-full sm:w-80">
            <Search className="w-4 h-4 absolute left-3 top-2.5 text-slate-500" />
            <input
              type="text"
              placeholder="Search reference, recipient, title..."
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
              className="w-full pl-9 pr-3 py-1.5 rounded-lg bg-slate-950 border border-slate-700/80 text-xs text-white placeholder-slate-500 focus:outline-none focus:border-blue-500 transition"
            />
          </div>

          <div className="flex items-center gap-2 w-full sm:w-auto">
            <Filter className="w-3.5 h-3.5 text-slate-400" />
            <span className="text-xs text-slate-400">Status:</span>
            <select
              value={statusFilter}
              onChange={(e) => setStatusFilter(e.target.value)}
              className="px-2.5 py-1.5 rounded-lg bg-slate-950 border border-slate-700/80 text-xs text-white focus:outline-none focus:border-blue-500 transition"
            >
              <option value="ALL">All Statuses</option>
              <option value="DRAFT">Draft</option>
              <option value="SUBMITTED">Submitted</option>
              <option value="COMPLETED">Completed</option>
              <option value="CANCELLED">Cancelled</option>
              <option value="DENIED">Denied</option>
            </select>
          </div>
        </div>

        {/* Payout Table Section */}
        <section className="rounded-xl bg-slate-900/40 border border-slate-800 overflow-hidden">
          {loadingCases ? (
            <div className="py-20 flex flex-col items-center justify-center gap-2 text-slate-500 text-xs">
              <div className="w-6 h-6 border-2 border-blue-500 border-t-transparent rounded-full animate-spin" />
              <span>Querying database records...</span>
            </div>
          ) : filteredCases.length === 0 ? (
            <div className="p-12 text-center space-y-3">
              <CreditCard className="w-10 h-10 text-slate-600 mx-auto" />
              <div>
                <p className="text-sm font-semibold text-slate-300">No matching payout cases</p>
                <p className="text-xs text-slate-500 mt-1 max-w-sm mx-auto">
                  {cases.length === 0
                    ? "Get started by creating your first sandbox settlement payout case."
                    : "No payout cases match your search or filter criteria."}
                </p>
              </div>
              {cases.length === 0 && (
                <Link
                  href="/app/payouts/new"
                  className="inline-flex items-center gap-2 px-4 py-2 rounded-lg bg-blue-600 hover:bg-blue-500 text-white text-xs font-semibold transition"
                >
                  <PlusCircle className="w-3.5 h-3.5" />
                  <span>Create Case</span>
                </Link>
              )}
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-left text-xs">
                <thead>
                  <tr className="border-b border-slate-800 bg-slate-900/70 text-slate-400 uppercase tracking-wider font-semibold">
                    <th className="py-3 pl-4">Case Reference</th>
                    <th className="py-3">Title & Recipient</th>
                    <th className="py-3">Amount (Minor Units)</th>
                    <th className="py-3">Dual-Custody Policy</th>
                    <th className="py-3">Case Status</th>
                    <th className="py-3">Created Date</th>
                    <th className="py-3 text-right pr-4">Action</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-800/60">
                  {filteredCases.map((c) => (
                    <tr key={c.id} className="hover:bg-slate-800/30 transition">
                      <td className="py-3.5 pl-4 font-mono font-medium text-blue-400">
                        <div className="flex items-center gap-1.5 flex-wrap">
                          <Link href={`/app/payouts/${c.id}`} className="hover:underline">
                            {c.reference}
                          </Link>
                          {c.is_synthetic_fixture && (
                            <span className="px-1.5 py-0.5 rounded text-[9px] font-semibold tracking-wider uppercase bg-amber-500/10 text-amber-400 border border-amber-500/30">
                              SIMULATED
                            </span>
                          )}
                        </div>
                      </td>
                      <td className="py-3.5">
                        <div className="font-medium text-white">{c.title}</div>
                        <div className="text-slate-400 text-[11px] font-mono">
                          {c.recipient_name} &bull; {c.recipient_email}
                        </div>
                      </td>
                      <td className="py-3.5 font-mono font-semibold text-white">
                        {formatCentsToUSD(c.amount_cents)}
                      </td>
                      <td className="py-3.5">
                        {c.dual_approval_required ? (
                          <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[10px] font-medium bg-amber-500/10 text-amber-400 border border-amber-500/20">
                            <Lock className="w-2.5 h-2.5" />
                            Dual-Approval Required
                          </span>
                        ) : (
                          <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[10px] font-medium bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
                            <CheckCircle2 className="w-2.5 h-2.5" />
                            Single Officer
                          </span>
                        )}
                      </td>
                      <td className="py-3.5">
                        <span
                          className={`px-2 py-0.5 rounded text-[10px] font-semibold tracking-wide ${
                            c.status === "COMPLETED"
                              ? "bg-emerald-500/20 text-emerald-300 border border-emerald-500/30"
                              : c.status === "SUBMITTED"
                              ? "bg-blue-500/20 text-blue-300 border border-blue-500/30"
                              : c.status === "CANCELLED"
                              ? "bg-rose-500/20 text-rose-300 border border-rose-500/30"
                              : "bg-slate-800 text-slate-300 border border-slate-700"
                          }`}
                        >
                          {c.status}
                        </span>
                      </td>
                      <td className="py-3.5 text-slate-400 text-[11px] whitespace-nowrap">
                        {formatDate(c.created_at)}
                      </td>
                      <td className="py-3.5 text-right pr-4">
                        <Link
                          href={`/app/payouts/${c.id}`}
                          className="px-3 py-1 rounded bg-slate-800 hover:bg-slate-700 text-slate-200 hover:text-white transition font-medium border border-slate-700 inline-flex items-center gap-1 text-[11px]"
                        >
                          <span>Inspect</span>
                          <ArrowRight className="w-3 h-3" />
                        </Link>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {/* Pagination Footer */}
          {totalCases > pageSize && (
            <div className="p-4 border-t border-slate-800 bg-slate-900/50 flex items-center justify-between text-xs text-slate-400">
              <span>
                Showing {page * pageSize + 1} -{" "}
                {Math.min((page + 1) * pageSize, totalCases)} of {totalCases} cases
              </span>
              <div className="flex items-center gap-2">
                <button
                  onClick={() => setPage((p) => Math.max(0, p - 1))}
                  disabled={page === 0}
                  className="px-2.5 py-1 rounded bg-slate-800 hover:bg-slate-700 disabled:opacity-40 disabled:cursor-not-allowed text-white transition flex items-center gap-1"
                >
                  <ChevronLeft className="w-3.5 h-3.5" />
                  <span>Previous</span>
                </button>
                <span className="font-mono text-slate-300">
                  Page {page + 1} of {totalPages}
                </span>
                <button
                  onClick={() => setPage((p) => p + 1)}
                  disabled={(page + 1) * pageSize >= totalCases}
                  className="px-2.5 py-1 rounded bg-slate-800 hover:bg-slate-700 disabled:opacity-40 disabled:cursor-not-allowed text-white transition flex items-center gap-1"
                >
                  <span>Next</span>
                  <ChevronRight className="w-3.5 h-3.5" />
                </button>
              </div>
            </div>
          )}
        </section>
      </main>
    </div>
  );
}
