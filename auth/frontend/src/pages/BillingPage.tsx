import React, { useState } from "react";
import { useAuth } from "../contexts/AuthContext";
import { Plan } from "../api-types";
import {
  Check,
  CreditCard,
  Zap,
  Download,
  ExternalLink,
  AlertCircle,
  Loader2,
} from "lucide-react";
import { billingData } from "./billingPage.data";

// --- Micro-Components ---

const UsageBar = ({
  label,
  used,
  limit,
  unit,
}: {
  label: string;
  used: number;
  limit: string;
  unit: string;
}) => {
  const percentage = Math.min(
    (used / parseInt(limit.replace(/,/g, ""))) * 100,
    100,
  );

  return (
    <div className="mb-6">
      <div className="flex justify-between text-sm mb-2">
        <span className="text-white font-medium">{label}</span>
        <span className="text-neutral-400">
          {used.toLocaleString()} / {limit} {unit}
        </span>
      </div>
      <div className="h-2 w-full bg-neutral-800 rounded-full overflow-hidden">
        <div
          className="h-full bg-purple-500 rounded-full transition-all duration-1000 ease-out"
          style={{ width: `${percentage}%` }}
        />
      </div>
    </div>
  );
};

const PlanCard = ({
  title,
  price,
  features,
  isCurrent,
  onAction,
  isLoading,
  actionLabel,
}: any) => (
  <div
    className={`
        relative p-6 rounded-xl border flex flex-col h-full
        ${
          isCurrent
            ? "bg-neutral-900/40 border-purple-500/50 shadow-[0_0_30px_-10px_rgba(168,85,247,0.15)]"
            : "bg-black border-neutral-800"
        }
    `}
  >
    {isCurrent && (
      <div className="absolute -top-3 left-1/2 -translate-x-1/2 px-3 py-1 bg-purple-500 text-white text-[10px] font-bold uppercase tracking-wider rounded-full shadow-lg">
        Current Plan
      </div>
    )}

    <div className="mb-6">
      <h3 className="text-lg font-semibold text-white">{title}</h3>
      <div className="mt-2 flex items-baseline gap-1">
        <span className="text-3xl font-bold text-white">{price}</span>
        {price !== "Free" && <span className="text-neutral-500">/month</span>}
      </div>
    </div>

    <ul className="space-y-3 mb-8 flex-1">
      {features.map((feature: string, i: number) => (
        <li key={i} className="flex items-start gap-3 text-sm text-neutral-300">
          <Check className="w-4 h-4 text-green-500 mt-0.5 shrink-0" />
          {feature}
        </li>
      ))}
    </ul>

    <button
      onClick={onAction}
      disabled={isCurrent || isLoading}
      className={`
                w-full py-2.5 px-4 rounded-lg text-sm font-medium transition-all
                ${
                  isCurrent
                    ? "bg-neutral-800 text-neutral-500 cursor-default"
                    : "bg-white text-black hover:bg-neutral-200 shadow-[0_0_15px_-3px_rgba(255,255,255,0.3)]"
                }
                disabled:opacity-70 disabled:cursor-not-allowed
            `}
    >
      {isLoading ? (
        <span className="flex items-center justify-center gap-2">
          <Loader2 className="w-4 h-4 animate-spin" /> Processing...
        </span>
      ) : (
        actionLabel
      )}
    </button>
  </div>
);

// --- Main Page ---

