import { useMemo, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { CheckCircle, ArrowLeft, Loader2, AlertCircle } from "lucide-react";
import { Plan } from "../api-types";
import { useAuth } from "../contexts/AuthContext";
import { billingData } from "./billingPage.data";

interface LocationState {
  targetPlan?: Plan;
}

const PlanChangePage: React.FC = () => {
  const navigate = useNavigate();
  const location = useLocation();
  const { user, updateUserAndToken, authApiClient } = useAuth();
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const targetPlan = (location.state as LocationState | null)?.targetPlan;
  const currentPlan = user?.plan ?? Plan.FREE;

  const resolvedTargetPlan = useMemo(() => {
    if (targetPlan !== undefined) return targetPlan;
    return currentPlan === Plan.PAID ? Plan.FREE : Plan.PAID;
  }, [targetPlan, currentPlan]);

  const isUpgrade = resolvedTargetPlan === Plan.PAID;
  const planData = isUpgrade ? billingData.plans.pro : billingData.plans.free;

  const handleConfirm = async () => {
    setIsLoading(true);
    setError(null);
    try {
      const response = await authApiClient.put("/v1/user/profile", {
        plan: resolvedTargetPlan,
      });
      const { user: updatedUser, jwtToken } = response.data;
      updateUserAndToken(updatedUser, jwtToken);
      navigate("/dashboard/billing", { replace: true });
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
    <div className="max-w-3xl mx-auto space-y-8 animate-fade-in">
      <div className="flex items-center gap-3">
        <button
          onClick={() => navigate("/dashboard/billing")}
          className="p-2 rounded-lg border border-neutral-800 text-neutral-400 hover:text-white hover:bg-neutral-900/60 transition-colors"
        >
          <ArrowLeft className="w-4 h-4" />
        </button>
        <div>
          <h1 className="text-2xl font-bold text-white">
            {isUpgrade ? "Upgrade Plan" : "Downgrade Plan"}
          </h1>
          <p className="text-neutral-400 text-sm">
            {isUpgrade
              ? "Review the Pro plan benefits before confirming your upgrade."
              : "You can downgrade to Free at any time. Your data stays intact."}
          </p>
        </div>
      </div>

      {error && (
        <div className="p-4 rounded-lg bg-red-900/10 border border-red-900/20 text-red-400 text-sm flex items-center gap-2">
          <AlertCircle className="w-4 h-4" /> {error}
        </div>
      )}

      <div className="p-6 rounded-xl border border-neutral-800 bg-neutral-900/30">
        <div className="flex items-center justify-between">
          <div>
            <h2 className="text-xl font-semibold text-white">{planData.title}</h2>
            <p className="text-neutral-400 text-sm mt-1">{planData.subtitle}</p>
          </div>
          <div className="text-right">
            <div className="text-3xl font-bold text-white">{planData.price}</div>
            {planData.price !== "Free" && (
              <div className="text-neutral-500 text-sm">/month</div>
            )}
          </div>
        </div>

        <ul className="mt-6 space-y-3">
          {planData.features.map((feature: string, i: number) => (
            <li key={i} className="flex items-start gap-3 text-sm text-neutral-300">
              <CheckCircle className="w-4 h-4 text-green-500 mt-0.5 shrink-0" />
              {feature}
            </li>
          ))}
        </ul>
      </div>

      <div className="flex flex-col sm:flex-row gap-3">
        <button
          onClick={() => navigate("/dashboard/billing")}
          className="flex-1 px-4 py-2 rounded-lg border border-neutral-800 text-neutral-300 hover:text-white hover:bg-neutral-900/60 transition-colors"
        >
          Cancel
        </button>
        <button
          onClick={handleConfirm}
          disabled={isLoading}
          className="flex-1 px-4 py-2 rounded-lg bg-white text-black font-medium hover:bg-neutral-200 transition-colors disabled:opacity-70 disabled:cursor-not-allowed"
        >
          {isLoading ? (
            <span className="flex items-center justify-center gap-2">
              <Loader2 className="w-4 h-4 animate-spin" /> Processing...
            </span>
          ) : isUpgrade ? (
            "Confirm Upgrade"
          ) : (
            "Confirm Downgrade"
          )}
        </button>
      </div>
    </div>
  );
};

export default PlanChangePage;
