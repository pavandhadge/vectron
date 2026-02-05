import { useEffect, useMemo, useState } from "react";
import { Outlet, useLocation, Link as RouterLink } from "react-router-dom";
import { useAuth } from "../contexts/AuthContext";
import {
  ChevronRight,
  Home,
  Database,
  Activity,
  Zap,
  Plus,
  BarChart3,
  Server,
  Globe,
  ArrowRight,
} from "lucide-react";
import { managementApi } from "../services/managementApi";
import { formatBytes, formatCompactNumber, formatDateTime, formatNumber } from "../utils/format";

// --- Components ---

const StatCard = ({ title, value, subtext, icon, trend }: any) => (
  <div className="p-6 rounded-xl border border-neutral-800 bg-neutral-900/30 hover:bg-neutral-900/50 transition-colors">
    <div className="flex items-start justify-between mb-4">
      <div className="p-2 rounded-lg bg-neutral-800 text-neutral-400">
        {icon}
      </div>
      {trend && (
        <span
          className={`text-xs font-medium px-2 py-1 rounded-full ${trend > 0 ? "bg-green-900/20 text-green-400" : "bg-red-900/20 text-red-400"}`}
        >
          {trend > 0 ? "+" : ""}
          {trend}%
        </span>
      )}
    </div>
    <div className="space-y-1">
      <h3 className="text-sm font-medium text-neutral-400">{title}</h3>
      <div className="text-2xl font-bold text-white">{value}</div>
      <p className="text-xs text-neutral-500">{subtext}</p>
    </div>
  </div>
);

const QuickAccessCard = ({ title, description, href, icon }: {
  title: string;
  description: string;
  href: string;
  icon: React.ReactNode;
}) => (
  <RouterLink
    to={href}
    className="block p-4 rounded-lg border border-neutral-800 bg-neutral-900/30 hover:bg-neutral-900/50 hover:border-purple-500/50 transition-all group"
  >
    <div className="flex items-center justify-between">
      <div className="flex items-center gap-3">
        <div className="p-2 rounded-lg bg-neutral-800 text-neutral-400 group-hover:bg-purple-500/10 group-hover:text-purple-400 transition-colors">
          {icon}
        </div>
        <div>
          <h3 className="text-white font-medium group-hover:text-purple-400 transition-colors">{title}</h3>
          <p className="text-sm text-neutral-400">{description}</p>
        </div>
      </div>
      <ArrowRight className="w-4 h-4 text-neutral-500 group-hover:text-purple-400 transition-colors" />
    </div>
  </RouterLink>
);

const EmptyState = () => (
  <div className="border border-dashed border-neutral-800 rounded-xl p-12 flex flex-col items-center justify-center text-center bg-neutral-900/10">
    <div className="w-16 h-16 bg-neutral-800/50 rounded-full flex items-center justify-center mb-4">
      <Database className="w-8 h-8 text-neutral-500" />
    </div>
    <h3 className="text-lg font-medium text-white mb-2">
      No collections found
    </h3>
    <p className="text-neutral-400 max-w-sm mb-6">
      Get started by creating your first vector collection to store and query
      embeddings.
    </p>
    <button className="flex items-center gap-2 px-4 py-2 bg-white text-black rounded-md font-medium hover:bg-neutral-200 transition-colors">
      <Plus className="w-4 h-4" /> Create Collection
    </button>
  </div>
);