const BillingPage: React.FC = () => {
  const { user, updateUserAndToken, authApiClient } = useAuth();
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const isPaid = user?.plan === Plan.PAID;
  const currentPlanData = isPaid
    ? billingData.plans.pro
    : billingData.plans.free;

  const handleSubscribe = async () => {
    setIsLoading(true);
    setError(null);

    try {
      const response = await authApiClient.put("/v1/user/profile", {
        plan: Plan.PAID,
      });
      const { user, jwtToken } = response.data;
      updateUserAndToken(user, jwtToken);
    } catch (err: any) {
      const errorMessage =
        err.response?.data?.message ||
        err.message ||
        "An unexpected error occurred";
      setError(errorMessage);
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <div className="max-w-5xl mx-auto space-y-10 animate-fade-in">
      {/* Header */}
      <div>
        <h1 className="text-3xl font-bold tracking-tight text-white mb-2">
          {billingData.title} & Usage
        </h1>
        <p className="text-neutral-400">
          Manage your subscription, view usage, and download invoices.
        </p>
      </div>

      {/* Usage Section */}
      <div className="grid md:grid-cols-3 gap-6">
        <div className="md:col-span-2 p-6 rounded-xl border border-neutral-800 bg-[#0a0a0a]">
          <h2 className="text-lg font-semibold text-white mb-6 flex items-center gap-2">
            <Zap className="w-4 h-4 text-yellow-500" /> Current Usage
          </h2>

          <UsageBar
            label="Vectors Stored"
            used={currentPlanData.usage.vectors.used}
            limit={currentPlanData.usage.vectors.limit}
            unit={currentPlanData.usage.vectors.unit}
          />

          <UsageBar
            label="Read Operations"
            used={currentPlanData.usage.reads.used}
            limit={currentPlanData.usage.reads.limit}
            unit={currentPlanData.usage.reads.unit}
          />

          <div className="mt-6 pt-4 border-t border-neutral-800 flex items-center justify-between text-xs text-neutral-500">
            <span>Billing cycle resets on Feb 1, 2026</span>
            {!isPaid && (
              <span className="flex items-center gap-1 text-orange-400">
                <AlertCircle className="w-3 h-3" /> Approaching limits
              </span>
            )}
          </div>
        </div>

        {/* Payment Method Stub */}
        <div className="p-6 rounded-xl border border-neutral-800 bg-[#0a0a0a] flex flex-col justify-between">
          <div>
            <h2 className="text-lg font-semibold text-white mb-4 flex items-center gap-2">
              <CreditCard className="w-4 h-4 text-purple-500" /> Payment Method
            </h2>
            {isPaid ? (
              <div className="flex items-center gap-3 p-3 rounded-lg bg-neutral-900 border border-neutral-800">
                <div className="w-8 h-5 bg-white rounded flex items-center justify-center">
                  <div className="w-2 h-2 rounded-full bg-red-500 opacity-80" />
                  <div className="w-2 h-2 rounded-full bg-yellow-500 opacity-80 -ml-1" />
                </div>
                <div className="text-sm">
                  <div className="text-white font-medium">•••• 4242</div>
                  <div className="text-neutral-500 text-xs">Expires 12/28</div>
                </div>
              </div>
            ) : (
              <p className="text-sm text-neutral-500">
                No payment method added.
              </p>
            )}
          </div>
          <button className="text-sm text-neutral-400 hover:text-white underline underline-offset-4 text-left mt-4">
            Manage payment details
          </button>
        </div>
      </div>

      {/* Plans Section */}
      <div>
        <h2 className="text-xl font-bold text-white mb-6">Available Plans</h2>
        {error && (
          <div className="mb-6 p-4 rounded-lg bg-red-900/10 border border-red-900/20 text-red-400 text-sm flex items-center gap-2">
            <AlertCircle className="w-4 h-4" /> {error}
          </div>
        )}

        <div className="grid md:grid-cols-2 gap-6">
          <PlanCard
            {...billingData.plans.free}
            isCurrent={!isPaid}
            actionLabel="Current Plan"
          />
          <PlanCard
            {...billingData.plans.pro}
            isCurrent={isPaid}
            isLoading={isLoading}
            onAction={handleSubscribe}
            actionLabel={billingData.subscribeButtonText}
          />
        </div>
      </div>

      {/* Invoice History (Static Mock) */}
      <div className="pt-8 border-t border-neutral-800">
        <div className="flex items-center justify-between mb-6">
          <h2 className="text-xl font-bold text-white">Invoice History</h2>
          <button className="text-sm text-neutral-400 hover:text-white flex items-center gap-1">
            View all <ExternalLink className="w-3 h-3" />
          </button>
        </div>

        <div className="rounded-xl border border-neutral-800 bg-[#0a0a0a] overflow-hidden">
          <table className="w-full text-left text-sm">
            <thead className="bg-neutral-900/50 text-neutral-400 border-b border-neutral-800">
              <tr>
                <th className="px-6 py-3 font-medium">Date</th>
                <th className="px-6 py-3 font-medium">Amount</th>
                <th className="px-6 py-3 font-medium">Status</th>
                <th className="px-6 py-3 font-medium text-right">Invoice</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-neutral-800">
              {isPaid ? (
                billingData.invoiceHistory.map((invoice, i) => (
                  <tr key={i}>
                    <td className="px-6 py-4 text-white">{invoice.date}</td>
                    <td className="px-6 py-4 text-neutral-300">
                      {invoice.amount}
                    </td>
                    <td className="px-6 py-4">
                      <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-green-500/10 text-green-400 border border-green-500/20">
                        {invoice.status}
                      </span>
                    </td>
                    <td className="px-6 py-4 text-right">
                      <button className="text-neutral-400 hover:text-white">
                        <Download className="w-4 h-4" />
                      </button>
                    </td>
                  </tr>
                ))
              ) : (
                <tr>
                  <td
                    colSpan={4}
                    className="px-6 py-8 text-center text-neutral-500"
                  >
                    No invoices found.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
};

export default BillingPage;
