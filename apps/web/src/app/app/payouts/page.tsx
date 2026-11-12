"use client";

import React, { useEffect, useState, useMemo, useCallback } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import {
  CreditCard,
  PlusCircle,
  Search,
  ArrowRight,
  RefreshCw,
  Lock,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  Sparkles,
} from "lucide-react";
import { useAuth } from "@/lib/auth";
import Header from "@/components/Header";
import { PayoutCase, formatCentsToUSD, formatDate } from "@/lib/payouts";
import {
  Button,
  Badge,
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from "@/components/ui";

const STATUS_TABS = [
  { id: "ALL", label: "All Cases" },
  { id: "DRAFT", label: "Drafts" },
  { id: "SUBMITTED", label: "Submitted" },
  { id: "COMPLETED", label: "Completed" },
  { id: "CANCELLED", label: "Cancelled" },
  { id: "FAILED", label: "Failed" },
];

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
      const res = await fetch(`/api/v1/payout-cases?limit=100&offset=0`, {
        method: "GET",
        credentials: "include",
      });

      if (res.ok) {
        const data = await res.json();
        const rawCases: PayoutCase[] = data.cases || [];
        setCases(rawCases);
        setTotalCases(data.total_cases ?? rawCases.length);
      }
    } catch {
      // Fail safely
    } finally {
      setLoadingCases(false);
    }
  }, []);

  useEffect(() => {
    if (user) {
      fetchCases();
    }
  }, [user, fetchCases]);

  // Client-side filtering & search
  const filteredCases = useMemo(() => {
    return cases.filter((c) => {
      // Status filter
      if (statusFilter !== "ALL" && c.status !== statusFilter) {
        return false;
      }

      // Keyword search
      if (!searchTerm.trim()) return true;
      const term = searchTerm.toLowerCase();
      return (
        c.reference?.toLowerCase().includes(term) ||
        c.title?.toLowerCase().includes(term) ||
        c.recipient_name?.toLowerCase().includes(term) ||
        c.recipient_email?.toLowerCase().includes(term)
      );
    });
  }, [cases, statusFilter, searchTerm]);

  const totalPages = Math.ceil(totalCases / pageSize);

  return (
    <div className="min-h-screen bg-slate-50 text-slate-900 flex flex-col">
      <Header />

      <main
        id="main-content"
        tabIndex={-1}
        className="flex-1 max-w-7xl w-full mx-auto px-4 sm:px-6 py-8 space-y-6 outline-none"
      >
        {/* Page Header */}
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
          <div className="space-y-1">
            <div className="flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-blue-600">
              <span>Settlement Registry</span>
            </div>
            <h1 className="text-xl sm:text-2xl font-bold text-slate-900 tracking-tight">
              Payout Cases &amp; Investigation
            </h1>
            <p className="text-xs text-slate-500 max-w-2xl leading-relaxed">
              Organization-scoped payout cases. Dual-custody is strictly enforced on operations exceeding $250.00 USD.
            </p>
          </div>

          <div className="flex items-center gap-3">
            <Link href="/app/payouts/new">
              <Button variant="primary" size="md">
                <PlusCircle className="w-4 h-4" aria-hidden="true" />
                <span>Create Payout Case</span>
              </Button>
            </Link>
            <Button
              variant="outline"
              size="md"
              onClick={fetchCases}
              disabled={loadingCases}
              aria-label="Refresh payout cases"
              title="Refresh Registry"
            >
              <RefreshCw
                className={`w-4 h-4 ${loadingCases ? "animate-spin" : ""}`}
                aria-hidden="true"
              />
            </Button>
          </div>
        </div>

        {/* Status Tabs & Search Bar */}
        <div className="space-y-3">
          {/* Status Tabs */}
          <div
            role="tablist"
            aria-label="Filter by case status"
            className="flex items-center gap-1.5 overflow-x-auto pb-1"
          >
            {STATUS_TABS.map((tab) => {
              const active = statusFilter === tab.id;
              return (
                <button
                  key={tab.id}
                  role="tab"
                  aria-selected={active}
                  onClick={() => setStatusFilter(tab.id)}
                  className={`min-h-[36px] px-3.5 py-1.5 rounded-lg text-xs font-semibold whitespace-nowrap transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 ${
                    active
                      ? "bg-blue-600 text-white shadow-sm"
                      : "bg-white border border-slate-200 text-slate-600 hover:text-slate-900 hover:bg-slate-50"
                  }`}
                >
                  {tab.label}
                </button>
              );
            })}
          </div>

          {/* Search Box */}
          <div className="p-3 rounded-xl bg-white border border-slate-200/80 shadow-sm flex items-center gap-3">
            <Search className="w-4 h-4 text-slate-400 shrink-0 ml-2" aria-hidden="true" />
            <input
              type="text"
              id="payout-search"
              aria-label="Search payout cases by reference, recipient, or title"
              placeholder="Search by case reference, recipient email, name, or title..."
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
              className="w-full min-h-[40px] px-2 py-1.5 bg-transparent text-xs text-slate-900 placeholder-slate-400 focus:outline-none"
            />
            {searchTerm && (
              <button
                type="button"
                onClick={() => setSearchTerm("")}
                className="text-xs text-slate-600 hover:text-slate-900 px-2 py-1 rounded bg-slate-100"
              >
                Clear
              </button>
            )}
          </div>
        </div>

        {/* Registry Table Card */}
        <section className="rounded-xl bg-white border border-slate-200/80 overflow-hidden shadow-sm">
          {loadingCases ? (
            <div
              className="py-20 flex flex-col items-center justify-center gap-3 text-slate-500 text-xs"
              role="status"
              aria-live="polite"
            >
              <div
                className="w-6 h-6 border-2 border-blue-600 border-t-transparent rounded-full animate-spin"
                aria-hidden="true"
              />
              <span>Querying database records...</span>
            </div>
          ) : filteredCases.length === 0 ? (
            <div
              className="p-12 text-center space-y-4"
              role="status"
              aria-live="polite"
            >
              <div className="w-12 h-12 rounded-xl bg-slate-100 border border-slate-200 flex items-center justify-center mx-auto text-slate-400">
                <CreditCard className="w-6 h-6" aria-hidden="true" />
              </div>
              <div className="space-y-1">
                <p className="text-sm font-semibold text-slate-900">No matching payout cases</p>
                <p className="text-xs text-slate-500 max-w-sm mx-auto">
                  {cases.length === 0
                    ? "Get started by creating your first sandbox settlement payout case."
                    : "No payout cases match your current search and status filters."}
                </p>
              </div>
              {cases.length === 0 && (
                <Link href="/app/payouts/new">
                  <Button variant="primary" size="md">
                    <PlusCircle className="w-4 h-4" aria-hidden="true" />
                    <span>Create Case Now</span>
                  </Button>
                </Link>
              )}
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="pl-6">Case Reference</TableHead>
                  <TableHead>Title &amp; Recipient</TableHead>
                  <TableHead>Amount</TableHead>
                  <TableHead>Dual-Custody Policy</TableHead>
                  <TableHead>Case Status</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead className="text-right pr-6">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filteredCases.map((c) => (
                  <TableRow key={c.id}>
                    <TableCell className="pl-6 font-mono font-medium text-blue-600">
                      <div className="flex items-center gap-1.5 flex-wrap">
                        <Link
                          href={`/app/payouts/${c.id}`}
                          className="hover:underline focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-blue-500 rounded font-semibold"
                        >
                          {c.reference}
                        </Link>
                        {c.is_synthetic_fixture && (
                          <Badge variant="warning" size="sm">
                            SAMPLE RECORD
                          </Badge>
                        )}
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="font-medium text-slate-900">{c.title}</div>
                      <div className="text-slate-500 text-[11px] font-mono">
                        {c.recipient_name} &bull; {c.recipient_email}
                      </div>
                    </TableCell>
                    <TableCell className="font-mono font-semibold text-slate-900">
                      {formatCentsToUSD(c.amount_cents)}
                    </TableCell>
                    <TableCell>
                      {c.dual_approval_required ? (
                        <Badge variant="warning" size="sm" dot>
                          Dual-Custody
                        </Badge>
                      ) : (
                        <Badge variant="success" size="sm">
                          Single-Officer
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell>
                      <Badge
                        variant={
                          c.status === "COMPLETED"
                            ? "success"
                            : c.status === "SUBMITTED"
                            ? "info"
                            : c.status === "CANCELLED"
                            ? "danger"
                            : c.status === "FAILED"
                            ? "danger"
                            : "neutral"
                        }
                        size="sm"
                      >
                        {c.status}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-slate-500 text-[11px] whitespace-nowrap">
                      {formatDate(c.created_at)}
                    </TableCell>
                    <TableCell className="text-right pr-6">
                      <div className="inline-flex items-center gap-2">
                        <Link href={`/app/payouts/${c.id}`}>
                          <Button variant="outline" size="sm">
                            <span>Inspect</span>
                          </Button>
                        </Link>
                        <Link href={`/app/payouts/${c.id}/investigate`}>
                          <Button variant="secondary" size="sm" className="gap-1 text-blue-600">
                            <Sparkles className="w-3 h-3 text-blue-600" aria-hidden="true" />
                            <span>AI Triage</span>
                          </Button>
                        </Link>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}

          {/* Pagination Footer */}
          {totalCases > pageSize && (
            <div className="p-4 border-t border-slate-100 bg-slate-50/50 flex flex-col sm:flex-row sm:items-center justify-between gap-3 text-xs text-slate-500">
              <span>
                Showing {page * pageSize + 1} -{" "}
                {Math.min((page + 1) * pageSize, totalCases)} of {totalCases} cases
              </span>
              <div className="flex items-center gap-2">
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() => setPage((p) => Math.max(0, p - 1))}
                  disabled={page === 0}
                  className="gap-1"
                >
                  <ChevronLeft className="w-3.5 h-3.5" aria-hidden="true" />
                  <span>Previous</span>
                </Button>
                <span className="font-mono text-slate-700 px-2">
                  Page {page + 1} of {totalPages}
                </span>
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() => setPage((p) => p + 1)}
                  disabled={(page + 1) * pageSize >= totalCases}
                  className="gap-1"
                >
                  <span>Next</span>
                  <ChevronRight className="w-3.5 h-3.5" aria-hidden="true" />
                </Button>
              </div>
            </div>
          )}
        </section>
      </main>
    </div>
  );
}
