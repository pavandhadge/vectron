import React from "react";
import { Outlet, useLocation, Link as RouterLink } from "react-router-dom";
import { useAuth } from "../contexts/AuthContext";
import {
  ChevronRight,
  Home,
  Database,
  Activity,
  Zap,
  Plus,
} from "lucide-react";

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
  const { user } = useAuth();
  const location = useLocation();
  const pathnames = location.pathname.split("/").filter((x) => x);
  const isMainDashboard =
    pathnames.length === 1 && pathnames[0] === "dashboard";

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
              value="1,240,592"
              subtext="Across 4 collections"
              icon={<Database className="w-5 h-5" />}
              trend={12.5}
            />
            <StatCard
              title="Requests (24h)"
              value="84.3k"
              subtext="Avg. 3.2k / hour"
              icon={<Activity className="w-5 h-5" />}
              trend={-2.4}
            />
            <StatCard
              title="Avg. Latency"
              value="14ms"
              subtext="P99: 22ms"
              icon={<Zap className="w-5 h-5" />}
              trend={5.1}
            />
          </div>

          {/* Recent Activity / Content */}
          <div className="grid grid-cols-1 lg:grid-cols-3 gap-8">
            {/* Main Chart Area (Placeholder) */}
            <div className="lg:col-span-2 p-6 rounded-xl border border-neutral-800 bg-neutral-900/30">
              <h3 className="text-lg font-medium text-white mb-6">
                Request Volume
              </h3>
              <div className="h-64 flex items-end gap-2 px-2 pb-2 border-b border-neutral-800/50">
                {/* Simulated Bar Chart */}
                {[40, 65, 45, 80, 55, 90, 70, 85, 60, 75, 50, 95].map(
                  (h, i) => (
                    <div
                      key={i}
                      className="flex-1 bg-purple-500/20 hover:bg-purple-500/40 transition-colors rounded-t-sm relative group"
                      style={{ height: `${h}%` }}
                    >
                      <div className="absolute -top-8 left-1/2 -translate-x-1/2 bg-neutral-800 text-xs px-2 py-1 rounded opacity-0 group-hover:opacity-100 transition-opacity">
                        {h * 100}
                      </div>
                    </div>
                  ),
                )}
              </div>
              <div className="flex justify-between mt-4 text-xs text-neutral-500">
                <span>00:00</span>
                <span>12:00</span>
                <span>23:59</span>
              </div>
            </div>

            {/* Recent Deployments / Events */}
            <div className="p-6 rounded-xl border border-neutral-800 bg-neutral-900/30">
              <h3 className="text-lg font-medium text-white mb-4">
                Recent Activity
              </h3>
              <div className="space-y-6">
                {[
                  {
                    action: "Created collection 'users_v2'",
                    time: "2m ago",
                    user: "You",
                  },
                  { action: "API Key rotated", time: "1h ago", user: "System" },
                  {
                    action: "High latency alert resolved",
                    time: "4h ago",
                    user: "Monitor",
                  },
                  {
                    action: "Backup completed",
                    time: "1d ago",
                    user: "System",
                  },
                ].map((item, i) => (
                  <div key={i} className="flex gap-3 items-start">
                    <div className="w-2 h-2 mt-2 rounded-full bg-neutral-700" />
                    <div>
                      <p className="text-sm text-neutral-300">{item.action}</p>
                      <p className="text-xs text-neutral-500">
                        {item.time} • {item.user}
                      </p>
                    </div>
                  </div>
                ))}
              </div>
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
            <EmptyState />
          </div>
        </div>
      ) : (
        <Outlet />
      )}
    </div>
  );
};