export const Dashboard = () => {
  const { user, apiGatewayApiClient } = useAuth();
  const location = useLocation();
  const pathnames = location.pathname.split("/").filter((x) => x);
  const isMainDashboard =
    pathnames.length === 1 && pathnames[0] === "dashboard";

  const [systemHealth, setSystemHealth] = useState<any>(null);
  const [gatewayStats, setGatewayStats] = useState<any>(null);
  const [gatewayEndpoints, setGatewayEndpoints] = useState<any[]>([]);
  const [collections, setCollections] = useState<{ name: string; dimension: number; count: number }[]>([]);
  const [loading, setLoading] = useState(true);

  const fetchOverviewData = async () => {
    try {
      const [health, gateway, collectionsResp] = await Promise.all([
        managementApi.getSystemHealth(),
        managementApi.getGatewayStats(),
        apiGatewayApiClient.get("/v1/collections"),
      ]);

      setSystemHealth(health);
      setGatewayStats(gateway.stats);
      setGatewayEndpoints(gateway.endpoints || []);

      const data = collectionsResp.data;
      const mapped = (data.collections || []).map((c: any) => {
        if (typeof c === "string") {
          return { name: c, dimension: 0, count: 0 };
        }
        return {
          name: c.name || "Unnamed Collection",
          dimension: typeof c.dimension === "number" ? c.dimension : 0,
          count: typeof c.count === "number" ? c.count : 0,
        };
      });
      setCollections(mapped);
    } catch (err) {
      // Keep partial data if some calls succeed
      console.error("Failed to load overview data:", err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchOverviewData();
  }, []);

  const requestVolumeBars = useMemo(() => {
    if (!gatewayEndpoints || gatewayEndpoints.length === 0) return [];
    const sorted = [...gatewayEndpoints]
      .sort((a, b) => (b.count || 0) - (a.count || 0))
      .slice(0, 12);
    const max = Math.max(...sorted.map((e) => e.count || 0), 1);
    return sorted.map((e) => ({
      key: `${e.method || "GET"} ${e.path || "-"}`,
      height: Math.max(5, Math.round(((e.count || 0) / max) * 100)),
      count: e.count || 0,
    }));
  }, [gatewayEndpoints]);

  return (
    <div className="container mx-auto px-4 sm:px-6 py-8 min-h-screen bg-black text-white selection:bg-purple-500 selection:text-white">
      {/* Header & Breadcrumbs */}
      <div className="mb-8 space-y-4">
        <nav className="flex" aria-label="Breadcrumb">
          <ol className="inline-flex items-center space-x-1 md:space-x-2">
            <li className="inline-flex items-center">
              <RouterLink
                to="/dashboard"
                className="inline-flex items-center text-sm font-medium text-neutral-500 hover:text-white transition-colors"
              >
                <Home className="w-4 h-4 mr-2" />
                Dashboard
              </RouterLink>
            </li>
            {pathnames.slice(1).map((value, index) => {
              const to = `/dashboard/${pathnames.slice(1, index + 2).join("/")}`;
              return (
                <li key={to}>
                  <div className="flex items-center">
                    <ChevronRight className="w-4 h-4 text-neutral-600 mx-1" />
                    <RouterLink
                      to={to}
                      className="text-sm font-medium text-neutral-500 hover:text-white transition-colors capitalize"
                    >
                      {value}
                    </RouterLink>
                  </div>
                </li>
              );
            })}
          </ol>
        </nav>

        <div className="flex items-end justify-between">
          <div>
            <h1 className="text-3xl font-bold tracking-tight text-white">
              Overview
            </h1>
            <p className="text-neutral-400 mt-1">
              Welcome back,{" "}
              <span className="text-white font-medium">{user?.email}</span>
            </p>
          </div>
          {isMainDashboard && (
            <button className="hidden sm:flex items-center gap-2 px-4 py-2 bg-purple-600 text-white rounded-md font-medium hover:bg-purple-500 transition-colors shadow-[0_0_20px_-5px_rgba(147,51,234,0.5)]">
              <Plus className="w-4 h-4" /> New Project
            </button>
          )}
        </div>
      </div>

      {/* Dashboard Content */}
      {isMainDashboard ? (
        <div className="space-y-8 animate-fade-in">
          {/* Stats Grid */}
          <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
            <StatCard
              title="Total Vectors"
              value={systemHealth ? formatNumber(systemHealth.metrics.total_vectors, "0") : "--"}
              subtext={
                collections.length > 0
                  ? `Across ${collections.length} collections`
                  : "Across 0 collections"
              }
              icon={<Database className="w-5 h-5" />}
            />
            <StatCard
              title="Total Requests"
              value={gatewayStats ? formatCompactNumber(gatewayStats.totalRequests, "0") : "--"}
              subtext={
                gatewayStats
                  ? `${formatNumber(gatewayStats.requestsPerSecond, "0")} req/s`
                  : "No traffic data"
              }
              icon={<Activity className="w-5 h-5" />}
            />
            <StatCard
              title="Avg. Latency"
              value={gatewayStats ? `${formatNumber(gatewayStats.averageResponseTime, "0")}ms` : "--"}
              subtext={
                systemHealth
                  ? `${formatBytes(systemHealth.metrics.memory_used)} memory used`
                  : "No latency data"
              }
              icon={<Zap className="w-5 h-5" />}
            />
          </div>

          {/* Management Console Quick Access */}
          <div className="space-y-6">
            <div className="flex items-center justify-between">
              <h2 className="text-xl font-bold text-white">Management Console</h2>
              <RouterLink
                to="/dashboard/management"
                className="text-sm text-purple-400 hover:text-purple-300 transition-colors flex items-center gap-1"
              >
                View All <ArrowRight className="w-4 h-4" />
              </RouterLink>
            </div>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <QuickAccessCard
                title="System Overview"
                description="Monitor overall system health and metrics"
                href="/dashboard/management"
                icon={<BarChart3 className="w-4 h-4" />}
              />
              <QuickAccessCard
                title="API Gateway"
                description="View gateway stats and endpoint performance"
                href="/dashboard/management/gateway"
                icon={<Globe className="w-4 h-4" />}
              />
              <QuickAccessCard
                title="Worker Nodes"
                description="Monitor and manage worker node status"
                href="/dashboard/management/workers"
                icon={<Server className="w-4 h-4" />}
              />
              <QuickAccessCard
                title="Collections Manager"
                description="Advanced collection management and analytics"
                href="/dashboard/management/collections"
                icon={<Database className="w-4 h-4" />}
              />
            </div>
          </div>

          {/* Recent Activity / Content */}
          <div className="grid grid-cols-1 lg:grid-cols-3 gap-8">
            {/* Main Chart Area (Placeholder) */}
            <div className="lg:col-span-2 p-6 rounded-xl border border-neutral-800 bg-neutral-900/30">
              <h3 className="text-lg font-medium text-white mb-6">
                Request Volume
              </h3>
              {requestVolumeBars.length === 0 ? (
                <div className="h-64 flex items-center justify-center text-neutral-500 text-sm border border-dashed border-neutral-800 rounded-lg">
                  No endpoint traffic data yet
                </div>
              ) : (
                <div className="h-64 flex items-end gap-2 px-2 pb-2 border-b border-neutral-800/50">
                  {requestVolumeBars.map((bar) => (
                    <div
                      key={bar.key}
                      className="flex-1 bg-purple-500/20 hover:bg-purple-500/40 transition-colors rounded-t-sm relative group"
                      style={{ height: `${bar.height}%` }}
                    >
                      <div className="absolute -top-8 left-1/2 -translate-x-1/2 bg-neutral-800 text-xs px-2 py-1 rounded opacity-0 group-hover:opacity-100 transition-opacity">
                        {formatNumber(bar.count)}
                      </div>
                    </div>
                  ))}
                </div>
              )}
              <div className="flex justify-between mt-4 text-xs text-neutral-500">
                <span>Low</span>
                <span>Medium</span>
                <span>High</span>
              </div>
            </div>

            {/* Recent Deployments / Events */}
            <div className="p-6 rounded-xl border border-neutral-800 bg-neutral-900/30">
              <h3 className="text-lg font-medium text-white mb-4">
                Recent Activity
              </h3>
              {systemHealth?.alerts?.length ? (
                <div className="space-y-6">
                  {systemHealth.alerts.slice(0, 4).map((alert: any) => (
                    <div key={alert.id} className="flex gap-3 items-start">
                      <div className="w-2 h-2 mt-2 rounded-full bg-neutral-700" />
                      <div>
                        <p className="text-sm text-neutral-300">{alert.title}</p>
                        <p className="text-xs text-neutral-500">
                          {formatDateTime(alert.timestamp)} • {alert.source}
                        </p>
                      </div>
                    </div>
                  ))}
                </div>
              ) : (
                <div className="text-sm text-neutral-500">
                  No recent alerts
                </div>
              )}
              <button className="w-full mt-6 py-2 text-sm text-neutral-400 hover:text-white border border-neutral-800 rounded-md hover:bg-neutral-800 transition-colors">
                View Audit Log
              </button>
            </div>
          </div>

          {/* Example Empty State (Visual Check) */}
          <div className="pt-8">
            <h3 className="text-lg font-medium text-white mb-4">
              Your Collections
            </h3>
            {loading ? (
              <div className="text-sm text-neutral-500">Loading collections...</div>
            ) : collections.length === 0 ? (
              <EmptyState />
            ) : (
              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                {collections.slice(0, 6).map((collection) => (
                  <div
                    key={collection.name}
                    className="p-4 rounded-lg border border-neutral-800 bg-neutral-900/30"
                  >
                    <div className="flex items-center gap-2 mb-2">
                      <Database className="w-4 h-4 text-purple-400" />
                      <h4 className="text-white font-medium">{collection.name}</h4>
                    </div>
                    <div className="text-xs text-neutral-500">
                      {collection.dimension > 0 ? `${collection.dimension}D` : "--"} • {formatNumber(collection.count, "0")} vectors
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      ) : (
        <Outlet />
      )}
    </div>
  );
};
